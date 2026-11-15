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
go run ./cmd/aibench --lint-dir ./aibench-results/lint-all
```

## Lint

Setiap file dibandingkan dengan fixture brief yang sesuai dengan prefix nama file. Output console dan `lint_report.md` memuat `PASS`, `FAIL`, atau `WARN` per aturan beserta kutipan bukti maksimal 80 karakter. Exit code non-nol bila ada `FAIL`.

Aturan `FAIL`: email di luar brief; nomor telepon/`wa.me` di luar WhatsApp brief; jam/hari di luar brief; klaim terlarang V7-3/V9-1; pill buka permanen saat brief tanpa jam; CTA "Hubungi Kami" saat brief tanpa saluran kontak; DESIGN PLAN tanpa baris `FACTS ... EMAIL:` atau lebih dari 100 kata di luar baris FACTS; italic; script eksternal selain `cdn.tailwindcss.com` atau stylesheet selain Google Fonts; hex di luar palet brief/V8-4.

Aturan `WARN` (heuristik): `text-accent` di luar section `bg-ink`; dua section `bg-paper` berurutan; jumlah section di luar 5-7.
