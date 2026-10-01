#!/usr/bin/env bash
# Runs pkg/hubble.Observer against a REAL Hubble Relay: a throwaway kind
# cluster with Cilium + Hubble, real Cilium flows on UDP/2152, converted to
# NormalizedEvents. Closes ROADMAP.md Phase 4's "pkg/hubble never run against
# a real daemon" item (Hubble half). Needs Docker, kind, helm, NATS at :4222.
set -euo pipefail
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
KUBECONFIG_FILE="$(mktemp)"
export KUBECONFIG="$KUBECONFIG_FILE"

cleanup() {
  kill "${PF_PID:-}" 2>/dev/null || true
  kind delete cluster --name s5g-hubble 2>/dev/null || true
  rm -f "$KUBECONFIG_FILE"
}
trap cleanup EXIT

cat >"$KUBECONFIG_FILE.kind" <<YAML
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: s5g-hubble
networking:
  disableDefaultCNI: true
  kubeProxyMode: none
YAML
kind create cluster --config "$KUBECONFIG_FILE.kind" --kubeconfig "$KUBECONFIG_FILE"

helm repo add cilium https://helm.cilium.io/ >/dev/null 2>&1 || true
helm repo update >/dev/null
helm install cilium cilium/cilium --namespace kube-system \
  --set ipam.mode=kubernetes --set kubeProxyReplacement=true \
  --set k8sServiceHost=s5g-hubble-control-plane --set k8sServicePort=6443 \
  --set hubble.enabled=true --set hubble.relay.enabled=true >/dev/null
kubectl -n kube-system rollout status ds/cilium --timeout=180s
kubectl -n kube-system rollout status deploy/hubble-relay --timeout=150s

kubectl run web --image=nginx:alpine >/dev/null
kubectl wait --for=condition=Ready pod/web --timeout=90s
WEBIP="$(kubectl get pod web -o jsonpath='{.status.podIP}')"

kubectl -n kube-system port-forward deploy/hubble-relay 4245:4245 >/dev/null 2>&1 &
PF_PID=$!
sleep 5

echo "observing real Hubble flows while sending GTP-U (UDP/2152) datagrams..."
go run "$REPO_ROOT/scripts/hubbletest/main.go" -addr localhost:4245 -seconds 28 &
OBS=$!
sleep 4
kubectl run gtpuprobe --image=busybox --restart=Never --command -- \
  sh -c "echo p1 | nc -u -w1 $WEBIP 2152; sleep 2; echo p2 | nc -u -w1 $WEBIP 2152; sleep 2; echo p3 | nc -u -w1 $WEBIP 2152" >/dev/null
wait "$OBS"
