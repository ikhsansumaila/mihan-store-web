#!/usr/bin/env bash
# Pustaka bersama (di-source, bukan dijalankan): membuat & menghapus database UJI sementara di
# server MySQL bersama (container mysql_db) TANPA menyentuh database produksi `mihanstore`.
#
# Dipakai: source scripts/lib_testdb.sh; testdb_up; ...; testdb_down   (pasang `trap testdb_down EXIT`)
# Variabel (bisa di-override lewat env):
#   TDB     nama database uji, WAJIB berakhiran _test   (default mihanstore_test)
#   TUSER   nama user uji                               (default mihanstore_test_app)
#   THOST   mask host user uji                          (default subnet jaringan mysql-net)
#   TMAXCON batas koneksi user uji                      (default 15; server bersama max_connections=60)
#   LEGACY_FIXTURE=1  menambah akun "lama_sebelum_002" setelah migrasi 001 (dipakai tes integrasi)
# Setelah testdb_up tersedia: $TPW (password acak, jangan dicetak), $MIG_DIR.
set -euo pipefail
umask 077

LIB_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
BACKEND_DIR=$(cd "$LIB_DIR/.." && pwd)
MIG_DIR=$BACKEND_DIR/migrations
TDB=${TDB:-mihanstore_test}
TUSER=${TUSER:-mihanstore_test_app}
TMAXCON=${TMAXCON:-15}
GOIMAGE=${GOIMAGE:-golang:1.27-alpine}
MYSQL_NET=${MYSQL_NET:-mysql-net}
GOCACHE_HOST=${GOCACHE_HOST:-$HOME/.cache/mihanstore-gocache}   # cache build Go (mempercepat putaran berikutnya)

case "$TDB" in *_test) ;; *) echo "TDB harus berakhiran _test (dapat: $TDB)" >&2; exit 2;; esac
[ "$TDB" != "mihanstore" ] || { echo "menolak memakai database produksi" >&2; exit 2; }

if [ -z "${THOST:-}" ]; then
  sub=$(docker network inspect "$MYSQL_NET" --format '{{range .IPAM.Config}}{{.Subnet}}{{end}}' 2>/dev/null || true)
  # a.b.0.0/16 -> a.b.0.0/255.255.0.0 (hanya /16 yang didukung otomatis)
  case "$sub" in */16) THOST="${sub%/16}/255.255.0.0";; *) THOST='172.20.0.0/255.255.0.0';; esac
fi

# root [db]: SQL dari stdin, dijalankan sebagai root di container mysql_db (kata sandi root tidak pernah keluar dari container).
root() { docker exec -i mysql_db sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot '"${1:-}"; }
# sub_grants: ganti nama database/user produksi pada berkas migrasi hak akses dengan versi uji.
sub_grants() { sed -e "s|\`mihanstore\`\.|\`$TDB\`.|g" -e "s|'mihanstore_app'@'172.20.0.0/255.255.0.0'|'$TUSER'@'$THOST'|g" "$1"; }

testdb_down() {
  echo "DROP DATABASE IF EXISTS \`$TDB\`; DROP USER IF EXISTS '$TUSER'@'$THOST';" | root >/dev/null 2>&1 || true
}

testdb_up() {
  TPW=$(head -c 24 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 28)
  testdb_down
  echo "CREATE DATABASE \`$TDB\` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;" | root
  echo "CREATE USER '$TUSER'@'$THOST' IDENTIFIED BY '$TPW' WITH MAX_USER_CONNECTIONS $TMAXCON;
        GRANT SELECT, INSERT, UPDATE, DELETE ON \`$TDB\`.* TO '$TUSER'@'$THOST';" | root
  local f b
  for f in "$MIG_DIR"/*.sql; do
    b=$(basename "$f")
    case "$b" in
      005_*|009_*|012_*|015_*|018_*|022_*|024_*) sub_grants "$f" | root >/dev/null ;;
      *) root "$TDB" < "$f" >/dev/null ;;
    esac
    if [ "${LEGACY_FIXTURE:-0}" = 1 ] && [[ "$b" == 001_* ]]; then
      # Akun password lama sebelum migrasi 002 (diperiksa TestIntegrationSchemaSeedAndGrants).
      echo "INSERT INTO users (public_id, username, email, name, password_hash) VALUES (UUID(), 'lama_sebelum_002', 'lama@uji.test', 'Akun Lama', '\$argon2id\$v=19\$m=19456,t=2,p=1\$c2FsdHNhbHQ\$aGFzaGhhc2hoYXNo');" | root "$TDB"
    fi
  done
  export TPW
}

# gorun [args go test ...]: jalankan `go test` di container sementara pada jaringan mysql-net,
# memakai database uji. Berjalan sebagai user biasa agar berkas yang dibuat (golden) bukan milik root.
gorun() {
  mkdir -p "$GOCACHE_HOST"
  docker run --rm --network "$MYSQL_NET" --user "$(id -u):$(id -g)" \
    -e HOME=/tmp -e GOCACHE=/tmp/gocache -e GOFLAGS=-buildvcs=false \
    -e DB_HOST=mysql_db -e DB_NAME="$TDB" -e DB_USER="$TUSER" -e DB_PASSWORD="$TPW" \
    ${EXTRA_DOCKER_ARGS:-} \
    -v "$GOCACHE_HOST":/tmp/gocache -v mihanstore-gomod:/go/pkg/mod -v "$BACKEND_DIR":/src -w /src "$GOIMAGE" "$@"
}
