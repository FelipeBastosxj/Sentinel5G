#!/usr/bin/env bash
# Runs cmd/falco-bridge against a REAL Falco daemon: Falco captures host
# syscalls with its modern eBPF probe, fires a rule, and POSTs the alert to
# the bridge's http_output endpoint; the bridge publishes a NormalizedEvent
# on NATS. Closes ROADMAP.md Phase 4's "falco-bridge never run against a real
# daemon" item (Falco half). Needs Docker, a BTF kernel, and NATS at :4222.
#
# NOTE: Falco's modern eBPF probe needs Falco >= 0.40 on a 6.x kernel
# (0.39.x fails scap_init on 6.14 here). falcosecurity/falco:latest is used.
set -euo pipefail
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NATS_URL="${NATS_URL:-nats://localhost:4222}"
BRIDGE_PORT="${BRIDGE_PORT:-8091}"

BRIDGE_BIN="$(mktemp)"
go build -o "$BRIDGE_BIN" "$REPO_ROOT/cmd/falco-bridge"

cleanup() {
  kill "${BRIDGE_PID:-}" 2>/dev/null || true
  docker rm -f s5g-falco s5g-falco-sub 2>/dev/null || true
  rm -f "$BRIDGE_BIN"
}
trap cleanup EXIT

docker run -d --rm --name s5g-falco-sub --network host natsio/nats-box:latest \
  nats sub sentinel5g.events.normalized >/dev/null
NATS_URL="$NATS_URL" FALCO_BRIDGE_LISTEN_ADDR=":$BRIDGE_PORT" "$BRIDGE_BIN" &
BRIDGE_PID=$!
sleep 2

docker run -d --rm --name s5g-falco --network host --privileged --ulimit memlock=-1:-1 \
  -v /sys/kernel/btf:/sys/kernel/btf:ro -v /etc:/host/etc:ro -v /proc:/host/proc:ro \
  falcosecurity/falco:latest falco -o engine.kind=modern_ebpf \
  -o "http_output.enabled=true" -o "http_output.url=http://localhost:$BRIDGE_PORT/falco" \
  -o json_output=true -o stdout_output.enabled=true >/dev/null
echo "waiting for Falco's eBPF probe to attach..."; sleep 20

echo "triggering a rule: reading /etc/shadow (fires 'Read sensitive file untrusted')"
cat /etc/shadow >/dev/null 2>&1 || sudo cat /etc/shadow >/dev/null 2>&1 || true
sleep 4

echo "=== NormalizedEvent(s) the bridge published from real Falco alerts ==="
docker logs s5g-falco-sub 2>&1 | grep eventId | tail -3
