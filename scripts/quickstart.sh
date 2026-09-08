#!/usr/bin/env bash
# Sentinel5G one-command quickstart: from a fresh clone (or a Codespace with
# nothing pre-installed) to a real closed-loop mitigation firing, using only
# the published ghcr.io/felipebastosxj/* images -- no local Go/Python build.
#
# This is the exact sequence documented in README.md's "Quick Start" section,
# automated end to end, plus fixes for two real, environment-*agnostic*
# failure modes found the hard way (see comments below): kind/helm not being
# preinstalled, and kind's node(s) frequently being unable to resolve DNS at
# all regardless of *why* -- corporate VPNs, some WSL2 configs, some cloud
# VM network setups, and yes, GitHub Codespaces all hit this same class of
# problem, so the fix here is applied unconditionally on every run, on every
# machine, rather than special-cased to any one environment.
#
# Usage: ./scripts/quickstart.sh
# Safe to re-run: every step is idempotent (existing cluster/release/objects
# are reused or upgraded in place, not duplicated).
set -euo pipefail

CLUSTER_NAME="sentinel5g-quickstart"
NAMESPACE="sentinel5g-system"
DEMO_NAMESPACE="telecom-core"

# Overridable so CI (.github/workflows/e2e.yml) can point this exact script
# at images built from the PR's own code instead of the last published
# release, and load them into the kind cluster instead of pulling from a
# registry -- everything defaults to the normal, documented published-image
# behavior when unset, so plain `./scripts/quickstart.sh` is unaffected.
OPERATOR_IMAGE="${SENTINEL5G_OPERATOR_IMAGE:-ghcr.io/felipebastosxj/sentinel5g-operator:v0.2.2}"
KIND_LOAD_IMAGES="${SENTINEL5G_KIND_LOAD_IMAGES:-}"

# ANSI colors, disabled automatically when not writing to a real terminal
# (CI logs, piping to a file) so output stays readable either way.
if [ -t 1 ]; then
  BOLD=$'\033[1m'; GREEN=$'\033[32m'; YELLOW=$'\033[33m'; RESET=$'\033[0m'
else
  BOLD=""; GREEN=""; YELLOW=""; RESET=""
fi

step() { printf '\n%s==> %s%s\n' "$BOLD" "$1" "$RESET"; }
info() { printf '%s%s%s\n' "$YELLOW" "$1" "$RESET"; }
ok()   { printf '%s%s%s\n' "$GREEN" "$1" "$RESET"; }

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

# Explicit, isolated kubeconfig for this script alone -- never touches
# ~/.kube/config or whatever the host's default kubectl/context already is.
# Found the hard way: a machine with k3s (or minikube/microk8s/Docker
# Desktop Kubernetes/...) already installed can make "kubectl" itself
# default to THAT cluster's kubeconfig regardless of what kind just wrote,
# so this script never relies on ambient defaults for anything it runs.
export KUBECONFIG="$REPO_ROOT/.kube-quickstart.yaml"

# --- 1. Prerequisites -------------------------------------------------------
step "Checking prerequisites"

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required and wasn't found on PATH. Install it first:" >&2
  echo "  https://docs.docker.com/get-docker/" >&2
  exit 1
fi
if ! docker info >/dev/null 2>&1; then
  echo "docker is installed but the daemon isn't reachable (is it running?)." >&2
  exit 1
fi
if ! command -v kubectl >/dev/null 2>&1; then
  echo "kubectl is required and wasn't found on PATH. Install it first:" >&2
  echo "  https://kubernetes.io/docs/tasks/tools/#kubectl" >&2
  exit 1
fi

# Network egress check, run before anything that would otherwise fail as a
# slow, generic timeout deep inside `kind create cluster` or an image pull
# (corporate VPNs, locked-down cloud VM egress rules, and restricted
# GitHub Codespaces allowlists all hit this the same way -- see
# docs/troubleshooting.md#network-egress). --max-time 10, not something
# tighter: found the hard way that a real, working connection can still take
# 5+ seconds end to end when the host's default route prefers IPv6 and that
# route is broken/unreachable (curl tries it first, times out, then falls
# back to IPv4) -- a real, if slow, environment some hosts (including one of
# this project's own WSL2 dev setups) actually have, not a hypothetical.
for host in ghcr.io docker.io; do
  if ! curl -sS --max-time 10 -o /dev/null "https://$host/v2/"; then
    echo "ERROR: no network egress to https://$host -- quickstart needs to pull" >&2
    echo "container images from this host. See docs/troubleshooting.md#network-egress" >&2
    echo "for what to check (corporate VPN/firewall, cloud VM security group," >&2
    echo "GitHub Codespaces egress allowlist)." >&2
    exit 1
  fi
