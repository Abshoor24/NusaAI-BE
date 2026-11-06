# Project Name

> Backend AI Website Generator (Tahap A): user menjelaskan website yang diinginkan (brief), AI menghasilkan satu file HTML mandiri, lalu user merevisinya lewat instruksi teks.

## Overview

Backend untuk platform **AI Website Generator** (konsep: deskripsikan → generate → preview → revisi → unduh).

Sistem menyediakan fitur lengkap out-of-the-box:
- **REST API Clean Architecture**: Handler, Service, Repository, dan Provider abstraction.
- **PostgreSQL**: Connection pool terkelola (`pgxpool`), migrasi embedded otomatis via `embed.FS`.
- **Autentikasi**: Registrasi, login, verifikasi profil, JWT (`HS256`). Otorisasi berbasis kepemilikan resource per user.
- **AI Website Generate & Revise (SSE Streaming)**: Integrasi Google Gemini API, system prompt terkunci di server, streaming token real-time via Server-Sent Events (mendukung `Authorization` header dan query param `?token=`).
- **Versioning**: Setiap generate/revise menyimpan versi HTML lengkap baru; versi lama tidak pernah diubah.
- **Multimodal Image Attachments**: Gambar referensi (JPEG, PNG, WebP) via JSON base64 atau `multipart/form-data`, persistensi media lokal, dan serving gambar terisolasi per user.
- **Export**: Unduh website sebagai file `.html` tunggal.
- **Keamanan & Stabilitas**: Rate limiting 2 tingkat (General, Generate), CORS configurable, Security Headers, structured logging (`slog`), graceful shutdown, dan pemulihan status project saat startup.

Modul legacy **AI Chat & Conversations**, **RAG Knowledge Base**, dan **Code Execution Sandbox (Judge0)** beserta enam tabelnya sudah dihapus. Migrasi `0012` men-drop `conversations`, `messages`, `message_attachments`, `documents`, `document_chunks`, dan `code_executions`; schema aktif hanya berisi `users`, `projects`, `project_versions`, dan `project_attachments`. Preview website dirender di browser (`<iframe sandbox>`).

## Tech Stack

- **Bahasa & Routing**: Go (net/http + chi router v5)
- **Database**: PostgreSQL (driver `pgx/v5`, connection pooling)
- **Autentikasi**: JWT (golang-jwt/v5) + bcrypt
- **AI**: Google Generative Language API (Gemini)
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
│   │   ├── project/       domain inti AI Website Generator (handler/model/repository/service)
│   │   ├── ai/            /ai/health (handler) + Gemini provider (dipakai project)
│   │   └── business/      TEMPLATE/scaffold modul bisnis (handler/service/repository/migration)
│   ├── repository/        database access (UserRepository)
│   ├── routes/            route definition & middleware wiring
│   └── service/           business logic (AuthService)
├── migrations/            SQL migrations berurutan (0001 s.d. 0012, embedded via embed.FS)
├── pkg/
│   ├── logger/            structured logging (slog), env-aware formatting (text / JSON)
│   └── response/          standard JSON API response + request validation helpers
├── storage/               Supabase & PostgreSQL integration point
├── uploads/               penyimpanan gambar referensi project
├── docs/                  api.md, architecture.md, client-requirements.md
├── tests/                 database integration tests
├── .env.example
├── Dockerfile
├── docker-compose.yml
└── go.mod
```

## Requirements

- Go 1.25+
- Docker + Docker Compose (untuk database PostgreSQL)
- PostgreSQL (dengan pgvector) jika tidak menggunakan Docker

## Installation

1. Salin project ini ke workspace Anda.
2. Salin template konfigurasi environment:
   ```bash
   cp .env.example .env
   ```
3. Sesuaikan nilai di `.env` — minimal ganti `JWT_SECRET` dengan string acak panjang serta masukkan `AI_API_KEY` untuk mengaktifkan Gemini (server tetap boot tanpa API key; generate/revise akan mengembalikan `502` sampai key dikonfigurasi).
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
| `JWT_SECRET` | Secret untuk menandatangani JWT. Wajib ada; aplikasi gagal start jika kosong. Di production minimal 32 karakter dan bukan nilai placeholder/contoh. |

Variabel penting lainnya:
- `APP_ENV`, `APP_PORT`, `DATABASE_URL` (atau `DATABASE_HOST`, `DATABASE_PORT`, dsb.)
- `AI_PROVIDER`, `AI_API_KEY`, `AI_MODEL`, `AI_BASE_URL`
- `AI_MAX_REQUEST_BYTES` (default 1 MB, batas body JSON global), `AI_MAX_GENERATE_BODY_BYTES` (default 32 MB, khusus JSON generate/revise — pakai `multipart/form-data` untuk gambar. Catatan: 4 gambar × 10 MB menjadi ±53 MB setelah base64 sehingga tidak muat di body JSON 32 MB; batas gambar tidak diubah)
- `AI_MAX_OUTPUT_TOKENS` (opsional, > 0; kosong = default model), `AI_THINKING_BUDGET` (opsional, ≥ 0, 0 mematikan thinking bila didukung model; kosong = default model). Berguna bila output sering terpotong atau untuk membatasi biaya. Dikosongkan = `generationConfig` tidak dikirim.
- `AI_DEBUG_LOG_OUTPUT_EDGES` (default false; hanya development) — mencatat 200 karakter awal/akhir output gagal ke log. Output bisa berisi data pengguna, jangan aktifkan di luar development.
- `AI_STREAM_MAX_DURATION` (default 180s, wajib > 0) — batas total satu stream generate/revise; lewat batas → request ke Gemini dibatalkan, rollback, klien menerima `AI_PROVIDER_ERROR` "AI stream timed out".
- `AI_STREAM_IDLE_TIMEOUT` (default 60s, wajib > 0) — batas diam antar chunk provider; lewat batas → `AI_PROVIDER_ERROR` "AI stream stalled", rollback sama.
- `AI_RETRY_ON_UNAVAILABLE` (default 0, maks 2) + `AI_RETRY_BACKOFF` (default 2s) — ulangi pembukaan stream HANYA untuk 429/503 sebelum chunk pertama; default 0 = perilaku lama persis. Tiap retry bisa menambah puluhan detik karena Gemini kadang baru menjawab 503 setelah menunggu lama; total tetap dibatasi `AI_STREAM_MAX_DURATION`.
- `UPLOAD_DIR`, `UPLOAD_MAX_BYTES`
- `CORS_ALLOWED_ORIGINS` (CSV; default development `http://localhost:3000,http://localhost:5173`, di luar development default kosong), `RATE_LIMIT_*`
- `GENERATE_RATE_LIMIT_PER_MINUTE` (default 5), `PROJECT_MAX_IMAGES` (default 4), `PROJECT_MAX_HTML_BYTES` (default 524288 = 512 KiB)

