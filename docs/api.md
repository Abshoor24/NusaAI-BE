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
- `use_rag` (boolean, opsional): Jika `true`, mengaktifkan pencarian vector semantic pada dokumen knowledge pengguna dan menyisipkan konteks relevan ke dalam prompt. Default `false`.
- `document_id` (UUID string, opsional): Jika diisi bersama `use_rag: true`, membatasi pencarian RAG hanya pada dokumen tertentu milik pengguna.

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

## POST /api/v1/ai/chat/stream

Endpoint streaming **AI Assistant / AI Generator** menggunakan Server-Sent Events (SSE).

Menghasilkan respon bertahap token demi token secara real-time ke client. Format request identik dengan `POST /api/v1/ai/chat`.

- **Auth:** `Authorization: Bearer <token>` wajib
- **Rate limiting:** Mengikuti rate limit `AI_CHAT_RATE_LIMIT_PER_MINUTE` per IP.
- **Headers Response:** `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`

### Format Event SSE

1. **Token event** (dikirim berkala selama AI mengalirkan teks):
```text
event: token
data: {"token":"Halo"}
```

2. **Done event** (dikirim saat stream selesai & pesan lengkap berhasil disimpan ke DB):
```text
event: done
data: {"conversation_id":"7b8e1f54-...","message_id":"3c9d2e10-..."}
```

3. **Error event** (dikirim jika terjadi kegagalan provider saat streaming):
```text
event: error
data: {"error":"The AI provider returned an error"}
```

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

---

## POST /api/v1/ai/documents

Mengunggah/meng-ingest dokumen knowledge teks untuk RAG. Dokumen akan dipotong menjadi chunk-chunk dengan overlap dan di-embed menggunakan model vector `text-embedding-004` sebelum disimpan ke PostgreSQL (`pgvector`).

- **Auth:** `Authorization: Bearer <token>` wajib

### Request Body

```json
{
  "title": "Panduan Standar Operasional",
  "content": "Isi lengkap panduan operasional perusahaan dalam format teks...",
  "source": "manual"
}
```

### Response `201`

```json
{
  "success": true,
  "data": {
    "document": {
      "id": "8f3a1b2c-...",
      "user_id": "1a2b3c4d-...",
      "title": "Panduan Standar Operasional",
      "source": "manual",
      "created_at": "2026-09-17T10:00:00Z"
    },
    "chunk_count": 5
  },
  "message": "Document ingested successfully"
}
```

---

## GET /api/v1/ai/documents

Mengambil daftar seluruh dokumen knowledge milik pengguna yang sedang login beserta jumlah chunk-nya.

- **Auth:** `Authorization: Bearer <token>` wajib

Response `200`:

```json
{
  "success": true,
  "data": [
    {
      "id": "8f3a1b2c-...",
      "user_id": "1a2b3c4d-...",
      "title": "Panduan Standar Operasional",
      "source": "manual",
      "created_at": "2026-09-17T10:00:00Z",
      "chunk_count": 5
    }
  ],
  "message": "Request successful"
}
```

---

## GET /api/v1/ai/documents/{id}

Mengambil detail satu dokumen knowledge milik pengguna. Dilindungi dengan isolasi kepemilikan.

- **Auth:** `Authorization: Bearer <token>` wajib

Response `200`:

```json
{
  "success": true,
  "data": {
    "id": "8f3a1b2c-...",
    "user_id": "1a2b3c4d-...",
    "title": "Panduan Standar Operasional",
    "source": "manual",
    "created_at": "2026-09-17T10:00:00Z"
  },
  "message": "Request successful"
}
```

---

## DELETE /api/v1/ai/documents/{id}

Menghapus dokumen knowledge beserta seluruh vector chunk terkait (`ON DELETE CASCADE`).

- **Auth:** `Authorization: Bearer <token>` wajib

Response `200`:

```json
{
  "success": true,
  "data": null,
  "message": "Document deleted successfully"
}
```

---

## POST /api/v1/ai/documents/upload (alias: POST /api/v1/documents/upload)

Mengunggah file dokumen asli (PDF, DOCX, TXT) ke storage terisolasi per user. Backend membuat record dokumen dengan status `processing` dan menjalankan background job (asinkron) untuk ekstraksi teks, chunking, dan pembuatan vector embedding (`text-embedding-004`) ke `document_chunks`.

- **Auth:** `Authorization: Bearer <token>` wajib
- **Content-Type:** `multipart/form-data`
- **Tipe File yang Didukung:** PDF (`.pdf`), Microsoft Word (`.docx`), Plain Text (`.txt`)
- **Batasan Ukuran:** Maksimal 10 MB (dikonfigurasi via `UPLOAD_MAX_BYTES`)

### Form Fields

- `file` (binary, wajib): Berkas file dokumen yang akan diunggah.
- `title` (string, opsional): Judul dokumen. Jika kosong, default menggunakan nama asli file.

### Response `202 Accepted`

```json
{
  "success": true,
  "data": {
    "id": "e98e4d94-31ea-42f7-a36c-941da7ce3920",
    "user_id": "1a2b3c4d-...",
    "title": "Dokumen Kebijakan 2026",
    "source": "upload",
    "file_name": "kebijakan_2026.pdf",
    "file_type": "pdf",
    "file_size": 204850,
    "status": "processing",
    "created_at": "2026-09-18T08:00:00Z"
  },
  "message": "Document uploaded and queued for processing"
}
```

Status codes:
- `202` Berhasil diunggah dan dijadwalkan untuk background processing
- `400` File kosong, melebihi batas ukuran maksimum, atau ekstensi tidak didukung (`VALIDATION_ERROR`)
- `401` Token tidak valid atau tidak disediakan (`UNAUTHORIZED`)

---

## GET /api/v1/ai/documents/{id}/status (alias: GET /api/v1/documents/{id}/status)

Memeriksa status proses ekstraksi dan embedding dokumen knowledge (untuk polling dari frontend).

- **Auth:** `Authorization: Bearer <token>` wajib

### Response `200`

Status saat sedang diproses:
```json
{
  "success": true,
  "data": {
    "id": "e98e4d94-31ea-42f7-a36c-941da7ce3920",
    "title": "Dokumen Kebijakan 2026",
    "status": "processing",
    "chunk_count": 0
  },
  "message": "Request successful"
}
```

Status saat selesai dan siap digunakan dalam RAG chat:
```json
{
  "success": true,
  "data": {
    "id": "e98e4d94-31ea-42f7-a36c-941da7ce3920",
    "title": "Dokumen Kebijakan 2026",
    "status": "ready",
    "chunk_count": 12
  },
  "message": "Request successful"
}
```

Status jika pemrosesan gagal:
```json
{
  "success": true,
  "data": {
    "id": "e98e4d94-31ea-42f7-a36c-941da7ce3920",
    "title": "Dokumen Kebijakan 2026",
    "status": "failed",
    "chunk_count": 0,
    "error_message": "pdf contains no extractable text"
  },
  "message": "Request successful"
}
```

Status codes:
- `200` Berhasil mendapatkan status dokumen
- `401` Token tidak valid (`UNAUTHORIZED`)
- `404` Dokumen tidak ditemukan atau bukan milik pengguna (`NOT_FOUND`)


