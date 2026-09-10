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

# --- 1. Prerequisites -------------------------------------------------------
step "Checking prerequisites"

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required and wasn't found on PATH. Install it first:" >&2
  echo "  https://docs.docker.com/get-docker/" >&2
  exit 1
fi
if ! docker info >/dev/null 2>&1; then
  # Two genuinely different causes, and the distinction is the whole fix, so
  # name both instead of only asking "is it running?": on a fresh Linux box
  # or VM the daemon is usually up and it is the *user* that lacks access
  # (not in the "docker" group), which reports as the same unreachable
  # daemon here.
  echo "docker is installed but its daemon isn't reachable. Two common causes:" >&2
  echo "  1. The daemon isn't running:  sudo systemctl start docker" >&2
  echo "  2. Your user can't talk to it (not in the 'docker' group):" >&2
  echo "       sudo usermod -aG docker \"$(id -un)\"" >&2
  echo "     then start a new login shell (or run: newgrp docker) and re-run this." >&2
  exit 1
fi
# curl is used by this script's own preflight checks below and to fetch any
# missing CLI -- some minimal server/VM images (and slim container bases)
# genuinely ship without it, where every check below would otherwise fail as
# a confusing "curl: command not found" mid-run.
if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required and wasn't found on PATH. Install it first, e.g.:" >&2
  echo "  Debian/Ubuntu:  sudo apt-get install -y curl" >&2
  echo "  RHEL/Fedora:    sudo dnf install -y curl" >&2
  echo "  Alpine:         sudo apk add curl" >&2
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

# Everything this script has to *write* -- its isolated kubeconfig, any CLI
# it downloads, helm's own cache/config -- goes under one directory. The repo
# root is preferred (keeps it all next to the clone, is already covered by
# .gitignore, and is where CI looks for the kubeconfig), but it is NOT
# assumed writable: found the hard way that a clone very easily belongs to
# someone else or sits on a read-only mount -- a checkout unpacked with sudo
# (`sudo git clone`, a tarball extracted as root, an image with the repo
# baked in), a shared /opt or /srv path on a VM, an NFS mount with
# root_squash. The old hard-coded "$REPO_ROOT/bin" and
# "$REPO_ROOT/.kube-quickstart.yaml" turned every one of those into a bare
# "Permission denied" from inside curl or kind, so fall back to a per-user
# writable directory rather than failing.
pick_state_dir() {
  local candidate
  for candidate in \
    "$REPO_ROOT" \
    "${XDG_STATE_HOME:-${HOME:-/nonexistent}/.local/state}/sentinel5g-quickstart" \
    "${TMPDIR:-/tmp}/sentinel5g-quickstart-$(id -u)"
  do
    # An actual write, not [ -w ]: only this catches a read-only mount, a
    # restrictive ACL, or a pre-existing root-owned bin/ inside an
    # otherwise-writable checkout.
    if mkdir -p "$candidate/bin" 2>/dev/null \
      && ( : > "$candidate/.sentinel5g-write-probe" ) 2>/dev/null \
      && ( : > "$candidate/bin/.sentinel5g-write-probe" ) 2>/dev/null; then
      rm -f "$candidate/.sentinel5g-write-probe" "$candidate/bin/.sentinel5g-write-probe"
      echo "$candidate"
      return 0
    fi
  done
  return 1
}

if ! STATE_DIR="$(pick_state_dir)"; then
  echo "ERROR: nowhere writable to keep this script's kubeconfig and any CLI it" >&2
  echo "needs to download -- tried the repo itself, \$XDG_STATE_HOME/\$HOME, and" >&2
  echo "\$TMPDIR (/tmp). Make one of those writable, or re-run from a clone you own." >&2
  exit 1
fi
BIN_DIR="$STATE_DIR/bin"
if [ "$STATE_DIR" != "$REPO_ROOT" ]; then
  info "Repo directory isn't writable -- this run's kubeconfig and CLIs go in $STATE_DIR"
fi

