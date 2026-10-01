# Business Module Template

> Folder ini adalah **template/scaffold** untuk modul bisnis pertama.
> Client belum ditentukan, jadi tidak ada asumsi schema maupun endpoint bisnis.
> Jangan daftarkan folder ini ke router — kode di sini hanya contoh pola,
> tidak berisi endpoint yang berfungsi.

## Cara pakai (saat requirement client sudah diketahui)

1. **Salin folder ini** ke nama modul asli, contoh:

   ```bash
   # Windows PowerShell
   Copy-Item -Recurse internal/modules/business internal/modules/products
   ```

   Sesuaikan nama package di setiap file (`business` -> `products`), nama tipe,
   dan import path (`internal/modules/business/...` -> `internal/modules/products/...`).

2. **Isi `model/`** — definisikan DTO request/response sesuai kebutuhan client.
   Lihat `internal/modules/ai/model/model.go` untuk pola yang dipakai.

3. **Buat migration schema bisnis** — salin `migrations/*.example` ke folder
   `migrations/` dengan nomor urut berikutnya:

   ```bash
   migrations/0002_<nama>.up.sql
   migrations/0002_<nama>.down.sql
   ```

   Migrations ter-apply otomatis saat server start (lihat `storage/supabase.go`).
   Jangan mengubah isi migration yang sudah pernah dijalankan; buat nomor baru.

4. **Isi `repository/`** — implementasi akses database dengan parameterized
   query, ikuti pola `internal/repository/user_repository.go` (mapping error
   pgx ke `apperror`).

5. **Isi `service/`** — logika bisnis, bergantung pada interface repository
   (bukan implementasi) agar mudah di-test dengan mock.

6. **Isi `handler/`** — endpoint HTTP, gunakan `pkg/response` untuk
   decode/validasi dan format respons (lihat `internal/modules/ai/handler`).

7. **Wire ke router** — di `internal/routes/routes.go`:
   - tambahkan dependency ke struct `Dependencies`
   - buat service/handler di `routes.New`
   - daftarkan route di grup `/api/v1`

   Auth belum diputuskan. Saat client menentukan, pilih salah satu:
   - publik (tanpa middleware) seperti `/health` atau `/api/v1/auth/login`
   - login (butuh `middleware.RequireAuth`) seperti `/api/v1/auth/me` atau `/api/v1/ai/chat`

8. **Wire ke `cmd/server/main.go`** — buat repository + service baru dan masukkan
   ke `routes.New` saat memanggilnya.

## Konvensi

- Handler / service / repository berada di layer terpisah; dependency di-inject.
- Semua query database pakai parameterized query, jangan string concat.
- Setiap TODO bisnis ditandai `TODO: CLIENT REQUIREMENT`.
- Jangan membuat schema/endpoint bisnis sebelum requirement dikumpulkan.