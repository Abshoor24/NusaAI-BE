# Project Name

> Placeholder name. Rename when the client project is known.

## Overview

Backend untuk **Full AI Generator / AI Assistant Website** (konsep mirip ChatGPT, Gemini, Claude).

AI adalah produk utama dari sistem ini: pengguna dapat berinteraksi langsung, mengirim prompt untuk men-generate berbagai kebutuhan (teks, ide, kode, analisis), serta melakukan percakapan interaktif.

- REST API dengan Clean Architecture (handler / service / provider)
- PostgreSQL infrastructure (connection pool + auto-run migrations)
- Authentication foundation (register / login / JWT / me)
- AI Assistant interaction (Google Gemini provider, rate-limited per IP, multi-turn chat support)
- Standard API response format + centralized error handling
- Structured logging, request ID, CORS, rate limiting, graceful shutdown
- Docker development environment

## Tech Stack

- Go (net/http + chi router)
- PostgreSQL (pgx/v5, connection pooling)
- REST API
- JWT authentication (golang-jwt/v5)
- AI API (Google Gemini, provider abstraction with configurable base URL)
- Docker (development environment)

## Project Structure

```
backend/
├── cmd/server/            entrypoint: server startup, graceful shutdown, DI wiring
├── internal/
│   ├── apperror/          canonical application errors (typed, safe messages)
│   ├── config/            environment-based configuration + startup validation
│   ├── database/          PostgreSQL pool + embedded migration runner
│   ├── handler/           HTTP handlers + request parsing/validation + API responses
│   ├── jwt/               JWT token manager (sign/parse/validate)
│   ├── middleware/        request ID, logger, recovery, CORS, auth, role, rate limit, security
│   ├── model/             database models + request/response DTOs
│   ├── modules/ai/        AI abstraction: Service interface + Provider + Gemini impl
│   ├── modules/business/  TEMPLATE modul bisnis (handler/service/repository/migration)
│   ├── repository/        database access (UserRepository)
│   ├── routes/            route + middleware wiring
│   └── service/           business logic (AuthService)
├── migrations/            SQL migrations (embedded via embed.FS)
├── pkg/
│   ├── logger/            structured logging (slog), env-aware level
│   └── response/          standard API response + request validation helpers
├── storage/               Supabase integration (connection pool + migrations)
├── docs/                  api.md, architecture.md, client-requirements.md
├── tests/                 database integration test (skipped unless enabled)
├── .env.example
├── Dockerfile
├── docker-compose.yml
└── go.mod
```

## Requirements

- Go 1.25+
- Docker + Docker Compose (untuk database)
- PostgreSQL jika tidak menggunakan Docker

## Installation

1. Clone/salin project ini.
2. Copy environment contoh:

   ```bash
   cp .env.example .env
   ```

3. Isi nilai di `.env` — minimal ganti `JWT_SECRET` dengan string acak panjang.
4. Install dependencies:

   ```bash
   go mod tidy
   ```

## Running

Opsi A — PostgreSQL via Docker, server lokal:

```bash
docker compose up -d postgres
go run ./cmd/server
```

Opsi B — semuanya via Docker:

```bash
docker compose up -d --build
```

Opsi C — PostgreSQL sudah berjalan di mesin:

```bash
go run ./cmd/server
```

Server berjalan di `http://localhost:8080` (sesuai `APP_PORT`).

## Environment Variables

Lihat `.env.example` untuk daftar lengkap dan deskripsi singkat.

Variabel wajib:

| Variable | Keterangan |
| --- | --- |
| `JWT_SECRET` | Secret untuk menandatangani JWT. Wajib ada; aplikasi gagal start jika kosong. |

Variabel penting lainnya: `APP_ENV`, `APP_PORT`, `DATABASE_*`, `AI_API_KEY`,
`AI_MODEL`, `AI_BASE_URL`, `CORS_ALLOWED_ORIGINS`.

> Jangan pernah commit `.env` yang berisi secret nyata.

## Testing

```bash
go test ./...
```

Unit test (tanpa database, tanpa panggilan AI eksternal):

- health handler test
- config validation test
- request validation test
- auth service test (dengan mock repository)
- AI service test (dengan mock provider)

Integration test database (opsional, butuh database berjalan):

```bash
# pastikan .env mengarah ke database yang berjalan
TEST_DB_INTEGRATION=true go test ./tests/...
```

## Docker

```bash
docker compose up -d          # jalankan postgres saja + build server
docker compose up -d --build  # rebuild server image
docker compose down           # hentikan (volume data tetap tersimpan)
docker compose down -v        # hentikan + hapus volume (reset database)
```

## API

Endpoint yang tersedia:

| Method | Path | Auth | Keterangan |
| --- | --- | --- | --- |
| GET | `/health` | - | Liveness check |
| GET | `/health/db` | - | Database readiness check |
| POST | `/api/v1/auth/register` | - | Registrasi user baru |
| POST | `/api/v1/auth/login` | - | Login, menerima JWT |
| GET | `/api/v1/auth/me` | Bearer | Profil user yang sedang login |
| GET | `/api/v1/ai/health` | - | Cek konfigurasi AI lokal (tanpa panggil provider) |
| POST | `/api/v1/ai/chat` | Bearer | Interaksi utama AI Assistant (create/continue conversation + persistence) |
| POST | `/api/v1/ai/chat/stream` | Bearer | Streaming respon AI token-by-token via Server-Sent Events (SSE) |
| GET | `/api/v1/ai/conversations` | Bearer | Daftar riwayat percakapan user |
| GET | `/api/v1/ai/conversations/{id}` | Bearer | Detail percakapan beserta seluruh pesan |
| DELETE | `/api/v1/ai/conversations/{id}` | Bearer | Hapus percakapan |
| POST | `/api/v1/ai/documents` | Bearer | Ingest dokumen teks untuk RAG knowledge base |
| POST | `/api/v1/ai/documents/upload` | Bearer | Upload file dokumen (PDF, DOCX, TXT) dengan asynchronous processing (alias: `/api/v1/documents/upload`) |
| GET | `/api/v1/ai/documents` | Bearer | Daftar dokumen knowledge milik user |
| GET | `/api/v1/ai/documents/{id}` | Bearer | Detail dokumen knowledge milik user |
| GET | `/api/v1/ai/documents/{id}/status` | Bearer | Cek status background processing dokumen (`processing`, `ready`, `failed`, alias: `/api/v1/documents/{id}/status`) |
| DELETE | `/api/v1/ai/documents/{id}` | Bearer | Hapus dokumen knowledge beserta vector chunks & file fisik |

Dokumentasi lengkap: [docs/api.md](docs/api.md).


## Dokumentasi Lain

- [Arsitektur](docs/architecture.md)
- [Template kebutuhan client](docs/client-requirements.md)
