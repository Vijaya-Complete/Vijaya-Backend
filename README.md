# Vijaya Backend

Go API for the Vijaya children's magazine platform.

**Stack:** Go 1.22, Echo v4, PostgreSQL 16, Redis 7, MinIO (S3-compatible storage), Resend (email), JWT auth.

## Run locally

```bash
cp .env.example .env     # set RESEND_API_KEY and change the secrets
docker compose up --build
```

| Service       | URL                            |
|---------------|--------------------------------|
| API           | http://localhost:8080          |
| Health check  | http://localhost:8080/health   |
| MinIO console | http://localhost:9001          |

Database migrations in `db/migrations/` are applied automatically by the app on startup.
An admin account is created on first run from `ADMIN_EMAIL` / `ADMIN_PASSWORD` in `.env`.
