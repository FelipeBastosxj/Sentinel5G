#!/bin/bash
set -e
BASE="/mnt/c/Users/Felipe/Desktop/eventstream-observability-engine"

# ── Node.js via nvm4w (Windows) ──────────────────────────────────────────────
export PATH="/mnt/c/nvm4w/nodejs:/mnt/c/nvm4w/nodejs/node_modules/.bin:$PATH"

# Confirmar que node está disponível
if ! command -v node &>/dev/null; then
  echo "[ERRO] node não encontrado. Verifique se /mnt/c/nvm4w/nodejs existe." >&2
  exit 1
fi
echo "[start-dev] Usando Node.js: $(node --version) em $(which node)"
echo "[start-dev] Usando npm:     $(npm --version)"

# ── Carregar .env ────────────────────────────────────────────────────────────
if [ -f "$BASE/.env" ]; then
  export $(grep -v '^#' "$BASE/.env" | grep -v '^$' | xargs)
  echo "[start-dev] .env loaded"
fi
export DATABASE_URL="${DATABASE_URL:-postgresql://webhook_user:webhook_pass@localhost:5432/telecom_webhooks}"
export NODE_ENV="${NODE_ENV:-development}"

# ── Matar instâncias anteriores ──────────────────────────────────────────────
echo "[start-dev] Liberando portas 3001 3003 3004..."
for PORT in 3001 3003 3004; do
  PIDS=$(lsof -t -i :$PORT 2>/dev/null || true)
  if [ -n "$PIDS" ]; then
    kill -9 $PIDS 2>/dev/null || true
    echo "  Encerrou porta $PORT (PIDs: $PIDS)"
  fi
done
sleep 1

# ── Processing-service (3003) ────────────────────────────────────────────────
echo "[start-dev] Iniciando processing-service (3003)..."
(
  cd "$BASE"
  DATABASE_URL="$DATABASE_URL" PROCESSING_PORT=3003 \
  npm run start:dev --workspace=services/processing-service
) > /tmp/processing.log 2>&1 &
PROC_PID=$!

# ── Ingestion-service (3001) ─────────────────────────────────────────────────
echo "[start-dev] Iniciando ingestion-service (3001)..."
(
  cd "$BASE"
  DATABASE_URL="$DATABASE_URL" INGESTION_PORT=3001 PROCESSING_BASE_URL=http://localhost:3003 \
  npm run start:dev --workspace=services/ingestion-service
) > /tmp/ingestion.log 2>&1 &
ING_PID=$!

# ── Realtime-gateway (3004) ──────────────────────────────────────────────────
echo "[start-dev] Iniciando realtime-gateway (3004)..."
(
  cd "$BASE"
  DATABASE_URL="$DATABASE_URL" GATEWAY_PORT=3004 REALTIME_GATEWAY_CORS_ORIGIN=http://localhost:4200 \
  npm run start:dev --workspace=services/realtime-gateway
) > /tmp/gateway.log 2>&1 &
GW_PID=$!

echo ""
echo "[start-dev] PIDs → processing=$PROC_PID  ingestion=$ING_PID  gateway=$GW_PID"
echo "[start-dev] Aguardando 15s para inicialização completa..."
sleep 15

# ── Logs ──────────────────────────────────────────────────────────────────────
echo ""
echo "════════════════ PROCESSING (3003) ════════════════"
tail -20 /tmp/processing.log

echo ""
echo "════════════════ INGESTION (3001) ═════════════════"
tail -15 /tmp/ingestion.log

echo ""
echo "════════════════ GATEWAY (3004) ═══════════════════"
tail -15 /tmp/gateway.log

# ── Health checks ─────────────────────────────────────────────────────────────
echo ""
echo "════════════════ HEALTH CHECKS ══════════════════════"
echo -n "  Processing :3003/health      → "; curl -s http://localhost:3003/health 2>/dev/null || echo "OFFLINE"
echo ""
echo -n "  Ingestion  :3001/health      → "; curl -s http://localhost:3001/health 2>/dev/null || echo "OFFLINE"
echo ""
echo -n "  Workspace  POST :3003/workspace/auto → "
curl -s -X POST http://localhost:3003/workspace/auto \
  -H 'Content-Type: application/json' \
  -d '{"token":"dev-test-token-000001"}' 2>/dev/null || echo "OFFLINE"
echo ""
