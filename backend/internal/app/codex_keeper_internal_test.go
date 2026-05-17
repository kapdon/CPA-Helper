package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestConditionalKeeperRefreshCandidatesUseUsageQuotaAndCache(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())
	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()

	ctx := context.Background()
	cfg, err := app.loadConfig(ctx)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	cfg.CodexKeeper.AccountRefreshCacheMinutes = 10
	now := time.Now().In(appTimeLocation)

	insertKeeperUsageRecord(t, app, "active-request", now.Add(-time.Minute), `{"auth_index":"active-request.json","failed":true}`)
	insertKeeperUsageRecord(t, app, "old-request", now.Add(-20*time.Minute), `{"auth_index":"old-request.json"}`)
	insertKeeperUsageRecord(t, app, "cached-request", now.Add(-time.Minute), `{"auth_index":"cached-request.json"}`)
	insertKeeperUsageRecord(t, app, "no-auth-index", now.Add(-time.Minute), `{"request_id":"missing-auth-index"}`)

	insertKeeperStateForCandidate(t, app, "cached-request.json", nil, timePtrValue(now.Add(-2*time.Minute)))
	insertKeeperStateForCandidate(t, app, "quota-due.json", timePtrValue(now.Add(-time.Minute)), nil)
	insertKeeperStateForCandidate(t, app, "quota-future.json", timePtrValue(now.Add(time.Minute)), nil)
	insertKeeperStateForCandidate(t, app, "quota-cached.json", timePtrValue(now.Add(-time.Minute)), timePtrValue(now.Add(-2*time.Minute)))

	names, err := app.conditionalKeeperRefreshCandidates(ctx, cfg)
	if err != nil {
		t.Fatalf("conditionalKeeperRefreshCandidates: %v", err)
	}
	assertStringSet(t, names, []string{"active-request.json", "quota-due.json"})
}