# Anything installed below is used for this run only, never added to the
# system PATH.
export PATH="$BIN_DIR:$PATH"

# Explicit, isolated kubeconfig for this script alone -- never touches
# ~/.kube/config or whatever the host's default kubectl/context already is.
# Found the hard way: a machine with k3s (or minikube/microk8s/Docker
# Desktop Kubernetes/...) already installed can make "kubectl" itself
# default to THAT cluster's kubeconfig regardless of what kind just wrote,
# so this script never relies on ambient defaults for anything it runs.
export KUBECONFIG="$STATE_DIR/.kube-quickstart.yaml"

# Same reasoning for helm's own state, plus it keeps the script working when
# $HOME itself isn't writable (a service account on a VM, a container running
# as an arbitrary uid) -- helm otherwise fails writing its cache/config.
export HELM_CACHE_HOME="$STATE_DIR/helm/cache"
export HELM_CONFIG_HOME="$STATE_DIR/helm/config"
export HELM_DATA_HOME="$STATE_DIR/helm/data"

# --fail (-f) is the point of routing every download through here: without
# it, `curl -sLo file url` writes a 404/503 error *page* to the file and
# still exits 0, so a bad URL or a registry blip produced an "installed"
# CLI that only failed much later, as an unrelated-looking
# "cannot execute binary file". The retries cover the transient half of
# that same class of failure.
fetch_cli() {
  local name="$1" url="$2"
  if ! curl -fsSL --retry 3 --retry-delay 2 --max-time 180 -o "$BIN_DIR/$name" "$url"; then
    rm -f "$BIN_DIR/$name"
    echo "ERROR: failed to download $name from:" >&2
    echo "  $url" >&2
    echo "Check egress to that host, or install $name yourself and re-run." >&2
    exit 1
  fi
  chmod +x "$BIN_DIR/$name"
}

# kubectl is installed here rather than demanded as a prerequisite: it is no
# harder to fetch than kind or helm (same single static binary), and having
# the script hard-fail on the one CLI it could trivially provide itself was
# the most common way a first run died on a fresh machine or VM.
if ! command -v kubectl >/dev/null 2>&1; then
  info "kubectl not found -- installing to $BIN_DIR/kubectl (not touching system PATH)"
  # Whatever upstream currently calls stable, with a known-good pin as a
  # fallback so a hiccup fetching stable.txt doesn't become an unusable
  # kubectl. kubectl supports +/-1 minor against the API server and kind's
  # default node image tracks recent Kubernetes, so stable is right here.
  KUBECTL_VERSION="$(curl -fsSL --max-time 15 https://dl.k8s.io/release/stable.txt 2>/dev/null || true)"
  KUBECTL_VERSION="${KUBECTL_VERSION:-v1.31.4}"
  fetch_cli kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/${OS}/${ARCH}/kubectl"
fi
if ! command -v kind >/dev/null 2>&1; then
  info "kind not found -- installing to $BIN_DIR/kind (not touching system PATH)"
  fetch_cli kind "https://kind.sigs.k8s.io/dl/latest/kind-${OS}-${ARCH}"
fi
if ! command -v helm >/dev/null 2>&1; then
  info "helm not found -- installing to $BIN_DIR/helm (not touching system PATH)"
  if ! curl -fsSL --retry 3 --retry-delay 2 --max-time 180 \
      https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 \
      | HELM_INSTALL_DIR="$BIN_DIR" USE_SUDO=false bash >/dev/null; then
    echo "ERROR: failed to install helm into $BIN_DIR." >&2
    echo "Check egress to raw.githubusercontent.com/get.helm.sh, or install helm" >&2
    echo "yourself (https://helm.sh/docs/intro/install/) and re-run." >&2
    exit 1
  fi
fi

