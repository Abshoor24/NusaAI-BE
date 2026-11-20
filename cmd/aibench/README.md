# AIBench

Alat ukur manual untuk generate website lewat provider AI. Semua fixture brief memakai bentuk yang sama dengan `service.FormatBrief`, memakai data fiktif, dan tidak memanggil API pada mode `--lint-dir`.

## Brief

| Nama | Isi |
| --- | --- |
| `full` | Menu + harga, WhatsApp, alamat, jam, palet |
| `service` | Jasa tanpa harga, WhatsApp, alamat, jam |
| `wa_only` | Hanya WhatsApp |
| `minimal` | Hanya jenis usaha dan nama; tanpa kontak, jam, menu |
| `nopalette` | Seperti `full` tanpa palet |

Brief yang tidak dikenal akan menampilkan daftar brief dan gagal; tidak ada fallback diam-diam. Setiap run mencetak nama brief, panjang teks, dan 8 karakter pertama hash.

## Flags

| Flag | Keterangan |
| --- | --- |
| `--brief NAME` | Satu brief (`full`, `service`, `wa_only`, `minimal`, `nopalette`); kosong = semua |
| `--model NAME` | Model provider; default `gemini-3.8-flash` |
| `--thinking LEVEL` | `MINIMAL`, `LOW`, `MEDIUM`, `HIGH`; kosong = default provider |
| `--max-output N` | Maksimum output token; default 32768 |
| `--runs N` | Jumlah run per brief; default 3 |
| `--out PATH` | Folder hasil; default `./aibench-results` |
| `--lint` | Lint setiap HTML hasil dan tulis `lint_report.md` di `--out` |
| `--lint-dir PATH` | Lint semua `*.html` di folder tanpa memanggil API |
| `--parallel N` | Kirim N request sekaligus untuk satu brief; maksimum 5 |
| `--help`, `-h` | Tampilkan bantuan |

Hasil HTML dinamai `{brief}_{model}_{thinking-atau-default}_run{n}.html` dan tidak pernah menimpa file lama; bila nama sudah ada, akhiran `-2`, `-3`, dan seterusnya dipakai. File `_raw.json` dan `_runs.csv` memakai pola model yang sama.

Run dengan `Status=error` tidak menulis file HTML dan tidak menjalankan lint. Run tersebut tetap dicatat di `_raw.json`/`_runs.csv`, ditandai `dilewati (run gagal)` di console, dan tidak dihitung sebagai `PASS` atau `FAIL`.

## Kuota harian

Respons 429 yang memuat `quotaId` dengan `PerDay` atau `retryDelay` lebih dari 120 detik dianggap kuota harian habis. AIBench tidak melakukan retry, menghentikan seluruh run (termasuk request paralel yang masih berjalan), mencetak model dari body 429 bila berbeda dari `--model`, `quotaId`, `retryDelay`, dan perkiraan waktu reset, lalu keluar dengan exit code non-nol.

## Contoh

Uji satu brief:

```bash
go run ./cmd/aibench --brief minimal --lint
```

Uji semua brief dengan lint:

```bash
go run ./cmd/aibench --lint --out ./aibench-results/lint-all
```

Soak 10 run per brief:

```bash
go run ./cmd/aibench --runs 10 --lint --out ./aibench-results/soak
```

Uji paralel (memakai kuota API; peringatan dicetak):

```bash
go run ./cmd/aibench --brief full --parallel 3 --out ./aibench-results/parallel
```

Lint hasil yang sudah ada tanpa API:

```bash
go run ./cmd/aibench --lint-dir ./aibench-results/lint-all --out ./aibench-results/lint-all
```

## Lint

Setiap file dibandingkan dengan fixture brief yang sesuai dengan prefix nama file. Output console dan `lint_report.md` memuat `PASS`, `FAIL`, atau `WARN` per aturan beserta kutipan bukti maksimal 80 karakter. `lint_report.md` selalu ditulis ke folder `--out`; run gagal tidak masuk laporan dan tidak dihitung `PASS`/`FAIL`. Exit code non-nol bila ada `FAIL`. Contoh laporan hasil lint 10 run tersimpan di [`contoh-lint-report.md`](contoh-lint-report.md).

Aturan `FAIL`:

| Aturan | Keterangan |
| --- | --- |
| `email` | Email di luar brief |
| `phone/whatsapp` | Nomor telepon/`wa.me` di luar WhatsApp brief |
| `hours/days` | Jam/hari di luar brief |
| `forbidden-claims` | Klaim terlarang V7-3/V9-1 |
| `permanent-open-status` | Pill buka permanen saat brief tanpa jam |
| `contact-cta` | CTA kontak saat brief tanpa saluran kontak |
| `no-channel-text` | Brief tanpa WhatsApp/email/alamat/telepon, tetapi teks memuat "hubungi", "kontak kami", "tanya", "pesan via", atau "chat" |
| `invented-days` | Nama hari, "setiap hari", "libur", atau "tutup" padahal brief tanpa nama hari |
| `design-plan/FACTS` | DESIGN PLAN tanpa baris `FACTS ... EMAIL:` atau lebih dari 100 kata di luar baris FACTS |
| `italic` | Elemen/utility italic |
| `external-assets` | Script selain `cdn.tailwindcss.com` atau stylesheet selain Google Fonts |
| `palette-hex` | Hex di luar palet brief/V8-4; setiap hex mencetak konteks kelas/properti maksimal 40 karakter |

Hex dari salah satu palet cadangan V8-4 (Hangat, Hijau, Malam, Biru laut, Mawar) diturunkan menjadi `WARN` bila brief hanya memberi sebagian warna. Hex lain tetap `FAIL`.

Aturan `WARN` (heuristik):

| Aturan | Keterangan |
| --- | --- |
| `accent-outside-ink` | `text-accent` tidak berada di dalam leluhur terdekat berkelas `bg-ink` (bukan hanya section) |
| `consecutive-paper` | Dua section `bg-paper` berurutan |
| `section-count` | Jumlah section di luar 5-7 |
| `palette-hex` | Palet cadangan dicampur dengan warna brief |
| `repetition` | Frasa 4+ kata muncul lebih dari 2 kali pada teks terlihat |
| `english-ui` | Frasa UI Inggris pada halaman berbahasa Indonesia |
| `plan-vs-page` | Butir (2) DESIGN PLAN menyebut widget/tab/kalkulator, tetapi HTML tidak punya elemen interaktif selain tombol menu; `none` tidak ditandai |
| `dead-link-target` | Tautan `#...`/tombol menuju section yang hanya berisi teks pengisi tanpa fakta brief |

