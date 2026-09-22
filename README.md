# Project Name

> Backend AI Assistant, Multimodal Generator, RAG Knowledge Base, & Code Execution Sandbox.

## Overview

Backend komprehensif untuk platform **AI Assistant & Code Execution** (konsep terpadu layaknya ChatGPT / Claude dengan sandbox eksekusi kode terisolasi).

Sistem menyediakan fitur lengkap out-of-the-box:
- **REST API Clean Architecture**: Handler, Service, Repository, dan Provider abstraction.
- **PostgreSQL + pgvector**: Connection pool terkelola (`pgxpool`), migrasi embedded otomatis via `embed.FS`.
- **Autentikasi & Autorisasi**: Registrasi, login, verifikasi profil, JWT (`HS256`), role-based access control.
- **AI Assistant Multi-Turn & SSE Streaming**: Integrasi Google Gemini API, context memory, streaming token real-time via Server-Sent Events (mendukung `Authorization` header dan query param `?token=`).
- **Multimodal Image Attachments**: Pengiriman gambar (JPEG, PNG, WebP) via JSON base64 atau `multipart/form-data`, persistensi media lokal, dan serving gambar terisolasi per user.
- **RAG Knowledge Base**: Ingest teks dan upload berkas fisik (PDF, DOCX, TXT) dengan ekstraksi teks asinkron, semantic chunking + overlap, embedding `text-embedding-004`, dan vector search cosine similarity.
- **Code Execution Sandbox**: Eksekusi kode terisolasi via **Judge0 CE** untuk bahasa Python, JavaScript, dan Go dengan batasan CPU time, memory, dan wall time.
- **Keamanan & Stabilitas**: Rate limiting 3 tingkat (General, AI Chat, Code Execution), CORS configurable, Security Headers, structured logging (`slog`), dan graceful shutdown.

## Tech Stack

- **Bahasa & Routing**: Go (net/http + chi router v5)
- **Database**: PostgreSQL dengan ekstensi `pgvector` (driver `pgx/v5`, connection pooling)
- **Autentikasi**: JWT (golang-jwt/v5) + bcrypt
- **AI & Embedding**: Google Generative Language API (Gemini, `text-embedding-004`)
- **Code Execution Sandbox**: Judge0 CE API (Docker / self-hosted)
- **Containerization**: Docker & Docker Compose

## Project Structure

```
backend/
├── cmd/
│   └── server/            entrypoint: server startup, graceful shutdown, DI wiring
├── internal/
│   ├── apperror/          canonical application errors (typed, safe client messages)
│   ├── config/            environment configuration + startup fail-fast validation
│   ├── database/          PostgreSQL pool + embedded migration runner
│   ├── handler/           HTTP handlers dasar (health, auth)
│   ├── jwt/               JWT token manager (sign, parse, validate)
│   ├── middleware/        request ID, logger, recovery, CORS, auth, SSE auth, rate limit, security
│   ├── model/             database models + user auth DTOs
│   ├── modules/
│   │   ├── ai/            modul AI Assistant, Multimodal, & RAG (extractor, rag, provider, service, repo, handler)
│   │   ├── execution/     modul Code Sandbox Execution (Judge0 provider, service, repo, handler)
│   │   └── business/      TEMPLATE/scaffold modul bisnis (handler/service/repository/migration)
│   ├── repository/        database access (UserRepository)
│   ├── routes/            route definition & middleware wiring
│   └── service/           business logic (AuthService)
├── migrations/            SQL migrations berurutan (0001 s.d. 0009, embedded via embed.FS)
├── pkg/
│   ├── logger/            structured logging (slog), env-aware formatting (text / JSON)
│   └── response/          standard JSON API response + request validation helpers
├── storage/               Supabase & PostgreSQL integration point
├── uploads/               penyimpanan berkas lokal (dokumen RAG & lampiran gambar)
├── docs/                  api.md, architecture.md, client-requirements.md
├── tests/                 database integration tests
├── .env.example
├── Dockerfile
├── docker-compose.yml
└── go.mod
```

## Requirements

- Go 1.25+
- Docker + Docker Compose (untuk database PostgreSQL dan Judge0 CE)
- PostgreSQL (dengan pgvector) jika tidak menggunakan Docker

## Installation

1. Salin project ini ke workspace Anda.
2. Salin template konfigurasi environment:
   ```bash
   cp .env.example .env
   ```
3. Sesuaikan nilai di `.env` — minimal ganti `JWT_SECRET` dengan string acak panjang serta masukkan `AI_API_KEY` untuk mengaktifkan Gemini.
4. Download dependencies:
   ```bash
   go mod tidy
   ```

## Running

