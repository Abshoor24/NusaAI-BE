# API Documentation

Base URL: `http://localhost:8080` (sesuai konfigurasi `APP_PORT`).

Semua request dan response standar menggunakan format JSON, kecuali upload berkas (`multipart/form-data`), streaming Server-Sent Events (`text/event-stream`), dan endpoint serving media/attachment (`image/*`).

---

## Daftar Endpoint (API Reference)

| Method | Endpoint | Auth | Rate Limit | Deskripsi |
| --- | --- | --- | --- | --- |
| **System & Health** | | | | |
| `GET` | `/health` | Publik | Global | Liveness check server (tanpa database) |
| `GET` | `/health/db` | Publik | Global | Database readiness check (ping pgxpool) |
| **Authentication** | | | | |
| `POST` | `/api/v1/auth/register` | Publik | Global | Registrasi akun baru & langsung menerima JWT |
| `POST` | `/api/v1/auth/login` | Publik | Global | Login email & password, menerima JWT |
| `GET` | `/api/v1/auth/me` | Bearer | Global | Profil user yang sedang login |
| **AI Assistant & Multimodal Chat** | | | | |
| `GET` | `/api/v1/ai/health` | Publik | Global | Cek konfigurasi provider, API key, & model AI |
| `POST` | `/api/v1/ai/chat` | Bearer | AI Chat Limit | Interaksi AI multi-turn, RAG, & multimodal image attachment |
| `POST` | `/api/v1/ai/chat/stream` | Bearer / `?token=` | AI Chat Limit | Real-time streaming AI response token-by-token (SSE) |
| `GET` | `/api/v1/ai/conversations` | Bearer | Global | Daftar thread percakapan user (terbaru lebih dahulu) |
| `GET` | `/api/v1/ai/conversations/{id}` | Bearer | Global | Detail thread percakapan beserta seluruh riwayat pesan & lampiran |
| `DELETE` | `/api/v1/ai/conversations/{id}` | Bearer | Global | Menghapus thread percakapan dan seluruh riwayat pesan |
| **Attachments (Multimodal Media)** | | | | |
| `GET` | `/api/v1/ai/attachments/{id}` | Bearer | Global | Mengunduh/menampilkan file gambar lampiran milik user |
| `GET` | `/api/v1/attachments/{id}` | Bearer | Global | Alias untuk `/api/v1/ai/attachments/{id}` |
| **RAG Knowledge Base & Documents** | | | | |
| `POST` | `/api/v1/ai/documents` | Bearer | Global | Ingest dokumen teks langsung (chunking & vector embedding) |
| `POST` | `/api/v1/ai/documents/upload` | Bearer | Global | Upload file fisik (PDF, DOCX, TXT) dengan pemrosesan asinkron |
| `POST` | `/api/v1/documents/upload` | Bearer | Global | Alias untuk `/api/v1/ai/documents/upload` |
| `GET` | `/api/v1/ai/documents` | Bearer | Global | Daftar seluruh dokumen knowledge base milik user |
| `GET` | `/api/v1/ai/documents/{id}` | Bearer | Global | Detail metadata dokumen knowledge base |
| `GET` | `/api/v1/ai/documents/{id}/status` | Bearer | Global | Polling status background processing dokumen (`processing`, `ready`, `failed`) |
| `GET` | `/api/v1/documents/{id}/status` | Bearer | Global | Alias untuk `/api/v1/ai/documents/{id}/status` |
| `DELETE` | `/api/v1/ai/documents/{id}` | Bearer | Global | Hapus dokumen beserta seluruh vector chunks & file terkait |
| **Code Sandbox Execution** | | | | |
| `POST` | `/api/v1/execute` | Bearer | Exec Limit | Eksekusi kode sandboxed (Python, JavaScript, Go) via Judge0 |
| `GET` | `/api/v1/execute/history` | Bearer | Exec Limit | Daftar riwayat eksekusi kode user (pagination limit/offset) |
| `GET` | `/api/v1/execute/{id}` | Bearer | Exec Limit | Detail hasil riwayat eksekusi kode |