> Jangan pernah commit `.env` yang berisi secret nyata.

## Testing

Jalankan seluruh unit test (tanpa dependensi database atau panggilan eksternal):
```bash
go test ./...
```

Jalankan integration test database (membutuhkan database aktif):
```bash
# PERINGATAN: test ini memakai DATABASE_URL dari .env. Jangan jalankan
# dengan .env yang menunjuk database produksi.
TEST_DB_INTEGRATION=true go test ./tests/...
```

Jalankan integration test repository project dengan Postgres asli
(menjalankan migrasi 0001–0012 dan menguji atomisitas `generating` serta
penomoran versi konkuren). `TEST_DATABASE_URL` wajib menunjuk ke database
kosong khusus test — jangan pernah mengarahkannya ke database produksi.
Test membuat user/project sekali pakai miliknya sendiri lalu menghapusnya
kembali (cascade) setelah selesai, dan tidak menyentuh data lain.
Butuh image pgvector karena migrasi 0006 memakai ekstensi `vector`:
```bash
docker run -d --name be-test-pg \
  -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=backend_test \
  -p 5433:5432 pgvector/pgvector:pg17
TEST_DATABASE_URL=postgresql://postgres:postgres@localhost:5433/backend_test?sslmode=disable \
  go test ./tests/ -run TestProjectIntegration -v -count=1
```
Tanpa `TEST_DATABASE_URL`, test-test tersebut di-skip otomatis.

## Testing dengan Supabase Test Project (Cloud)

Untuk integration test dan smoke test terhadap Postgres asli tanpa Docker,
buat project Supabase **terpisah dari produksi** khusus test:

1. **Buat project baru** di dashboard Supabase (mis. `be-test`) — jangan
   pakai project produksi.
2. **Aktifkan ekstensi `vector`**: Dashboard → Database → Extensions →
   aktifkan `vector` (dibutuhkan migrasi 0006; `pgcrypto` untuk
   `gen_random_uuid()` umumnya sudah aktif). Jika migrasi gagal karena izin
   ekstensi, aktifkan lewat dashboard lalu ulangi.
3. **Gunakan Session pooler, bukan yang lain**: di Connect → pilih
   **Session pooler**, host berakhiran `.pooler.supabase.com`, **port 5432**.
   Jangan pakai Transaction pooler (**port 6543**, DDL/migrasi tidak cocok)
   dan hindari Direct connection untuk test ini.
4. **Export URL hanya di sesi shell** (jangan tulis ke file/commit):
   ```bash
   export TEST_DATABASE_URL='postgresql://postgres.<ref>:<password>@<host>.pooler.supabase.com:5432/postgres?sslmode=require'
   ```
5. **Jalankan test migrasi + integrasi**:
   ```bash
   go test ./tests/ -run 'MigrationIntegration|TestProjectIntegration' -v -count=1
   ```

## Verifikasi migrasi 0010 (database dummy)

