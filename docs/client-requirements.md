# Client & Business Requirements Guide — AI Website Generator

Dokumen ini merangkum kapabilitas dasar (*core platform features*) yang tersedia di backend **AI Website Generator**, serta panduan spesifikasi kebutuhan (*requirements gathering*) ketika mengadaptasi sistem ini untuk klien atau domain bisnis tertentu.

> [!NOTE]
> **Referensi API (request/response contract):** Untuk detail setiap endpoint (field schema, tipe data, error codes, contoh JSON) lihat **[docs/api.md](./api.md)**. Dokumen tersebut adalah *single source of truth* kontrak API antara backend dan frontend. Desain internal ada di **[docs/architecture.md](./architecture.md)**.

---

## 1. Ringkasan Produk

**AI Website Generator** adalah aplikasi web tempat user menjelaskan website yang ia inginkan, lalu AI membuatnya. Berbeda dengan chatbot generik, setelah login user langsung diarahkan ke pertanyaan utama: *"Website seperti apa yang ingin kamu buat?"*

### Alur User

```
Login
  └─► Isi brief (deskripsi, jenis website, nama bisnis, gaya, warna, bahasa, section, gambar referensi)
        └─► AI membuat website (progres tampil real-time)
              └─► Preview website (desktop / tablet / mobile)
                    └─► Revisi lewat instruksi teks ("ganti warna hero jadi biru tua")
                          └─► Unduh file .html
```

### Cakupan Tahap A

- Hasil berupa **satu file HTML mandiri** (Tailwind via CDN, JavaScript inline).
- Cocok untuk: landing page, portofolio, company profile, halaman toko online sederhana, blog satu halaman.
- Setiap revisi menghasilkan **versi baru**; versi lama tidak pernah hilang.
- Proyek multi-file (React/Vite) direncanakan pada **Tahap B**, setelah Tahap A stabil.

---

## 2. Core Platform Capabilities

| Area | Status | Fitur |
| --- | --- | --- |
| **Authentication & Access** | **Ready** | Registrasi, login, JWT HS256, profil (`/auth/me`); otorisasi berbasis kepemilikan resource, tanpa konsep role |
| **Project & Brief** | **Ready** | Membuat, melihat daftar, melihat detail, mengubah judul (`PATCH /projects/{id}`), dan menghapus project; brief terstruktur dengan validasi |
| **AI Generate** | **Ready** | Membuat versi 1 website dari brief, dialirkan real-time via SSE (token-by-token) |
| **AI Revise** | **Ready** | Merevisi dari versi tertentu lewat instruksi teks; AI selalu mengembalikan dokumen lengkap |
| **Versioning** | **Ready** | Riwayat versi per project, ambil versi tertentu, revisi dari versi lama (`base_version`) |
| **Export** | **Ready** | Unduh website sebagai file `.html` |
| **Multimodal Reference** | **Ready** | Upload gambar referensi (JPEG, PNG, WebP; logo, contoh desain, screenshot) via JSON base64 atau `multipart/form-data`; tersimpan per project dan terisolasi per user |
| **Safe Preview Contract** | **Ready** | Aturan preview aman (`<iframe sandbox>` tanpa `allow-same-origin`) didokumentasikan untuk frontend |
| **Security & Stability** | **Ready** | Rate limiting 2 tingkat (General, Generate), CORS configurable, Security Headers, structured logging (`slog`), graceful shutdown |
| **AI Chat & Conversations** | Dihapus | Kode dan tabelnya sudah dihapus; digantikan `generate` dan `revise` |
| **RAG Knowledge Base** | Dihapus | Kode dan tabel RAG sudah dihapus (migrasi `0012`) |
| **Code Sandbox (Judge0)** | Dihapus | Kode dan tabel execution sudah dihapus; preview dirender di browser |
| **Multi-file Project (React/Vite)** | Rencana Tahap B | Skema versi dirancang agar bisa diperluas |

---

## 3. Client Specification Worksheet

Bagian berikut diisi saat requirement gathering dengan klien.

### 3.1. Profil Klien & Target Produk

