#!/bin/bash
# expose.sh — Expõe a porta 3001 publicamente via Cloudflare Quick Tunnel.
# Uso:  wsl -e bash /mnt/c/.../expose.sh
#
# A URL gerada muda a cada execução. Para URL fixa, crie uma conta gratuita
# em https://dash.cloudflare.com e use um named tunnel.

CLOUDFLARED="/tmp/cloudflared"

# Baixa o cloudflared se necessário
if [ ! -f "$CLOUDFLARED" ]; then
  echo "[expose] Baixando cloudflared..."
  curl -L -o "$CLOUDFLARED" \
    "https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64"
  chmod +x "$CLOUDFLARED"
fi

echo "[expose] Iniciando túnel na porta 3001..."
echo "[expose] Ctrl+C para parar."
echo ""
exec "$CLOUDFLARED" tunnel --url http://localhost:3001