# Sanity-check that what is on PATH actually *runs*: a wrong-architecture or
# truncated binary, or a system CLI that is broken for its own reasons, is
# invisible to `command -v` and would otherwise surface as a confusing
# failure several steps later.
for cli in "kubectl version --client" "kind version" "helm version --short"; do
  # shellcheck disable=SC2086 -- deliberate word splitting: cli is "<bin> <args>"
  if ! $cli >/dev/null 2>&1; then
    name="${cli%% *}"
    echo "ERROR: '$name' is on PATH ($(command -v "$name")) but '$cli' failed." >&2
    echo "It looks broken or built for a different architecture ($OS/$ARCH here)." >&2
    echo "Remove or fix that copy and re-run -- this script will install its own." >&2
    exit 1
  fi
done
ok "docker, kubectl, kind, helm all available"

# --- 2. Cluster --------------------------------------------------------------
step "Creating (or reusing) a local kind cluster: $CLUSTER_NAME"

if kind get clusters 2>/dev/null | grep -qx "$CLUSTER_NAME"; then
  info "Cluster '$CLUSTER_NAME' already exists, reusing it"
  # Re-export the cluster's kubeconfig instead of assuming this script's
  # isolated one still describes it. The cluster outliving that file is
  # completely normal -- it was deleted or cleaned up, /tmp was swept, or
  # the state dir simply moved between runs because the repo's writability
  # changed -- and without this, the reuse path died on the next line with
  # `no context exists with the name: "kind-sentinel5g-quickstart"`.
  kind export kubeconfig --name "$CLUSTER_NAME" >/dev/null
else
  kind create cluster --name "$CLUSTER_NAME"
fi
kubectl config use-context "kind-${CLUSTER_NAME}" >/dev/null

# --- 3. DNS fix, applied only where actually needed --------------------------
# kind's node containers periodically come up unable to resolve any external
# hostname at all -- symptom: every image pull fails with something like
# "dial tcp: lookup ghcr.io on 172.x.x.1:53: ... i/o timeout". This is a
# known class of kind-on-Docker networking issue that shows up on corporate
# VPNs, some WSL2 setups, and some cloud VM network configs.
#
# Two things learned the hard way running this exact script on a real
# GitHub Codespace, in order:
#
# 1. An earlier version pointed every node's resolver at a public DNS
#    server (8.8.8.8/1.1.1.1) unconditionally, every run, reasoning that
#    doing so when DNS was already fine was harmless. Wrong on Codespaces:
#    a node's default resolver (Docker's own per-container embedded one)
#    already worked there, but Codespaces' network policy blocks a
#    container from querying a public resolver directly over UDP/53 -- so
#    the unconditional "fix" broke a node that would otherwise have
#    worked. Fixed by testing first, only overriding when resolution
#    genuinely fails.
#
# 2. That alone isn't enough, though: on a *second* real Codespace run, the
#    default resolver genuinely didn't work either, and falling back to
#    8.8.8.8/1.1.1.1 hit the exact same block as (1) -- a hard-coded public
#    resolver was never going to work there. What actually resolves DNS on
#    that host is Azure's own internal resolver (168.63.129.16 -- Codespaces
#    runs on Azure), reachable only from inside Azure's network, which
#    would be equally wrong to hard-code (breaks the same way on AWS/GCP/
#    bare metal). The fix that actually generalizes: discover whatever
#    upstream resolver the *host* itself successfully uses -- via
#    resolvectl/systemd-resolve, looking past systemd-resolved's meaningless
#    (from inside a different network namespace) 127.0.0.53 stub straight to
#    the real server behind it -- and try that before falling back to a
#    public resolver as a last resort.
step "Checking cluster nodes can resolve DNS"

