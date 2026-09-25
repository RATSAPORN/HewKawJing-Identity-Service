# HewKawJing Identity Service

Go/Gin API for email/password registration and login, backed by PostgreSQL.

## Run locally

With Docker Desktop running, both the API and PostgreSQL can run in containers.

1. Copy `.env.example` to `.env` if you do not already have one.
2. Set `DB_PASSWORD` and your database connection settings in `.env`.
3. Build and start both services: `docker compose up -d --build --wait`.

```powershell
# Container status and health
docker compose ps
curl.exe http://localhost:8080/health

# Follow API logs (Ctrl+C stops following logs, not the API)
docker compose logs -f identity-service

# Stop both services; database data is preserved
docker compose stop

# Start them again
docker compose up -d --wait
```

The API waits for PostgreSQL to be healthy, applies migrations, and reports healthy
when `/health` can reach the database. The image builds Go in a separate stage
and runs the binary as a non-root user. `.env` is excluded from the image;
Compose passes database settings at runtime. Inside Docker, the database hostname
is `postgres` and its port is `5432`, regardless of the host's `DB_HOST`/`DB_PORT`.
`PORT` and `DB_PORT` in `.env` select the published host ports.

To run Go directly instead, install Go 1.26.3, run
`docker compose up -d --wait postgres`, then `go run ./cmd`.
Stop the containerized API first with `docker compose stop identity-service` if
it is already using the same port. Keep `DB_HOST=localhost` in `.env` for this mode.

The default URL is `http://localhost:8080`. Set `PORT` to change it.
When `APP_ENV` is `local`, `dev`, or `sit`, startup applies pending SQL migrations
transactionally. Other environments require migrations to be applied separately.
`DB_SCHEMA` defaults to `public`; a custom schema must already exist.
The Docker database persists in a named volume and binds only to localhost.

## API

All paths start with `/api/v1/identity`.

| Method | Path | JSON body / authorization | Success |
| --- | --- | --- | --- |
| POST | `/register` | `display_name`, `email`, `password` | 201, user profile |
| POST | `/login` | `email`, `password` | 200, user and tokens |
| GET | `/me` | `Authorization: Bearer <access_token>` | 200, user profile |
| POST | `/refresh-token` | `refresh_token` | 200, replacement tokens |
| POST | `/logout` | `Authorization: Bearer <access_token>` | 200, session revoked |

Registration creates an account; call login afterward to obtain tokens.
Email is trimmed and lowercased. Display names must contain 1–255 characters.
Passwords must contain at least 8 characters and at most 72 UTF-8 bytes.
Passwords are hashed using bcrypt and are never returned by the API.

Login returns a `data` object with `user`, `access_token`, `refresh_token`,
`token_type: "Bearer"`, and `expires_in: 3600`.
These are opaque random tokens, not JWTs; only SHA-256 token hashes are stored.
Access tokens last one hour. Refresh tokens last seven days from login or the
latest successful refresh. Refresh rotates both tokens and invalidates the old
ones. Logout revokes both tokens for the current session. Other devices remain
logged in. Disabled and deleted accounts cannot authenticate or refresh.

Success responses use `{"code":"0000","message":"ok","data":...}`
(registration uses the message `registered`). Errors use `code` and `message`:
400 for invalid input, 401 for rejected credentials or tokens, 409 for duplicate
email, and 500 for server failures. No password hashes or internal database
details are included in responses.

Example in PowerShell, with the API running:

```powershell
$base = 'http://localhost:8080/api/v1/identity'
$registration = @{
    display_name = 'Demo User'
    email = 'demo@example.com'
    password = 'Example-password-42'
} | ConvertTo-Json
Invoke-RestMethod "$base/register" -Method Post -ContentType 'application/json' -Body $registration

$credentials = @{
    email = 'demo@example.com'
    password = 'Example-password-42'
} | ConvertTo-Json
$login = Invoke-RestMethod "$base/login" -Method Post -ContentType 'application/json' -Body $credentials
$headers = @{ Authorization = "Bearer $($login.data.access_token)" }
Invoke-RestMethod "$base/me" -Headers $headers
Invoke-RestMethod "$base/logout" -Method Post -Headers $headers
```

## Verification

```powershell
go test ./...
go vet ./...

# With PostgreSQL running, test the real repositories and migrations as well.
$env:IDENTITY_POSTGRES_TEST = '1'
go test ./routes -count=1 -v
Remove-Item Env:IDENTITY_POSTGRES_TEST
```

The PostgreSQL test uses `.env`, creates a temporary schema, and removes only
that schema afterward. It covers registration, login, token rotation and replay,
logout, expiration, and disabled/deleted accounts. Normal tests run without a
database. Docker can be stopped with `docker compose stop`.
