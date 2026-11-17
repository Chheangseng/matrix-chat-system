# Matrix Server with Golang Gin Proxy Gateway

This repository sets up a self-hosted [Matrix](https://matrix.org/) homeserver powered by `matrixdotorg/synapse:latest` and PostgreSQL 16, fronted by a high-performance **Golang Gin reverse proxy gateway** (`chat-htp-api`).

---

## Architecture Overview

```
                    ┌────────────────────────────────────────────────────────┐
                    │                      chat-htp-api                      │
[ Custom Chat UI ] ──► [ Gin Server (:8080) ]                                 │
                    │         │                                              │
                    │         ├──► [ Custom Middlewares: CORS, Log, Recovery]│
                    │         │                                              │
                    │         ├──► [ Custom API Routes: /api-server/* ]      │
                    │         │         │                                    │
                    │         │         ├──► [ GORM ORM ]                    │
                    │         │         │         │                          │
                    │         │         │         ▼                          │
                    │         │         │   [ PostgreSQL (chat-api-db :5433) ]
                    │         │                                              │
                    │         └──► [ Matrix Proxy Middleware (httputil) ]    │
                    └──────────────────────────┬─────────────────────────────┘
                                               │ (All other paths)
                                               ▼
                                 [ Matrix Synapse (:8008) ]
                                               │
                                               ▼
                                  [ PostgreSQL (synapse-db) ]
```

### Routing Rules

- **`/api-server/*`**: Handled directly by your Go Gin application for custom backend APIs, authentication, health checks, and business logic.
- **`/*` (Any other path)**: Automatically and transparently proxied to the Matrix Synapse homeserver (`/_matrix/*`, `/_synapse/*`, `/.well-known/*`, etc.) with full support for long-polling (`/sync`) and streaming.

### Security & Concealment

- **Port Isolation**: Synapse binds exclusively to `127.0.0.1:8008` (loopback only) instead of `0.0.0.0`, blocking external clients from accessing port 8008 directly.
- **Header Stripping**: Headers identifying the backend (`Server: Synapse/...`, `Synapse-Trace-Id`, CORS exposure) are automatically stripped from proxy responses.
- **Client Host Forwarding**: `X-Forwarded-Host` forwards the client's public gateway host to Synapse, ensuring Synapse generates links matching the gateway.
- **Redirect Rewriting**: Any HTTP 3xx `Location` header pointing to the internal Synapse address is rewritten to relative paths so the internal address is never exposed.
- **Auto-Discovery Concealment**: `GET /.well-known/matrix/client` advertises the gateway address (`http://<host>`), never the backend address.

---

## Quick Start

### 1. Environment Configuration

The configuration is managed via `.env`:

```bash
# Ports
SYNAPSE_HTTP_PORT=8008
APP_PORT=8080

# Go Gin Gateway Configuration
APP_ENV=development
SYNAPSE_TARGET_URL=http://localhost:8008
ALLOWED_ORIGINS=*

# Matrix Synapse Configuration
SYNAPSE_SERVER_NAME=localhost
SYNAPSE_REPORT_STATS=no

# Default Synapse Admin Account
SYNAPSE_ADMIN_USER=admin
SYNAPSE_ADMIN_PASSWORD=admin_password_123

# Synapse Database Configuration (PostgreSQL for Matrix Synapse)
POSTGRES_USER=synapse
POSTGRES_PASSWORD=synapse_secret_password
POSTGRES_DB=synapse

# API Database Configuration (Dedicated PostgreSQL for chat-htp-api via GORM)
API_DB_HOST=localhost
API_DB_PORT=5433
API_DB_USER=chat_api
API_DB_PASSWORD=chat_api_secret
API_DB_NAME=chat_api_db
API_DB_SSLMODE=disable

# Customer Support Account Configuration
SUPPORT_USER=support
SUPPORT_PASSWORD=support_password_123
SUPPORT_DISPLAY_NAME=Customer Support
SUPPORT_ADMIN=false
```

### 2. Start the Matrix Backend (Docker Compose)

```bash
docker compose up -d
```

Verify backend health:
```bash
docker compose ps
```

### 3. Run the Go Gin Gateway

#### Development Mode (with hot reload or go run):
```bash
go run ./cmd/server
```

#### Build & Run Binary:
```bash
go build -o bin/server.exe ./cmd/server
./bin/server.exe
```

---

## Endpoints

### 1. OIDC Single Sign-On Authentication (`/api-server/auth`)

Integrates with HTP OIDC Issuer (supporting PKCE S256, UserInfo discovery, and mobile app seamless pre-auth) and automatically provisions Matrix users & sessions:

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `GET` | `/api-server/auth/callback` | **OIDC Callback**: Handles code exchange, auto-provisions Synapse account, and returns Matrix session + user profile |
| `POST` | `/api-server/auth/mobile-login` | **Seamless Mobile SSO**: Accepts existing HTP Bearer token, auto-completes pre-auth, and directly returns Matrix access token |

#### Mobile Single Sign-On Example (`POST /api-server/auth/mobile-login`):
Headers: `Authorization: Bearer <htp_user_bearer_token>` (or JSON body: `{"token": "<token>"}`)

**Response (`200 OK`)**:
```json
{
  "matrix": {
    "access_token": "syt_...",
    "device_id": "...",
    "home_server": "localhost",
    "user_id": "@htp_user123:localhost"
  },
  "user": {
    "sub": "user123",
    "name": "Nguyen Van A",
    "nickname": "anguyen",
    "phone_number": "+84912345678",
    "picture": "https://..."
  },
  "oidc": {
    "access_token": "...",
    "id_token": "..."
  }
}
```

---

### 2. General Endpoints & Proxied Matrix APIs
| :--- | :--- | :--- |
| `GET /api-server/healthz` | **Go Gin Gateway** | Gateway health check (`{"status":"ok"}`) |
| `GET /api-server/info` | **Go Gin Gateway** | Gateway service metadata |
| `GET /_matrix/client/versions` | **Matrix Synapse** | Supported Matrix protocol versions |
| `GET /_matrix/client/v3/login` | **Matrix Synapse** | Login flows supported by homeserver |
| `POST /_matrix/client/v3/login` | **Matrix Synapse** | User login endpoint |
| `GET /_matrix/client/v3/sync` | **Matrix Synapse** | Real-time event long-polling stream |
| `GET /.well-known/matrix/client` | **Matrix Synapse** | Client discovery metadata |

---

## Testing

Run automated tests:
```bash
go test -v ./internal/...
```

---

## Default Admin User

A default admin user is **automatically provisioned** on container startup via `synapse-init-admin` in `docker-compose.yml`:

- **User ID**: `@admin:localhost` (or `@<username>:<server_name>`)
- **Username**: `admin` (configured via `SYNAPSE_ADMIN_USER` in `.env`)
- **Password**: `admin_password_123` (configured via `SYNAPSE_ADMIN_PASSWORD` in `.env`)

### Create Additional Users Manually

To create another user or reset an admin password:

```powershell
# Using PowerShell helper script:
.\scripts\create-admin.ps1 -Username "new_admin" -Password "secret_pass"

# Or using docker exec directly:
docker exec -it matrix-synapse register_new_matrix_user -u new_admin -p secret_pass -a -c /data/homeserver.yaml http://localhost:8008
```