# Best-effort: a non-loopback nameserver already in the host's own
# /etc/resolv.conf is usable as-is; otherwise dig through systemd-resolved
# (resolvectl on newer systems, systemd-resolve on older ones) for the real
# server behind its 127.0.0.53 stub. Empty output (neither available, or
# neither yields a real address) just means the public-resolver fallback
# below is tried first instead -- not a fatal condition on its own.
discover_host_dns() {
  local candidate
  candidate="$(grep -m1 -oE '^nameserver [0-9.]+' /etc/resolv.conf 2>/dev/null | awk '{print $2}')"
  if [ -n "$candidate" ] && [ "${candidate#127.}" = "$candidate" ]; then
    echo "$candidate"
    return
  fi
  if command -v resolvectl >/dev/null 2>&1; then
    candidate="$(resolvectl status 2>/dev/null | grep -oE '(Current DNS Server|DNS Servers): [0-9.]+' \
      | awk '{print $NF}' | grep -vE '^(127\.|169\.254\.)' | head -1)"
  elif command -v systemd-resolve >/dev/null 2>&1; then
    candidate="$(systemd-resolve --status 2>/dev/null | grep -oE '(Current DNS Server|DNS Servers): [0-9.]+' \
      | awk '{print $NF}' | grep -vE '^(127\.|169\.254\.)' | head -1)"
  fi
  [ -n "$candidate" ] && echo "$candidate"
}

# `timeout` wraps the whole `docker exec`, run from the host -- not relying
# on a `timeout` binary existing inside the node image itself -- because a
# genuinely broken resolver makes `getent hosts` hang rather than fail
# fast (found the hard way: an unbounded check here left the whole script
# stuck instead of falling through to the fallback below).
for node in $(docker ps --filter "label=io.x-k8s.kind.cluster=${CLUSTER_NAME}" --format '{{.Names}}'); do
  if timeout 5 docker exec "$node" getent hosts ghcr.io >/dev/null 2>&1; then
    continue
  fi

  fixed=false
  host_dns="$(discover_host_dns || true)"
  if [ -n "$host_dns" ]; then
    info "DNS inside node '$node' isn't working by default -- trying the host's own upstream resolver ($host_dns)"
    docker exec "$node" sh -c "printf 'nameserver %s\n' '$host_dns' > /etc/resolv.conf"
    if timeout 5 docker exec "$node" getent hosts ghcr.io >/dev/null 2>&1; then
      fixed=true
    fi
  fi

  if [ "$fixed" = false ]; then
    info "Falling back to a public resolver for node '$node'"
    docker exec "$node" sh -c 'printf "nameserver 8.8.8.8\nnameserver 1.1.1.1\n" > /etc/resolv.conf'
    if timeout 5 docker exec "$node" getent hosts ghcr.io >/dev/null 2>&1; then
      fixed=true
    fi
  fi

  if [ "$fixed" = false ]; then
    echo "WARNING: node '$node' still can't resolve DNS after trying the host's own" >&2
    echo "resolver and a public fallback. See docs/troubleshooting.md#dns-resolution-inside-kind." >&2
  fi
done
ok "Node DNS resolvers checked"

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

# The commands printed below have to work when pasted into the user's own
# shell, which knows nothing about this script's isolated KUBECONFIG -- and,
# for anything this script downloaded, nothing about $BIN_DIR either. Print
# the export, and the real path of each CLI when it isn't already on the
# user's PATH. Every printed path is quoted: a clone under a directory with
# a space in its name is entirely ordinary ("~/Desktop", and its localized
# equivalents such as "Área de trabalho"), and unquoted these commands
# silently split into the wrong arguments when pasted.
kubectl_hint="kubectl"; kind_hint="kind"
case "$(command -v kubectl)" in "$BIN_DIR"/*) kubectl_hint="\"$BIN_DIR/kubectl\"" ;; esac
case "$(command -v kind)" in "$BIN_DIR"/*) kind_hint="\"$BIN_DIR/kind\"" ;; esac

cat <<EOF

${BOLD}Point your shell at the cluster this script created:${RESET}
  export KUBECONFIG="$KUBECONFIG"

${BOLD}Look around:${RESET}
  $kubectl_hint get telecomsecuritypolicy -n $DEMO_NAMESPACE -w
  $kubectl_hint get pods -A
  $kubectl_hint logs -n $NAMESPACE deploy/sentinel5g-operator

${BOLD}Tear down when done:${RESET}
  $kind_hint delete cluster --name $CLUSTER_NAME
EOF
