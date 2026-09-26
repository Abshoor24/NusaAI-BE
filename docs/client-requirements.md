# Client & Business Requirements Guide

Dokumen ini merangkum kapabilitas dasar (*core platform features*) yang telah tersedia di backend, serta panduan spesifikasi kebutuhan (*requirements gathering*) ketika mengadaptasi sistem ini untuk klien atau domain bisnis tertentu.

> [!NOTE]
> **Referensi API (request/response contract):** Untuk detail lengkap setiap endpoint — field schema, tipe data, error codes, dan contoh JSON — lihat **[docs/api.md](./api.md)**. Dokumen tersebut adalah *single source of truth* untuk kontrak API antara backend dan frontend.

---

## 1. Core Platform Capabilities (Out of the Box)

Backend saat ini telah menyediakan fondasi fungsional lengkap untuk platform **AI Assistant, Generator, RAG, dan Code Sandbox**:

| Area | Status | Fitur yang Sudah Tersedia |
| --- | --- | --- |
| **Authentication & Access** | **Ready** | Registrasi, Login, JWT HS256, User Profile (`/auth/me`), Role-based baseline (`admin`). |
| **AI Multi-Turn Assistant** | **Ready** | Percakapan berkelanjutan dengan memori konteks, pembuatan thread otomatis, pengelolaan percakapan (list, get, delete). |
| **Real-time Streaming (SSE)** | **Ready** | Pengiriman respon bertahap token-by-token menggunakan Server-Sent Events dengan penanganan disconnect yang andal. |
| **Multimodal Vision** | **Ready** | Upload lampiran gambar (JPEG, PNG, WebP) via JSON base64 atau `multipart/form-data`, persistensi ke disk & database, serta penyajian gambar terisolasi per user. |
| **RAG Knowledge Base** | **Ready** | Ingest teks langsung atau upload dokumen fisik (PDF, DOCX, TXT) dengan ekstraksi otomatis, semantic chunking + overlap, embedding `text-embedding-004`, dan vector similarity search di PostgreSQL `pgvector`. |
| **Code Sandbox Execution** | **Ready** | Eksekusi kode sandbox terisolasi penuh via Judge0 CE untuk bahasa Python, JavaScript, dan Go dengan batasan CPU/Memory dan pencatatan riwayat eksekusi. |
| **Security & Stability** | **Ready** | Rate limiting 3 tingkat (General, AI Chat, Code Execution), CORS configurable, Security Headers, structured logging (`slog`), dan graceful shutdown. |

---

## 2. Client Specification Worksheet

Bagian berikut diisi saat melakukan requirement gathering dengan klien untuk menyesuaikan platform dengan kebutuhan spesifik produk mereka.

### 2.1. Profil Klien & Target Produk

- **Nama Klien / Organisasi**: `[Isi nama klien/perusahaan]`
- **Industri / Domain**: `[Contoh: EdTech, Legal, Healthcare, Software Dev, E-commerce, Corporate Internal]`
- **Tujuan Utama Produk**: `[Contoh: AI Coding Assistant untuk siswa, Customer Service bot cerdas berbasis SOP internal, dsb.]`
- **Target Pengguna**: `[Contoh: Mahasiswa, staf internal, pelanggan publik, programmer junior]`

### 2.2. Kebutuhan Persona & Prompting AI

Platform mendukung custom prompt override pada setiap sesi percakapan. Tentukan panduan persona default:
- **Tone & Karakter AI**: `[Formal, kasual, teknis, ramah, dsb.]`
- **Instruksi Sistem Default (`System Prompt`)**: `[Aturan perilaku dasar, pembatasan jawaban, bahasa yang digunakan]`
- **Domain Guardrails**: `[Topik terlarang atau instruksi untuk menolak pertanyaan di luar domain]`

### 2.3. Kebutuhan RAG & Basis Pengetahuan

- **Format Dokumen Sumber**: `[PDF, Word DOCX, TXT, Manual Input]`
- **Kategori Dokumen**: `[Buku panduan, SOP, dokumentasi API, regulasi hukum, FAQ]`
- **Akses Dokumen**:
  - [x] Dokumen privat per pengguna (default backend saat ini)
  - [ ] Dokumen publik / global yang dapat diakses oleh seluruh pengguna sistem *(memerlukan flag `is_global` di skema `documents`)*

### 2.4. Kebutuhan Code Execution Sandbox

- **Bahasa Pemrograman Tambahan**: `[Default: Python, JS, Go. Apakah memerlukan C++, Java, Rust, PHP, Ruby?]`
- **Batasan Resource per Eksekusi**:
  - CPU Time Limit: `[Default: 5 detik]`
  - Memory Limit: `[Default: 128 MB]`
  - Maximum Code Length: `[Default: 64 KB]`
- **Eksekusi Interaktif / STDIN**: `[Apakah frontend menyediakan input stdin interaktif atau batch]`

### 2.5. Autentikasi & Otorisasi Lanjutan

- **Metode Registrasi**:
  - [x] Registrasi mandiri terbuka (default)
  - [ ] Registrasi melalui undangan (invite-only)
  - [ ] Persetujuan admin (admin approval)
  - [ ] SSO / OAuth (Google, GitHub, Microsoft)
- **Struktur Role Pengguna**:
  - [x] Admin
  - [ ] Member / Regular User
  - [ ] Guest / Read-Only

---

## 3. Menambahkan Modul Bisnis Klien

Ketika klien membutuhkan fitur bisnis khusus (seperti billing langganan, manajemen lisensi, katalog produk, ticketing), gunakan modul template yang telah disiapkan di [internal/modules/business/](file:///C:/project/pakhir/be/internal/modules/business/README.md):

1. **Duplikasi Template Modul**:
   Salin direktori template ke nama domain bisnis baru, misalnya `internal/modules/billing` atau `internal/modules/courses`.
2. **Definisikan DTO & Model**:
   Rancang struktur data pada `model/model.go`.
3. **Tambahkan Migrasi Database**:
   Buat migrasi SQL baru berurutan di `migrations/` (misal `0010_billing_subscriptions.up.sql`).
4. **Implementasikan Repository & Service**:
   Bangun query parameterized di `repository/` dan logika bisnis di `service/`.
5. **Daftarkan Handler ke Router**:
   Tambahkan dependensi pada struct `Dependencies` di [internal/routes/routes.go](file:///C:/project/pakhir/be/internal/routes/routes.go) dan mapping endpoint di bawah `/api/v1`.
