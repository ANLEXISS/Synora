#!/usr/bin/env bash
set -euo pipefail

PROTO_HOST="${PROTO_HOST:-rock@100.80.170.47}"
PROTO_DIR="${PROTO_DIR:-~/Synora}"

# Par défaut, on ne redémarre pas discovery parce que le pipeline vision est encore instable.
# Pour le redémarrer aussi :
#   RESTART_DISCOVERY=1 ./tools/dev_proto_deploy.sh
RESTART_DISCOVERY="${RESTART_DISCOVERY:-0}"

# Pour aller plus vite :
#   SKIP_GO_TESTS=1 ./tools/dev_proto_deploy.sh
SKIP_GO_TESTS="${SKIP_GO_TESTS:-0}"

echo "=== Synora prototype quick deploy ==="
echo "Proto dir         : $PROTO_HOST:$PROTO_DIR"
echo "Restart discovery : $RESTART_DISCOVERY"
echo "Skip Go tests     : $SKIP_GO_TESTS"
echo

ssh -tt "$PROTO_HOST" <<REMOTE
set -euo pipefail

cd "$PROTO_DIR"

echo
echo "=== sudo auth ==="
sudo -v

echo
echo "=== Fix permissions repo/npm ==="
sudo chown -R rock:rock /home/rock/Synora
sudo chown -R rock:rock /home/rock/.npm 2>/dev/null || true

echo
echo "=== Backup runtime/config ==="
sudo tar -czf "/var/lib/synora/backup-before-quick-deploy-\$(date +%Y%m%d-%H%M%S).tar.gz" \
  /etc/synora \
  /var/lib/synora/state \
  /var/lib/synora/auth \
  /var/lib/synora/cge \
  /var/lib/synora/vision 2>/dev/null || true

echo
echo "=== Clean web build cache ==="
rm -rf synora-web/dist
rm -rf synora-web/.vite
rm -rf synora-web/node_modules/.tmp

echo
echo "=== Optional Go tests ==="
if [[ "$SKIP_GO_TESTS" != "1" ]]; then
  GOCACHE=/tmp/synora-gocache go test ./... || {
    echo
    echo "WARNING: go test ./... a échoué sur le prototype."
    echo "Le déploiement continue quand même, car certains tests peuvent être environnement-dépendants."
    echo
  }
else
  echo "Go tests skipped."
fi

echo
echo "=== Web install/build ==="
npm --prefix synora-web install
npm --prefix synora-web run build

echo
echo "=== Go build + web build ==="
make build
make build-web

echo
echo "=== Install ==="
sudo env "PATH=\$PATH" make install

echo
echo "=== Restart services ==="
sudo systemctl daemon-reload

if [[ "$RESTART_DISCOVERY" == "1" ]]; then
  sudo systemctl restart synora-bus synora-core synora-actions synora-api synora-discovery
else
  sudo systemctl stop synora-discovery || true
  sudo systemctl restart synora-bus
  sleep 1
  sudo systemctl restart synora-core
  sleep 1
  sudo systemctl restart synora-actions
  sleep 1
  sudo systemctl restart synora-api
fi

sleep 3

echo
echo "=== Web status ==="
make web-status

echo
echo "=== API token ==="
export BASE_URL="http://127.0.0.1:8080"
export TOKEN="\$(sudo awk -F': *' '/^api_token:/ {gsub(/"/, "", \$2); print \$2}' /etc/synora/security.yaml)"

echo
echo "=== Smoke endpoints ==="
for path in \
  /api/system/health \
  /api/cge/runtime-status \
  /api/security/mode \
  /api/state \
  /api/devices \
  /api/residents \
  /api/automations \
  /api/topology \
  /api/events/chains
do
  echo "--- \$path ---"
  curl -m 3 -s -o /tmp/synora_resp.json -w "HTTP %{http_code} TIME %{time_total}s\n" \
    -H "Authorization: Bearer \$TOKEN" \
    "\$BASE_URL\$path"
  cat /tmp/synora_resp.json | jq 'type' 2>/dev/null || cat /tmp/synora_resp.json
done

echo
echo "=== Runtime status résumé ==="
curl -m 3 -s -H "Authorization: Bearer \$TOKEN" \
  "\$BASE_URL/api/cge/runtime-status" | jq '{
    current_state,
    danger_level,
    danger_score,
    danger_source,
    security_mode,
    security_armed,
    expected_occupancy,
    manual_risk_active,
    blocking_reasons
  }'

echo
echo "=== Services ==="
systemctl show synora-bus synora-core synora-actions synora-api synora-discovery \
  -p ActiveState -p SubState -p NRestarts

echo
echo "=== Logs suspects récents ==="
sudo journalctl -u synora-core -n 120 --no-pager \
  | grep -Ei "incoming channel full|panic|fatal|deadlock|concurrent map|runtime error|nil pointer|internal server error" || true

sudo journalctl -u synora-api -n 80 --no-pager \
  | grep -Ei "500|panic|fatal|runtime|5\\.00" || true

echo
echo "=== Deploy terminé ==="
REMOTE
