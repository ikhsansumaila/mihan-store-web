#!/usr/bin/env bash
# Tes kontrak API (golden). Satu perintah:
#   backend/scripts/contract.sh                 # PEMBANDING: gagal dengan diff bila kontrak berubah
#   UPDATE_GOLDEN=1 backend/scripts/contract.sh # REKAM ulang backend/testdata/golden/*.json
# Membuat database uji sementara BARU (mihanstore_contract_test + user uji bermaks 15 koneksi) dari
# semua migrasi, menjalankan tes (-tags contract) di container Go, lalu SELALU menghapus keduanya.
# Tidak menyentuh produksi dan tidak men-build/menjalankan image toko.
set -euo pipefail
cd "$(dirname "$0")/.."
export TDB=${TDB:-mihanstore_contract_test} TUSER=${TUSER:-mihanstore_contract_app}
source scripts/lib_testdb.sh
trap testdb_down EXIT
testdb_up
EXTRA_DOCKER_ARGS="-e UPDATE_GOLDEN=${UPDATE_GOLDEN:-0}" \
  gorun go test -count=1 -tags contract -run '^TestContract$' "$@" ./internal/app/
