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

Response error **tidak pernah** berisi stack trace, SQL, kredensial, path filesystem, secret, maupun API key.

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
- `401` email atau password salah

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

## GET /api/v1/ai/health

Health check konfigurasi AI lokal. Hanya memeriksa apakah provider, API key, dan model sudah terkonfigurasi. Tidak memanggil API provider eksternal sehingga tidak menghabiskan kuota.

- **Auth:** tidak diperlukan

Response `200`:

```json
{
  "success": true,
  "data": {
    "provider": "gemini",
    "configured": true,
    "checks": {
      "provider": true,
      "api_key": true,
      "model": true
    }
  },
  "message": "Request successful"
}
```

---

## POST /api/v1/ai/chat

Endpoint interaksi utama **AI Assistant / AI Generator**.

Menerima prompt pengguna secara generik untuk menghasilkan teks, kode, analisis, atau output kreatif lainnya. Otomatis membuat percakapan baru jika `conversation_id` tidak disertakan, atau melanjutkan percakapan yang ada dengan menyertakan context window riwayat pesan.

- **Auth:** `Authorization: Bearer <token>` wajib
- **Rate limiting:** Mengikuti rate limit `AI_CHAT_RATE_LIMIT_PER_MINUTE` per IP.

### Request Pertama (Membuat Conversation Baru)

```json
{
  "message": "Buatkan struktur REST API untuk aplikasi sekolah"
}
```

### Request Lanjutan (Melanjutkan Percakapan Sebelumnya)

```json
{
  "conversation_id": "7b8e1f54-...",
  "message": "Sekarang buatkan versi menggunakan Go"
}
```

### Field Request

- `conversation_id` (UUID string, opsional): ID thread percakapan. Jika kosong, dibuat thread baru.
- `message` (string, wajib): Prompt pengguna (maksimal 10.000 karakter).
- `system` (string, opsional): Instruksi sistem override.

### Response `200`

```json
{
  "success": true,
  "data": {
    "conversation_id": "7b8e1f54-...",
    "message": {
      "id": "3c9d2e10-...",
      "role": "assistant",
      "content": "Berikut rancangan struktur REST API untuk aplikasi sekolah..."
    }
  },
  "message": "Request successful"
}
```

Status codes:

- `200` sukses
- `400` body tidak valid / `message` kosong (`VALIDATION_ERROR`)
- `401` token otentikasi tidak ada atau tidak valid (`UNAUTHORIZED`)
- `404` conversation_id tidak ditemukan atau bukan milik pengguna (`NOT_FOUND`)
- `429` rate limit terlampaui (`RATE_LIMIT_EXCEEDED`)
- `502` AI provider error (`AI_PROVIDER_ERROR`)

---

## GET /api/v1/ai/conversations

Mengambil daftar thread percakapan milik pengguna yang sedang login, diurutkan dari yang paling baru diperbarui.

- **Auth:** `Authorization: Bearer <token>` wajib

Response `200`:

```json
{
  "success": true,
  "data": [
    {
      "id": "7b8e1f54-...",
      "user_id": "1a2b3c4d-...",
      "title": "Buatkan struktur REST API untuk aplikasi sekolah",
      "created_at": "2026-09-16T07:45:00Z",
      "updated_at": "2026-09-16T07:46:00Z"
    }
  ],
  "message": "Request successful"
}
```

---

## GET /api/v1/ai/conversations/{id}

Mengambil detail thread percakapan beserta seluruh riwayat pesan (`messages`) di dalamnya. Dilindungi dengan isolasi kepemilikan (`WHERE id = $1 AND user_id = $2`).

- **Auth:** `Authorization: Bearer <token>` wajib

Response `200`:

```json
{
  "success": true,
  "data": {
    "id": "7b8e1f54-...",
    "user_id": "1a2b3c4d-...",
    "title": "Buatkan struktur REST API untuk aplikasi sekolah",
    "created_at": "2026-09-16T07:45:00Z",
    "updated_at": "2026-09-16T07:46:00Z",
    "messages": [
      {
        "id": "msg-1",
        "conversation_id": "7b8e1f54-...",
        "role": "user",
        "content": "Buatkan struktur REST API untuk aplikasi sekolah",
        "created_at": "2026-09-16T07:45:00Z"
      },
      {
        "id": "msg-2",
        "conversation_id": "7b8e1f54-...",
        "role": "assistant",
        "content": "Berikut rancangan struktur REST API...",
        "created_at": "2026-09-16T07:45:05Z"
      }
    ]
  },
  "message": "Request successful"
}
```

Status codes:

- `200` sukses
- `401` token tidak valid (`UNAUTHORIZED`)
- `404` conversation tidak ditemukan atau bukan milik pengguna (`NOT_FOUND`)

---

## DELETE /api/v1/ai/conversations/{id}

Menghapus thread percakapan beserta seluruh pesannya.

- **Auth:** `Authorization: Bearer <token>` wajib

Response `200`:

```json
{
  "success": true,
  "data": null,
  "message": "Conversation deleted successfully"
}
```

Status codes:

- `200` sukses
- `401` token tidak valid (`UNAUTHORIZED`)
- `404` conversation tidak ditemukan (`NOT_FOUND`)

