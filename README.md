# CapCycle Backend

Go API for CapCycle. This repository owns the Cloud Run service, Cloud SQL migrations, GCS image upload integration, JWT authentication, RBAC, transactions, messages, reviews, likes, view events, Gemini-assisted listing support, and dynamic pricing.

## Local Development

```bash
go mod download
docker compose up -d mysql
go run ./cmd/api
```

Default local credentials are defined in `.env.example`. The API falls back to in-memory storage if MySQL is unavailable.

```text
DB_HOST=127.0.0.1
DB_PORT=3306
DB_USER=capcycle
DB_PASSWORD=capcycle
DB_NAME=capcycle
```

Health check:

```bash
curl http://localhost:8080/health
```

Demo users:

```text
demo@capcycle.test / password
buyer@capcycle.test / password
```

## Production

Cloud Run is deployed by `.github/workflows/deploy-cloud-run.yml`.

Required GitHub Secrets:

```text
GCP_PROJECT_ID
GCP_WORKLOAD_IDENTITY_PROVIDER
GCP_SERVICE_ACCOUNT
CORS_ORIGIN
PUBLIC_BASE_URL
GCS_BUCKET
GCS_PUBLIC_BASE_URL
CLOUD_SQL_CONNECTION_NAME
```

Required GCP Secret Manager secrets:

```text
JWT_SECRET
DB_USER
DB_PASSWORD
DB_NAME
GEMINI_API_KEY
```

The frontend repository should set `VITE_API_BASE_URL` to the Cloud Run URL.