### Opsi A — PostgreSQL via Docker, Server Go Lokal:
```bash
docker compose up -d postgres
go run ./cmd/server
```

### Opsi B — Seluruh Service via Docker:
```bash
docker compose up -d --build
```

### Opsi C — PostgreSQL Berjalan Lokal di Host:
```bash
go run ./cmd/server
```

Server berjalan di `http://localhost:8080` (sesuai `APP_PORT`).

## Environment Variables

Lihat [.env.example](.env.example) untuk daftar lengkap dan deskripsi singkat.

Variabel wajib:
| Variable | Keterangan |
| --- | --- |
| `JWT_SECRET` | Secret untuk menandatangani JWT. Wajib ada; aplikasi gagal start jika kosong. |

Variabel penting lainnya:
- `APP_ENV`, `APP_PORT`, `DATABASE_URL` (atau `DATABASE_HOST`, `DATABASE_PORT`, dsb.)
- `AI_PROVIDER`, `AI_API_KEY`, `AI_MODEL`, `AI_BASE_URL`
- `EXEC_PROVIDER`, `EXEC_BASE_URL`, `EXEC_API_KEY`
- `UPLOAD_DIR`, `UPLOAD_MAX_BYTES`
- `CORS_ALLOWED_ORIGINS`, `RATE_LIMIT_*`, `AI_CHAT_RATE_LIMIT_*`, `EXEC_RATE_LIMIT_*`

> Jangan pernah commit `.env` yang berisi secret nyata.

## Testing

Jalankan seluruh unit test (tanpa dependensi database atau panggilan eksternal):
```bash
go test ./...
```

Jalankan integration test database (membutuhkan database aktif):
```bash
TEST_DB_INTEGRATION=true go test ./tests/...
```

## Docker

```bash
docker compose up -d          # Jalankan postgres & services pendukung
docker compose up -d --build  # Rebuild image server aplikasi
docker compose down           # Hentikan container (volume data tetap tersimpan)
docker compose down -v        # Hentikan container dan reset volume database
```

## API Summary

| Method | Path | Auth | Keterangan |
| --- | --- | --- | --- |
| GET | `/health` | - | Liveness check |
| GET | `/health/db` | - | Database readiness check |
| POST | `/api/v1/auth/register` | - | Registrasi user baru |
| POST | `/api/v1/auth/login` | - | Login, menerima JWT |
| GET | `/api/v1/auth/me` | Bearer | Profil user yang sedang login |
| GET | `/api/v1/ai/health` | - | Cek konfigurasi AI lokal |
| POST | `/api/v1/ai/chat` | Bearer | Interaksi AI multi-turn, RAG, & multimodal image attachment |
| POST | `/api/v1/ai/chat/stream` | Bearer / `?token=` | Streaming respon AI token-by-token via SSE |
| GET | `/api/v1/ai/conversations` | Bearer | Daftar riwayat percakapan user |
| GET | `/api/v1/ai/conversations/{id}` | Bearer | Detail percakapan beserta seluruh pesan & lampiran |
| DELETE | `/api/v1/ai/conversations/{id}` | Bearer | Hapus percakapan |
| GET | `/api/v1/ai/attachments/{id}` | Bearer | Mengunduh/menampilkan file gambar lampiran (alias: `/attachments/{id}`) |
| POST | `/api/v1/ai/documents` | Bearer | Ingest dokumen teks untuk RAG knowledge base |
| POST | `/api/v1/ai/documents/upload` | Bearer | Upload file dokumen (PDF, DOCX, TXT) dengan pemrosesan asinkron (alias: `/documents/upload`) |
| GET | `/api/v1/ai/documents` | Bearer | Daftar dokumen knowledge milik user |
| GET | `/api/v1/ai/documents/{id}` | Bearer | Detail dokumen knowledge milik user |
| GET | `/api/v1/ai/documents/{id}/status` | Bearer | Cek status background processing dokumen (`processing`, `ready`, `failed`, alias: `/documents/{id}/status`) |
| DELETE | `/api/v1/ai/documents/{id}` | Bearer | Hapus dokumen knowledge beserta vector chunks & file fisik |
| POST | `/api/v1/execute` | Bearer | Eksekusi kode sandboxed via Judge0 CE (Python, JavaScript, Go) |
| GET | `/api/v1/execute/history` | Bearer | Riwayat eksekusi kode milik user |
| GET | `/api/v1/execute/{id}` | Bearer | Detail riwayat eksekusi kode |

Dokumentasi lengkap:
- [API Reference & Specification](docs/api.md)
- [System Architecture & Design](docs/architecture.md)
- [Client & Business Requirements Guide](docs/client-requirements.md)
