# CPA-Helper Backend

Go backend for the local CPA-Helper application.

Run from this directory:

```powershell
go mod download
go run ./cmd/cpa-helper
```

The executable provides explicit operational subcommands:

```powershell
go run ./cmd/cpa-helper migrate  # run database migrations and exit
go run ./cmd/cpa-helper serve    # start only after read-only startup checks pass
go run ./cmd/cpa-helper doctor   # run read-only startup checks and exit
```

Running without a subcommand is the user-facing startup path: it runs
migrations, performs startup checks, then starts the service.

Useful checks:

```powershell
go fmt ./...
go test ./...
```

Local build output goes under `bin/`:

```powershell
go build -o bin/cpa-helper.exe ./cmd/cpa-helper
```

Database migrations are managed by embedded goose migrations in `migrations/`.
Use `cpa-helper migrate` for an explicit migration-only operation. Use
`cpa-helper serve` when an operator wants the service to fail instead of
modifying the database at startup. The Docker image default command runs the
same integrated startup path as the binary with no subcommand; Alembic is not
part of the Go runtime.
For Docker upgrades, the new image only needs the persisted SQLite database;
it does not require the old Python source tree or Alembic files.

