# Architecture

## Overview

Backend dibangun sebagai **reusable foundation**: tidak ada business logic
khusus client, semua keputusan bisnis ditandai `TODO: CLIENT REQUIREMENT`.

Prinsip utama:

1. **Separation of concerns** — handler, service, repository tidak saling
   tumpang tindih.
2. **Dependency injection** — semua dependensi di-wire dari `main.go`, tidak
   ada global mutable state.
3. **Abstraction over implementation** — AI dan repository menggunakan
   interface agar mudah diganti/di-mock.
4. **Fail fast** — config divalidasi saat startup, database di-ping saat
   startup, migration otomatis dijalankan.
5. **Safe by default** — error internal tidak pernah diekspos, secret tidak
   pernah di-log, CORS dibatasi.

## Request Flow

```
React + TypeScript (nanti)
        │  REST / JSON
        ▼
  Go HTTP Server (net/http + chi)
        │
        ▼
  Router (middleware chain)
   ├─ RequestID        → X-Request-ID (dipakai di log & response)
   ├─ RequestLogger    → structured log per request (method, path, status, duration)
   ├─ Recovery         → panic → 500 generik (detail hanya di log)
   ├─ SecurityHeaders  → nosniff, X-Frame-Options, Referrer-Policy
   └─ [API group] CORS → allowlist dari CORS_ALLOWED_ORIGINS
   └─ [API group] RateLimit → token bucket per IP
        │
        ▼
  Handler (parse, validate, respond)
        │
        ▼
  Service (business logic)
        │
        ▼
  Repository (parameterized SQL)
        │
        ▼
  PostgreSQL
```

Handler → Service → Repository adalah alur satu arah. Handler **tidak pernah**
menyentuh SQL, Repository **tidak pernah** memformat response HTTP.

### Kenapa layering ini?

| Layer | Tanggung jawab | Jangan lakukan di sini |
| --- | --- | --- |
| Handler | parse JSON, validasi request, menulis response | SQL, business logic |
| Service | business logic (register, login, JWT issuance) | SQL, HTTP details |
| Repository | akses database (query parameterized) | business decisions, response |
| AI Service | memanggil provider LLM lewat interface | business rules dari client |

## AI Flow

```
Go Backend
   │
   ▼
AI Service (interface: GenerateResponse(ctx, AIRequest) → AIResponse)
   │
   ▼
Provider (interface: Complete(ctx, providerRequest) → providerResponse)
   │
   ▼
LLM API (Gemini Generative Language API, base URL configurable)
```

- **AI Service** (`internal/ai/service.go`) adalah satu-satunya yang dipakai
  HTTP handler. Handler tidak tahu provider apa yang dipakai.
- **Provider** (`internal/ai/provider.go`) adalah contract transport.
  `GeminiProvider` (`internal/ai/gemini_provider.go`) adalah implementasi
  native Gemini dengan `AI_BASE_URL` yang bisa diarahkan ke endpoint Gemini
  lain atau gateway OpenAI-compatible bila provider berbeda dibutuhkan.
- **Model** (`AIRequest{Message, Context []string}`, `AIResponse{Message, Model}`)
  dirancang netral sehingga nantinya bisa dipakai untuk chatbot, rekomendasi,
  document Q&A, semantic search, content assistance, lead qualification.
- Prompt spesifik perusahaan **tidak dibuat** — itu `CLIENT REQUIREMENT`.

## Configuration

`internal/config/config.go` membaca environment variable dan memvalidasinya
saat startup. Secret hanya dari environment, tidak pernah hardcode.

```
.env / env vars
      │
      ▼
config.Load() → validated *config.Config
      │
      ▼
main.go → DI ke router, service, repository
```

## Database

- `internal/database/postgres.go`: connection pool (`pgxpool`), min/max conns,
  connect timeout, health check period, ping saat startup, graceful close.
- `internal/database/migrate.go`: migration runner dengan `golang-migrate`.
  File SQL di-embed lewat `embed.FS`, sehingga binary self-contained dan
  migration berjalan otomatis saat startup.
- `migrations/`: satu-satunya tabel saat ini adalah `users` (untuk fondasi
  authentication). **Tidak ada** tabel bisnis.
- Semua query menggunakan parameterization (`$1, $2, ...`), tidak pernah
  string concatenation.

## Authentication & Authorization

