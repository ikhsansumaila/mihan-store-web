#!/usr/bin/env bash
# Tes e2e (-tags e2e) BACKEND SAJA, terhadap container backend UJI + database uji sementara.
# - Membangun image uji `mihanstore-backend:e2e-test` dari backend/Dockerfile (docker build biasa; BUKAN
#   docker compose, image produksi `mihanstore-backend:latest` tidak disentuh).
# - Server JWKS Google/Access, webhook Discord, dan sumber data wilayah TIRUAN disajikan oleh proses tes
#   (e2e-runner:9000). Tidak ada lalu lintas ke Google/Cloudflare Access/Discord/wilayah.id asli.
# - Turnstile memakai kunci uji resmi Cloudflare (butuh internet ke challenges.cloudflare.com).
# - Tanpa frontend (E2E_FRONTEND_URL kosong): pemeriksaan sisi frontend dilewati (skip).
# Jaringan, container, database & user uji SELALU dihapus di akhir.
set -uo pipefail
cd "$(dirname "$0")/.."
export TDB=${TDB:-mihanstore_e2e_test} TUSER=${TUSER:-mihanstore_e2e_app} LEGACY_FIXTURE=1
source scripts/lib_testdb.sh
set +e   # pustaka mengaktifkan -e; di sini kegagalan perintah dicek manual
NET=mihanstore-e2e
IMG=mihanstore-backend:e2e-test
AUD=e2eaud0000000000000000000000abcd
GC=e2e-client.apps.googleusercontent.com
ADMIN=e2e.admin@uji.test
cleanup() {
  docker rm -f mst-e2e-backend e2e-runner >/dev/null 2>&1
  docker network rm $NET >/dev/null 2>&1
  docker rmi "$IMG" >/dev/null 2>&1
  testdb_down
}
trap cleanup EXIT
cleanup
testdb_up
docker build -q -t "$IMG" -f Dockerfile . >/dev/null || { echo "build image uji gagal"; exit 1; }
docker network create $NET >/dev/null
HMAC=$(head -c 48 /dev/urandom | base64 | tr -dc A-Za-z0-9)
docker run -d --name mst-e2e-backend --network "$MYSQL_NET" \
  -e DB_HOST=mysql_db -e DB_NAME="$TDB" -e DB_USER="$TUSER" -e DB_PASSWORD="$TPW" \
  -e TURNSTILE_SECRET_KEY=1x0000000000000000000000000000000AA \
  -e ADMIN_EMAILS=$ADMIN -e CF_ACCESS_TEAM_DOMAIN=divine-rice-4f1d.cloudflareaccess.com -e CF_ACCESS_AUD_STORE=$AUD \
  -e TEST_ONLY_CF_ACCESS_JWKS_URL=http://e2e-runner:9000/access/certs \
  -e GOOGLE_CLIENT_ID=$GC -e AUTH_HMAC_SECRET="$HMAC" -e TEST_ONLY_GOOGLE_JWKS_URL=http://e2e-runner:9000/google/certs \
  -e DISCORD_ORDER_WEBHOOK_URL=http://e2e-runner:9000/discord/api/webhooks/1/TOKENUJI-RAHASIA \
  -e PUBLIC_BASE_URL=https://store.mihan.web.id \
  -e REGION_API_BASE=http://e2e-runner:9000/wilayah/api -e TEST_ONLY_REGION_MIN_PROVINCES=1 -e TEST_ONLY_REGION_COOLDOWN_SECONDS=0 \
  -e PAYMENT_PROOF_DIR=/tmp/payment-proofs \
  "$IMG" >/dev/null
docker network connect --alias backend $NET mst-e2e-backend
docker run -d --name e2e-runner --network "$MYSQL_NET" --user "$(id -u):$(id -g)" \
  -e HOME=/tmp -e GOCACHE=/tmp/gocache -e GOFLAGS=-buildvcs=false \
  -e DB_HOST=mysql_db -e DB_NAME="$TDB" -e DB_USER="$TUSER" -e DB_PASSWORD="$TPW" \
  -e E2E_BACKEND_URL=http://backend:8080 -e E2E_FRONTEND_URL= \
  -e E2E_AUD=$AUD -e E2E_GOOGLE_CLIENT=$GC -e E2E_ADMIN_EMAIL=$ADMIN \
  -v mihanstore-gomod:/go/pkg/mod -v "$BACKEND_DIR":/src -w /src "$GOIMAGE" \
  sh -c 'go test -count=1 -tags e2e -run E2E -v ./internal/app/ 2>&1' >/dev/null
docker network connect $NET e2e-runner
rc=$(docker wait e2e-runner)
docker logs e2e-runner 2>&1 | grep -E "^(--- |PASS|FAIL|ok|\s+--- )|_test.go" | grep -v "=== RUN"
echo "rc=$rc"
exit "$rc"