func TestAutomaticKeeperRunsRespectCacheButManualRefreshBypasses(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	usageCalls := 0
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]any{{"name": "cached.json", "type": "codex"}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files/download":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":         "cached.json",
				"type":         "codex",
				"account_type": "free",
				"disabled":     false,
				"priority":     0,
				"access_token": "test-token",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/api-call":
			usageCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status_code": 200,
				"body": map[string]any{
					"plan_type": "free",
					"rate_limit": map[string]any{
						"primary_window": map[string]any{
							"used_percent":        10,
							"reset_after_seconds": 3600,
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer cpa.Close()

	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()
	configureKeeperTestCPA(t, app, cpa.URL, func(cfg *AppConfig) {
		cfg.CodexKeeper.AccountRefreshCacheMinutes = 10
	})
	insertKeeperStateForCandidate(t, app, "cached.json", nil, timePtrValue(time.Now().In(appTimeLocation).Add(-time.Minute)))

	stats, _, err := app.executeKeeperRunForAccounts(context.Background(), "daemon", nil, func(string) {})
	if err != nil {
		t.Fatalf("daemon run: %v", err)
	}
	if stats.Skipped != 1 {
		t.Fatalf("daemon skipped = %d, want 1", stats.Skipped)
	}
	if usageCalls != 0 {
		t.Fatalf("daemon usage calls = %d, want 0", usageCalls)
	}

	_, _, err = app.executeKeeperRunForAccounts(context.Background(), "accounts", []string{"cached.json"}, func(string) {})
	if err != nil {
		t.Fatalf("manual account refresh: %v", err)
	}
	if usageCalls != 1 {
		t.Fatalf("manual usage calls = %d, want 1", usageCalls)
	}
}

func TestConditionalKeeperRunUsesAutomaticPriorityPolicy(t *testing.T) {
	t.Setenv("CPA_HELPER_DATA_DIR", t.TempDir())

	priorityPatches := []int{}
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]any{{"name": "quota.json", "type": "codex"}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v0/management/auth-files/download":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":         "quota.json",
				"type":         "codex",
				"account_type": "free",
				"disabled":     false,
				"priority":     0,
				"access_token": "test-token",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v0/management/api-call":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status_code": 200,
				"body": map[string]any{
					"plan_type": "free",
					"rate_limit": map[string]any{
						"primary_window": map[string]any{
							"used_percent":        100,
							"reset_after_seconds": 3600,
						},
					},
				},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/v0/management/auth-files/fields":
			var payload struct {
				Priority *int `json:"priority"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if payload.Priority != nil {
				priorityPatches = append(priorityPatches, *payload.Priority)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer cpa.Close()

	app, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer app.Close()
	configureKeeperTestCPA(t, app, cpa.URL, func(cfg *AppConfig) {
		cfg.CodexKeeper.DryRun = false
		cfg.CodexKeeper.QuotaThreshold = 50
	})

	stats, _, err := app.executeKeeperRunForAccounts(context.Background(), "conditional", []string{"quota.json"}, func(string) {})
	if err != nil {
		t.Fatalf("conditional run: %v", err)
	}
	if stats.PriorityDegraded != 1 {
		t.Fatalf("priority_degraded = %d, want 1", stats.PriorityDegraded)
	}
	if len(priorityPatches) != 1 || priorityPatches[0] != -1 {
		t.Fatalf("priority patches = %#v, want [-1]", priorityPatches)
	}
}

func configureKeeperTestCPA(t *testing.T, app *App, url string, mutate func(*AppConfig)) {
	t.Helper()
	ctx := context.Background()
	cfg, err := app.loadConfig(ctx)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	cfg.Collector.CLIProxyURL = url
	cfg.Collector.ManagementKey = "test-management-key"
	cfg.Collector.Enabled = false
	cfg.CodexKeeper.ScheduleCron = "0 0 29 2 *"
	cfg.CodexKeeper.CPATimeoutSeconds = 1
	cfg.CodexKeeper.UsageTimeoutSeconds = 1
	if mutate != nil {
		mutate(&cfg)
	}
	if err := app.saveConfig(ctx, cfg); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}
}

func insertKeeperUsageRecord(t *testing.T, app *App, dedupe string, timestamp time.Time, rawJSON string) {
	t.Helper()
	now := dbTime(time.Now().In(appTimeLocation))
	_, err := app.db.Exec(`
		INSERT INTO usage_records (
			created_at, timestamp, usage_username, api_key_description, provider,
			model, endpoint, source, request_id, auth, latency_ms, failed,
			input_tokens, output_tokens, cached_tokens, reasoning_tokens,
			total_tokens, dedupe_key, raw_json
		) VALUES (?, ?, NULL, NULL, 'codex', 'gpt-test', '/v1/responses',
			'test', ?, 'api_key', 10, 1, 1, 1, 0, 0, 2, ?, ?)
	`, now, dbTime(timestamp), dedupe, "conditional-"+dedupe, rawJSON)
	if err != nil {
		t.Fatalf("insert usage record %s: %v", dedupe, err)
	}
}

func insertKeeperStateForCandidate(t *testing.T, app *App, name string, primaryResetAt *time.Time, lastCheckedAt *time.Time) {
	t.Helper()
	now := dbTime(time.Now().In(appTimeLocation))
	_, err := app.db.Exec(`
		INSERT INTO codex_keeper_auth_states (
			auth_name, disabled, primary_reset_at, last_checked_at, created_at, updated_at
		) VALUES (?, 0, ?, ?, ?, ?)
		ON CONFLICT(auth_name) DO UPDATE SET
			primary_reset_at = excluded.primary_reset_at,
			last_checked_at = excluded.last_checked_at,
			updated_at = excluded.updated_at
	`, name, dbTimePtr(primaryResetAt), dbTimePtr(lastCheckedAt), now, now)
	if err != nil {
		t.Fatalf("insert keeper state %s: %v", name, err)
	}
}

func timePtrValue(value time.Time) *time.Time {
	return &value
}

func assertStringSet(t *testing.T, got []string, want []string) {
	t.Helper()
	gotSet := map[string]bool{}
	for _, item := range got {
		gotSet[item] = true
	}
	if len(gotSet) != len(want) {
		t.Fatalf("names = %#v, want set %#v", got, want)
	}
	for _, item := range want {
		if !gotSet[item] {
			t.Fatalf("names = %#v, want set %#v", got, want)
		}
	}
}