done

OS="$(uname -s)"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "Unsupported architecture: $ARCH (this script supports amd64/arm64)" >&2; exit 1 ;;
esac
case "$OS" in
  Linux) OS=linux ;;
  Darwin) OS=darwin ;;
  *)
    echo "This script supports Linux and macOS (including WSL2). On native" >&2
    echo "Windows, run it from inside WSL2 instead -- see docs/getting-started.md." >&2
    exit 1
    ;;
esac

if ! command -v kind >/dev/null 2>&1; then
  info "kind not found -- installing to ./bin/kind (not touching system PATH)"
  mkdir -p "$REPO_ROOT/bin"
  curl -sLo "$REPO_ROOT/bin/kind" "https://kind.sigs.k8s.io/dl/latest/kind-${OS}-${ARCH}"
  chmod +x "$REPO_ROOT/bin/kind"
  export PATH="$REPO_ROOT/bin:$PATH"
fi
if ! command -v helm >/dev/null 2>&1; then
  info "helm not found -- installing to ./bin/helm (not touching system PATH)"
  mkdir -p "$REPO_ROOT/bin"
  curl -sL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 \
    | HELM_INSTALL_DIR="$REPO_ROOT/bin" USE_SUDO=false bash >/dev/null
  export PATH="$REPO_ROOT/bin:$PATH"
fi
ok "docker, kubectl, kind, helm all available"

# --- 2. Cluster --------------------------------------------------------------
step "Creating (or reusing) a local kind cluster: $CLUSTER_NAME"

if kind get clusters 2>/dev/null | grep -qx "$CLUSTER_NAME"; then
  info "Cluster '$CLUSTER_NAME' already exists, reusing it"
else
  kind create cluster --name "$CLUSTER_NAME"
fi
kubectl config use-context "kind-${CLUSTER_NAME}" >/dev/null

# --- 3. DNS fix, applied unconditionally -------------------------------------
# kind's node containers periodically come up unable to resolve any external
# hostname at all -- symptom: every image pull fails with something like
# "dial tcp: lookup ghcr.io on 172.x.x.1:53: ... i/o timeout". This has
# nothing to do with Sentinel5G's images specifically (Docker Hub pulls fail
# the exact same way) and nothing to do with any one hosting environment --
# it's a known class of kind-on-Docker networking issue that shows up on
# corporate VPNs, some WSL2 setups, some cloud VM network configs, and
# GitHub Codespaces alike. Rather than trying to detect it (fragile: the
# exact error text/timing varies), just point every node's resolver at a
# known-good public DNS server unconditionally, every run -- a few
# milliseconds of harmless work when DNS was already fine, and a real fix
# when it wasn't. Applies to every node in the cluster, not just a
# single-node assumption.
step "Ensuring cluster nodes can resolve DNS (idempotent, safe if already fine)"
for node in $(docker ps --filter "label=io.x-k8s.kind.cluster=${CLUSTER_NAME}" --format '{{.Names}}'); do
  docker exec "$node" sh -c 'printf "nameserver 8.8.8.8\nnameserver 1.1.1.1\n" > /etc/resolv.conf'
done
ok "Node DNS resolvers set"

step "Waiting for the cluster's own node to be Ready"
kubectl wait --for=condition=Ready node --all --timeout=120s

# Only meaningful against a pre-existing, non-kind cluster: KUBECONFIG can be
# pointed at a managed cluster (the AWS/Azure VM scenario this preflight
# exists for) whose kubeconfig user isn't cluster-admin -- a fresh kind
# cluster's own generated kubeconfig always is, so this never fires there.
# Without this, a missing ClusterRoleBinding only surfaces as a generic
# "forbidden" error from partway through `helm upgrade --install` below.
# See docs/troubleshooting.md#rbac.
step "Checking cluster-admin permissions"
if ! kubectl auth can-i create clusterrolebindings >/dev/null 2>&1; then
  echo "ERROR: the current kubeconfig user cannot create ClusterRoleBindings." >&2
  echo "Sentinel5G's Helm chart installs a ClusterRole/ClusterRoleBinding" >&2
  echo "(see charts/sentinel5g-operator/templates/clusterrole.yaml)." >&2
  echo "See docs/troubleshooting.md#rbac." >&2
  exit 1
