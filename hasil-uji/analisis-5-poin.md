# Analisis 5 Poin Hasil Generate

Sumber: `a-service.html` dan `b-minimal.html` di folder `hasil-uji/`.
Aturan hari mengikuti parser repo: `Selasa-Sabtu` hanya menghasilkan `selasa` dan `sabtu` (tanpa ekspansi rentang).

## Ringkasan

| Poin | a-service | b-minimal |
|---|---|---|
| 1. Baris FACTS di DESIGN PLAN | PASS | PASS |
| 2. Tidak ada section/tombol Menu + "belum tersedia" | PASS | PASS |
| 3. Tidak ada nama hari di luar brief | FAIL | PASS* |
| 4. Alamat & telepon di teks <= 2x | FAIL | PASS |
| 5. Tidak ada email/nomor/harga di luar brief | PASS | PASS |

\* b-minimal tidak menyebut nama hari, tetapi memuat frasa "setiap hari" (L194). Rule `invented-days` repo menandainya FAIL.

## a-service

1. FACTS: PASS — L10:
   `FACTS: MENU: NONE | WA: 0812-0000-2222 | EMAIL: NONE | ADDRESS: Jalan Rekaan No. 2, Kota Fiktif | HOURS: Selasa-Sabtu 08.00-17.00 | PRIMARY: #10223F | ACCENT: #E0A93B`

2. Menu/belum tersedia: PASS — tidak ada "belum tersedia" (0 match). L9 hanya menyebut:
   `Sections: Beranda, Layanan, Jam Operasional, Kontak.`
   Kemunculan "menu" hanya kontrol navigasi seluler (komentar `// Menu Navigasi Seluler` L423 dan `aria-label="Buka menu navigasi"`), bukan section konten.

3. Hari: FAIL — brief hanya punya `Selasa` dan `Sabtu`; HTML menampilkan seluruh minggu:
   - L311-312: `Senin` / `Tutup`
   - L319: `Rabu`
   - L323: `Kamis`
   - L327: `Jumat`
   - L335-336: `Minggu` / `Tutup`
   Lint resmi repo juga FAIL: `hours/days — hari Senin, hari Rabu, hari Kamis, hari Jumat, hari Minggu`.

4. Alamat/telepon: FAIL — alamat muncul 4x di teks terlihat:
   - L145: `... di Jalan Rekaan No. 2, Kota Fiktif. Buka Selasa sampai Sabtu ...`
   - L355: `Jalan Rekaan No. 2, Kota Fiktif`
   - L394: `Jalan Rekaan No. 2, Kota Fiktif`
   - L414: `Jalan Rekaan No. 2, Kota Fiktif`
   Telepon muncul 1x di teks: L379 `0812-0000-2222`.
   Catatan: alamat mentah 6x termasuk baris FACTS (5x tanpa FACTS); tautan `wa.me` 6x dengan nomor yang sama dan sesuai brief.

5. Email/nomor/harga: PASS — tidak ada email, tidak ada `Rp`/`IDR`, dan nomor yang muncul hanya `0812-0000-2222` sesuai brief. Lint resmi: `email PASS`, `phone/whatsapp PASS`.

## b-minimal

1. FACTS: PASS — L10:
   `FACTS: MENU: NONE | WA: NONE | EMAIL: NONE | ADDRESS: NONE | HOURS: NONE | PRIMARY: NONE | ACCENT: NONE -->`

2. Menu/belum tersedia: PASS — tidak ada "belum tersedia" (0 match). L9 hanya menyebut:
   `(5) hero, tentang, layanan, penutup.`
   Kemunculan "menu" hanya kontrol navigasi seluler (`aria-label="Buka menu"` L85, `aria-label="Tutup menu"` L98), bukan section konten.

3. Hari: PASS untuk nama hari — tidak ditemukan nama hari apa pun.
   Catatan: L194 memuat `... memanggang roti langsung setiap hari ...`. Lint resmi repo FAIL pada rule `invented-days` karena brief minimal tidak menyebut hari.

4. Alamat/telepon: PASS — alamat 0x, telepon 0x, tautan `wa.me`/`tel:` 0x.

5. Email/nomor/harga: PASS — tidak ada email, tidak ada `Rp`/`IDR`, tidak ada nomor. Lint resmi: `email PASS`, `phone/whatsapp PASS`.

## Validasi lint resmi repo

- `service_a-service.html`: FAIL `hours/days` (Senin, Rabu, Kamis, Jumat, Minggu); PASS `email`, `phone/whatsapp`, `design-plan/FACTS`. Catatan tambahan: FAIL `italic` (bukan bagian 5 poin).
- `minimal_b-minimal.html`: FAIL `invented-days` ("setiap hari"); PASS `email`, `phone/whatsapp`, `hours/days`, `design-plan/FACTS`.
- Laporan lengkap: `hasil-uji/lint-report.md`.

## Catatan

- Check 4 memakai teks terlihat. Jika href/tautan dihitung, a-service punya 6 tautan `wa.me` (nomor sama) sehingga telepon juga melebihi 2x.
- Tidak ditemukan "belum tersedia", email, atau harga `Rp`/`IDR` di kedua HTML.
