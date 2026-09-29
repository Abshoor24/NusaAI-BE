# API Documentation — AI Website Generator

> **Single Source of Truth** untuk semua request/response kontrak antara backend dan frontend.
> Setiap field, tipe data, dan constraint di dokumen ini harus selalu sama dengan kode (struct DTO, handler, validasi, dan package `apperror`). Jika kode berubah, dokumen ini wajib diperbarui pada commit yang sama.

Produk ini adalah **AI Website Generator**: user login, menjelaskan website yang ingin dibuat (brief), AI menghasilkan website, lalu user merevisinya lewat instruksi teks. Pada **Tahap A** (dokumen ini), setiap versi website adalah **satu file HTML mandiri** (*single self-contained HTML document*).

---

## Perubahan dari Versi Sebelumnya (Generic AI Assistant)

| Area | Status | Keterangan |
| --- | --- | --- |
| Auth (`/auth/*`) | Tetap | Tidak ada perubahan kontrak |
| Health (`/health`, `/health/db`, `/ai/health`) | Tetap | Tidak ada perubahan kontrak |
| Projects, Generate, Revise, Versions, Export, Attachments | **Baru** | Lihat bagian 3 s.d. 7 |
| `POST /ai/chat`, `POST /ai/chat/stream` | **Dihapus dari router** | Digantikan `generate` dan `revise` |
| `/ai/conversations*` | **Dihapus dari router** | Digantikan `/projects*` |
| `/ai/attachments/{id}` | **Dihapus dari router** | Digantikan `/projects/{id}/attachments/{attachmentId}` |
| `/ai/documents*` (RAG) | **Ditunda** | Tidak diregistrasi di router; kode dan tabel dipertahankan untuk tahap berikutnya |
| `/execute*` (Judge0 sandbox) | **Ditunda** | Tidak diregistrasi di router; preview website dirender di browser |
| Field `system` (override system prompt) | **Dihapus** | System prompt dikunci di server |

Endpoint yang ditandai "dihapus dari router" atau "ditunda" akan mengembalikan `404 Not Found` standar router.

---

## Table of Contents

