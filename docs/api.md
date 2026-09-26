# API Documentation

> **Single Source of Truth** untuk semua request/response kontrak antara backend dan frontend.  
> Setiap field, tipe data, dan constraint di dokumen ini diambil langsung dari kode (struct DTO, handler, validasi, dan apperror package) — bukan dari asumsi atau dokumentasi lama.

---

## Table of Contents

- [Base URL](#base-url)
- [Autentikasi Umum](#autentikasi-umum)
- [Format Response Standar](#format-response-standar)
- [Error Codes](#error-codes)
- [Rate Limiting](#rate-limiting)
- [1. System & Health](#1-system--health)
  - [GET /health](#get-health)
  - [GET /health/db](#get-healthdb)
  - [GET /api/v1/ai/health](#get-apiv1aihealth)
- [2. Auth](#2-auth)
  - [POST /api/v1/auth/register](#post-apiv1authregister)
  - [POST /api/v1/auth/login](#post-apiv1authlogin)
  - [GET /api/v1/auth/me](#get-apiv1authme)
- [3. AI Chat](#3-ai-chat)
  - [POST /api/v1/ai/chat](#post-apiv1aichat)
  - [POST /api/v1/ai/chat/stream (SSE)](#post-apiv1aichatstream-sse)
- [4. Conversations](#4-conversations)
  - [GET /api/v1/ai/conversations](#get-apiv1aiconversations)
  - [GET /api/v1/ai/conversations/{id}](#get-apiv1aiconversationsid)
  - [DELETE /api/v1/ai/conversations/{id}](#delete-apiv1aiconversationsid)
- [5. Attachments](#5-attachments)
  - [GET /api/v1/ai/attachments/{id}](#get-apiv1aiattachmentsid)
- [6. RAG Documents](#6-rag-documents)
  - [POST /api/v1/ai/documents (Ingest Teks)](#post-apiv1aidocuments-ingest-teks)
  - [POST /api/v1/ai/documents/upload (Upload File)](#post-apiv1aidocumentsupload-upload-file)
  - [GET /api/v1/ai/documents](#get-apiv1aidocuments)
  - [GET /api/v1/ai/documents/{id}](#get-apiv1aidocumentsid)
  - [GET /api/v1/ai/documents/{id}/status](#get-apiv1aidocumentsidstatus)
  - [DELETE /api/v1/ai/documents/{id}](#delete-apiv1aidocumentsid)
- [7. Code Execution](#7-code-execution)
  - [POST /api/v1/execute](#post-apiv1execute)
  - [GET /api/v1/execute/history](#get-apiv1executehistory)
  - [GET /api/v1/execute/{id}](#get-apiv1executeid)

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

**Pengecualian:** Endpoint `POST /api/v1/ai/chat/stream` (SSE) juga menerima token via query parameter `?token=<jwt>` — hanya untuk endpoint ini saja karena keterbatasan browser `EventSource` API.

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
      "email": "must be a valid email address",
      "password": "must be at least 8 characters"
    }
  }
}
```

> [!NOTE]
> Response error **tidak pernah** mengekspos stack trace, query SQL, path file lokal, secret environment, API key, maupun detail internal lainnya. Detail teknis hanya dicatat di server log.
>
> Setiap response menyertakan header tracking `X-Request-ID`.

---

## Error Codes

Daftar lengkap semua error code yang digunakan di kode (dari `internal/apperror/apperror.go`):

| Code | HTTP Status | Kapan Muncul |
| --- | --- | --- |
| `VALIDATION_ERROR` | 400 | Field request tidak valid, wajib tapi kosong, format salah, atau melebihi batasan panjang. Response menyertakan `fields` map jika berasal dari struct validator. |
| `BAD_REQUEST` | 400 | Body request bukan JSON valid, payload melebihi ukuran maksimum, atau JSON mengandung field yang tidak dikenali. |
| `UNAUTHORIZED` | 401 | Token JWT hilang, tidak valid, kedaluwarsa, atau kredensial login (email/password) salah. |
| `FORBIDDEN` | 403 | Role user tidak mencukupi untuk mengakses resource (saat ini belum digunakan di route manapun selain middleware internal). |
| `NOT_FOUND` | 404 | Resource (conversation, dokumen, attachment, eksekusi) tidak ditemukan, atau bukan milik user yang sedang login. |
| `CONFLICT` | 409 | Duplikat resource unik — saat ini hanya untuk email yang sudah terdaftar saat register. |
| `RATE_LIMITED` | 429 | Terlalu banyak request dari IP yang sama dalam satu menit. Selalu disertai header `Retry-After: 60`. |
| `AI_PROVIDER_ERROR` | 502 | Upstream LLM provider (Gemini) error, tidak dapat dijangkau, atau timeout. Detail teknis tidak diekspos ke client. |
| `EXECUTION_PROVIDER_ERROR` | 502 | Upstream execution engine (Judge0) error, tidak dapat dijangkau, timeout, atau tidak mengembalikan token eksekusi. |
| `SERVICE_UNAVAILABLE` | 503 | Database tidak tersedia atau tidak dapat dijangkau. |
| `DATABASE_ERROR` | 500 | Operasi database gagal (query error, constraint violation, dsb.). |
| `INTERNAL_SERVER_ERROR` | 500 | Kesalahan internal server yang tidak terklasifikasi. |

---

## Rate Limiting

Backend menerapkan **3 tingkatan rate limiting** berbasis token bucket per alamat IP client:

| Tier | Endpoint yang Terkena | Default Limit | Env Var Konfigurasi |
| --- | --- | --- | --- |
| **General API** | Semua endpoint `/api/v1/*` | 120 req/menit | `RATE_LIMIT_PER_MINUTE`, `RATE_LIMIT_BURST` |
| **AI Chat** | `POST /api/v1/ai/chat`, `POST /api/v1/ai/chat/stream` | 10 req/menit | `AI_CHAT_RATE_LIMIT_PER_MINUTE` |
| **Execution Sandbox** | `POST /api/v1/execute`, `GET /api/v1/execute/history`, `GET /api/v1/execute/{id}` | 15 req/menit | `EXEC_RATE_LIMIT_PER_MINUTE`, `EXEC_RATE_LIMIT_BURST` |

> [!IMPORTANT]
> AI Chat limit dan Execution limit adalah **tambahan** di atas General API limit — request yang melampaui salah satunya akan mendapat 429.

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

#### Request

| Header | Nilai |
| --- | --- |
| `Authorization` | `Bearer <token>` |

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

## 3. AI Chat

### POST /api/v1/ai/chat

Interaksi utama AI Assistant. Mendukung percakapan multi-turn, integrasi RAG knowledge base, dan multimodal image attachments.

- **Auth:** Wajib (`Authorization: Bearer <token>`)
- **Rate Limit:** AI Chat (default 10 req/menit per IP) **+ General API (120 req/menit)**
- **Content-Type yang Didukung:**
  - `application/json` — untuk teks murni atau gambar dalam base64
  - `multipart/form-data` — untuk upload file gambar langsung

#### Opsi A: Request `application/json`

| Header | Nilai |
| --- | --- |
| `Content-Type` | `application/json` |
| `Authorization` | `Bearer <token>` |

**Body Schema:**

| Field | Tipe | Wajib | Constraint | Deskripsi |
| --- | --- | --- | --- | --- |
| `message` | `string` | Ya | Max 10.000 karakter | Teks prompt pengguna |
| `conversation_id` | `string` (UUID) | Tidak | — | ID thread yang ingin dilanjutkan. Jika kosong/tidak ada, percakapan baru dibuat otomatis |
| `system` | `string` | Tidak | — | Override system prompt default. Jika kosong, server menggunakan system prompt bawaan |
| `use_rag` | `boolean` | Tidak | Default `false` | Aktifkan RAG retrieval dari knowledge base dokumen user |
| `document_id` | `string` (UUID) | Tidak | — | Batasi RAG ke dokumen spesifik. Hanya dipakai jika `use_rag: true` |
| `images` | `array<ImagePayload>` | Tidak | — | Daftar gambar yang dilampirkan (multimodal) |

**Schema `ImagePayload` (untuk JSON):**

| Field | Tipe | Wajib | Constraint |
| --- | --- | --- | --- |
| `mime_type` | `string` | Ya | `"image/jpeg"`, `"image/jpg"`, `"image/png"`, atau `"image/webp"` |
| `data` | `string` (base64) | Ya | Data biner gambar dikodekan base64. Maks ukuran setelah decode sesuai `UPLOAD_MAX_BYTES` (default 10 MB) |
| `file_name` | `string` | Tidak | Nama file opsional (digunakan untuk fallback MIME detection jika `mime_type` kosong) |

```json
{
  "conversation_id": "7b8e1f54-3e9a-4c2f-b441-1188339900aa",
  "message": "Analisis diagram arsitektur ini dan berikan rekomendasi perbaikan",
  "system": "Anda adalah Lead Solutions Architect berpengalaman.",
  "use_rag": true,
  "document_id": "8f3a1b2c-473d-4c31-9014-cb9aa15df41e",
  "images": [
    {
      "file_name": "diagram.png",
      "mime_type": "image/png",
      "data": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
    }
  ]
}
```

#### Opsi B: Request `multipart/form-data`

Gunakan format ini saat pengguna memilih file gambar langsung dari input `<input type="file">` di browser.

| Header | Nilai |
| --- | --- |
| `Content-Type` | `multipart/form-data; boundary=...` (di-set browser otomatis) |
| `Authorization` | `Bearer <token>` |

**Form Fields:**

| Field | Tipe | Wajib | Deskripsi |
| --- | --- | --- | --- |
| `message` | `string` | Ya | Teks prompt pengguna |
| `conversation_id` | `string` | Tidak | UUID thread yang dilanjutkan |
| `system` | `string` | Tidak | Override system prompt |
| `use_rag` | `string` | Tidak | Nilai string `"true"` atau `"1"` untuk mengaktifkan RAG |
| `document_id` | `string` | Tidak | UUID dokumen untuk batasi RAG |
| `image` / `images` / `file` / `files` | `file (binary)` | Tidak | File gambar yang diupload. Server membaca dari semua field name ini. Mendukung multi-file |

> Ukuran total multipart dibatasi 32 MB buffer. Ukuran per file dibatasi oleh `UPLOAD_MAX_BYTES` (default 10 MB).

#### Response `200 OK`

```json
{
  "success": true,
  "message": "Request successful",
  "data": {
    "conversation_id": "7b8e1f54-3e9a-4c2f-b441-1188339900aa",
    "message": {
      "id": "9d1e2f3a-7a8b-4c5d-9e0f-1a2b3c4d5e6f",
      "role": "assistant",
      "content": "Berdasarkan diagram arsitektur yang dilampirkan, saya melihat beberapa area yang dapat ditingkatkan...",
      "attachments": [
        {
          "id": "a1b2c3d4-e5f6-4a5b-8c9d-0e1f2a3b4c5d",
          "message_id": "8c0d1e2f-3a4b-5c6d-7e8f-9a0b1c2d3e4f",
          "file_name": "diagram.png",
          "file_type": "png",
          "file_size": 154200,
          "mime_type": "image/png",
          "created_at": "2026-09-26T10:15:00Z"
        }
      ]
    },
    "sources": [
      {
        "id": "f1e2d3c4-b5a6-4789-8901-234567890abc",
        "document_id": "8f3a1b2c-473d-4c31-9014-cb9aa15df41e",
        "chunk_index": 2,
        "content": "Standar keamanan sistem mengharuskan enkripsi end-to-end pada semua layer komunikasi...",
        "similarity": 0.875
      }
    ]
  }
}
```

**Schema `data` (`ChatResponse`):**

| Field | Tipe | Deskripsi |
| --- | --- | --- |
| `conversation_id` | `string` (UUID) | ID thread percakapan (baru atau yang dilanjutkan) |
| `message.id` | `string` (UUID) | ID pesan asisten |
| `message.role` | `string` | Selalu `"assistant"` |
| `message.content` | `string` | Isi respons AI |
| `message.attachments` | `array<MessageAttachment>` | Lampiran gambar dari user message. **Ada hanya jika user melampirkan gambar**; field ini berisi attachment milik user message, bukan assistant |
| `sources` | `array<ChunkSearchResult>` | Chunk dokumen yang digunakan untuk RAG. **Ada hanya jika `use_rag: true`**; kosong jika RAG tidak menemukan hasil relevan |

**Schema `MessageAttachment`:**

| Field | Tipe | Deskripsi |
| --- | --- | --- |
| `id` | `string` (UUID) | ID attachment |
| `message_id` | `string` (UUID) | ID pesan yang memiliki attachment ini |
| `file_name` | `string` | Nama file original |
| `file_type` | `string` | Tipe file yang dinormalisasi (`"jpeg"`, `"png"`, `"webp"`) |
| `file_size` | `integer` | Ukuran file dalam bytes |
| `mime_type` | `string` | MIME type (`"image/jpeg"`, `"image/png"`, `"image/webp"`) |
| `created_at` | `string` (RFC3339) | Waktu attachment dibuat |

**Schema `ChunkSearchResult`:**

| Field | Tipe | Deskripsi |
| --- | --- | --- |
| `id` | `string` (UUID) | ID chunk |
| `document_id` | `string` (UUID) | ID dokumen induk |
| `chunk_index` | `integer` | Urutan chunk dalam dokumen (0-based) |
| `content` | `string` | Isi teks chunk |
| `similarity` | `float64` | Skor kemiripan cosine (0.0 – 1.0, semakin tinggi semakin relevan) |

#### Error Responses

| Status | Code | Kapan |
| --- | --- | --- |
| 400 | `VALIDATION_ERROR` | `message` kosong, gambar format tidak didukung, ukuran gambar melebihi limit, atau parsing multipart gagal |
| 400 | `BAD_REQUEST` | Body request melebihi ukuran maksimum atau bukan JSON valid |
| 401 | `UNAUTHORIZED` | Token tidak ada atau tidak valid |
| 404 | `NOT_FOUND` | `conversation_id` tidak ditemukan atau bukan milik user |
| 429 | `RATE_LIMITED` | Limit AI Chat atau General API terlampaui |
| 502 | `AI_PROVIDER_ERROR` | Gemini provider error atau tidak bisa dijangkau |

**Catatan format gambar yang didukung:** `image/jpeg`, `image/jpg`, `image/png`, `image/webp`. Format lain (GIF, BMP, TIFF, dsb.) akan ditolak dengan `VALIDATION_ERROR`: `"Unsupported image type. Only JPEG, PNG, and WebP are supported"`.

---

### POST /api/v1/ai/chat/stream (SSE)

Mengalirkan respons AI secara real-time token-by-token menggunakan **Server-Sent Events (SSE)**. Fungsional identik dengan `/ai/chat` tetapi mengembalikan streaming response alih-alih satu response JSON.

- **Auth:** Wajib. Menerima dua cara:
  1. `Authorization: Bearer <token>` (header — direkomendasikan)
  2. `?token=<jwt>` (query param — hanya untuk endpoint ini, untuk kompatibilitas `EventSource` API browser)
- **Rate Limit:** AI Chat (default 10 req/menit per IP) **+ General API (120 req/menit)**
- **Format Request:** Identik dengan `POST /api/v1/ai/chat` — mendukung `application/json` dan `multipart/form-data`

> [!WARNING]
> Endpoint ini adalah **satu-satunya** endpoint yang menerima JWT via query param `?token=`. Semua endpoint lain **harus** menggunakan header `Authorization: Bearer`.

#### Request

| Header | Nilai |
| --- | --- |
| `Content-Type` | `application/json` atau `multipart/form-data` |
| `Authorization` | `Bearer <token>` (atau gunakan `?token=<jwt>` di URL) |

```json
{
  "conversation_id": "7b8e1f54-3e9a-4c2f-b441-1188339900aa",
  "message": "Buatkan fungsi generator prima di Go",
  "use_rag": false
}
```

#### Response Headers (HTTP 200 OK — SSE dimulai)

```http
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive
X-Accel-Buffering: no
```

> [!NOTE]
> Server **segera** mengirim HTTP 200 dan membuka koneksi SSE sebelum token pertama diterima dari AI provider. Jika ada error **sebelum** streaming dimulai (mis. token tidak valid, conversation not found), server masih mengembalikan JSON error biasa dengan HTTP 4xx/5xx.

#### Format Event SSE

Server mengirim tiga jenis event dalam urutan:

**1. `event: token`** — Dikirim berulang kali selama teks dihasilkan

```
event: token
data: {"token":"Berikut"}

event: token
data: {"token":" adalah"}

event: token
data: {"token":" fungsi"}
```

**Data schema:**

| Field | Tipe | Deskripsi |
| --- | --- | --- |
| `token` | `string` | Potongan teks (bisa satu kata, satu karakter, atau beberapa kata) |

**2. `event: done`** — Dikirim **setelah** seluruh teks selesai dihasilkan dan respons sudah tersimpan ke database

```
event: done
data: {"conversation_id":"7b8e1f54-3e9a-4c2f-b441-1188339900aa","message_id":"9d1e2f3a-7a8b-4c5d-9e0f-1a2b3c4d5e6f"}
```

**Data schema:**

| Field | Tipe | Deskripsi |
| --- | --- | --- |
| `conversation_id` | `string` (UUID) | ID thread percakapan (baru atau yang dilanjutkan) |
| `message_id` | `string` (UUID) | ID pesan asisten yang sudah tersimpan di database |

> [!IMPORTANT]
> Event `done` hanya dikirim setelah pesan asisten berhasil disimpan ke database. Gunakan `message_id` ini untuk mengambil detail pesan via `GET /api/v1/ai/conversations/{id}` jika diperlukan.

**3. `event: error`** — Dikirim jika AI provider mengembalikan error **di tengah streaming** (setelah HTTP 200 sudah terkirim)

```
event: error
data: {"error":"The AI provider returned an error"}
```

**Data schema:**

| Field | Tipe | Deskripsi |
| --- | --- | --- |
| `error` | `string` | Pesan error generic (detail teknis disembunyikan) |

#### Contoh Implementasi Frontend (fetch + ReadableStream)

```javascript
const token = localStorage.getItem('jwt_token');

const response = await fetch('/api/v1/ai/chat/stream', {
  method: 'POST',
  headers: {
    'Authorization': `Bearer ${token}`,
    'Content-Type': 'application/json',
  },
  body: JSON.stringify({
    message: 'Buatkan fungsi generator prima di Go',
    use_rag: false
  }),
});

// Tangani error sebelum stream dimulai (401, 404, 429, 502, dsb.)
if (!response.ok) {
  const errData = await response.json();
  throw new Error(errData.error?.message || 'Streaming request failed');
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

  for (const eventBlock of events) {
    const lines = eventBlock.split('\n');
    let eventType = '';
    let eventData = '';

    for (const line of lines) {
      if (line.startsWith('event: ')) eventType = line.slice(7).trim();
      if (line.startsWith('data: ')) eventData = line.slice(6).trim();
    }

    if (eventType === 'token') {
      const { token } = JSON.parse(eventData);
      appendToUI(token); // tampilkan token ke UI secara inkremental
    } else if (eventType === 'done') {
      const { conversation_id, message_id } = JSON.parse(eventData);
      console.log(`Selesai. Conv: ${conversation_id}, Msg: ${message_id}`);
    } else if (eventType === 'error') {
      const { error } = JSON.parse(eventData);
      showError(error);
    }
  }
}
```

#### Error Responses (sebelum streaming dimulai)

| Status | Code | Kapan |
| --- | --- | --- |
| 400 | `VALIDATION_ERROR` | `message` kosong atau gambar tidak valid |
| 401 | `UNAUTHORIZED` | Token tidak ada atau tidak valid |
| 404 | `NOT_FOUND` | `conversation_id` tidak ditemukan atau bukan milik user |
| 429 | `RATE_LIMITED` | Limit AI Chat atau General API terlampaui |
| 502 | `AI_PROVIDER_ERROR` | Provider gagal memulai stream (sebelum HTTP 200 dikirim) |

---

## 4. Conversations

### GET /api/v1/ai/conversations

Mengambil daftar seluruh thread percakapan milik user yang sedang login, diurutkan berdasarkan aktivitas terbaru (`updated_at DESC`).

- **Auth:** Wajib (`Authorization: Bearer <token>`)
- **Rate Limit:** General API (120 req/menit)

#### Request

| Header | Nilai |
| --- | --- |
| `Authorization` | `Bearer <token>` |

#### Response `200 OK`

```json
{
  "success": true,
  "message": "Request successful",
  "data": [
    {
      "id": "7b8e1f54-3e9a-4c2f-b441-1188339900aa",
      "user_id": "c1f76d49-4eb5-4a5f-9e77-5f725cf9a9f2",
      "title": "Analisis arsitektur berikut dan berikan",
      "created_at": "2026-09-26T10:15:00Z",
      "updated_at": "2026-09-26T10:15:30Z"
    },
    {
      "id": "3a4b5c6d-7e8f-4a1b-9c2d-3e4f5a6b7c8d",
      "user_id": "c1f76d49-4eb5-4a5f-9e77-5f725cf9a9f2",
      "title": "Buatkan fungsi generator prima",
      "created_at": "2026-09-26T09:00:00Z",
      "updated_at": "2026-09-26T09:05:00Z"
    }
  ]
}
```

**Schema item array (`Conversation`):**

| Field | Tipe | Deskripsi |
| --- | --- | --- |
| `id` | `string` (UUID) | ID thread percakapan |
| `user_id` | `string` (UUID) | ID pemilik percakapan |
| `title` | `string` | Judul otomatis berdasarkan pesan pertama (maks 40 karakter) |
| `created_at` | `string` (RFC3339) | Waktu thread dibuat |
| `updated_at` | `string` (RFC3339) | Waktu terakhir ada pesan di thread ini |

> Jika user belum punya percakapan, `data` adalah array kosong `[]`.

#### Error Responses

| Status | Code | Kapan |
| --- | --- | --- |
| 401 | `UNAUTHORIZED` | Token tidak ada atau tidak valid |

---

### GET /api/v1/ai/conversations/{id}

Mengambil riwayat lengkap sebuah percakapan beserta seluruh pesan dan lampiran.

- **Auth:** Wajib (`Authorization: Bearer <token>`)
- **Rate Limit:** General API (120 req/menit)
- **Security:** Hanya mengembalikan percakapan milik user yang sedang login (`WHERE id = $1 AND user_id = $2`)

#### Request

| Header | Nilai |
| --- | --- |
| `Authorization` | `Bearer <token>` |

**Path param:** `{id}` — UUID percakapan

#### Response `200 OK`

```json
{
  "success": true,
  "message": "Request successful",
  "data": {
    "id": "7b8e1f54-3e9a-4c2f-b441-1188339900aa",
    "user_id": "c1f76d49-4eb5-4a5f-9e77-5f725cf9a9f2",
    "title": "Analisis arsitektur berikut dan berikan",
    "created_at": "2026-09-26T10:15:00Z",
    "updated_at": "2026-09-26T10:15:30Z",
    "messages": [
      {
        "id": "8c0d1e2f-3a4b-5c6d-7e8f-9a0b1c2d3e4f",
        "conversation_id": "7b8e1f54-3e9a-4c2f-b441-1188339900aa",
        "role": "user",
        "content": "Analisis arsitektur berikut dan berikan rekomendasi",
        "attachments": [
          {
            "id": "a1b2c3d4-e5f6-4a5b-8c9d-0e1f2a3b4c5d",
            "message_id": "8c0d1e2f-3a4b-5c6d-7e8f-9a0b1c2d3e4f",
            "file_name": "diagram.png",
            "file_type": "png",
            "file_size": 154200,
            "mime_type": "image/png",
            "created_at": "2026-09-26T10:15:00Z"
          }
        ],
        "created_at": "2026-09-26T10:15:00Z"
      },
      {
        "id": "9d1e2f3a-7a8b-4c5d-9e0f-1a2b3c4d5e6f",
        "conversation_id": "7b8e1f54-3e9a-4c2f-b441-1188339900aa",
        "role": "assistant",
        "content": "Berdasarkan diagram yang dilampirkan, berikut rekomendasi saya...",
        "attachments": null,
        "created_at": "2026-09-26T10:15:05Z"
      }
    ]
  }
}
```

**Schema `data` (`ConversationDetail`):**

Mewarisi semua field `Conversation` (`id`, `user_id`, `title`, `created_at`, `updated_at`) ditambah:

| Field | Tipe | Deskripsi |
| --- | --- | --- |
| `messages` | `array<Message>` | Semua pesan dalam percakapan, urutan kronologis |

**Schema `Message`:**

| Field | Tipe | Deskripsi |
| --- | --- | --- |
| `id` | `string` (UUID) | ID pesan |
| `conversation_id` | `string` (UUID) | ID percakapan induk |
| `role` | `string` | `"user"` atau `"assistant"` |
| `content` | `string` | Isi teks pesan |
| `attachments` | `array<MessageAttachment>` \| `null` | Lampiran gambar. `null` atau tidak ada jika pesan tidak punya lampiran |
| `created_at` | `string` (RFC3339) | Waktu pesan dibuat |

#### Error Responses

| Status | Code | Kapan |
| --- | --- | --- |
| 400 | `VALIDATION_ERROR` | `id` di path kosong |
| 401 | `UNAUTHORIZED` | Token tidak ada atau tidak valid |
| 404 | `NOT_FOUND` | Percakapan tidak ditemukan atau bukan milik user |

---

### DELETE /api/v1/ai/conversations/{id}

Menghapus thread percakapan beserta seluruh pesan dan lampiran terkait.

- **Auth:** Wajib (`Authorization: Bearer <token>`)
- **Rate Limit:** General API (120 req/menit)
- **Security:** Hanya bisa menghapus percakapan milik user sendiri

#### Request

| Header | Nilai |
| --- | --- |
| `Authorization` | `Bearer <token>` |

**Path param:** `{id}` — UUID percakapan

#### Response `200 OK`

```json
{
  "success": true,
  "message": "Conversation deleted successfully",
  "data": null
}
```

#### Error Responses

| Status | Code | Kapan |
| --- | --- | --- |
| 400 | `VALIDATION_ERROR` | `id` di path kosong |
| 401 | `UNAUTHORIZED` | Token tidak ada atau tidak valid |
| 404 | `NOT_FOUND` | Percakapan tidak ditemukan atau bukan milik user |

---

## 5. Attachments

### GET /api/v1/ai/attachments/{id}

*(Alias: `GET /api/v1/attachments/{id}`)*

Mengunduh atau menampilkan biner gambar yang pernah dilampirkan pada pesan. Response adalah **biner gambar langsung**, bukan JSON.

- **Auth:** Wajib (`Authorization: Bearer <token>`)
- **Rate Limit:** General API (120 req/menit)
- **Security:** Server memvalidasi bahwa attachment tersebut adalah milik conversation user yang sedang login

#### Request

| Header | Nilai |
| --- | --- |
| `Authorization` | `Bearer <token>` |

**Path param:** `{id}` — UUID attachment (dari field `attachments[].id` di response chat/conversation)

#### Response `200 OK` — Binary Image

```http
HTTP/1.1 200 OK
Content-Type: image/png
Content-Disposition: inline; filename="diagram.png"
Content-Length: 154200
```

Response body adalah stream biner gambar. Gunakan langsung sebagai `src` pada `<img>` tag dengan Blob URL, atau tampilkan via `<img src="/api/v1/ai/attachments/{id}">` dengan header autentikasi (via interceptor fetch/axios).

#### Error Responses

| Status | Code | Kapan |
| --- | --- | --- |
| 400 | `VALIDATION_ERROR` | `id` di path kosong |
| 401 | `UNAUTHORIZED` | Token tidak ada atau tidak valid |
| 404 | `NOT_FOUND` | Attachment tidak ditemukan atau bukan milik user |

---

## 6. RAG Documents

### POST /api/v1/ai/documents (Ingest Teks)

Ingest dokumen teks langsung untuk knowledge base RAG. Server melakukan **chunking** (ukuran 800 karakter, overlap 150 karakter) dan **vector embedding** menggunakan model `text-embedding-004` secara **sinkron** — response dikembalikan setelah seluruh proses selesai, status langsung `"ready"`.

- **Auth:** Wajib (`Authorization: Bearer <token>`)
- **Rate Limit:** General API (120 req/menit)

#### Request

| Header | Nilai |
| --- | --- |
| `Content-Type` | `application/json` |
| `Authorization` | `Bearer <token>` |

**Body Schema (`IngestDocumentRequest`):**

| Field | Tipe | Wajib | Constraint | Deskripsi |
| --- | --- | --- | --- | --- |
| `title` | `string` | Ya | Min 1, maks 255 karakter | Judul dokumen |
| `content` | `string` | Ya | Min 5, maks 500.000 karakter | Isi teks lengkap dokumen |
| `source` | `string` | Tidak | — | Label sumber dokumen (mis. `"manual"`, `"website"`, `"pdf"`). Default: `"text"` |

```json
{
  "title": "Standard Operating Procedure v2.0",
  "content": "BAB 1: PENDAHULUAN\n\nDokumen ini menjelaskan prosedur operasional standar untuk seluruh karyawan...\n\nBAB 2: KEAMANAN DATA\n\nSeluruh data pelanggan harus dienkripsi menggunakan AES-256...",
  "source": "manual"
}
```

#### Response `201 Created`

```json
{
  "success": true,
  "message": "Document ingested successfully",
  "data": {
    "document": {
      "id": "8f3a1b2c-473d-4c31-9014-cb9aa15df41e",
      "user_id": "c1f76d49-4eb5-4a5f-9e77-5f725cf9a9f2",
      "title": "Standard Operating Procedure v2.0",
      "source": "manual",
      "status": "ready",
      "created_at": "2026-09-26T08:00:00Z"
    },
    "chunk_count": 6
  }
}
```

**Schema `data` (`IngestDocumentResponse`):**

| Field | Tipe | Deskripsi |
| --- | --- | --- |
| `document` | `Document` | Metadata dokumen yang baru dibuat (lihat schema di bawah) |
| `chunk_count` | `integer` | Jumlah chunk yang berhasil di-embed dan disimpan |

**Schema `Document`:**

| Field | Tipe | Deskripsi |
| --- | --- | --- |
| `id` | `string` (UUID) | ID dokumen |
| `user_id` | `string` (UUID) | ID pemilik dokumen |
| `title` | `string` | Judul dokumen |
| `source` | `string` | Label sumber (`"text"`, `"manual"`, `"upload"`, dsb.) |
| `file_name` | `string` | Nama file (hanya ada untuk dokumen upload; omitted untuk ingest teks) |
| `file_type` | `string` | Tipe file (hanya ada untuk dokumen upload; omitted untuk ingest teks) |
| `file_size` | `integer` | Ukuran file bytes (hanya ada untuk dokumen upload; omitted/0 untuk ingest teks) |
| `status` | `string` | `"processing"`, `"ready"`, atau `"failed"` |
| `error_message` | `string` | Pesan error jika status `"failed"` (omitted jika tidak ada error) |
| `created_at` | `string` (RFC3339) | Waktu dokumen dibuat |

#### Error Responses

| Status | Code | Kapan |
| --- | --- | --- |
| 400 | `VALIDATION_ERROR` | `title` kosong/terlalu panjang, `content` terlalu pendek/panjang, atau field wajib tidak ada |
| 400 | `BAD_REQUEST` | Body bukan JSON valid |
| 401 | `UNAUTHORIZED` | Token tidak ada atau tidak valid |
| 502 | `AI_PROVIDER_ERROR` | Gagal generate embedding (Gemini tidak bisa dijangkau) |

---

### POST /api/v1/ai/documents/upload (Upload File)

*(Alias: `POST /api/v1/documents/upload`)*

Mengunggah file dokumen fisik (PDF, DOCX, atau TXT). Server menyimpan file, membuat record dengan status `"processing"`, dan menjalankan ekstraksi teks + embedding secara **asinkron di background**. Response dikembalikan segera setelah file tersimpan.

- **Auth:** Wajib (`Authorization: Bearer <token>`)
- **Rate Limit:** General API (120 req/menit)
- **Format file yang didukung:** `.pdf`, `.docx`, `.txt`
- **Ukuran file maks:** Default 10 MB (dikonfigurasi via `UPLOAD_MAX_BYTES`)
- **Background processing timeout:** 5 menit

#### Request

| Header | Nilai |
| --- | --- |
| `Content-Type` | `multipart/form-data` |
| `Authorization` | `Bearer <token>` |

**Form Fields:**

| Field | Tipe | Wajib | Deskripsi |
| --- | --- | --- | --- |
| `file` | `file (binary)` | Ya | File dokumen. Wajib menggunakan field name `file` |
| `title` | `string` | Tidak | Judul dokumen. Jika tidak diisi, menggunakan nama file asli |

#### Response `202 Accepted`

```json
{
  "success": true,
  "message": "Document uploaded and queued for processing",
  "data": {
    "id": "e98e4d94-31ea-42f7-a36c-941da7ce3920",
    "user_id": "c1f76d49-4eb5-4a5f-9e77-5f725cf9a9f2",
    "title": "Buku Pedoman Karyawan 2026",
    "source": "upload",
    "file_name": "buku_pedoman.pdf",
    "file_type": "pdf",
    "file_size": 524288,
    "status": "processing",
    "created_at": "2026-09-26T08:30:00Z"
  }
}
```

> `status` selalu `"processing"` di response ini. Gunakan `GET /api/v1/ai/documents/{id}/status` untuk polling sampai status menjadi `"ready"` atau `"failed"`.

#### Error Responses

| Status | Code | Kapan |
| --- | --- | --- |
| 400 | `VALIDATION_ERROR` | Field `file` tidak ada, nama file kosong, format file tidak didukung, atau ukuran file melebihi limit |
| 400 | `BAD_REQUEST` | Multipart form tidak valid |
| 401 | `UNAUTHORIZED` | Token tidak ada atau tidak valid |

---

### GET /api/v1/ai/documents

Mengambil daftar seluruh dokumen knowledge base milik user yang sedang login beserta metadata dan jumlah chunk.

- **Auth:** Wajib (`Authorization: Bearer <token>`)
- **Rate Limit:** General API (120 req/menit)

#### Request

| Header | Nilai |
| --- | --- |
| `Authorization` | `Bearer <token>` |

#### Response `200 OK`

```json
{
  "success": true,
  "message": "Request successful",
  "data": [
    {
      "id": "e98e4d94-31ea-42f7-a36c-941da7ce3920",
      "user_id": "c1f76d49-4eb5-4a5f-9e77-5f725cf9a9f2",
      "title": "Buku Pedoman Karyawan 2026",
      "source": "upload",
      "file_name": "buku_pedoman.pdf",
      "file_type": "pdf",
      "file_size": 524288,
      "status": "ready",
      "created_at": "2026-09-26T08:30:00Z",
      "chunk_count": 18
    },
    {
      "id": "8f3a1b2c-473d-4c31-9014-cb9aa15df41e",
      "user_id": "c1f76d49-4eb5-4a5f-9e77-5f725cf9a9f2",
      "title": "Standard Operating Procedure v2.0",
      "source": "manual",
      "status": "ready",
      "created_at": "2026-09-26T08:00:00Z",
      "chunk_count": 6
    }
  ]
}
```

**Schema item array (`DocumentDetail`):**

Mewarisi semua field `Document` ditambah:

| Field | Tipe | Deskripsi |
| --- | --- | --- |
| `chunk_count` | `integer` | Jumlah chunk yang sudah terindeks. `0` jika masih `"processing"` atau `"failed"` |

#### Error Responses

| Status | Code | Kapan |
| --- | --- | --- |
| 401 | `UNAUTHORIZED` | Token tidak ada atau tidak valid |

---

### GET /api/v1/ai/documents/{id}

Mengambil detail metadata satu dokumen knowledge base.

- **Auth:** Wajib (`Authorization: Bearer <token>`)
- **Rate Limit:** General API (120 req/menit)
- **Security:** Hanya mengembalikan dokumen milik user yang sedang login

#### Request

| Header | Nilai |
| --- | --- |
| `Authorization` | `Bearer <token>` |

**Path param:** `{id}` — UUID dokumen

#### Response `200 OK`

```json
{
  "success": true,
  "message": "Request successful",
  "data": {
    "id": "e98e4d94-31ea-42f7-a36c-941da7ce3920",
    "user_id": "c1f76d49-4eb5-4a5f-9e77-5f725cf9a9f2",
    "title": "Buku Pedoman Karyawan 2026",
    "source": "upload",
    "file_name": "buku_pedoman.pdf",
    "file_type": "pdf",
    "file_size": 524288,
    "status": "ready",
    "created_at": "2026-09-26T08:30:00Z"
  }
}
```

> Perhatian: endpoint ini mengembalikan `Document` (tanpa `chunk_count`). Untuk mendapatkan `chunk_count`, gunakan `GET /api/v1/ai/documents` (list) atau `GET /api/v1/ai/documents/{id}/status`.

#### Error Responses

| Status | Code | Kapan |
| --- | --- | --- |
| 400 | `VALIDATION_ERROR` | `id` di path kosong |
| 401 | `UNAUTHORIZED` | Token tidak ada atau tidak valid |
| 404 | `NOT_FOUND` | Dokumen tidak ditemukan atau bukan milik user |

---

### GET /api/v1/ai/documents/{id}/status

*(Alias: `GET /api/v1/documents/{id}/status`)*

Memeriksa status background processing dokumen yang diunggah. Gunakan endpoint ini untuk **polling** setelah `POST /api/v1/ai/documents/upload`.

- **Auth:** Wajib (`Authorization: Bearer <token>`)
- **Rate Limit:** General API (120 req/menit)

#### Request

| Header | Nilai |
| --- | --- |
| `Authorization` | `Bearer <token>` |

**Path param:** `{id}` — UUID dokumen

#### Response `200 OK`

Response selalu `200` terlepas dari status dokumen. Status aktual ada di field `status`:

**Status `"processing"` (masih diproses):**
```json
{
  "success": true,
  "message": "Request successful",
  "data": {
    "id": "e98e4d94-31ea-42f7-a36c-941da7ce3920",
    "title": "Buku Pedoman Karyawan 2026",
    "status": "processing",
    "chunk_count": 0
  }
}
```

**Status `"ready"` (selesai, siap digunakan untuk RAG):**
```json
{
  "success": true,
  "message": "Request successful",
  "data": {
    "id": "e98e4d94-31ea-42f7-a36c-941da7ce3920",
    "title": "Buku Pedoman Karyawan 2026",
    "status": "ready",
    "chunk_count": 18
  }
}
```

**Status `"failed"` (gagal ekstraksi atau embedding):**
```json
{
  "success": true,
  "message": "Request successful",
  "data": {
    "id": "e98e4d94-31ea-42f7-a36c-941da7ce3920",
    "title": "Buku Pedoman Karyawan 2026",
    "status": "failed",
    "chunk_count": 0,
    "error_message": "Document contains no extractable text"
  }
}
```

**Schema `data` (`DocumentStatusResponse`):**

| Field | Tipe | Deskripsi |
| --- | --- | --- |
| `id` | `string` (UUID) | ID dokumen |
| `title` | `string` | Judul dokumen |
| `status` | `string` | `"processing"`, `"ready"`, atau `"failed"` |
| `chunk_count` | `integer` | Jumlah chunk terindeks. `0` selama `"processing"` atau jika `"failed"` |
| `error_message` | `string` | Pesan error yang bisa ditampilkan ke user jika `status: "failed"` (field ini tidak ada jika status bukan failed) |

#### Polling Pattern yang Direkomendasikan

```javascript
async function pollDocumentStatus(docId, token, intervalMs = 2000, maxWaitMs = 300000) {
  const start = Date.now();
  while (Date.now() - start < maxWaitMs) {
    const res = await fetch(`/api/v1/ai/documents/${docId}/status`, {
      headers: { 'Authorization': `Bearer ${token}` }
    });
    const { data } = await res.json();

    if (data.status === 'ready') return data;
    if (data.status === 'failed') throw new Error(data.error_message || 'Processing failed');

    await new Promise(r => setTimeout(r, intervalMs));
  }
  throw new Error('Polling timeout exceeded');
}
```

#### Error Responses

| Status | Code | Kapan |
| --- | --- | --- |
| 400 | `VALIDATION_ERROR` | `id` di path kosong |
| 401 | `UNAUTHORIZED` | Token tidak ada atau tidak valid |
| 404 | `NOT_FOUND` | Dokumen tidak ditemukan atau bukan milik user |

---

### DELETE /api/v1/ai/documents/{id}

Menghapus dokumen knowledge base beserta seluruh chunk vektor terkait dari database, dan file fisik dari storage jika dokumen berasal dari upload.

- **Auth:** Wajib (`Authorization: Bearer <token>`)
- **Rate Limit:** General API (120 req/menit)
- **Security:** Hanya bisa menghapus dokumen milik user sendiri

#### Request

| Header | Nilai |
| --- | --- |
| `Authorization` | `Bearer <token>` |

**Path param:** `{id}` — UUID dokumen

#### Response `200 OK`

```json
{
  "success": true,
  "message": "Document deleted successfully",
  "data": null
}
```

#### Error Responses

| Status | Code | Kapan |
| --- | --- | --- |
| 400 | `VALIDATION_ERROR` | `id` di path kosong |
| 401 | `UNAUTHORIZED` | Token tidak ada atau tidak valid |
| 404 | `NOT_FOUND` | Dokumen tidak ditemukan atau bukan milik user |

---

## 7. Code Execution

Backend menyediakan eksekusi kode terisolasi via **Judge0 CE**. Kode dijalankan dalam container dengan pembatasan ketat pada CPU time, wall time, dan memory.

**Bahasa yang Didukung (Whitelist):**

| Bahasa | Alias yang Diterima | Judge0 Language ID |
| --- | --- | --- |
| `python` | `py`, `python3` | 71 (Python 3.8.1) |
| `javascript` | `js`, `node`, `nodejs` | 63 (Node.js 12.14.0) |
| `go` | `golang` | 60 (Go 1.13.5) |

**Default Resource Limits (dikonfigurasi via env):**

| Limit | Default | Env Var |
| --- | --- | --- |
| CPU time | 5 detik | `EXEC_CPU_TIME_LIMIT_SEC` |
| Wall time | 10 detik | `EXEC_WALL_TIME_LIMIT_SEC` |
| Memory | 128 MB | `EXEC_MEMORY_LIMIT_KB` |
| Source code size | 64 KB | `EXEC_MAX_CODE_BYTES` |

---

### POST /api/v1/execute

Mengeksekusi kode program di sandbox Judge0.

- **Auth:** Wajib (`Authorization: Bearer <token>`)
- **Rate Limit:** Execution Sandbox (default 15 req/menit) **+ General API (120 req/menit)**

#### Request

| Header | Nilai |
| --- | --- |
| `Content-Type` | `application/json` |
| `Authorization` | `Bearer <token>` |

**Body Schema (`ExecuteRequest`):**

| Field | Tipe | Wajib | Constraint | Deskripsi |
| --- | --- | --- | --- | --- |
| `language` | `string` | Ya | Harus salah satu dari whitelist di atas | Bahasa pemrograman |
| `code` | `string` | Ya | Maks 64 KB (default) | Source code yang akan dieksekusi |
| `stdin` | `string` | Tidak | — | Input standar yang dikirim ke program saat runtime |

```json
{
  "language": "python",
  "code": "import sys\nname = sys.stdin.read().strip()\nprint(f'Hello, {name}!')",
  "stdin": "Antigravity"
}
```

#### Response `200 OK`

> [!IMPORTANT]
> Response selalu `200 OK` terlepas dari apakah program berhasil berjalan atau tidak. Hasil eksekusi (sukses, error, timeout, dsb.) ada di dalam field `data` — periksa `exit_code`, `status`, dan `stderr`.

**Eksekusi berhasil (Accepted):**
```json
{
  "success": true,
  "message": "Code executed successfully",
  "data": {
    "id": "c7a8b123-5e6f-4a1b-9c2d-3e4f5a6b7c8d",
    "language": "python",
    "stdout": "Hello, Antigravity!\n",
    "stderr": "",
    "exit_code": 0,
    "execution_time_ms": 28,
    "memory_kb": 3120,
    "status": "Accepted"
  }
}
```

**Time Limit Exceeded (infinite loop, dsb.):**
```json
{
  "success": true,
  "message": "Code executed successfully",
  "data": {
    "id": "f8b9c234-1a2b-4c3d-8e4f-5a6b7c8d9e0f",
    "language": "python",
    "stdout": "",
    "stderr": "Time Limit Exceeded",
    "exit_code": 124,
    "execution_time_ms": 5002,
    "memory_kb": 12450,
    "status": "Time Limit Exceeded"
  }
}
```

**Compilation/Syntax Error:**
```json
{
  "success": true,
  "message": "Code executed successfully",
  "data": {
    "id": "d1e2f345-2b3c-4d5e-9f0a-1b2c3d4e5f6a",
    "language": "go",
    "stdout": "",
    "stderr": "main.go:3:1: syntax error: non-declaration statement outside function body",
    "exit_code": 1,
    "execution_time_ms": 0,
    "memory_kb": 0,
    "status": "Compilation Error"
  }
}
```

**Schema `data` (`ExecuteResponse`):**

| Field | Tipe | Deskripsi |
| --- | --- | --- |
| `id` | `string` (UUID) | ID record eksekusi yang tersimpan di database |
| `language` | `string` | Bahasa yang dieksekusi (canonical: `"python"`, `"javascript"`, `"go"`) |
| `stdout` | `string` | Output standar dari program |
| `stderr` | `string` | Error output + compile output (jika ada) digabung |
| `exit_code` | `integer` | Exit code program. `0` = sukses; `124` = TLE; `1` = error lain |
| `execution_time_ms` | `integer` | Waktu eksekusi dalam milidetik |
| `memory_kb` | `integer` | Memori yang digunakan dalam kilobytes |
| `status` | `string` | Deskripsi status dari Judge0 (mis. `"Accepted"`, `"Time Limit Exceeded"`, `"Runtime Error"`, `"Compilation Error"`) |

#### Error Responses

| Status | Code | Kapan |
| --- | --- | --- |
| 400 | `VALIDATION_ERROR` | `code` kosong, ukuran code melebihi limit, atau bahasa tidak dikenali |
| 400 | `BAD_REQUEST` | Body bukan JSON valid atau payload melebihi ukuran maksimum |
| 401 | `UNAUTHORIZED` | Token tidak ada atau tidak valid |
| 429 | `RATE_LIMITED` | Limit Execution Sandbox atau General API terlampaui |
| 502 | `EXECUTION_PROVIDER_ERROR` | Judge0 tidak bisa dijangkau, timeout saat polling, atau tidak mengembalikan execution token |

**Contoh 400 untuk bahasa tidak dikenali:**
```json
{
  "success": false,
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Language 'ruby' is not supported. Supported languages are: python, javascript, go"
  }
}
```

---

### GET /api/v1/execute/history

Mengambil daftar riwayat eksekusi kode milik user yang sedang login, diurutkan dari yang terbaru.

- **Auth:** Wajib (`Authorization: Bearer <token>`)
- **Rate Limit:** Execution Sandbox (default 15 req/menit) **+ General API (120 req/menit)**

#### Request

| Header | Nilai |
| --- | --- |
| `Authorization` | `Bearer <token>` |

**Query Parameters:**

| Parameter | Tipe | Default | Constraint | Deskripsi |
| --- | --- | --- | --- | --- |
| `limit` | `integer` | `20` | Min 1, maks 100 | Jumlah item yang dikembalikan |
| `offset` | `integer` | `0` | Min 0 | Offset untuk pagination |

Contoh: `GET /api/v1/execute/history?limit=10&offset=0`

> Nilai `limit` yang tidak valid (bukan integer, ≤0, atau >100) diabaikan dan server menggunakan default `20`. Nilai `offset` yang tidak valid diabaikan dan menggunakan default `0`.

#### Response `200 OK`

```json
{
  "success": true,
  "message": "Request successful",
  "data": [
    {
      "id": "c7a8b123-5e6f-4a1b-9c2d-3e4f5a6b7c8d",
      "user_id": "c1f76d49-4eb5-4a5f-9e77-5f725cf9a9f2",
      "language": "python",
      "code": "import sys\nname = sys.stdin.read().strip()\nprint(f'Hello, {name}!')",
      "stdin": "Antigravity",
      "stdout": "Hello, Antigravity!\n",
      "stderr": "",
      "exit_code": 0,
      "execution_time_ms": 28,
      "memory_kb": 3120,
      "status": "Accepted",
      "created_at": "2026-09-26T09:00:00Z"
    }
  ]
}
```

**Schema item array (`CodeExecution`):**

| Field | Tipe | Deskripsi |
| --- | --- | --- |
| `id` | `string` (UUID) | ID record eksekusi |
| `user_id` | `string` (UUID) | ID pemilik |
| `language` | `string` | Bahasa yang dieksekusi |
| `code` | `string` | Source code yang dieksekusi |
| `stdin` | `string` | Input standar (field tidak ada jika kosong) |
| `stdout` | `string` | Output standar |
| `stderr` | `string` | Error output |
| `exit_code` | `integer` | Exit code |
| `execution_time_ms` | `integer` | Waktu eksekusi dalam milidetik |
| `memory_kb` | `integer` | Memori yang digunakan dalam kilobytes |
| `status` | `string` | Status dari Judge0 |
| `created_at` | `string` (RFC3339) | Waktu eksekusi dicatat |

> Jika user belum punya riwayat eksekusi, `data` adalah array kosong `[]`.

#### Error Responses

| Status | Code | Kapan |
| --- | --- | --- |
| 401 | `UNAUTHORIZED` | Token tidak ada atau tidak valid |
| 429 | `RATE_LIMITED` | Limit Execution Sandbox atau General API terlampaui |

---

### GET /api/v1/execute/{id}

Mengambil detail satu riwayat eksekusi kode berdasarkan ID.

- **Auth:** Wajib (`Authorization: Bearer <token>`)
- **Rate Limit:** Execution Sandbox (default 15 req/menit) **+ General API (120 req/menit)**
- **Security:** Hanya mengembalikan eksekusi milik user yang sedang login (`WHERE id = $1 AND user_id = $2`)

#### Request

| Header | Nilai |
| --- | --- |
| `Authorization` | `Bearer <token>` |

**Path param:** `{id}` — UUID record eksekusi

#### Response `200 OK`

```json
{
  "success": true,
  "message": "Request successful",
  "data": {
    "id": "c7a8b123-5e6f-4a1b-9c2d-3e4f5a6b7c8d",
    "user_id": "c1f76d49-4eb5-4a5f-9e77-5f725cf9a9f2",
    "language": "python",
    "code": "import sys\nname = sys.stdin.read().strip()\nprint(f'Hello, {name}!')",
    "stdin": "Antigravity",
    "stdout": "Hello, Antigravity!\n",
    "stderr": "",
    "exit_code": 0,
    "execution_time_ms": 28,
    "memory_kb": 3120,
    "status": "Accepted",
    "created_at": "2026-09-26T09:00:00Z"
  }
}
```

> Schema `data` identik dengan item di `GET /api/v1/execute/history`.

#### Error Responses

| Status | Code | Kapan |
| --- | --- | --- |
| 400 | `VALIDATION_ERROR` | `id` di path kosong |
| 401 | `UNAUTHORIZED` | Token tidak ada atau tidak valid |
| 404 | `NOT_FOUND` | Record eksekusi tidak ditemukan atau bukan milik user |
| 429 | `RATE_LIMITED` | Limit Execution Sandbox atau General API terlampaui |