---

## Standard Response Format

### Success

```json
{
  "success": true,
  "data": {},
  "message": "Request successful"
}
```

### Error

```json
{
  "success": false,
  "error": {
    "code": "SOME_ERROR_CODE",
    "message": "Human readable message"
  }
}
```

### Error dengan Detail Validasi

```json
{
  "success": false,
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Invalid request",
    "fields": {
      "email": "must be a valid email address",
      "password": "password must be at least 8 characters"
    }
  }
}
```

### Error Codes

| Code | HTTP | Keterangan |
| --- | --- | --- |
| `VALIDATION_ERROR` | 400 | Format request/field tidak valid atau melebihi batasan ukuran |
| `BAD_REQUEST` | 400 | Request malformed (mis. JSON syntax error, payload melebihi limit) |
| `UNAUTHORIZED` | 401 | Token JWT hilang, tidak valid, kedaluwarsa, atau kredensial login salah |
| `FORBIDDEN` | 403 | Role/izin pengguna tidak mencukupi |
| `NOT_FOUND` | 404 | Resource tidak ditemukan atau bukan milik pengguna |
| `CONFLICT` | 409 | Duplikat resource unik (mis. email sudah terdaftar) |
| `RATE_LIMITED` | 429 | Terlalu banyak request (dilengkapi header `Retry-After`) |
| `AI_PROVIDER_ERROR` | 502 | Upstream LLM provider error / timeout (detail teknis disembunyikan) |
| `EXECUTION_PROVIDER_ERROR` | 502 | Upstream execution engine (Judge0) error / unreachable |
| `SERVICE_UNAVAILABLE` | 503 | Database tidak tersedia |
| `INTERNAL_SERVER_ERROR` | 500 | Kesalahan internal server (detail hanya dicatat di server log) |

> [!NOTE]
> Response error **tidak pernah** mengekspos stack trace, query SQL, path file lokal, secret environment, maupun API key.
> Setiap response menyertakan header tracking `X-Request-ID`.

---

## Rate Limiting & 429 Response

Backend menerapkan 3 tingkatan rate limiting berbasis token bucket per alamat IP pengguna:
1. **General API Rate Limit**: Mengontrol endpoint publik dan standar (`RATE_LIMIT_PER_MINUTE`, default 120 req/menit).
2. **AI Chat Rate Limit**: Melindungi kuota provider AI (`AI_CHAT_RATE_LIMIT_PER_MINUTE`, default 10 req/menit).
3. **Execution Sandbox Rate Limit**: Mengontrol beban komputasi eksekusi kode (`EXEC_RATE_LIMIT_PER_MINUTE`, default 15 req/menit).

Saat limit terlampaui, server mengembalikan status HTTP `429 Too Many Requests` beserta header `Retry-After` (dalam detik):

```http
HTTP/1.1 429 Too Many Requests
Content-Type: application/json
Retry-After: 60
X-Request-ID: req_abc123
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

Contoh penanganan di frontend:
```javascript
if (response.status === 429) {
  const retryAfter = parseInt(response.headers.get('Retry-After') || '60', 10);
  console.warn(`Rate limited. Coba lagi dalam ${retryAfter} detik.`);
  await new Promise(r => setTimeout(r, retryAfter * 1000));
}
```

---

## Autentikasi SSE Streaming (`POST /api/v1/ai/chat/stream`)

Endpoint streaming dilindungi oleh JWT. Server mendukung **dua cara** pengiriman token:

1. **Header HTTP Standar** (Direkomendasikan untuk library `fetch` atau `@microsoft/fetch-event-source`):
   ```http
   POST /api/v1/ai/chat/stream HTTP/1.1
   Host: localhost:8080
   Authorization: Bearer <token>
   Content-Type: application/json
   ```
2. **Query Parameter `?token=<jwt>`** (Disediakan khusus untuk kompatibilitas lingkungan yang tidak mengizinkan custom header):
   ```http
   POST /api/v1/ai/chat/stream?token=<jwt> HTTP/1.1
   Host: localhost:8080
   Content-Type: application/json
   ```

> [!WARNING]
> Endpoint `/api/v1/ai/chat/stream` adalah **satu-satunya** endpoint yang menerima token melalui query param `?token=`. Semua endpoint lain wajib menggunakan header `Authorization: Bearer <token>`.

### Implementasi Frontend Menggunakan `fetch` + `ReadableStream` (POST SSE)

```javascript
const token = localStorage.getItem('jwt_token');

