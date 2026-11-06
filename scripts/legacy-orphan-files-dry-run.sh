#!/usr/bin/env bash
#
# DRY RUN ONLY: print legacy upload files that migration 0012 will orphan.
# This script NEVER deletes, moves, or rewrites a file. The only rm in this
# script removes its own temporary directory.
#
# REQUIRED:
#   DATABASE_URL  PostgreSQL URL for a TEST/STAGING database that is still at
#                 migration 0011. Do not use a production database.
#
# OPTIONAL:
#   LEGACY_UPLOAD_ROOT  Directory to prefix when a recorded path is relative.
#                       Default: repository root (the server usually runs
#                       from there, so "uploads/..." resolves correctly).
#
# Usage (from the repository root):
#   DATABASE_URL='postgresql://.../test?sslmode=require' \
#     ./scripts/legacy-orphan-files-dry-run.sh
#
# Output:
#   WOULD DELETE  legacy path exists on disk and is not an active project file
#   SKIP          path is still referenced by project_attachments
#   MISSING       legacy row points to a file that no longer exists
#
# This is intentionally conservative: anything uncertain is printed for
# manual review, never removed. Review the output, take a database backup,
# then delete files manually or with a separate, reviewed command.
set -euo pipefail

if [[ -z "${DATABASE_URL:-}" ]]; then
    echo "ERROR: set DATABASE_URL to a non-production test/staging database." >&2
    echo "       This script is read-only, but it must run before migration 0012." >&2
    exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
sql_file="$repo_root/scripts/legacy-upload-paths.sql"
upload_root="${LEGACY_UPLOAD_ROOT:-$repo_root}"

if [[ ! -f "$sql_file" ]]; then
    echo "ERROR: missing $sql_file" >&2
    exit 2
fi
command -v psql >/dev/null 2>&1 || {
    echo "ERROR: psql is required but was not found in PATH." >&2
    exit 2
}

tmp_dir="$(mktemp -d)"
trap 'rm -rf -- "$tmp_dir"' EXIT
legacy_tsv="$tmp_dir/legacy.tsv"
active_tsv="$tmp_dir/active.tsv"

# Unit separator: non-whitespace IFS preserves empty fields (e.g. a documents
# row with file_path IS NULL) and is not valid in a Windows filename.
sep=$'\x1f'

# Read legacy paths (message_attachments + documents) from the read-only SQL.
psql "$DATABASE_URL" -X -q -v ON_ERROR_STOP=1 \
    -A -t -F "$sep" -f "$sql_file" >"$legacy_tsv"

# Read active project attachment paths so a shared path is never proposed for
# deletion. This is also read-only.
psql "$DATABASE_URL" -X -q -v ON_ERROR_STOP=1 \
    -A -t -F "$sep" \
    -c 'BEGIN TRANSACTION READ ONLY;' \
    -c 'SELECT storage_path FROM project_attachments;' \
    -c 'COMMIT;' >"$active_tsv"

# Trim surrounding whitespace, resolve relative paths against upload_root,
# and canonicalize ".." when realpath is available.
normalize_path() {
    local p="$1"
    p="${p#"${p%%[![:space:]]*}"}"
    p="${p%"${p##*[![:space:]]}"}"
    [[ -z "$p" ]] && return 0

    if [[ "$p" != /* && ! "$p" =~ ^[A-Za-z]:[\\/] ]]; then
        p="$upload_root/${p#./}"
    fi

    if command -v realpath >/dev/null 2>&1; then
        realpath -m -- "$p" 2>/dev/null || printf '%s\n' "$p"
    else
        printf '%s\n' "$p"
    fi
}

declare -A active=()
while IFS= read -r p; do
    [[ -z "$p" ]] && continue
    active["$(normalize_path "$p")"]=1
done <"$active_tsv"

declare -A seen=()
would_delete=0
skipped_active=0
missing=0
empty_path=0

echo "DRY RUN ONLY: no files will be deleted."
echo "Legacy database rows: $(wc -l <"$legacy_tsv" | tr -d ' ')"
echo "Active project paths: $(wc -l <"$active_tsv" | tr -d ' ')"
echo

while IFS="$sep" read -r source id user_id path file_name file_size; do
    if [[ -z "${path:-}" ]]; then
        empty_path=$((empty_path + 1))
        continue
    fi

    normalized="$(normalize_path "$path")"
    [[ -z "$normalized" ]] && continue

    if [[ -n "${active["$normalized"]+x}" ]]; then
        printf 'SKIP\t%s\t%s\t%s\n' "$source" "$id" "$path"
        skipped_active=$((skipped_active + 1))
        continue
    fi

    # De-duplicate files referenced by more than one legacy row.
    if [[ -n "${seen["$normalized"]+x}" ]]; then
        continue
    fi
    seen["$normalized"]=1

    if [[ -f "$normalized" ]]; then
        printf 'WOULD DELETE\t%s\t%s\t%s\n' "$source" "$id" "$path"
        would_delete=$((would_delete + 1))
    else
        printf 'MISSING\t%s\t%s\t%s\n' "$source" "$id" "$path"
        missing=$((missing + 1))
    fi
done <"$legacy_tsv"

echo
printf 'SUMMARY\twould_delete=%d\tskipped_active=%d\tmissing=%d\tempty_path=%d\n' \
    "$would_delete" "$skipped_active" "$missing" "$empty_path"
echo "DRY RUN COMPLETE: nothing was deleted."
