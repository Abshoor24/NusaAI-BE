#!/usr/bin/env bash
# Smoke test untuk AI Website Generator (Tahap A).
#
# TIDAK ADA RAHASIA DI DALAM SKRIP INI. Jangan jalankan terhadap database
# produksi dalam kondisi apa pun. Prasyarat di environment sesi shell:
#   TEST_DATABASE_URL   -> database Supabase KHUSUS TEST (Session pooler,
#                        port 5432, BUKAN 6543), sudah dimigrasi 0001-0010.
#   GEMINI_API_KEY      -> key Gemini khusus test (untuk alur normal).
#                         Tanpa key ini, hanya kasus 1 dan 3 yang dijalankan.
#
# Menjalankan server (env shell mengalahkan .env karena godotenv.Load tidak
# menimpa variabel yang sudah ada — lihat internal/config/config.go):
#   DATABASE_URL="$TEST_DATABASE_URL" AI_API_KEY="$GEMINI_API_KEY" \
#     JWT_SECRET="$(openssl rand -hex 32)" go run ./cmd/server
#
# Penggunaan:
#   BASE_URL=http://localhost:8080 ./scripts/smoke.sh           # semua
#   ./scripts/smoke.sh happy                                    # alur normal
#   ./scripts/smoke.sh bad_key|disconnect|concurrent            # satu kasus gagal
#
# Tiap kasus (termasuk bad_key/disconnect/concurrent) mendaftarkan user
# sendiri, jadi tidak bergantung pada token dari alur normal dan aman
# dijalankan berurutan maupun satuan.
#
# Variabel env:
#   BASE_URL       -> alamat server (default http://localhost:8080).
#   GEMINI_API_KEY -> hanya dibaca sebagai penanda; skrip tidak pernah
#                     mengirim key ini ke mana pun. Alur normal (happy),
#                     disconnect, dan concurrent butuh server yang berjalan
#                     dengan key Gemini asli.
#   Kasus bad_key justru butuh server yang dijalankan dengan key Gemini
#   PALSU (mis. AI_API_KEY=fake-key-test) agar generate mengembalikan 502.
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

pass=0
fail=0

log()  { printf '[smoke] %s\n' "$*"; }
ok()   { pass=$((pass + 1)); log "PASS: $*"; }
bad()  { fail=$((fail + 1)); log "FAIL: $*"; }

# expect_status <diharapkan> <aktual> <label>
expect_status() {
  if [ "$2" = "$1" ]; then ok "$3 (HTTP $2)"; else bad "$3 (mau HTTP $1, dapat HTTP $2)"; fi
}

# jget <file-json> <path-python> — ambil nilai JSON tanpa jq.
jget() { python3 -c "import json,sys; print(json.load(sys.stdin)$2)" < "$1"; }

need_cmd() { command -v "$1" >/dev/null 2>&1 || { log "butuh perintah '$1'"; exit 2; }; }
need_cmd curl
need_cmd python3

PASSWORD="SmokeTest123"

# print_body_redacted <file-json> — cetak respons untuk diagnosis dengan
# field token dibuang bila ada (tidak pernah mencetak token).
print_body_redacted() {
  python3 -c "
import json, sys
try:
    d = json.load(open('$1'))
except Exception as e:
    print('(respons bukan JSON, tampil mentah di bawah)', file=sys.stderr)
    raise SystemExit(1)
if isinstance(d.get('data'), dict):
    d['data'].pop('token', None)
print(json.dumps(d))
" 2>/dev/null || cat "$1"
}

step_register() {
  # Email dibuat di sini (bukan global) agar tiap pemanggilan — tiap kasus —
  # selalu mendapat user unik dan tidak kena 409 email-sudah-ada.
  local email="smoke-$(date +%s%N)-$RANDOM@example.com"
  log "register $email"
  code=$(curl -s -o "$WORKDIR/register.json" -w '%{http_code}' -X POST "$BASE_URL/api/v1/auth/register" \
    -H 'Content-Type: application/json' \
    -d "{\"email\":\"$email\",\"password\":\"$PASSWORD\"}")
  if [ "$code" != 201 ]; then
    log "register gagal (HTTP $code), respons (tanpa token):"
    print_body_redacted "$WORKDIR/register.json"
    log "hentikan kasus ini"
    return 1
  fi
  ok "register (HTTP 201)"
  TOKEN=$(jget "$WORKDIR/register.json" "['data']['token']")
  [ -n "$TOKEN" ] && ok "token tersimpan" || { bad "token kosong"; return 1; }
}