fi
ok "Current user can create ClusterRoleBindings"

if [ -n "$KIND_LOAD_IMAGES" ]; then
  step "Loading locally built images into the kind cluster"
  for image in $KIND_LOAD_IMAGES; do
    kind load docker-image "$image" --name "$CLUSTER_NAME"
  done
fi

# --- 4. NATS JetStream --------------------------------------------------------
step "Deploying NATS JetStream"
kubectl apply -f deployments/quickstart/nats.yaml
kubectl rollout status deployment/nats --timeout=120s

# --- 5. Sentinel5G operator (Helm) -------------------------------------------
step "Installing the Sentinel5G operator (Helm)"
# upgrade --install, not install: makes this step -- and the whole script --
# safe to re-run without "cannot reuse a name that is still in use" errors.
# image.repository/image.tag are always passed explicitly (split from
# OPERATOR_IMAGE) rather than left to the chart's own values.yaml defaults --
# a no-op when SENTINEL5G_OPERATOR_IMAGE is unset (the split reproduces
# values.yaml's own default), and what lets CI point this at a PR's own
# locally built, kind-loaded image (see SENTINEL5G_KIND_LOAD_IMAGES above).
helm upgrade --install sentinel5g charts/sentinel5g-operator \
  --namespace "$NAMESPACE" --create-namespace \
  --set image.repository="${OPERATOR_IMAGE%:*}" \
  --set image.tag="${OPERATOR_IMAGE##*:}" \
  --set nats.url=nats://nats.default.svc.cluster.local:4222 \
  --wait --timeout=180s

kubectl wait --for=condition=Established crd/telecomsecuritypolicies.security.sentinel5g.io --timeout=60s
ok "Operator deployed and its CRD is established"

# --- 6. Demo workload + policy ------------------------------------------------
step "Deploying a demo workload and protecting it"
kubectl apply -f deployments/quickstart/demo-workload.yaml
kubectl wait --for=condition=Ready pod/amf-0 pod/attacker -n "$DEMO_NAMESPACE" --timeout=120s
kubectl apply -f config/samples/security_v1alpha1_telecomsecuritypolicy.yaml

info "Confirming the demo workload is reachable before any mitigation fires..."
BEFORE=$(kubectl exec -n "$DEMO_NAMESPACE" attacker -- curl -s -o /dev/null -w '%{http_code}' http://amf-0 || true)
echo "  amf-0 responded: $BEFORE"

# --- 7. Simulate an attack -----------------------------------------------------
step "Simulating an attack (publishing a real ThreatScoreEvent over NATS)"
kubectl run nats-box --rm -i --restart=Never --image=natsio/nats-box:latest --namespace="$DEMO_NAMESPACE" -- \
  nats pub sentinel5g.threats.scored '{"sourceEventId":"quickstart-1","namespace":"telecom-core","podName":"amf-0","sourceIp":"203.0.113.7","score":0.93,"model":"autoencoder-v1","detectedAt":"2026-01-05T12:00:00Z"}' \
  --server nats://nats.default.svc.cluster.local:4222 >/dev/null

step "Waiting for the operator to close the loop (Phase -> Mitigating)"
for _ in $(seq 1 30); do
  PHASE=$(kubectl get telecomsecuritypolicy protect-amf-core -n "$DEMO_NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null || true)
  if [ "$PHASE" = "Mitigating" ]; then
    break
  fi
  sleep 1
done

echo
if [ "$PHASE" = "Mitigating" ]; then
  ok "Success -- TelecomSecurityPolicy protect-amf-core is now Phase: Mitigating"
else
  info "Still Phase: ${PHASE:-<none>} after 30s -- check 'kubectl get events -n $DEMO_NAMESPACE' if this doesn't move to Mitigating shortly."
fi

cat <<EOF

${BOLD}Look around:${RESET}
  kubectl get telecomsecuritypolicy -n $DEMO_NAMESPACE -w
  kubectl get pods -A
  kubectl logs -n $NAMESPACE deploy/sentinel5g-operator

${BOLD}Tear down when done:${RESET}
  kind delete cluster --name $CLUSTER_NAME
EOF