- [Base URL](#base-url)
- [Autentikasi Umum](#autentikasi-umum)
- [Format Response Standar](#format-response-standar)
- [Error Codes](#error-codes)
- [Rate Limiting](#rate-limiting)
- [1. System & Health](#1-system--health)
- [2. Auth](#2-auth)
- [3. Projects](#3-projects)
  - [POST /api/v1/projects](#post-apiv1projects)
  - [GET /api/v1/projects](#get-apiv1projects)
  - [GET /api/v1/projects/{id}](#get-apiv1projectsid)
  - [DELETE /api/v1/projects/{id}](#delete-apiv1projectsid)
- [4. Generate & Revise (SSE)](#4-generate--revise-sse)
  - [POST /api/v1/projects/{id}/generate](#post-apiv1projectsidgenerate-sse)
  - [POST /api/v1/projects/{id}/revise](#post-apiv1projectsidrevise-sse)
- [5. Versions](#5-versions)
  - [GET /api/v1/projects/{id}/versions](#get-apiv1projectsidversions)
  - [GET /api/v1/projects/{id}/versions/{number}](#get-apiv1projectsidversionsnumber)
- [6. Export](#6-export)
  - [GET /api/v1/projects/{id}/export](#get-apiv1projectsidexport)
- [7. Attachments (Gambar Referensi)](#7-attachments-gambar-referensi)
  - [GET /api/v1/projects/{id}/attachments](#get-apiv1projectsidattachments)
  - [GET /api/v1/projects/{id}/attachments/{attachmentId}](#get-apiv1projectsidattachmentsattachmentid)
- [8. Menampilkan Preview di Frontend](#8-menampilkan-preview-di-frontend)

---

## Base URL

| Lingkungan | Base URL |
| --- | --- |
| Development | `http://localhost:8080` (default `APP_PORT`) |
| Production | Dikonfigurasi via environment `APP_HOST` + `APP_PORT` |

Semua endpoint API berada di bawah prefix `/api/v1`.

---

## Autentikasi Umum

Backend menggunakan **JWT (JSON Web Token)** dengan algoritma **HS256**, ditandatangani dengan `JWT_SECRET`.

### Cara Mendapatkan Token

1. Daftar via `POST /api/v1/auth/register` → token langsung dikembalikan di response.
2. Login via `POST /api/v1/auth/login` → token dikembalikan di response.

### Cara Menggunakan Token

Untuk semua endpoint protected, sertakan token di **Authorization header**:

```http
Authorization: Bearer <token>
```

**Pengecualian:** Endpoint SSE (`POST /api/v1/projects/{id}/generate` dan `POST /api/v1/projects/{id}/revise`) juga menerima token via query parameter `?token=<jwt>`. Header tetap direkomendasikan; query param disediakan untuk kompatibilitas klien yang tidak bisa mengirim header kustom.

### JWT Payload (Claims)

Token berisi claims berikut:
- `sub` — User ID (UUID)
- `email` — Email user
- `role` — Role user (`"admin"` adalah satu-satunya role yang ada saat ini)
- `iss` — Issuer (dikonfigurasi via `JWT_ISSUER`, default `"backend"`)
- `iat` — Issued At (Unix timestamp)
- `exp` — Expiry (Unix timestamp; default expiry 24 jam, dikonfigurasi via `JWT_EXPIRATION`)

### Token Expired / Invalid

Jika token tidak ada, kedaluwarsa, atau tanda tangan tidak valid, server akan mengembalikan:

```json
HTTP 401 Unauthorized

{
  "success": false,
  "error": {
    "code": "UNAUTHORIZED",
    "message": "Invalid or expired authentication token"
  }
}
```

Jika header `Authorization` tidak ada sama sekali:

```json
HTTP 401 Unauthorized

{
  "success": false,
  "error": {
    "code": "UNAUTHORIZED",
    "message": "Missing authentication token"
  }
}
```

---

## Format Response Standar

Semua response JSON menggunakan wrapper yang konsisten.

### Response Sukses

```json
{
  "success": true,
  "message": "Request successful",
  "data": { ... }
}
```

> `data` bisa berupa objek, array, atau `null` (untuk operasi delete).

### Response Error

```json
{
  "success": false,
  "error": {
    "code": "ERROR_CODE",
    "message": "Human-readable error message"
  }
}
```

### Response Error dengan Detail Validasi Field

Ketika error code adalah `VALIDATION_ERROR` dan error berasal dari struct validation (bukan parse error), response menyertakan field `fields`:

```json
{
  "success": false,
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Invalid request",
    "fields": {
      "brief.description": "must be at least 10 characters"
    }
  }
}
```

> [!NOTE]
> Response error **tidak pernah** mengekspos stack trace, query SQL, path file lokal, secret environment, API key, maupun detail internal lainnya. Detail teknis hanya dicatat di server log.
>
> Setiap response menyertakan header tracking `X-Request-ID`.

> [!NOTE]
> Request JSON yang mengandung **field tidak dikenal** ditolak dengan `BAD_REQUEST`. Contoh: mengirim field `system` (yang sudah dihapus) akan ditolak.

---

## Error Codes

| Code | HTTP Status | Kapan Muncul |
| --- | --- | --- |
| `VALIDATION_ERROR` | 400 | Field request tidak valid, wajib tapi kosong, format salah, atau melebihi batasan panjang. Response menyertakan `fields` map jika berasal dari struct validator. |
| `BAD_REQUEST` | 400 | Body request bukan JSON valid, payload melebihi ukuran maksimum, atau JSON mengandung field yang tidak dikenali. |
| `UNAUTHORIZED` | 401 | Token JWT hilang, tidak valid, kedaluwarsa, atau kredensial login (email/password) salah. |
| `FORBIDDEN` | 403 | Role user tidak mencukupi untuk mengakses resource (saat ini belum digunakan di route manapun selain middleware internal). |
| `NOT_FOUND` | 404 | Project, versi, atau attachment tidak ditemukan, atau bukan milik user yang sedang login. |
| `CONFLICT` | 409 | Email sudah terdaftar saat register; **atau** state project tidak mengizinkan operasi (lihat bagian 4: generate/revise). |
| `RATE_LIMITED` | 429 | Terlalu banyak request dari IP yang sama dalam satu menit. Selalu disertai header `Retry-After: 60`. |
| `AI_PROVIDER_ERROR` | 502 | Upstream LLM provider (Gemini) error, tidak dapat dijangkau, atau timeout. Detail teknis tidak diekspos ke client. |
| `EXECUTION_PROVIDER_ERROR` | 502 | **Tidak digunakan saat ini** (modul execution ditunda). Kode dipertahankan di `apperror` untuk tahap berikutnya. |
| `SERVICE_UNAVAILABLE` | 503 | Database tidak tersedia atau tidak dapat dijangkau. |
| `DATABASE_ERROR` | 500 | Operasi database gagal (query error, constraint violation, dsb.). |
| `INTERNAL_SERVER_ERROR` | 500 | Kesalahan internal server yang tidak terklasifikasi. |

---

## Rate Limiting

Backend menerapkan **2 tingkatan rate limiting** berbasis token bucket per alamat IP client:

| Tier | Endpoint yang Terkena | Default Limit | Env Var Konfigurasi |
| --- | --- | --- | --- |
| **General API** | Semua endpoint `/api/v1/*` | 120 req/menit | `RATE_LIMIT_PER_MINUTE`, `RATE_LIMIT_BURST` |
| **Generate** | `POST /projects/{id}/generate`, `POST /projects/{id}/revise` | 5 req/menit | `GENERATE_RATE_LIMIT_PER_MINUTE` |

> [!IMPORTANT]
> Limit Generate adalah **tambahan** di atas General API limit — request yang melampaui salah satunya akan mendapat 429.

Endpoint `/health` dan `/health/db` **tidak kena rate limit**.

### Response 429

```http
HTTP/1.1 429 Too Many Requests
Content-Type: application/json
Retry-After: 60
X-Request-ID: req_abc123def456
```

```json
{
  "success": false,
  "error": {
    "code": "RATE_LIMITED",
    "message": "Too many requests, please try again later"
  }
}
```

Header `Retry-After: 60` selalu ada pada response 429 — nilai tetap 60 detik.

```javascript
// Contoh penanganan di frontend
if (response.status === 429) {
  const retryAfter = parseInt(response.headers.get('Retry-After') || '60', 10);
  await new Promise(r => setTimeout(r, retryAfter * 1000));
  // retry request
}
```

---

## 1. System & Health

### GET /health

Liveness check. Memverifikasi web server aktif tanpa menyentuh database.

- **Auth:** Tidak diperlukan
- **Rate Limit:** Tidak ada

#### Response `200 OK`

```json
{
  "success": true,
  "message": "Request successful",
  "data": {
    "status": "ok"
  }
}
```

---

### GET /health/db

Readiness check koneksi database PostgreSQL (ping ke pgxpool).

- **Auth:** Tidak diperlukan
- **Rate Limit:** Tidak ada

#### Response `200 OK`

```json
{
  "success": true,
  "message": "Request successful",
  "data": {
    "status": "ok",
    "database": "connected"
  }
}
```

#### Response `503 Service Unavailable`

```json
{
  "success": false,
  "error": {
    "code": "SERVICE_UNAVAILABLE",
    "message": "Database is unavailable"
  }
}
```

---

### GET /api/v1/ai/health

Health check konfigurasi runtime AI. Memeriksa apakah provider, API key, dan model sudah dikonfigurasi — tanpa memanggil API eksternal sehingga tidak mengkonsumsi kuota.

- **Auth:** Tidak diperlukan
- **Rate Limit:** General API (120 req/menit)

#### Response `200 OK`

```json
{
  "success": true,
  "message": "Request successful",
  "data": {
    "provider": "gemini",
    "configured": true,
    "checks": {
      "provider": true,
      "api_key": true,
      "model": true
    }
  }
}
```

> `configured` adalah `true` hanya jika ketiga field (`provider`, `api_key`, `model`) semuanya terisi.

---

## 2. Auth

### POST /api/v1/auth/register

Mendaftarkan akun baru dan langsung mengembalikan JWT aktif.

- **Auth:** Tidak diperlukan
- **Rate Limit:** General API (120 req/menit)

#### Request

| Header | Nilai |
| --- | --- |
| `Content-Type` | `application/json` |

**Body:**

| Field | Tipe | Wajib | Constraint |
| --- | --- | --- | --- |
| `email` | `string` | Ya | Format email valid; dinormalisasi ke huruf kecil otomatis |
| `password` | `string` | Ya | Min 8 karakter, maks 72 karakter (batasan bcrypt) |

```json
{
  "email": "developer@example.com",
  "password": "PasswordSuperAman123"
}
```

#### Response `201 Created`

```json
{
  "success": true,
  "message": "User registered successfully",
  "data": {
    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJlbWFpbCI6ImRldmVsb3BlckBleGFtcGxlLmNvbSIsInJvbGUiOiJhZG1pbiIsInN1YiI6ImMxZjc2ZDQ5LTRlYjUtNGE1Zi05ZTc3LTVmNzI1Y2Y5YTlmMiIsImlzcyI6ImJhY2tlbmQiLCJpYXQiOjE3Mjc2NzM2MDAsImV4cCI6MTcyNzc2MDAwMH0.abc123",
    "token_type": "Bearer",
    "expires_in": 86400,
    "user": {
      "id": "c1f76d49-4eb5-4a5f-9e77-5f725cf9a9f2",
      "email": "developer@example.com",
      "role": "admin",
      "created_at": "2026-09-26T10:00:00Z"
    }
  }
}
```

**Schema `data`:**

| Field | Tipe | Deskripsi |
| --- | --- | --- |
| `token` | `string` | JWT Bearer token yang harus disimpan dan disertakan di request berikutnya |
| `token_type` | `string` | Selalu `"Bearer"` |
| `expires_in` | `integer` | Sisa masa berlaku token dalam detik (default 86400 = 24 jam) |
| `user.id` | `string` (UUID) | ID unik user |
| `user.email` | `string` | Email user (sudah dinormalisasi ke lowercase) |
| `user.role` | `string` | Role user — saat ini selalu `"admin"` |
| `user.created_at` | `string` (RFC3339) | Waktu pembuatan akun |

#### Error Responses

| Status | Code | Kapan |
| --- | --- | --- |
| 400 | `VALIDATION_ERROR` | Email tidak valid, password terlalu pendek/panjang, atau field wajib kosong |
| 400 | `BAD_REQUEST` | Body bukan JSON valid atau mengandung unknown field |
| 409 | `CONFLICT` | Email sudah terdaftar |

**Contoh 400 VALIDATION_ERROR:**
```json
{
  "success": false,
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Invalid request",
    "fields": {
      "email": "must be a valid email address",
      "password": "must be at least 8 characters"
    }
  }
}
```

**Contoh 409 CONFLICT:**
```json
{
  "success": false,
  "error": {
    "code": "CONFLICT",
    "message": "An account with this email already exists"
  }
}
```

---

### POST /api/v1/auth/login

Autentikasi dengan email dan password untuk mendapatkan JWT.

- **Auth:** Tidak diperlukan
- **Rate Limit:** General API (120 req/menit)

#### Request

| Header | Nilai |
| --- | --- |
| `Content-Type` | `application/json` |

**Body:**

| Field | Tipe | Wajib | Constraint |
| --- | --- | --- | --- |
| `email` | `string` | Ya | Format email valid |
| `password` | `string` | Ya | Tidak ada constraint panjang saat login |

```json
{
  "email": "developer@example.com",
  "password": "PasswordSuperAman123"
}
```

#### Response `200 OK`

```json
{
  "success": true,
  "message": "Login successful",
  "data": {
    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJlbWFpbCI6ImRldmVsb3BlckBleGFtcGxlLmNvbSIsInJvbGUiOiJhZG1pbiIsInN1YiI6ImMxZjc2ZDQ5LTRlYjUtNGE1Zi05ZTc3LTVmNzI1Y2Y5YTlmMiIsImlzcyI6ImJhY2tlbmQiLCJpYXQiOjE3Mjc2NzM2MDAsImV4cCI6MTcyNzc2MDAwMH0.abc123",
    "token_type": "Bearer",
    "expires_in": 86400,
    "user": {
      "id": "c1f76d49-4eb5-4a5f-9e77-5f725cf9a9f2",
      "email": "developer@example.com",
      "role": "admin",
      "created_at": "2026-09-26T10:00:00Z"
    }
  }
}
```

> Schema `data` identik dengan response register.

#### Error Responses

| Status | Code | Kapan |
| --- | --- | --- |
| 400 | `VALIDATION_ERROR` | Email tidak valid atau field wajib kosong |
| 400 | `BAD_REQUEST` | Body bukan JSON valid |
| 401 | `UNAUTHORIZED` | Email tidak ditemukan atau password salah |

> [!IMPORTANT]
> Untuk alasan keamanan (mencegah user enumeration), error `UNAUTHORIZED` dikembalikan baik untuk email yang tidak terdaftar **maupun** password yang salah — dengan pesan yang sama: `"Invalid email or password"`.

**Contoh 401 UNAUTHORIZED:**
```json
{
  "success": false,
  "error": {
    "code": "UNAUTHORIZED",
    "message": "Invalid email or password"
  }
}
```

---

### GET /api/v1/auth/me

Mengambil data profil pengguna yang sedang login berdasarkan token JWT.

- **Auth:** Wajib (`Authorization: Bearer <token>`)
- **Rate Limit:** General API (120 req/menit)

#### Response `200 OK`

```json
{
  "success": true,
  "message": "Request successful",
  "data": {
    "id": "c1f76d49-4eb5-4a5f-9e77-5f725cf9a9f2",
    "email": "developer@example.com",
    "role": "admin",
    "created_at": "2026-09-26T10:00:00Z"
  }
}
```

**Schema `data` (`UserResponse`):**

| Field | Tipe | Deskripsi |
| --- | --- | --- |
| `id` | `string` (UUID) | ID unik user |
| `email` | `string` | Email user |
| `role` | `string` | Role user (`"admin"`) |
| `created_at` | `string` (RFC3339) | Waktu pembuatan akun |

#### Error Responses

| Status | Code | Kapan |
| --- | --- | --- |
| 401 | `UNAUTHORIZED` | Token tidak ada atau tidak valid |
| 404 | `NOT_FOUND` | User di database sudah dihapus padahal token masih valid |

---

## 3. Projects

**Project** adalah satu website yang sedang dibuat user. Setiap project menyimpan **brief** (deskripsi keinginan user) dan riwayat **versi** hasil generate/revisi.

### Alur Penggunaan Utama

```
Login
  └─► Halaman "Website seperti apa yang ingin kamu buat?"
        └─► POST /projects            (kirim brief)
              └─► POST /projects/{id}/generate   (SSE, versi 1)
                    └─► GET  /projects/{id}/versions/1   (ambil HTML final → preview)
                          └─► POST /projects/{id}/revise (SSE, versi 2, 3, ...)
                                └─► GET /projects/{id}/export (unduh .html)
```

### Status Project

| `status` | Arti | Operasi yang Diizinkan |
| --- | --- | --- |
| `draft` | Project dibuat, belum pernah di-generate | `generate` |
| `generating` | Sedang ada proses generate/revise berjalan | *(tidak ada; `generate`/`revise` → `409 CONFLICT`)* |
| `ready` | Punya minimal satu versi dan tidak sedang diproses | `revise` |
| `failed` | Generate pertama gagal (belum punya versi) | `generate` (coba lagi) |

Transisi: `draft → generating → ready | failed`, `failed → generating → ready | failed`, `ready → generating → ready`.
Jika **revise** gagal, status kembali ke `ready` (versi sebelumnya tetap utuh).

### Schema `Brief`

| Field | Tipe | Wajib | Constraint | Deskripsi |
| --- | --- | --- | --- | --- |
| `description` | `string` | Ya | 10–4000 karakter | Penjelasan bebas website yang diinginkan |
| `website_type` | `string` | Tidak | Salah satu: `landing_page`, `portfolio`, `company_profile`, `online_store`, `blog`, `other`. Default `landing_page` | Jenis website |
| `business_name` | `string` | Tidak | Maks 120 karakter | Nama bisnis / brand / pemilik |
| `style` | `string` | Tidak | Maks 200 karakter | Gaya visual (mis. "modern minimalis", "playful", "elegan") |
| `color_palette` | `string` | Tidak | Maks 200 karakter | Preferensi warna (mis. "biru navy dan putih", `#0F172A`) |
| `language` | `string` | Tidak | `id` atau `en`. Default `id` | Bahasa konten website |
| `sections` | `array<string>` | Tidak | Maks 12 item, tiap item maks 50 karakter | Section yang diinginkan (mis. `["hero","about","pricing","contact"]`) |

### Schema `ProjectResponse`

| Field | Tipe | Deskripsi |
| --- | --- | --- |
| `id` | `string` (UUID) | ID project |
| `title` | `string` | Judul project |
| `status` | `string` | `draft`, `generating`, `ready`, atau `failed` |
| `brief` | `Brief` | Brief yang tersimpan |
| `version_count` | `integer` | Jumlah versi yang sudah ada |
| `latest_version` | `VersionResponse` \| `null` | **Hanya ada di `GET /projects/{id}`.** Versi terbaru lengkap dengan `html`; `null` jika belum ada versi |
| `created_at` | `string` (RFC3339) | Waktu project dibuat |
| `updated_at` | `string` (RFC3339) | Waktu project terakhir berubah |

---

### POST /api/v1/projects

Membuat project baru dari brief. **Tidak** memanggil AI — hanya menyimpan brief. Generate dilakukan lewat endpoint terpisah.

- **Auth:** Wajib (`Authorization: Bearer <token>`)
- **Rate Limit:** General API (120 req/menit)

#### Request

| Header | Nilai |
| --- | --- |
| `Content-Type` | `application/json` |
| `Authorization` | `Bearer <token>` |

**Body:**

| Field | Tipe | Wajib | Constraint | Deskripsi |
| --- | --- | --- | --- | --- |
| `title` | `string` | Tidak | Maks 200 karakter | Judul project. Jika kosong, server memakai `brief.business_name`; jika itu juga kosong, memakai 60 karakter pertama `brief.description` |
| `brief` | `Brief` | Ya | Lihat schema `Brief` | Brief website |

```json
{
  "title": "Website Kopi Senja",
  "brief": {
    "description": "Landing page untuk kedai kopi lokal di Purwokerto dengan menu, jam buka, lokasi, dan tombol pesan via WhatsApp.",
    "website_type": "landing_page",
    "business_name": "Kopi Senja",
    "style": "hangat, rustic, modern",
    "color_palette": "cokelat kopi dan krem",
    "language": "id",
    "sections": ["hero", "menu", "about", "location", "contact"]
  }
}
```

#### Response `201 Created`

```json
{
  "success": true,
  "message": "Project created",
  "data": {
    "id": "5e2b7a10-9c34-4f0e-8d55-2a1c6b7e9f01",
    "title": "Website Kopi Senja",
    "status": "draft",
    "brief": {
      "description": "Landing page untuk kedai kopi lokal di Purwokerto dengan menu, jam buka, lokasi, dan tombol pesan via WhatsApp.",
      "website_type": "landing_page",
      "business_name": "Kopi Senja",
      "style": "hangat, rustic, modern",
      "color_palette": "cokelat kopi dan krem",
      "language": "id",
      "sections": ["hero", "menu", "about", "location", "contact"]
    },
    "version_count": 0,
    "created_at": "2026-09-30T08:00:00Z",
    "updated_at": "2026-09-30T08:00:00Z"
  }
}
```

#### Error Responses

| Status | Code | Kapan |
| --- | --- | --- |
| 400 | `VALIDATION_ERROR` | `brief.description` kosong/terlalu pendek/terlalu panjang, `website_type` atau `language` di luar daftar, atau melebihi batas panjang lainnya |
| 400 | `BAD_REQUEST` | Body bukan JSON valid, melebihi ukuran maksimum, atau mengandung field tidak dikenal |
| 401 | `UNAUTHORIZED` | Token tidak ada atau tidak valid |
| 429 | `RATE_LIMITED` | Limit General API terlampaui |

---

### GET /api/v1/projects

Mengambil daftar project milik user yang sedang login, diurutkan dari yang terakhir diperbarui.

- **Auth:** Wajib
- **Rate Limit:** General API (120 req/menit)

**Query Parameters:**

| Parameter | Tipe | Default | Constraint | Deskripsi |
| --- | --- | --- | --- | --- |
| `limit` | `integer` | `20` | Min 1, maks 100 | Jumlah item yang dikembalikan |
| `offset` | `integer` | `0` | Min 0 | Offset untuk pagination |

> Nilai `limit`/`offset` yang tidak valid diabaikan dan server memakai default.

#### Response `200 OK`

```json
{
  "success": true,
  "message": "Request successful",
  "data": [
    {
      "id": "5e2b7a10-9c34-4f0e-8d55-2a1c6b7e9f01",
      "title": "Website Kopi Senja",
      "status": "ready",
      "brief": { "description": "Landing page untuk kedai kopi lokal ...", "website_type": "landing_page", "language": "id" },
      "version_count": 3,
      "created_at": "2026-09-30T08:00:00Z",
      "updated_at": "2026-09-30T08:20:00Z"
    }
  ]
}
```

> Item array memakai schema `ProjectResponse` **tanpa** `latest_version`. Jika user belum punya project, `data` adalah array kosong `[]`.

#### Error Responses

| Status | Code | Kapan |
| --- | --- | --- |
| 401 | `UNAUTHORIZED` | Token tidak ada atau tidak valid |
| 429 | `RATE_LIMITED` | Limit General API terlampaui |

---

### GET /api/v1/projects/{id}

Mengambil detail satu project beserta **versi terbaru lengkap dengan HTML**-nya (untuk langsung ditampilkan di preview).

- **Auth:** Wajib
- **Rate Limit:** General API (120 req/menit)
- **Security:** Hanya mengembalikan project milik user yang sedang login (`WHERE id = $1 AND user_id = $2`)

**Path param:** `{id}` — UUID project

#### Response `200 OK`

```json
{
  "success": true,
  "message": "Request successful",
  "data": {
    "id": "5e2b7a10-9c34-4f0e-8d55-2a1c6b7e9f01",
    "title": "Website Kopi Senja",
    "status": "ready",
    "brief": { "description": "Landing page untuk kedai kopi lokal ...", "website_type": "landing_page", "language": "id" },
    "version_count": 2,
    "latest_version": {
      "id": "b3c1d2e4-1f2a-4b3c-9d4e-5f6a7b8c9d0e",
      "project_id": "5e2b7a10-9c34-4f0e-8d55-2a1c6b7e9f01",
      "version_number": 2,
      "kind": "revise",
      "prompt": "Ganti warna hero menjadi biru tua",
      "html": "<!DOCTYPE html><html lang=\"id\">...</html>",
      "created_at": "2026-09-30T08:20:00Z"
    },
    "created_at": "2026-09-30T08:00:00Z",
    "updated_at": "2026-09-30T08:20:00Z"
  }
}
```

#### Error Responses

| Status | Code | Kapan |
| --- | --- | --- |
| 400 | `VALIDATION_ERROR` | `id` bukan UUID valid |
| 401 | `UNAUTHORIZED` | Token tidak ada atau tidak valid |
| 404 | `NOT_FOUND` | Project tidak ditemukan atau bukan milik user |
| 429 | `RATE_LIMITED` | Limit General API terlampaui |

---

### DELETE /api/v1/projects/{id}

Menghapus project beserta seluruh versi dan attachment-nya (record database **dan** file gambar di disk).

- **Auth:** Wajib
- **Rate Limit:** General API (120 req/menit)
- **Security:** Hanya project milik user yang sedang login

#### Response `200 OK`

```json
{
  "success": true,
  "message": "Project deleted",
  "data": null
}
```

#### Error Responses

| Status | Code | Kapan |
| --- | --- | --- |
| 400 | `VALIDATION_ERROR` | `id` bukan UUID valid |
| 401 | `UNAUTHORIZED` | Token tidak ada atau tidak valid |
| 404 | `NOT_FOUND` | Project tidak ditemukan atau bukan milik user |
| 409 | `CONFLICT` | Project sedang `generating` |

---

## 4. Generate & Revise (SSE)

Kedua endpoint ini mengalirkan hasil AI secara real-time menggunakan **Server-Sent Events (SSE)**. Isi stream adalah **potongan teks HTML** yang sedang ditulis AI.

- **Auth:** Wajib. `Authorization: Bearer <token>` (direkomendasikan) atau `?token=<jwt>`
- **Rate Limit:** Generate (default 5 req/menit per IP) **+ General API (120 req/menit)**
- **Content-Type yang Didukung:** `application/json` dan `multipart/form-data`
- **Ukuran body:** klien sebaiknya memakai `multipart/form-data` saat mengirim gambar, sedangkan body JSON dibatasi sesuai konfigurasi (`AI_MAX_GENERATE_BODY_BYTES`, default 32 MB).

### Format Event SSE (berlaku untuk generate dan revise)

**Response Headers (HTTP 200 OK — SSE dimulai)**

```http
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive
X-Accel-Buffering: no
```

> [!NOTE]
> Server mengirim HTTP 200 dan membuka koneksi SSE segera setelah semua validasi awal lolos. Error yang terjadi **sebelum** itu (token invalid, project tidak ditemukan, `409 CONFLICT`, validasi gagal) dikembalikan sebagai JSON error biasa dengan HTTP 4xx/5xx.

**1. `event: token`** — dikirim berulang selama HTML ditulis

```
event: token
data: {"token":"<!DOCTYPE html>\n<html lang=\"id\">"}
```

| Field | Tipe | Deskripsi |
| --- | --- | --- |
| `token` | `string` | Potongan teks (bisa satu kata, satu baris, atau beberapa baris) |

**2. `event: done`** — dikirim **setelah** HTML lolos validasi dan versi baru tersimpan di database

```
event: done
data: {"project_id":"5e2b7a10-9c34-4f0e-8d55-2a1c6b7e9f01","version_id":"b3c1d2e4-1f2a-4b3c-9d4e-5f6a7b8c9d0e","version_number":2}
```

| Field | Tipe | Deskripsi |
| --- | --- | --- |
| `project_id` | `string` (UUID) | ID project |
| `version_id` | `string` (UUID) | ID versi baru |
| `version_number` | `integer` | Nomor versi baru (mulai dari 1) |

**3. `event: error`** — dikirim jika terjadi kegagalan **di tengah streaming**

```
event: error
data: {"error":"The AI provider returned an error"}
```

| Field | Tipe | Deskripsi |
| --- | --- | --- |
| `error` | `string` | Pesan error generic (detail teknis disembunyikan) |

Penyebab `error` di tengah stream: provider Gemini error/timeout, koneksi client terputus, atau hasil AI **tidak lolos validasi** (bukan dokumen HTML, atau melebihi `PROJECT_MAX_HTML_BYTES`). Pada semua kasus ini **tidak ada versi baru yang disimpan**.

> [!IMPORTANT]
> **Jangan render stream `token` langsung sebagai website.** Stream hanya untuk indikator progres (mis. menampilkan kode yang sedang ditulis). Model kadang membungkus output dengan pagar kode Markdown; server membersihkannya sebelum menyimpan. Sumber HTML final yang valid adalah `GET /projects/{id}/versions/{number}` setelah event `done`.

---

### POST /api/v1/projects/{id}/generate (SSE)

Membuat **versi 1** website dari brief project.

**Syarat state:** status project harus `draft` atau `failed` (belum punya versi).

#### Request

**Opsi A: `application/json`**

| Field | Tipe | Wajib | Constraint | Deskripsi |
| --- | --- | --- | --- | --- |
| `images` | `array<ImagePayload>` | Tidak | Maks `PROJECT_MAX_IMAGES` (default 4) | Gambar referensi (logo, contoh desain, screenshot) |

Body boleh berupa objek kosong `{}`.

**Schema `ImagePayload`:**

| Field | Tipe | Wajib | Constraint |
| --- | --- | --- | --- |
| `mime_type` | `string` | Ya | `"image/jpeg"`, `"image/jpg"`, `"image/png"`, atau `"image/webp"` |
| `data` | `string` (base64) | Ya | Data biner gambar base64. Maks ukuran setelah decode sesuai `UPLOAD_MAX_BYTES` (default 10 MB) |
| `file_name` | `string` | Tidak | Nama file opsional |

```json
{
  "images": [
    {
      "file_name": "logo.png",
      "mime_type": "image/png",
      "data": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
    }
  ]
}
```

**Opsi B: `multipart/form-data`**

| Field | Tipe | Wajib | Deskripsi |
| --- | --- | --- | --- |
| `image` / `images` / `file` / `files` | `file (binary)` | Tidak | File gambar referensi. Mendukung multi-file |

> Ukuran total multipart dibatasi 32 MB buffer. Ukuran per file dibatasi `UPLOAD_MAX_BYTES` (default 10 MB). Format selain JPEG, PNG, WebP ditolak dengan `VALIDATION_ERROR`.

#### Yang Dilakukan Server

1. Memvalidasi kepemilikan project dan state.
2. Menandai project `generating` secara atomik.
3. Menyimpan gambar referensi (jika ada) sebagai attachment project.
4. Membangun prompt: system prompt terkunci di server + brief + gambar.
5. Mengalirkan output Gemini sebagai event `token`.
6. Setelah selesai: membersihkan pagar kode, memvalidasi dokumen HTML, menyimpan sebagai **versi 1** (`kind = "generate"`), status → `ready`, lalu mengirim `done`.

#### Error Responses (sebelum streaming dimulai)

| Status | Code | Kapan |
| --- | --- | --- |
| 400 | `VALIDATION_ERROR` | `id` bukan UUID, gambar format tidak didukung, ukuran gambar melebihi limit, jumlah gambar melebihi `PROJECT_MAX_IMAGES`, atau parsing multipart gagal |
| 400 | `BAD_REQUEST` | Body bukan JSON valid, melebihi ukuran maksimum, atau mengandung field tidak dikenal |
| 401 | `UNAUTHORIZED` | Token tidak ada atau tidak valid |
| 404 | `NOT_FOUND` | Project tidak ditemukan atau bukan milik user |
| 409 | `CONFLICT` | Project sedang `generating`, **atau** project sudah punya versi (gunakan `revise`) |
| 429 | `RATE_LIMITED` | Limit Generate atau General API terlampaui |
| 502 | `AI_PROVIDER_ERROR` | Provider gagal memulai stream (sebelum HTTP 200 dikirim) |

**Contoh 409 (sudah punya versi):**
```json
{
  "success": false,
  "error": {
    "code": "CONFLICT",
    "message": "Project already has a version, use revise instead"
  }
}
```

**Contoh 409 (sedang diproses):**
```json
{
  "success": false,
  "error": {
    "code": "CONFLICT",
    "message": "A generation is already in progress for this project"
  }
}
```

---

### POST /api/v1/projects/{id}/revise (SSE)

Membuat **versi baru** berdasarkan HTML versi sebelumnya ditambah instruksi revisi. AI selalu mengembalikan **dokumen HTML lengkap** (bukan diff), yang disimpan sebagai versi baru. Versi lama tidak pernah diubah.

**Syarat state:** status project harus `ready` (sudah punya minimal satu versi).

#### Request

**Opsi A: `application/json`**

| Field | Tipe | Wajib | Constraint | Deskripsi |
| --- | --- | --- | --- | --- |
| `instruction` | `string` | Ya | 1–4000 karakter | Instruksi revisi, mis. "Tambahkan section testimoni dan ganti warna tombol jadi hijau" |
| `base_version` | `integer` | Tidak | ≥ 1, harus ada | Nomor versi yang dijadikan dasar revisi. Default: versi terbaru |
| `images` | `array<ImagePayload>` | Tidak | Maks `PROJECT_MAX_IMAGES` | Gambar tambahan untuk revisi ini |

```json
{
  "instruction": "Ganti warna hero menjadi biru tua dan tambahkan section testimoni",
  "base_version": 2
}
```

**Opsi B: `multipart/form-data`**

| Field | Tipe | Wajib | Deskripsi |
| --- | --- | --- | --- |
| `instruction` | `string` | Ya | Instruksi revisi |
| `base_version` | `string` | Tidak | Nomor versi dasar (string angka) |
| `image` / `images` / `file` / `files` | `file (binary)` | Tidak | Gambar tambahan |

> Hanya gambar yang dikirim **pada request revisi ini** yang diberikan ke model. Gambar dari request sebelumnya tidak dikirim ulang.

Event SSE identik dengan generate. Event `done` berisi `version_number` baru (versi terakhir + 1).

#### Error Responses (sebelum streaming dimulai)

| Status | Code | Kapan |
| --- | --- | --- |
| 400 | `VALIDATION_ERROR` | `instruction` kosong/terlalu panjang, `base_version` tidak valid, gambar tidak valid |
| 400 | `BAD_REQUEST` | Body bukan JSON valid, melebihi ukuran maksimum, atau mengandung field tidak dikenal |
| 401 | `UNAUTHORIZED` | Token tidak ada atau tidak valid |
| 404 | `NOT_FOUND` | Project atau `base_version` tidak ditemukan / bukan milik user |
| 409 | `CONFLICT` | Project sedang `generating`, **atau** belum punya versi (gunakan `generate`) |
| 429 | `RATE_LIMITED` | Limit Generate atau General API terlampaui |
| 502 | `AI_PROVIDER_ERROR` | Provider gagal memulai stream |

---

### Contoh Implementasi Frontend (fetch + ReadableStream)

```javascript
const token = localStorage.getItem('jwt_token');

async function streamGenerate(projectId, onToken) {
  const response = await fetch(`/api/v1/projects/${projectId}/generate`, {
    method: 'POST',
    headers: {
      'Authorization': `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({}),
  });

  // Error sebelum stream dimulai (401, 404, 409, 429, 502, dsb.)
  if (!response.ok) {
    const errData = await response.json();
    throw new Error(errData.error?.message || 'Generate request failed');
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;

    buffer += decoder.decode(value, { stream: true });
    const events = buffer.split('\n\n');
    buffer = events.pop(); // simpan fragmen yang belum lengkap

    for (const block of events) {
      let eventType = '';
      let eventData = '';
      for (const line of block.split('\n')) {
        if (line.startsWith('event: ')) eventType = line.slice(7).trim();
        if (line.startsWith('data: ')) eventData = line.slice(6).trim();
      }

      if (eventType === 'token') {
        onToken(JSON.parse(eventData).token);          // progres saja
      } else if (eventType === 'done') {
        const { version_number } = JSON.parse(eventData);
        return version_number;                          // lalu GET /versions/{n}
      } else if (eventType === 'error') {
        throw new Error(JSON.parse(eventData).error);
      }
    }
  }
}
```

---

## 5. Versions

### Schema `VersionResponse`

| Field | Tipe | Deskripsi |
| --- | --- | --- |
| `id` | `string` (UUID) | ID versi |
| `project_id` | `string` (UUID) | ID project |
| `version_number` | `integer` | Nomor urut versi per project, mulai dari 1 |
| `kind` | `string` | `"generate"` (versi 1) atau `"revise"` |
| `prompt` | `string` | Teks yang memicu versi ini: untuk `generate` berisi `brief.description`, untuk `revise` berisi `instruction` |
| `html` | `string` | Dokumen HTML lengkap. **Hanya ada di `GET /versions/{number}` dan `latest_version`; tidak ada di daftar versi** |
| `created_at` | `string` (RFC3339) | Waktu versi dibuat |

---

### GET /api/v1/projects/{id}/versions

Daftar riwayat versi project (tanpa `html`), diurutkan dari versi terbaru.

- **Auth:** Wajib
- **Rate Limit:** General API (120 req/menit)
- **Security:** Hanya project milik user yang sedang login

#### Response `200 OK`

```json
{
  "success": true,
  "message": "Request successful",
  "data": [
    {
      "id": "b3c1d2e4-1f2a-4b3c-9d4e-5f6a7b8c9d0e",
      "project_id": "5e2b7a10-9c34-4f0e-8d55-2a1c6b7e9f01",
      "version_number": 2,
      "kind": "revise",
      "prompt": "Ganti warna hero menjadi biru tua",
      "created_at": "2026-09-30T08:20:00Z"
    },
    {
      "id": "a1b2c3d4-0000-4b3c-9d4e-5f6a7b8c9d0e",
      "project_id": "5e2b7a10-9c34-4f0e-8d55-2a1c6b7e9f01",
      "version_number": 1,
      "kind": "generate",
      "prompt": "Landing page untuk kedai kopi lokal ...",
      "created_at": "2026-09-30T08:05:00Z"
    }
  ]
}
```

> Jika project belum punya versi, `data` adalah array kosong `[]`.

#### Error Responses

| Status | Code | Kapan |
| --- | --- | --- |
| 400 | `VALIDATION_ERROR` | `id` bukan UUID valid |
| 401 | `UNAUTHORIZED` | Token tidak ada atau tidak valid |
| 404 | `NOT_FOUND` | Project tidak ditemukan atau bukan milik user |
| 429 | `RATE_LIMITED` | Limit General API terlampaui |

---

### GET /api/v1/projects/{id}/versions/{number}

Mengambil satu versi lengkap dengan `html`.

- **Auth:** Wajib
- **Rate Limit:** General API (120 req/menit)

**Path params:** `{id}` — UUID project; `{number}` — nomor versi (integer ≥ 1)

#### Response `200 OK`

```json
{
  "success": true,
  "message": "Request successful",
  "data": {
    "id": "b3c1d2e4-1f2a-4b3c-9d4e-5f6a7b8c9d0e",
    "project_id": "5e2b7a10-9c34-4f0e-8d55-2a1c6b7e9f01",
    "version_number": 2,
    "kind": "revise",
    "prompt": "Ganti warna hero menjadi biru tua",
    "html": "<!DOCTYPE html><html lang=\"id\">...</html>",
    "created_at": "2026-09-30T08:20:00Z"
  }
}
```

#### Error Responses

| Status | Code | Kapan |
| --- | --- | --- |
| 400 | `VALIDATION_ERROR` | `id` bukan UUID valid atau `number` bukan integer ≥ 1 |
| 401 | `UNAUTHORIZED` | Token tidak ada atau tidak valid |
| 404 | `NOT_FOUND` | Project atau versi tidak ditemukan / bukan milik user |
| 429 | `RATE_LIMITED` | Limit General API terlampaui |

---

## 6. Export

### GET /api/v1/projects/{id}/export

Mengunduh website sebagai **file `.html`**. Response adalah **file langsung**, bukan JSON.

- **Auth:** Wajib
- **Rate Limit:** General API (120 req/menit)

**Query Parameters:**

| Parameter | Tipe | Default | Deskripsi |
| --- | --- | --- | --- |
| `version` | `integer` | Versi terbaru | Nomor versi yang diunduh |

#### Response `200 OK`

```http
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
Content-Disposition: attachment; filename="website-kopi-senja-v2.html"
X-Content-Type-Options: nosniff
```

Body adalah dokumen HTML lengkap. Karena endpoint membutuhkan header `Authorization`, unduh via `fetch` lalu buat Blob URL:

```javascript
const res = await fetch(`/api/v1/projects/${id}/export?version=2`, {
  headers: { Authorization: `Bearer ${token}` },
});
const blob = await res.blob();
const url = URL.createObjectURL(blob);
const a = document.createElement('a');
a.href = url;
a.download = 'website.html';
a.click();
URL.revokeObjectURL(url);
```

#### Error Responses

| Status | Code | Kapan |
| --- | --- | --- |
| 400 | `VALIDATION_ERROR` | `id` bukan UUID atau `version` bukan integer ≥ 1 |
| 401 | `UNAUTHORIZED` | Token tidak ada atau tidak valid |
| 404 | `NOT_FOUND` | Project tidak ditemukan / bukan milik user, atau project belum punya versi, atau versi tidak ada |
| 429 | `RATE_LIMITED` | Limit General API terlampaui |

---

## 7. Attachments (Gambar Referensi)

Gambar yang diunggah saat `generate`/`revise` disimpan sebagai attachment milik project. Attachment hanya untuk referensi bagi model dan tampilan di UI; **tidak** otomatis disisipkan ke file HTML hasil.

### Schema `ProjectAttachment`

| Field | Tipe | Deskripsi |
| --- | --- | --- |
| `id` | `string` (UUID) | ID attachment |
| `project_id` | `string` (UUID) | ID project |
| `file_name` | `string` | Nama file original |
| `file_type` | `string` | Tipe ternormalisasi (`"jpeg"`, `"png"`, `"webp"`) |
| `file_size` | `integer` | Ukuran file dalam bytes |
| `mime_type` | `string` | `"image/jpeg"`, `"image/png"`, atau `"image/webp"` |
| `created_at` | `string` (RFC3339) | Waktu attachment dibuat |

---

### GET /api/v1/projects/{id}/attachments

Daftar attachment sebuah project, dari yang terbaru.

- **Auth:** Wajib
- **Rate Limit:** General API (120 req/menit)

#### Response `200 OK`

```json
{
  "success": true,
  "message": "Request successful",
  "data": [
    {
      "id": "a1b2c3d4-e5f6-4a5b-8c9d-0e1f2a3b4c5d",
      "project_id": "5e2b7a10-9c34-4f0e-8d55-2a1c6b7e9f01",
      "file_name": "logo.png",
      "file_type": "png",
      "file_size": 154200,
      "mime_type": "image/png",
      "created_at": "2026-09-30T08:01:00Z"
    }
  ]
}
```

> Jika tidak ada attachment, `data` adalah array kosong `[]`.

#### Error Responses

| Status | Code | Kapan |
| --- | --- | --- |
| 400 | `VALIDATION_ERROR` | `id` bukan UUID valid |
| 401 | `UNAUTHORIZED` | Token tidak ada atau tidak valid |
| 404 | `NOT_FOUND` | Project tidak ditemukan atau bukan milik user |
| 429 | `RATE_LIMITED` | Limit General API terlampaui |

---

### GET /api/v1/projects/{id}/attachments/{attachmentId}

Mengunduh atau menampilkan biner gambar attachment. Response adalah **biner gambar langsung**, bukan JSON.

- **Auth:** Wajib
- **Rate Limit:** General API (120 req/menit)
- **Security:** Server memvalidasi bahwa attachment milik project yang dimiliki user yang sedang login

#### Response `200 OK` — Binary Image

```http
HTTP/1.1 200 OK
Content-Type: image/png
Content-Disposition: inline; filename="logo.png"
Content-Length: 154200
X-Content-Type-Options: nosniff
```

Karena membutuhkan header `Authorization`, ambil via `fetch` lalu gunakan Blob URL sebagai `src` pada `<img>`.

#### Error Responses

| Status | Code | Kapan |
| --- | --- | --- |
| 400 | `VALIDATION_ERROR` | `id` atau `attachmentId` bukan UUID valid |
| 401 | `UNAUTHORIZED` | Token tidak ada atau tidak valid |
| 404 | `NOT_FOUND` | Attachment tidak ditemukan atau bukan milik user |
| 429 | `RATE_LIMITED` | Limit General API terlampaui |

---

## 8. Menampilkan Preview di Frontend

HTML hasil AI adalah **konten tidak tepercaya** (*untrusted*). Aturan wajib untuk frontend:

1. Render di `<iframe>` dengan atribut `sandbox="allow-scripts allow-popups"`.
2. **Jangan pernah** menambahkan `allow-same-origin` pada sandbox tersebut. Tanpa itu, script di dalam website hasil tidak bisa mengakses `localStorage` (tempat JWT disimpan) maupun DOM aplikasi utama.
3. Jangan menyisipkan HTML hasil AI langsung ke DOM aplikasi (`innerHTML`, `dangerouslySetInnerHTML`, dsb.).

```jsx
// React
<iframe
  title="Preview"
  sandbox="allow-scripts allow-popups"
  srcDoc={version.html}
  style={{ width: '100%', height: '100%', border: 0 }}
/>
```

Untuk preview responsif, ubah lebar `<iframe>` (mis. 375px untuk mobile, 768px untuk tablet, 100% untuk desktop).