step_create() {
  log "create project"
  code=$(curl -s -o "$WORKDIR/project.json" -w '%{http_code}' -X POST "$BASE_URL/api/v1/projects" \
    -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" \
    -d '{"title":"Kedai Kopi Smoke","brief":{"description":"Landing page kedai kopi lokal dengan menu dan lokasi.","website_type":"landing_page","business_name":"Kopi Smoke","language":"id","sections":["hero","menu","location"]}}')
  if [ "$code" != 201 ]; then
    log "create project gagal (HTTP $code), respons:"
    cat "$WORKDIR/project.json"
    log "hentikan kasus ini"
    return 1
  fi
  ok "create project (HTTP 201)"
  PROJECT_ID=$(jget "$WORKDIR/project.json" "['data']['id']")
  [ -n "$PROJECT_ID" ] && ok "project id $PROJECT_ID" || { bad "project id kosong"; return 1; }
}

step_generate() {
  log "generate (SSE)"
  code=$(curl -s -N -o "$WORKDIR/generate.sse" -w '%{http_code}' -X POST "$BASE_URL/api/v1/projects/$PROJECT_ID/generate" \
    -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" -d '{}')
  expect_status 200 "$code" "generate SSE"
  if grep -q 'event: done' "$WORKDIR/generate.sse"; then ok "stream memuat event: done"; else bad "stream tanpa event: done"; fi
}

step_version() {
  log "get version 1"
  code=$(curl -s -o "$WORKDIR/version.json" -w '%{http_code}' "$BASE_URL/api/v1/projects/$PROJECT_ID/versions/1" \
    -H "Authorization: Bearer $TOKEN")
  expect_status 200 "$code" "get version 1"
}

step_revise() {
  log "revise (SSE)"
  code=$(curl -s -N -o "$WORKDIR/revise.sse" -w '%{http_code}' -X POST "$BASE_URL/api/v1/projects/$PROJECT_ID/revise" \
    -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" \
    -d '{"instruction":"Ganti warna hero jadi biru tua"}')
  expect_status 200 "$code" "revise SSE"
  if grep -q 'event: done' "$WORKDIR/revise.sse"; then ok "revise memuat event: done"; else bad "revise tanpa event: done"; fi
}

step_export() {
  log "export html"
  code=$(curl -s -OJ --output-dir "$WORKDIR" -w '%{http_code}' "$BASE_URL/api/v1/projects/$PROJECT_ID/export" \
    -H "Authorization: Bearer $TOKEN")
  # -OJ menyimpan sesuai Content-Disposition; cek ada file .html terunduh.
  expect_status 200 "$code" "export html"
  if ls "$WORKDIR"/*.html >/dev/null 2>&1; then ok "file .html terunduh"; else bad "tidak ada file .html terunduh"; fi
}

project_status() {
  curl -s "$BASE_URL/api/v1/projects/$1" -H "Authorization: Bearer $TOKEN" |
    python3 -c "import json,sys; print(json.load(sys.stdin)['data']['status'])"
}

# Kasus 1: API key salah -> JSON 502 dan status kembali normal (bukan generating).
# Syarat: server SUDAH dijalankan dengan key Gemini palsu.
case_bad_key() {
  log "kasus 1: API key palsu"
  step_register || return 1
  step_create || return 1
  code=$(curl -s -o "$WORKDIR/badkey.json" -w '%{http_code}' -X POST "$BASE_URL/api/v1/projects/$PROJECT_ID/generate" \
    -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" -d '{}')
  expect_status 502 "$code" "generate dengan key palsu"
  sleep 2
  st=$(project_status "$PROJECT_ID")
  if [ "$st" != "generating" ]; then ok "status kembali normal ($st)"; else bad "status tertahan generating"; fi
}

# Kasus 2: klien terputus di tengah stream -> status tidak boleh generating.
case_disconnect() {
  log "kasus 2: klien terputus"
  step_register || return 1
  step_create || return 1
  timeout -s KILL 6 curl -s -N -o "$WORKDIR/cut.sse" -X POST "$BASE_URL/api/v1/projects/$PROJECT_ID/generate" \
    -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" -d '{}' || true
  sleep 5
  st=$(project_status "$PROJECT_ID")
  if [ "$st" != "generating" ]; then ok "status setelah disconnect ($st)"; else bad "status tertahan generating"; fi
}

# Kasus 3: dua generate bersamaan -> tepat satu sukses (200), satu 409.
case_concurrent() {
  log "kasus 3: dua generate bersamaan"
  step_register || return 1
  step_create || return 1
  curl -s -o "$WORKDIR/c1.txt" -w '%{http_code}' -X POST "$BASE_URL/api/v1/projects/$PROJECT_ID/generate" \
    -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" -d '{}' >"$WORKDIR/c1.code" &
  p1=$!
  curl -s -o "$WORKDIR/c2.txt" -w '%{http_code}' -X POST "$BASE_URL/api/v1/projects/$PROJECT_ID/generate" \
    -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" -d '{}' >"$WORKDIR/c2.code" &
  p2=$!
  wait $p1 || true; wait $p2 || true
  c1=$(cat "$WORKDIR/c1.code"); c2=$(cat "$WORKDIR/c2.code")
  log "hasil paralel: $c1 dan $c2"
  if { [ "$c1" = 200 ] && [ "$c2" = 409 ]; } || { [ "$c1" = 409 ] && [ "$c2" = 200 ]; }; then
    ok "tepat satu 200 dan satu 409"
  else
    bad "mau {200,409}, dapat {$c1,$c2}"
  fi
}

case "${1:-all}" in
  happy)
    step_register; step_create; step_generate; step_version; step_revise; step_export
    ;;
  bad_key)    case_bad_key || bad "kasus bad_key dibatalkan" ;;
  disconnect) case_disconnect || bad "kasus disconnect dibatalkan" ;;
  concurrent) case_concurrent || bad "kasus concurrent dibatalkan" ;;
  all)
    step_register; step_create; step_generate; step_version; step_revise; step_export
    if [ -z "${GEMINI_API_KEY:-}" ]; then
      log "GEMINI_API_KEY kosong — lewati kasus yang butuh Gemini asli"
    fi
    case_concurrent || bad "kasus concurrent dibatalkan"
    ;;
  *) log "pakai: $0 [happy|bad_key|disconnect|concurrent|all]"; exit 2 ;;
esac

log "selesai: $pass lulus, $fail gagal"
[ "$fail" -eq 0 ]