const response = await fetch('/api/v1/ai/chat/stream', {
  method: 'POST',
  headers: {
    'Authorization': `Bearer ${token}`,
    'Content-Type': 'application/json',
  },
  body: JSON.stringify({
    conversation_id: '7b8e1f54-3e9a-4c2f-b441-1188339900aa', // opsional
    message: 'Buatkan fungsi generator prima di Go',
    use_rag: false
  }),
});

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
      process.stdout.write(token); // Tampilkan token di UI
    } else if (eventType === 'done') {
      const { conversation_id, message_id } = JSON.parse(eventData);
      console.log(`\nSelesai. Conv: ${conversation_id}, Msg: ${message_id}`);
    } else if (eventType === 'error') {
      const { error } = JSON.parse(eventData);
      console.error('Stream error:', error);
    }
  }
}
```

---

## 1. System & Health Check

### GET /health

Liveness check service. Memverifikasi web server aktif tanpa membebani koneksi database.

- **Auth:** Tidak diperlukan

#### Response `200 OK`
```json
{
  "success": true,
  "data": {
    "status": "ok"
  },
  "message": "Request successful"
}
```

---

### GET /health/db

Readiness check koneksi database PostgreSQL (ping ke pgxpool).

- **Auth:** Tidak diperlukan

#### Response `200 OK`
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

## 2. Authentication

### POST /api/v1/auth/register

Mendaftarkan akun baru dan langsung menghasilkan token JWT aktif.

- **Auth:** Tidak diperlukan

#### Request Body
```json
{
  "email": "developer@example.com",
  "password": "PasswordSuperAman123"
}
```

**Ketentuan Validasi:**
- `email`: Wajib, format email valid, dinormalisasi otomatis ke huruf kecil.
- `password`: Wajib, minimal 8 karakter dan maksimal 72 karakter (batasan bcrypt).
- User yang baru mendaftar default memiliki role `admin`.

#### Response `201 Created`
```json
{
  "success": true,
  "data": {
    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "token_type": "Bearer",
    "expires_in": 86400,
    "user": {
      "id": "c1f76d49-4eb5-4a5f-9e77-5f725cf9a9f2",
      "email": "developer@example.com",
      "role": "admin",
      "created_at": "2026-09-22T10:00:00Z"
    }
  },
  "message": "User registered successfully"
}
```

---

### POST /api/v1/auth/login

Autentikasi dengan email dan password untuk mendapatkan JWT.

- **Auth:** Tidak diperlukan

#### Request Body
```json
{
  "email": "developer@example.com",
  "password": "PasswordSuperAman123"
}
```

#### Response `200 OK`
Sama dengan format response `register` (mengembalikan `token`, `token_type`, `expires_in`, dan objek `user`).

---

### GET /api/v1/auth/me

Mengambil data profil pengguna yang sedang login berdasarkan token JWT.

- **Auth:** Wajib (`Authorization: Bearer <token>`)

#### Response `200 OK`
```json
{
  "success": true,
  "data": {
    "id": "c1f76d49-4eb5-4a5f-9e77-5f725cf9a9f2",
    "email": "developer@example.com",
    "role": "admin",
    "created_at": "2026-09-22T10:00:00Z"
  },
  "message": "Request successful"
}
```

---

## 3. AI Assistant & Multimodal Chat

### GET /api/v1/ai/health

Health check konfigurasi runtime AI (memeriksa provider, API key, dan model tanpa memanggil API eksternal sehingga hemat kuota).

- **Auth:** Tidak diperlukan

#### Response `200 OK`
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

### POST /api/v1/ai/chat

Interaksi utama AI Assistant / AI Generator. Mendukung percakapan multi-turn, integrasi RAG knowledge base, dan multimodal image attachments.

- **Auth:** Wajib (`Authorization: Bearer <token>`)
- **Rate Limit:** Terikat pada `AI_CHAT_RATE_LIMIT_PER_MINUTE` (default 10 req/menit per IP)
- **Format Request yang Didukung:**
  1. `application/json` (Teks murni atau lampiran gambar dalam format base64)
  2. `multipart/form-data` (Upload berkas gambar langsung bersama field form)

#### Opsi A: Request Format JSON (`application/json`)

```json
{
  "conversation_id": "7b8e1f54-3e9a-4c2f-b441-1188339900aa",
  "message": "Analisis arsitektur berikut dan berikan rekomendasi perbaikan",
  "system": "Anda adalah Lead Solutions Architect berpengalaman tinggi.",
  "use_rag": true,
  "document_id": "8f3a1b2c-473d-4c31-9014-cb9aa15df41e",
  "images": [
    {
      "file_name": "diagram-arsitektur.png",
      "mime_type": "image/png",
      "data": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
    }
  ]
}
```

#### Opsi B: Request Format Multipart (`multipart/form-data`)

Gunakan `multipart/form-data` saat pengguna memilih berkas gambar langsung dari form browser:
- `message` (string, wajib): Isi prompt pengguna.
- `conversation_id` (string UUID, opsional): ID thread jika melanjutkan percakapan.
- `system` (string, opsional): System instruction override.
- `use_rag` (string "true" / "1", opsional): Mengaktifkan RAG retrieval.
- `document_id` (string UUID, opsional): Membatasi RAG ke dokumen spesifik.
- `image` / `images` / `file` / `files` (binary): Berkas gambar yang diunggah. Mendukung multi-file.

**Ketentuan Multimodal & Gambar:**
- Format gambar yang didukung: JPEG (`image/jpeg`, `image/jpg`), PNG (`image/png`), dan WebP (`image/webp`).
- Ukuran maksimal gambar per berkas: Mengikuti `UPLOAD_MAX_BYTES` (default 10 MB).
- Berkas gambar disimpan di folder lokal `uploads/` dan tercatat di database pada tabel `message_attachments`.

#### Response `200 OK`
```json
{
  "success": true,
  "data": {
    "conversation_id": "7b8e1f54-3e9a-4c2f-b441-1188339900aa",
    "message": {
      "id": "9d1e2f3a-7a8b-4c5d-9e0f-1a2b3c4d5e6f",
      "role": "assistant",
      "content": "Berdasarkan diagram yang Anda lampirkan dan dokumen kebijakan...",
      "attachments": [
        {
          "id": "a1b2c3d4-e5f6-4a5b-8c9d-0e1f2a3b4c5d",
          "message_id": "8c0d1e2f-3a4b-5c6d-7e8f-9a0b1c2d3e4f",
          "file_name": "diagram-arsitektur.png",
          "file_type": "png",
          "file_size": 154200,
          "mime_type": "image/png",
          "created_at": "2026-09-22T10:15:00Z"
        }
      ]
    },
    "sources": [
      {
        "id": "chunk-11223344",
        "document_id": "8f3a1b2c-473d-4c31-9014-cb9aa15df41e",
        "chunk_index": 2,
        "content": "Standar keamanan sistem mengharuskan enkripsi end-to-end...",
        "similarity": 0.875
      }
    ]
  },
  "message": "Request successful"
}
```

---

### POST /api/v1/ai/chat/stream

Mengalirkan respon AI Assistant secara real-time token-by-token via Server-Sent Events (SSE).

- **Auth:** Wajib (`Authorization: Bearer <token>` atau query param `?token=<jwt>`)
- **Headers Response:** `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`
- **Request:** Format request identik dengan `POST /api/v1/ai/chat` (menerima `application/json` maupun `multipart/form-data`).

#### Urutan Event SSE:

1. **`event: token`** (dikirim berkala selama teks dihasilkan):
   ```text
   event: token
   data: {"token":"Halo"}

   event: token
   data: {"token":" ada"}

   event: token
   data: {"token":" yang bisa saya bantu?"}
   ```

2. **`event: done`** (dikirim setelah seluruh stream selesai & respon lengkap tersimpan ke database):
   ```text
   event: done
   data: {"conversation_id":"7b8e1f54-3e9a-4c2f-b441-1188339900aa","message_id":"9d1e2f3a-7a8b-4c5d-9e0f-1a2b3c4d5e6f"}
   ```

3. **`event: error`** (dikirim jika terjadi error provider di tengah streaming):
   ```text
   event: error
   data: {"error":"The AI provider returned an error"}
   ```

---

### GET /api/v1/ai/conversations

Mengambil daftar seluruh thread percakapan milik pengguna yang sedang login, diurutkan berdasarkan aktivitas terbaru (`updated_at DESC`).

- **Auth:** Wajib (`Authorization: Bearer <token>`)

#### Response `200 OK`
```json
{
  "success": true,
  "data": [
    {
      "id": "7b8e1f54-3e9a-4c2f-b441-1188339900aa",
      "user_id": "c1f76d49-4eb5-4a5f-9e77-5f725cf9a9f2",
      "title": "Analisis arsitektur berikut dan berikan rekomendasi",
      "created_at": "2026-09-22T10:15:00Z",
      "updated_at": "2026-09-22T10:15:30Z"
    }
  ],
  "message": "Request successful"
}
```

---

### GET /api/v1/ai/conversations/{id}

Mengambil riwayat lengkap sebuah percakapan beserta seluruh pesan (`messages`) dan lampiran berkas gambar (`attachments`).

- **Auth:** Wajib (`Authorization: Bearer <token>`)
- **Security:** Terisolasi per user (`WHERE id = $1 AND user_id = $2`).

#### Response `200 OK`
```json
{
  "success": true,
  "data": {
    "id": "7b8e1f54-3e9a-4c2f-b441-1188339900aa",
    "user_id": "c1f76d49-4eb5-4a5f-9e77-5f725cf9a9f2",
    "title": "Analisis arsitektur berikut",
    "created_at": "2026-09-22T10:15:00Z",
    "updated_at": "2026-09-22T10:15:30Z",
    "messages": [
      {
        "id": "8c0d1e2f-3a4b-5c6d-7e8f-9a0b1c2d3e4f",
        "conversation_id": "7b8e1f54-3e9a-4c2f-b441-1188339900aa",
        "role": "user",
        "content": "Analisis arsitektur berikut",
        "attachments": [
          {
            "id": "a1b2c3d4-e5f6-4a5b-8c9d-0e1f2a3b4c5d",
            "message_id": "8c0d1e2f-3a4b-5c6d-7e8f-9a0b1c2d3e4f",
            "file_name": "diagram-arsitektur.png",
            "file_type": "png",
            "file_size": 154200,
            "mime_type": "image/png",
            "created_at": "2026-09-22T10:15:00Z"
          }
        ],
        "created_at": "2026-09-22T10:15:00Z"
      },
      {
        "id": "9d1e2f3a-7a8b-4c5d-9e0f-1a2b3c4d5e6f",
        "conversation_id": "7b8e1f54-3e9a-4c2f-b441-1188339900aa",
        "role": "assistant",
        "content": "Berdasarkan diagram yang dilampirkan...",
        "created_at": "2026-09-22T10:15:05Z"
      }
    ]
  },
  "message": "Request successful"
}
```

---

### DELETE /api/v1/ai/conversations/{id}

Menghapus thread percakapan beserta seluruh pesan dan lampiran terkait (`ON DELETE CASCADE`).

- **Auth:** Wajib (`Authorization: Bearer <token>`)

#### Response `200 OK`
```json
{
  "success": true,
  "data": null,
  "message": "Conversation deleted successfully"
}
```

---

## 4. Message Attachments (Multimodal Serving)

### GET /api/v1/ai/attachments/{id}
*(Alias: `GET /api/v1/attachments/{id}`)*

Mengunduh atau menampilkan streaming biner berkas gambar yang pernah dilampirkan pada pesan.

- **Auth:** Wajib (`Authorization: Bearer <token>`)
- **Security:** Server memvalidasi bahwa attachment tersebut adalah milik conversation user yang sedang login.
- **Headers Response:**
  - `Content-Type`: MIME type asli gambar (`image/png`, `image/jpeg`, dsb.)
  - `Content-Disposition`: `inline; filename="<nama-file>"`
  - `Content-Length`: Ukuran file dalam bytes

#### Response `200 OK`
Streaming byte biner gambar.

#### Response `404 Not Found`
```json
{
  "success": false,
  "error": {
    "code": "NOT_FOUND",
    "message": "Attachment not found"
  }
}
```

---

## 5. RAG Knowledge Base & Documents

### POST /api/v1/ai/documents

Ingest dokumen teks langsung untuk basis pengetahuan RAG. Teks dipecah menjadi chunk (ukuran 800 karakter, overlap 150), di-generate vektor embedding-nya menggunakan model `text-embedding-004` (dimensi 768), dan disimpan ke PostgreSQL `pgvector`.

- **Auth:** Wajib (`Authorization: Bearer <token>`)

#### Request Body
```json
{
  "title": "Standard Operating Procedure V2",
  "content": "Isi lengkap pedoman operasional teknis perusahaan...",
  "source": "manual"
}
```

#### Response `201 Created`
```json
{
  "success": true,
  "data": {
    "document": {
      "id": "8f3a1b2c-473d-4c31-9014-cb9aa15df41e",
      "user_id": "c1f76d49-4eb5-4a5f-9e77-5f725cf9a9f2",
      "title": "Standard Operating Procedure V2",
      "source": "manual",
      "created_at": "2026-09-22T08:00:00Z"
    },
    "chunk_count": 6
  },
  "message": "Document ingested successfully"
}
```

---

### POST /api/v1/ai/documents/upload
*(Alias: `POST /api/v1/documents/upload`)*

Mengunggah berkas fisik dokumen (**PDF**, **Word DOCX**, atau **Plain TXT**). Server menyimpan berkas ke storage lokal terisolasi, membuat record dengan status `processing`, dan menjadwalkan background job untuk ekstraksi teks, chunking, serta vector embedding secara asinkron.

- **Auth:** Wajib (`Authorization: Bearer <token>`)
- **Content-Type:** `multipart/form-data`
- **Format yang Didukung:** `.pdf`, `.docx`, `.txt`
- **Ukuran Maksimum:** Default 10 MB (dikonfigurasi via `UPLOAD_MAX_BYTES`)

#### Form Fields:
- `file` (binary, wajib): Berkas dokumen.
- `title` (string, opsional): Judul dokumen. Jika dikosongkan, menggunakan nama asli file.

#### Response `202 Accepted`
```json
{
  "success": true,
  "data": {
    "id": "e98e4d94-31ea-42f7-a36c-941da7ce3920",
    "user_id": "c1f76d49-4eb5-4a5f-9e77-5f725cf9a9f2",
    "title": "Buku Pedoman Karyawan 2026",
    "source": "upload",
    "file_name": "buku_pedoman.pdf",
    "file_type": "pdf",
    "file_size": 524288,
    "status": "processing",
    "created_at": "2026-09-22T08:30:00Z"
  },
  "message": "Document uploaded and queued for processing"
}
```

---

### GET /api/v1/ai/documents/{id}/status
*(Alias: `GET /api/v1/documents/{id}/status`)*

Memeriksa status background processing dari berkas dokumen yang diunggah (digunakan frontend untuk polling).

- **Auth:** Wajib (`Authorization: Bearer <token>`)

#### Response `200 OK` (Sedang Diproses)
```json
{
  "success": true,
  "data": {
    "id": "e98e4d94-31ea-42f7-a36c-941da7ce3920",
    "title": "Buku Pedoman Karyawan 2026",
    "status": "processing",
    "chunk_count": 0
  },
  "message": "Request successful"
}
```

#### Response `200 OK` (Selesai & Siap untuk RAG Chat)
```json
{
  "success": true,
  "data": {
    "id": "e98e4d94-31ea-42f7-a36c-941da7ce3920",
    "title": "Buku Pedoman Karyawan 2026",
    "status": "ready",
    "chunk_count": 18
  },
  "message": "Request successful"
}
```

#### Response `200 OK` (Gagal Ekstraksi/Embedding)
```json
{
  "success": true,
  "data": {
    "id": "e98e4d94-31ea-42f7-a36c-941da7ce3920",
    "title": "Buku Pedoman Karyawan 2026",
    "status": "failed",
    "chunk_count": 0,
    "error_message": "pdf contains no extractable text"
  },
  "message": "Request successful"
}
```

---

### GET /api/v1/ai/documents

Mengambil daftar seluruh dokumen knowledge base milik user login beserta metadata dan total chunk yang telah terindeks.

- **Auth:** Wajib (`Authorization: Bearer <token>`)

#### Response `200 OK`
```json
{
  "success": true,
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
      "created_at": "2026-09-22T08:30:00Z",
      "chunk_count": 18
    }
  ],
  "message": "Request successful"
}
```

---

### GET /api/v1/ai/documents/{id}

Mengambil detail sebuah dokumen knowledge base.

- **Auth:** Wajib (`Authorization: Bearer <token>`)

#### Response `200 OK`
```json
{
  "success": true,
  "data": {
    "id": "e98e4d94-31ea-42f7-a36c-941da7ce3920",
    "user_id": "c1f76d49-4eb5-4a5f-9e77-5f725cf9a9f2",
    "title": "Buku Pedoman Karyawan 2026",
    "source": "upload",
    "file_name": "buku_pedoman.pdf",
    "file_type": "pdf",
    "file_size": 524288,
    "status": "ready",
    "created_at": "2026-09-22T08:30:00Z"
  },
  "message": "Request successful"
}
```

---

### DELETE /api/v1/ai/documents/{id}

Menghapus dokumen knowledge base, menghapus seluruh chunk vektor dari database (`ON DELETE CASCADE`), dan menghapus berkas fisik dari storage jika bersumber dari upload.

- **Auth:** Wajib (`Authorization: Bearer <token>`)

#### Response `200 OK`
```json
{
  "success": true,
  "data": null,
  "message": "Document deleted successfully"
}
```

---

## 6. Code Sandbox Execution

Backend menyediakan modul eksekusi kode terisolasi penuh menggunakan engine **Judge0 CE**. Kode dijalankan di dalam container terisolasi dengan pembatasan CPU time, wall time, dan memory yang ketat. Hasil eksekusi dicatat ke tabel `code_executions`.

- **Auth:** Wajib (`Authorization: Bearer <token>`)
- **Rate Limit:** Terikat pada `EXEC_RATE_LIMIT_PER_MINUTE` (default 15 req/menit per IP)
- **Ukuran Maksimum Source Code:** Default 64 KB (`EXEC_MAX_CODE_BYTES`)
- **Bahasa yang Didukung (Whitelist):**
  - `python` (alias: `py`, `python3`) → Judge0 ID 71 (Python 3.8.1)
  - `javascript` (alias: `js`, `node`, `nodejs`) → Judge0 ID 63 (Node.js 12.14.0)
  - `go` (alias: `golang`) → Judge0 ID 60 (Go 1.13.5)

---

### POST /api/v1/execute

Mengeksekusi kode program di sandbox Judge0.

#### Request Body
```json
{
  "language": "python",
  "code": "import sys\nname = sys.stdin.read().strip()\nprint(f'Hello, {name}!')",
  "stdin": "Antigravity"
}
```

#### Response `200 OK` (Eksekusi Berhasil / Accepted)
```json
{
  "success": true,
  "data": {
    "id": "c7a8b123-5e6f-4a1b-9c2d-3e4f5a6b7c8d",
    "language": "python",
    "stdout": "Hello, Antigravity!\n",
    "stderr": "",
    "exit_code": 0,
    "execution_time_ms": 28,
    "memory_kb": 3120,
    "status": "Accepted"
  },
  "message": "Code executed successfully"
}
```

#### Response `200 OK` (Batas Waktu Terlampaui / Time Limit Exceeded)
Jika terdapat infinite loop pada kode pengguna:
```json
{
  "success": true,
  "data": {
    "id": "f8b9c234-1a2b-4c3d-8e4f-5a6b7c8d9e0f",
    "language": "python",
    "stdout": "",
    "stderr": "Time Limit Exceeded",
    "exit_code": 124,
    "execution_time_ms": 5002,
    "memory_kb": 12450,
    "status": "Time Limit Exceeded"
  },
  "message": "Code executed successfully"
}
```

#### Response `200 OK` (Compilation / Syntax Error)
```json
{
  "success": true,
  "data": {
    "id": "d1e2f345-2b3c-4d5e-9f0a-1b2c3d4e5f6a",
    "language": "go",
    "stdout": "",
    "stderr": "main.go:3:1: syntax error: non-declaration statement outside function body",
    "exit_code": 1,
    "execution_time_ms": 0,
    "memory_kb": 0,
    "status": "Compilation Error"
  },
  "message": "Code executed successfully"
}
```

---

### GET /api/v1/execute/history

Mengambil daftar riwayat eksekusi kode milik user yang sedang login.

- **Auth:** Wajib (`Authorization: Bearer <token>`)
- **Query Parameters:**
  - `limit` (integer, opsional, default 20, max 100)
  - `offset` (integer, opsional, default 0)

#### Response `200 OK`
```json
{
  "success": true,
  "data": [
    {
      "id": "c7a8b123-5e6f-4a1b-9c2d-3e4f5a6b7c8d",
      "user_id": "c1f76d49-4eb5-4a5f-9e77-5f725cf9a9f2",
      "language": "python",
      "code": "import sys\nname = sys.stdin.read().strip()\nprint(f'Hello, {name}!')",
      "stdout": "Hello, Antigravity!\n",
      "stderr": "",
      "exit_code": 0,
      "execution_time_ms": 28,
      "memory_kb": 3120,
      "status": "Accepted",
      "created_at": "2026-09-22T09:00:00Z"
    }
  ],
  "message": "Request successful"
}
```

---

### GET /api/v1/execute/{id}

Mengambil detail riwayat satu eksekusi kode berdasarkan ID.

- **Auth:** Wajib (`Authorization: Bearer <token>`)
- **Security:** Terisolasi per user (`WHERE id = $1 AND user_id = $2`).

#### Response `200 OK`
```json
{
  "success": true,
  "data": {
    "id": "c7a8b123-5e6f-4a1b-9c2d-3e4f5a6b7c8d",
    "user_id": "c1f76d49-4eb5-4a5f-9e77-5f725cf9a9f2",
    "language": "python",
    "code": "import sys\nname = sys.stdin.read().strip()\nprint(f'Hello, {name}!')",
    "stdout": "Hello, Antigravity!\n",
    "stderr": "",
    "exit_code": 0,
    "execution_time_ms": 28,
    "memory_kb": 3120,
    "status": "Accepted",
    "created_at": "2026-09-22T09:00:00Z"
  },
  "message": "Request successful"
}
```