- **Nama Klien / Organisasi**: `[Isi nama klien/perusahaan]`
- **Industri / Domain**: `[Contoh: agensi digital, UMKM, EdTech, properti, jasa profesional]`
- **Tujuan Utama Produk**: `[Contoh: alat bantu UMKM membuat landing page tanpa developer; alat internal agensi untuk prototipe cepat]`
- **Target Pengguna**: `[Contoh: pemilik UMKM, freelancer, tim marketing, mahasiswa]`
- **Apakah hasil website dipakai langsung (dipublikasikan) atau hanya prototipe?**: `[Menentukan seberapa penting kualitas produksi dan kebutuhan hosting]`

### 3.2. Jenis Website & Brief

Backend saat ini mendukung field brief berikut (lihat `Brief` di [api.md](./api.md)):

| Field | Wajib | Keterangan |
| --- | --- | --- |
| `description` | Ya | 10–4000 karakter |
| `website_type` | Tidak | `landing_page` (default), `portfolio`, `company_profile`, `online_store`, `blog`, `other` |
| `business_name` | Tidak | Maks 120 karakter |
| `style` | Tidak | Gaya visual, maks 200 karakter |
| `color_palette` | Tidak | Preferensi warna, maks 200 karakter |
| `language` | Tidak | `id` (default) atau `en` |
| `sections` | Tidak | Maks 12 item, tiap item maks 50 karakter |

Yang perlu ditentukan bersama klien:

- **Jenis website yang perlu diprioritaskan**: `[Pilih dari daftar di atas; jenis di luar daftar menuntut penambahan whitelist dan penyesuaian system prompt]`
- **Bahasa konten tambahan**: `[Default: Indonesia dan Inggris. Perlu bahasa lain?]`
- **Field brief tambahan**: `[Contoh: info kontak, link media sosial, target audiens, kata kunci SEO, harga/produk]` *(Field baru cukup ditambahkan di struct `Brief`; brief disimpan sebagai JSONB sehingga tidak memerlukan migrasi.)*
- **Section standar per jenis website**: `[Contoh landing page: hero, fitur, testimoni, harga, FAQ, kontak]`

### 3.3. Gaya Generator & Guardrails

System prompt **dikunci di server**; user tidak dapat menimpanya. Kustomisasi per klien dilakukan dengan mengubah system prompt di server, bukan lewat request.

- **Standar desain default**: `[Contoh: modern minimalis, font tertentu, gaya tombol, spacing]`
- **Gaya penulisan konten (copywriting)**: `[Formal, santai, persuasif, teknis]`
- **Aturan teknis output**: `[Default: satu HTML, Tailwind CDN, JS inline, responsif, aksesibel]`
- **Integrasi yang diperbolehkan di halaman hasil**: `[Contoh: tombol WhatsApp, Google Maps embed, Google Fonts, form via layanan pihak ketiga]`
- **Guardrails konten**: `[Default: menolak phishing, peniruan brand/institusi, dan konten ilegal. Tambahkan topik terlarang lain jika ada]`
- **Watermark / atribusi pada hasil**: `[Perlukah footer "Dibuat dengan ..." pada website hasil?]`

### 3.4. Batas Output & Penggunaan

| Parameter | Default Saat Ini | Nilai untuk Klien |
| --- | --- | --- |
| Rate limit `generate`/`revise` | 5 req/menit per IP (`GENERATE_RATE_LIMIT_PER_MINUTE`) | `[ ]` |
| Rate limit API umum | 120 req/menit per IP | `[ ]` |
| Jumlah gambar referensi per request | 4 (`PROJECT_MAX_IMAGES`) | `[ ]` |
| Ukuran maksimum per gambar | 10 MB (`UPLOAD_MAX_BYTES`) | `[ ]` |
| Ukuran maksimum HTML hasil | 512 KiB / 524288 bytes (`PROJECT_MAX_HTML_BYTES`) | `[ ]` |
| Format gambar | JPEG, PNG, WebP | `[ ]` |

Pertanyaan terbuka:

- **Apakah limit per IP cukup?** Jika banyak pengguna berbagi satu IP (kantor, kampus), limit per IP bisa terasa terlalu ketat. Alternatifnya adalah kuota per user *(memerlukan perubahan; lihat 4)*.
- **Kuota bulanan / paket berbayar?** `[Ya/Tidak; jika ya, gunakan modul bisnis pada bagian 4]`
- **Batas jumlah project per user?** `[Default: tidak dibatasi]`
- **Retensi data**: `[Berapa lama project dan versi disimpan? Perlu fitur hapus otomatis?]`

