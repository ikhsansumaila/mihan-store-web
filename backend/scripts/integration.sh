#!/usr/bin/env bash
# Tes integrasi (-tags integration) terhadap database uji sementara yang dibuat dari semua migrasi
# (+ akun lama sebelum migrasi 002). Contoh: backend/scripts/integration.sh -v
# Database & user uji SELALU dihapus di akhir. Tidak menyentuh produksi.
set -euo pipefail
cd "$(dirname "$0")/.."
export TDB=${TDB:-mihanstore_test} TUSER=${TUSER:-mihanstore_test_app} LEGACY_FIXTURE=1
source scripts/lib_testdb.sh
trap testdb_down EXIT
testdb_up
gorun go test -count=1 -tags integration "$@" ./...
