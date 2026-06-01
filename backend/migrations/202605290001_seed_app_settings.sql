-- +goose Up
INSERT INTO app_settings (
	id,
	collector_enabled,
	cliaproxy_url,
	management_key,
	queue_name,
	batch_size,
	poll_interval_seconds,
	retry_interval_seconds,
	codex_keeper_settings,
	codex_keeper_priority_rules,
	litellm_proxy_enabled,
	litellm_proxy_url,
	model_request_url,
	session_secret,
	created_at,
	updated_at
)
SELECT
	1,
	0,
	'http://127.0.0.1:8317',
	'',
	'usage',
	100,
	2.0,
	10.0,
	'{}',
	'{}',
	0,
	'',
	'http://127.0.0.1:8317',
	lower(hex(randomblob(48))),
	strftime('%Y-%m-%dT%H:%M:%f+08:00', 'now', '+8 hours'),
	strftime('%Y-%m-%dT%H:%M:%f+08:00', 'now', '+8 hours')
WHERE NOT EXISTS (
	SELECT 1 FROM app_settings WHERE id = 1
);

-- +goose Down
