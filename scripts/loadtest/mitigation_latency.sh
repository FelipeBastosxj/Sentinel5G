#!/usr/bin/env bash
# Closed-loop mitigation latency: how long from "a score crossed the
# threshold" to "the drop is in force". docs/observability.md targets
# single-digit milliseconds.
#
# Measured in two halves, because they fail for different reasons and a
# single end-to-end number hides which one moved:
#
#   kernel  -- the eBPF map write itself (pkg/ebpf.Block/BlockTunnel).
#              Every map write is visible to the XDP program on the very
#              next packet, so there is no propagation delay to add: this
#              IS the time from decision to enforcement.
#   cluster -- NATS publish -> operator consumes -> policy reaches
#              Phase: Mitigating. This is the half that carries JetStream
#              delivery, the Pod lookup, policy matching and a status
#              write against the API server.
#
# The kernel half needs root. The cluster half needs a running cluster with
# the operator installed (scripts/quickstart.sh gives you one) and KUBECONFIG
# pointing at it; it is skipped if kubectl can't reach one.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OBJ="${OBJ:-$REPO_ROOT/bpf/packet_filter.o}"
NAMESPACE="${NAMESPACE:-sentinel5g-system}"
DEMO_NAMESPACE="${DEMO_NAMESPACE:-telecom-core}"
POLICY="${POLICY:-protect-amf-core}"
NATS_URL="${NATS_URL:-nats://nats.default.svc.cluster.local:4222}"
SAMPLES="${SAMPLES:-10}"

echo "== kernel half: decision -> drop in force =="
if [ "$(id -u)" -ne 0 ]; then
  echo "  skipped (needs root)"
elif [ ! -f "$OBJ" ]; then
  echo "  skipped ($OBJ not found -- run 'make -C bpf')"
else
  ( cd "$REPO_ROOT" && SENTINEL5G_BPF_OBJECT="$OBJ" \
      go test -tags privileged ./pkg/ebpf/ -run TestBlocklistIsBounded -bench 'Block' -benchtime 2000x \
      2>&1 | grep -E 'Benchmark|full at|^---|FAIL' | sed 's/^/  /' )
fi

echo
echo "== cluster half: score published -> Phase: Mitigating =="
if ! kubectl get ns "$NAMESPACE" >/dev/null 2>&1; then
  echo "  skipped (no cluster with $NAMESPACE -- run scripts/quickstart.sh)"
  exit 0
fi

reset_policy() {
  kubectl delete tsp "$POLICY" -n "$DEMO_NAMESPACE" --ignore-not-found >/dev/null 2>&1 || true
  kubectl apply -f "$REPO_ROOT/config/samples/security_v1alpha1_telecomsecuritypolicy.yaml" >/dev/null
  # Wait for the reconciler to reach Monitoring, so the measurement starts
  # from a steady state rather than racing the policy's own creation.
  for _ in $(seq 1 30); do
    [ "$(kubectl get tsp "$POLICY" -n "$DEMO_NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null)" = "Monitoring" ] && return
    sleep 0.2
  done
}

# The publish runs in a Pod, so `kubectl run` returning tells us nothing
# useful about when the message actually landed -- Pod startup dominates.
# And polling `kubectl get` cannot resolve the answer either: one round
# trip to the API server costs about as much as the whole closed loop. So:
# a watch is opened BEFORE publishing and the first Mitigating it streams
# is the timestamp. That bounds the operator's share by one API-server
# notification rather than by a poll interval.
#
# Measured separately and printed, because it is the floor on what this
# method can see: the round-trip cost of the watch itself.
rtt_start=$(date +%s%N)
kubectl get tsp "$POLICY" -n "$DEMO_NAMESPACE" >/dev/null 2>&1
rtt_ms=$(( ($(date +%s%N) - rtt_start) / 1000000 ))
echo "  (one kubectl round trip to this API server: ${rtt_ms} ms -- the resolution floor)"

total=0
samples_taken=0
for i in $(seq 1 "$SAMPLES"); do
  reset_policy

  watch_out="$(mktemp)"
  # --output-watch-events so the stream is parseable; the first line is the
  # current state, subsequent ones are changes.
  kubectl get tsp "$POLICY" -n "$DEMO_NAMESPACE" --watch --output-watch-events \
    -o jsonpath='{.object.status.phase}{"\n"}' > "$watch_out" 2>/dev/null &
  watch_pid=$!
  sleep 1  # let the watch establish before publishing

  kubectl run "latency-$i-$RANDOM" --rm -i --restart=Never --image=natsio/nats-box:0.14.1 \
    -n "$DEMO_NAMESPACE" --command -- \
    nats pub --server "$NATS_URL" sentinel5g.threats.scored \
    '{"sourceEventId":"latency","namespace":"'"$DEMO_NAMESPACE"'","podName":"amf-0","sourceIp":"203.0.113.7","teid":19844,"score":0.99,"model":"autoencoder-v1","detectedAt":"2026-01-05T12:00:00Z"}' \
    >/dev/null 2>&1
  published_ns=$(date +%s%N)

  observed_ns=""
  for _ in $(seq 1 200); do
    if grep -q Mitigating "$watch_out" 2>/dev/null; then
      observed_ns=$(date +%s%N)
      break
    fi
    sleep 0.005
  done
  kill "$watch_pid" 2>/dev/null || true
  wait "$watch_pid" 2>/dev/null || true
  rm -f "$watch_out"

  if [ -z "$observed_ns" ]; then
    echo "  sample $i: TIMED OUT" >&2
    continue
  fi
  ms=$(( (observed_ns - published_ns) / 1000000 ))
  total=$((total + ms))
  samples_taken=$((samples_taken + 1))
  printf '  sample %2d: %4d ms\n' "$i" "$ms"
done
[ "$samples_taken" -gt 0 ] && echo "  mean: $((total / samples_taken)) ms over $samples_taken samples"
echo
echo "What this number contains: the nats-box Pod's own publish, JetStream"
echo "delivery, the operator's Pod lookup and policy match, its status write"
echo "to the API server, and the API server notifying this watch. The eBPF"
echo "map write measured above is a rounding error inside it."