Round-trip naik-turun-naik (`10 → 9 → 10`) **pada database yang sedang
dipakai** (`DATABASE_URL` di `.env`). Hanya untuk database berisi data
dummy: di tengah jalan tabel `projects`, `project_versions`, dan
`project_attachments` **dihapus lalu dibuat ulang** (baris-barisnya hilang).

Syarat:

- Server **dihentikan** (tidak ada writer lain selama test).
- Isi database hanya data dummy; Anda menerima risikonya.
- Jangan pernah mengarahkan test ini ke database produksi.

Git Bash:

```bash
MIGRATION_ROUNDTRIP_CONFIRM=saya-paham-data-dummy go test ./tests/... -run Roundtrip -v -count=1
```

PowerShell:

```powershell
$env:MIGRATION_ROUNDTRIP_CONFIRM="saya-paham-data-dummy"; go test ./tests/... -run Roundtrip -v -count=1; Remove-Item Env:MIGRATION_ROUNDTRIP_CONFIRM
```

Test menolak berjalan kecuali `schema_migrations` tepat `version=10,
dirty=false`, menolak port pooler 6543 dan sslmode non-TLS, mencetak
identitas koneksi (host, port, dbname, user — tanpa password) dan jumlah
baris tabel 0010 sebelum menghapus, lalu memverifikasi: tabel 0010 hilang
di versi 9, objek 0001–0009 tidak berubah sedikit pun, skema kembali identik
di versi 10, dan constraint unik menolak nomor versi duplikat. Akhir test
(selalu, via cleanup) database dikembalikan ke versi 10.

Memeriksa hasil akhir: `SELECT version, dirty FROM schema_migrations;`
harus `10, false`.

Pemulihan jika `dirty=true` (jangan perbaiki otomatis):

1. `SELECT * FROM schema_migrations;` — catat versi dan dirty.
2. Periksa tabel mana yang ada: `SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY 1;`
3. Hanya setelah kondisi skema cocok dengan suatu versi, jalankan:
   `UPDATE schema_migrations SET version=<benar>, dirty=false;`
   lalu ulangi test. Jika ragu, pulihkan dari backup.

## Smoke Test API (`scripts/smoke.sh`)

Skrip bash tanpa rahasia di dalamnya (`BASE_URL` default
`http://localhost:8080`): register → create project → generate (SSE) →
ambil versi 1 → revise → export, plus tiga kasus gagal terpisah
(`bad_key`, `disconnect`, `concurrent`).

```bash
./scripts/smoke.sh            # alur normal + kasus concurrent
./scripts/smoke.sh bad_key    # server harus jalan dengan key Gemini palsu
./scripts/smoke.sh disconnect
./scripts/smoke.sh concurrent
```

**Menjalankan server untuk smoke test.** Variabel environment shell
**mengalahkan** `.env` (`godotenv.Load` tidak menimpa variabel yang sudah
ada — lihat `internal/config/config.go`), jadi tanpa mengedit `.env`:

```bash
DATABASE_URL="$TEST_DATABASE_URL" AI_API_KEY="$GEMINI_API_KEY" \
  JWT_SECRET="$(openssl rand -hex 32)" go run ./cmd/server
```

Gunakan `TEST_DATABASE_URL` (database test, bukan produksi) dan key Gemini
khusus test. Tanpa `GEMINI_API_KEY`, jalankan hanya kasus yang tidak butuh
Gemini asli. Menjalankan ulang terlalu cepat bisa kena rate limit Generate
(429, tunggu ±60 detik).

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
| POST | `/api/v1/projects` | Bearer | Membuat project baru dari brief |
| GET | `/api/v1/projects` | Bearer | Daftar project milik user |
| GET | `/api/v1/projects/{id}` | Bearer | Detail project + versi terbaru (HTML) |
| DELETE | `/api/v1/projects/{id}` | Bearer | Hapus project + versi + file gambar |
| POST | `/api/v1/projects/{id}/generate` | Bearer / `?token=` | Generate versi 1 via SSE |
| POST | `/api/v1/projects/{id}/revise` | Bearer / `?token=` | Revisi menjadi versi baru via SSE |
| GET | `/api/v1/projects/{id}/versions` | Bearer | Riwayat versi (tanpa HTML) |
| GET | `/api/v1/projects/{id}/versions/{number}` | Bearer | Satu versi lengkap dengan HTML |
| GET | `/api/v1/projects/{id}/export` | Bearer | Unduh website sebagai file `.html` |
| GET | `/api/v1/projects/{id}/attachments` | Bearer | Daftar gambar referensi project |
| GET | `/api/v1/projects/{id}/attachments/{attachmentId}` | Bearer | Biner gambar attachment |

Dokumentasi lengkap:
- [API Reference & Specification](docs/api.md)
- [System Architecture & Design](docs/architecture.md)
- [Client & Business Requirements Guide](docs/client-requirements.md)