- **Authentication** ("siapa user ini?"): JWT (HS256) ditandatangani dengan
  `JWT_SECRET`. Claims minimal: `sub` (user id), `email`, `role`, `iss`,
  `iat`, `exp`. Middleware `RequireAuth` memvalidasi token dan menyimpan
  claims di context.
- **Authorization** ("apa yang boleh dilakukan user ini?"): middleware
  `RequireRole("admin")`. Saat ini hanya ada role `admin`. Route admin belum
  ada (menunggu requirement client).
- Password di-hash dengan **bcrypt**; plaintext tidak pernah disimpan atau
  di-log.
- Login memakai pesan error generik agar tidak terjadi user enumeration.

## Error Handling

Semua error aplikasi dibungkus `apperror.Error` yang memiliki:

- `Kind` → HTTP status + error code konsisten (VALIDATION_ERROR, UNAUTHORIZED,
  FORBIDDEN, NOT_FOUND, CONFLICT, DATABASE_ERROR, AI_PROVIDER_ERROR, dst.)
- `Message` → aman ditampilkan ke client
- `Err` → cause asli, hanya untuk log server (tidak pernah ke client)

`handler.RespondError` memastikan detail internal hanya muncul di log.

## Logging

`log/slog` (stdlib):

- `development`/`test` → text, level DEBUG
- `production` → JSON, level INFO

Yang **tidak pernah** di-log: password, JWT, API key, authorization header,
request body, dan informasi sensitif lainnya.

## Middleware (ringkas)

| Middleware | Fungsi |
| --- | --- |
| `RequestID` | generate/validate `X-Request-ID`, taruh di context |
| `RequestLogger` | structured log per request |
| `Recovery` | tangkap panic → 500 generik |
| `SecurityHeaders` | nosniff, frame deny, referrer policy |
| `CORS` | allowlist origin dari env |
| `RateLimit` | token bucket per IP (diterapkan di grup `/api/v1`) |
| `RequireAuth` | validasi JWT (per route group) |
| `RequireRole` | cek role (per route group, untuk nanti) |

## Diagram

```
                    ┌─────────────────────────────────────────────┐
                    │                 main.go                      │
                    │   config → logger → db → services → router   │
                    └─────────────────────────────────────────────┘
                                       │
                    ┌──────────────────▼──────────────────────────┐
                    │             router (chi)                    │
                    │  RequestID · Logger · Recovery · Security   │
                    │  ┌────────────── /api/v1 ─────────────────┐ │
                    │  │ CORS · RateLimit                      │ │
                    │  │  /auth/register · /auth/login         │ │
                    │  │  ┌─ RequireAuth ──────────────────┐   │ │
                    │  │  │  /auth/me · /ai/chat          │   │ │
                    │  │  └───────────────────────────────┘   │ │
                    │  └──────────────────────────────────────┘ │
                    └──────────────────┬────────────────────────┘
                                       │
              ┌────────────────────────┼────────────────────────┐
              ▼                        ▼                        ▼
     ┌───────────────┐      ┌─────────────────┐      ┌────────────────┐
     │   Handler     │      │     Handler     │      │    Handler     │
     │   (health)    │      │ (auth)          │      │ (ai)           │
     └──────┬────────┘      └────────┬────────┘      └───────┬────────┘
            │                       ▼                       │
            │              ┌────────────────┐                │
            │              │   AuthService  │                │
            │              │  (bcrypt, JWT) │                │
            │              └───────┬────────┘                │
            │                      ▼                         ▼
            │              ┌────────────────┐       ┌────────────────┐
            │              │  UserRepository│       │  AI Service    │
            │              └───────┬────────┘       └───────┬────────┘
            │                      ▼                       ▼
            │              ┌────────────────┐       ┌────────────────┐
            │              │  PostgreSQL    │       │  AI Provider   │
            │              └────────────────┘       │ (LLM API)      │
            │                                       └────────────────┘
            ▼
   ┌────────────────┐
   │  DBPinger ping  │
   └────────────────┘
```

## Catatan Pengembangan

- Project mengikuti prinsip **"Build the foundation, not the fictional
  client's features."**
- Setiap bagian yang membutuhkan requirement client ditandai
  `TODO: CLIENT REQUIREMENT` di kode dan di dokumen.
- Struktur boleh berkembang, tetapi jangan menambah kompleksitas tanpa
  alasan nyata.
