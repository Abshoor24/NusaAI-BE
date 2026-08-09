# API Documentation

Base URL: `http://localhost:8080` (sesuai `APP_PORT`).

Semua request/response menggunakan JSON.

## Standard Response Format

Success:

```json
{
  "success": true,
  "data": {},
  "message": "Request successful"
}
```

Error:

```json
{
  "success": false,
  "error": {
    "code": "SOME_ERROR_CODE",
    "message": "Human readable message"
  }
}
```

Error dengan detail validasi:

```json
{
  "success": false,
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Invalid request",
    "fields": {
      "email": "must be a valid email address"
    }
  }
}
```

### Error Codes

| Code | HTTP | Keterangan |
| --- | --- | --- |
| `VALIDATION_ERROR` | 400 | Body/field tidak valid |
| `BAD_REQUEST` | 400 | Request tidak valid (mis. body terlalu besar) |
| `UNAUTHORIZED` | 401 | Token hilang, tidak valid, atau kredensial salah |
| `FORBIDDEN` | 403 | Role tidak diizinkan |
| `NOT_FOUND` | 404 | Resource tidak ditemukan |
| `CONFLICT` | 409 | Duplikat (mis. email sudah terdaftar) |
| `RATE_LIMIT_EXCEEDED` | 429 | Terlalu banyak request |
| `AI_PROVIDER_ERROR` | 502 | LLM provider error (detail tidak diekspos) |
| `SERVICE_UNAVAILABLE` | 503 | Database tidak tersedia |
| `INTERNAL_SERVER_ERROR` | 500 | Error tak terduga (detail tidak diekspos) |

Response error **tidak pernah** berisi stack trace, SQL, kredensial, path
filesystem, secret, maupun API key.

Setiap response menyertakan header `X-Request-ID`.

---

## GET /health

Liveness check. Tidak menyentuh database.

- **Auth:** tidak diperlukan

Response `200`:

```json
{
  "success": true,
  "data": { "status": "ok" },
  "message": "Request successful"
}
```

---

## GET /health/db

Database readiness check (ping ke connection pool).

- **Auth:** tidak diperlukan

Response `200`:

```json
{
  "success": true,
  "data": {
    "status": "ok",
    "database": "connected"
  },
  "message": "Request successful"
}
```

Response `503` jika database tidak tersedia:

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

## POST /api/v1/auth/register

Membuat akun baru dan langsung menerima JWT.

- **Auth:** tidak diperlukan

Request:

```json
{
  "email": "user@example.com",
  "password": "secret123"
}
```

Rules:

- `email` wajib dan format email valid.
- `password` wajib, minimal 8 dan maksimal 72 karakter.
- Email dinormalisasi ke huruf kecil.
- Setiap user yang terdaftar saat ini mendapat role `admin`.

Response `201`:

```json
{
  "success": true,
  "data": {
    "token": "<jwt>",
    "token_type": "Bearer",
    "expires_in": 86400,
    "user": {
      "id": "1a2b3c4d-...",
      "email": "user@example.com",
      "role": "admin",
      "created_at": "2026-08-09T10:00:00Z"
    }
  },
  "message": "User registered successfully"
}
```

Status codes:

- `201` sukses
- `400` body tidak valid (`VALIDATION_ERROR` / `BAD_REQUEST`)
- `409` email sudah terdaftar

---

## POST /api/v1/auth/login

Login dengan email + password, menerima JWT.

- **Auth:** tidak diperlukan

Request:

```json
{
  "email": "user@example.com",
  "password": "secret123"
}
```

Response `200`: sama dengan register (`token`, `user`, dst).

Status codes:

- `200` sukses
- `400` body tidak valid
- `401` email atau password salah (pesan generik, tidak membedakan keduanya
  untuk mencegah user enumeration)

---

## GET /api/v1/auth/me

Mengembalikan profil user yang sedang login berdasarkan token.

- **Auth:** `Authorization: Bearer <token>` wajib

Response `200`:

```json
{
  "success": true,
  "data": {
    "id": "1a2b3c4d-...",
    "email": "user@example.com",
    "role": "admin",
    "created_at": "2026-08-09T10:00:00Z"
  },
  "message": "Request successful"
}
```

Status codes:

- `200` sukses
- `401` token hilang/tidak valid/kedaluwarsa
- `404` user tidak ditemukan

---

## POST /api/v1/ai/chat

Prototype AI endpoint untuk membuktikan abstraksi AI end-to-end.

> **PENTING:** endpoint ini adalah **PROTOTYPE**. Prompt/system instruction
> yang spesifik untuk bisnis client **belum ada** dan tidak boleh diasumsikan.

- **Auth:** `Authorization: Bearer <token>` wajib

Request:

```json
{
  "message": "Hello",
  "context": []
}
```

Rules:

- `message` wajib.
- `context` opsional: array string yang nantinya dapat berisi dokumen, FAQ,
  hasil vector search, dsb. (CLIENT REQUIREMENT).
- Ukuran body dibatasi (`AI_MAX_REQUEST_BYTES`, default 1 MB).
- Timeout panggilan AI dibatasi (`AI_TIMEOUT`, default 30 detik).

Response `200`:

```json
{
  "success": true,
  "data": {
    "message": "...",
    "model": "gpt-4o-mini"
  },
  "message": "Request successful"
}
```

Status codes:

- `200` sukses
- `400` body tidak valid / terlalu besar
- `401` token hilang/tidak valid
- `429` rate limit terlampaui
- `502` AI provider error (detail provider tidak diekspos ke client)

Catatan: jika `AI_MODEL` kosong, endpoint mengembalikan `AI_PROVIDER_ERROR`.
API key hanya disimpan di backend, tidak pernah dikirim ke frontend.