### 3.5. Preview & Export

- **Preview**: dirender frontend di `<iframe sandbox="allow-scripts allow-popups">` **tanpa** `allow-same-origin`. Ini aturan keamanan wajib karena HTML hasil AI adalah konten tidak tepercaya.
- **Ukuran preview responsif**: `[Default: 375px mobile, 768px tablet, 100% desktop]`
- **Export**: file `.html` tunggal. `[Perlu format lain, misalnya .zip atau PDF? Tahap A hanya mendukung .html]`
- **Publish / hosting / subdomain otomatis**: `[Di luar cakupan Tahap A. Perlu?]`
- **Custom domain**: `[Di luar cakupan Tahap A. Perlu?]`

### 3.6. Autentikasi & Otorisasi

- **Metode Registrasi**:
  - [x] Registrasi mandiri terbuka (default)
  - [ ] Registrasi melalui undangan (invite-only)
  - [ ] Persetujuan admin (admin approval)
  - [ ] SSO / OAuth (Google, GitHub, Microsoft)
- **Otorisasi & Peran Pengguna**: tidak ada konsep role (kolom `users.role` sudah dihapus pada migrasi `0011`). Setiap user terautentikasi punya hak yang sama; pembeda akses hanyalah kepemilikan resource (project milik user lain terbaca sebagai `404`). Jika klien butuh peran (mis. admin/member), rancang sebagai fitur baru.
- **Berbagi project antar user / kolaborasi tim**: `[Default: tidak. Setiap project privat milik satu user]`

### 3.7. Fitur Lanjutan (Backlog)

Centang yang dibutuhkan klien untuk dijadwalkan setelah Tahap A:

- [ ] **Tahap B:** proyek multi-file (React/Vite) dengan preview hasil bundling
- [ ] Basis pengetahuan brand/klien sebagai fitur baru (modul RAG perlu dibangun ulang) agar hasil sesuai identitas bisnis
- [ ] Template / galeri contoh sebagai titik awal
- [ ] Rollback: menjadikan versi lama sebagai versi terbaru
- [ ] Berbagi link preview hasil website
- [ ] Publish langsung ke hosting
- [ ] Kuota dan billing per user

---

## 4. Menambahkan Modul Bisnis Klien

Untuk fitur bisnis khusus (billing langganan, kuota generate, katalog template, manajemen lisensi, ticketing), gunakan modul template di [`internal/modules/business/`](../internal/modules/business/README.md). Modul [`internal/modules/project/`](../internal/modules/project/) dapat dijadikan contoh nyata strukturnya.

1. **Duplikasi Template Modul**:
   Salin direktori template ke nama domain baru, mis. `internal/modules/billing` atau `internal/modules/templates`.
2. **Definisikan DTO & Model**:
   Rancang struktur data pada `model/model.go`.
3. **Tambahkan Migrasi Database**:
   Buat migrasi SQL berurutan di `migrations/`. Nomor terakhir yang terpakai adalah `0012_drop_legacy_tables`, jadi migrasi berikutnya mulai dari `0013` (mis. `0013_billing_subscriptions.up.sql`). Jangan mengubah migrasi yang sudah diterapkan.
4. **Implementasikan Repository & Service**:
   Query parameterized di `repository/`, logika bisnis di `service/`. Semua query harus dibatasi `user_id` pemilik resource.
5. **Daftarkan Handler ke Router**:
   Tambahkan dependensi pada struct `Dependencies` di [`internal/routes/routes.go`](../internal/routes/routes.go) dan mapping endpoint di bawah `/api/v1`.
6. **Dokumentasikan Kontrak**:
   Tambahkan endpoint baru ke [api.md](./api.md) pada commit yang sama.

Contoh kebutuhan yang umum untuk produk ini:

| Kebutuhan | Pendekatan |
| --- | --- |
| Kuota generate per user | Modul `billing`/`quota`: hitung `generate` + `revise` per periode; cek di service sebelum transisi ke `generating` |
| Template awal | Modul `templates`: simpan brief/HTML contoh; endpoint untuk membuat project dari template |
| Field brief baru | Tambah field di struct `Brief` (JSONB, tanpa migrasi), perbarui validasi, system prompt, dan `api.md` |
