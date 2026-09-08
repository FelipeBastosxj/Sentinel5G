# Troubleshooting

Sentinel5G's `./scripts/quickstart.sh` and Helm chart are exercised against
plain Linux/macOS/WSL2 dev machines, GitHub Actions runners (see
`.github/workflows/e2e.yml`), and — per user reports — a range of cloud VMs
and GitHub Codespaces, each with their own quirks. This page collects the
causes that are known to bite, keyed by symptom, instead of leaving them
scattered across other docs.

## Network egress (ghcr.io / docker.io)

**Symptom:** `quickstart.sh` fails fast at "Checking prerequisites" with
`ERROR: no network egress to https://ghcr.io` (or `docker.io`) — or, without
that preflight, a slow, generic timeout deep inside `kind create cluster` or
an image pull instead.

Sentinel5G's own images are only published to GHCR (`ghcr.io`); once
published to Docker Hub too (see the project roadmap), `docker.io` will also
be reachable-required. Either host being blocked breaks the quickstart the
same way. Common causes:

- Corporate VPN or firewall egress rules that only allowlist specific hosts.
- A cloud VM's security group / network ACL blocking outbound HTTPS to
  arbitrary hosts.
- GitHub Codespaces' own egress allowlist (**Settings → Codespaces →
  Allowlist**, or an org-level policy) not including `ghcr.io`/`docker.io`.

Confirm which host is actually unreachable and why:

```sh
curl -v --max-time 10 https://ghcr.io/v2/
curl -v --max-time 10 https://docker.io/v2/
```

A reachable registry returns `HTTP/2 401` (unauthenticated, but reachable —
that's success for this check). A hang or connection error means egress is
actually blocked; fix the allowlist/firewall/security group rather than
retrying.

## DNS resolution inside kind

**Symptom:** image pulls inside the kind cluster fail with something like
`dial tcp: lookup ghcr.io on 172.x.x.1:53: ... i/o timeout`, even though the
host itself can resolve DNS fine.

This is a known class of kind-on-Docker networking issue, unrelated to
Sentinel5G specifically — it shows up on corporate VPNs, some WSL2 setups,
and some cloud dev environments.

`quickstart.sh` handles this in three steps, each only applied if the
previous one didn't already fix it:

1. **Test first.** `getent hosts ghcr.io` inside the node, bounded by a 5s
   `timeout` (a genuinely broken resolver makes `getent` hang rather than
   fail fast). If this already succeeds, nothing is touched. An earlier
   version skipped this and always overrode `/etc/resolv.conf`, reasoning
   that doing so when DNS was already fine was harmless — wrong on GitHub
   Codespaces, where a node's default resolver (Docker's own per-container
   embedded one) already worked, but Codespaces' network policy blocks a
   container from querying a public resolver directly over UDP/53, so the
   unconditional override broke a node that would otherwise have worked.
2. **Try the host's own real upstream resolver**, discovered by looking at
   `/etc/resolv.conf` (if it's not just a `127.0.0.53` systemd-resolved
   stub, meaningless from inside a different network namespace) or, if it
   is, digging behind that stub via `resolvectl`/`systemd-resolve`. This
   matters because a hard-coded public resolver isn't reachable from every
   host either: on a real Codespace, the *default* resolver also failed
   outright, and the only thing that worked was Azure's own internal
   resolver (`168.63.129.16` — Codespaces runs on Azure), reachable only
   from inside Azure's network. Discovering the real upstream instead of
   guessing is what generalizes to AWS/GCP/bare-metal hosts too.
3. **Fall back to a public resolver** (`8.8.8.8`/`1.1.1.1`) only if step 2
   didn't find a candidate or it didn't work either.

If none of the three works, the script warns instead of failing silently —
that's a genuinely unusual network setup worth filing an issue about.

If you're diagnosing a similar issue outside the quickstart script, the
same idea applies — test, try the host's real resolver, then fall back:

```sh
for node in $(docker ps --filter "label=io.x-k8s.kind.cluster=<cluster-name>" --format '{{.Names}}'); do
  timeout 5 docker exec "$node" getent hosts ghcr.io >/dev/null 2>&1 && continue
  host_dns=$(resolvectl status 2>/dev/null | grep -oE '(Current DNS Server|DNS Servers): [0-9.]+' \
    | awk '{print $NF}' | grep -vE '^(127\.|169\.254\.)' | head -1)
  if [ -n "$host_dns" ]; then
    docker exec "$node" sh -c "printf 'nameserver %s\n' '$host_dns' > /etc/resolv.conf"
  else
    docker exec "$node" sh -c 'printf "nameserver 8.8.8.8\nnameserver 1.1.1.1\n" > /etc/resolv.conf'
  fi
done
```

## RBAC / cluster-admin requirements

**Symptom:** `quickstart.sh` fails fast at "Checking cluster-admin
permissions" with `ERROR: the current kubeconfig user cannot create
ClusterRoleBindings` — or, without that preflight, a `helm upgrade
--install` that fails partway through with a generic `... is forbidden: User
"..." cannot create resource "clusterroles" ...`.

Sentinel5G's Helm chart installs a `ClusterRole`/`ClusterRoleBinding` (see
`charts/sentinel5g-operator/templates/clusterrole.yaml`) so the operator can
watch pods and reconcile `TelecomSecurityPolicy` objects cluster-wide. A
freshly created `kind` cluster's own generated kubeconfig is always
cluster-admin, so this never fires against a local kind cluster — it only
matters when `KUBECONFIG` points at a managed cluster (an EKS/AKS/GKE
cluster, or any cluster where cluster-admin isn't the default) whose RBAC
restricts what your identity can create. Ask whoever administers that
cluster for a cluster-admin-equivalent binding, or install the chart's CRD
and RBAC objects separately using an identity that does have that access.

## ARM64 vs amd64

As of this writing, Sentinel5G's published images
(`ghcr.io/felipebastosxj/sentinel5g-operator`,
`ghcr.io/felipebastosxj/sentinel5g-ai-engine`) are `linux/amd64` only. On an
ARM64 host (Apple Silicon Macs, AWS Graviton/Azure Ampere VMs, some
Codespaces machine types), Docker will run the image under emulation
(slower, and occasionally surfaces syscalls emulation doesn't support
cleanly) rather than fail outright — if something behaves oddly on ARM64
specifically, this is the first thing to suspect. Multi-arch
(`linux/amd64`+`linux/arm64`) images, published to both GHCR and Docker Hub,
are tracked as project work; check the CHANGELOG for whether that's landed
yet.

`quickstart.sh` itself (and the `kind`/`helm` binaries it downloads) already
supports both `amd64` and `arm64` — this limitation is specific to
Sentinel5G's own published operator/ai-engine images.

## eBPF: kernel, capabilities, and build

**Symptom:** `ebpf.enabled: true` is set, but `EbpfBlock` actions still
don't drop any traffic, and the operator log shows a line like:

```json
{"msg":"eBPF blocklist not attached; EbpfBlock actions will be no-ops","cause":"...","reason":"..."}
```

The `cause` field (added by `pkg/ebpf.ClassifyAttachError`) tells you which
of these it is:

- **"was not found"** — the compiled `bpf/packet_filter.o` isn't at the
  configured `--bpf-object` path. The published operator image bakes this in
  at build time (see the `Dockerfile`'s `bpf-builder` stage), so this
  normally only happens if you're running a custom-built image, or if
  `ebpf.objectPath` was overridden to a path that doesn't match where the
  image actually put it.
- **"insufficient privileges"** — the container is missing `CAP_BPF`
  (kernel 5.8+) or `CAP_SYS_ADMIN` (older kernels) plus `CAP_NET_ADMIN`. Set
  `ebpf.enabled: true` in the Helm chart's values — this now also adds
  `ebpf.capabilities` (default `["BPF", "NET_ADMIN"]`) to the manager
  container's `securityContext` automatically; override
  `ebpf.capabilities: ["SYS_ADMIN", "NET_ADMIN"]` for a pre-5.8 kernel.
- **"does not exist on this node"** — `ebpf.interface` (default `eth0`)
  doesn't match a real interface inside the container's network namespace.
  Note the operator's Deployment doesn't set `hostNetwork: true`, so
  "the interface" here is the pod's own veth, not the node's physical NIC —
  see the ROADMAP for the known multi-node coverage gap this implies.
- **"unrecognized reason"** (usually alongside a kernel-level error in
  `reason`) — most often an incompatible or very old kernel that doesn't
  support the BPF program/map types used. `bpf/packet_filter.c` doesn't rely
  on CO-RE relocations (see `docs/architecture.md`), but it does need a
  kernel with working XDP + ring buffer + LRU hash map support.

Building `bpf/packet_filter.c` yourself (outside the operator's Dockerfile,
e.g. for local iteration) needs `clang`+`llvm`+`libbpf-dev` — nothing else.
It builds against `bpf/headers/vmlinux_min.h`, a small hand-maintained
header, not a `bpftool btf dump` of your own kernel's BTF, so there's no
dependency on `bpftool`, kernel headers, or `/sys/kernel/btf/vmlinux` being
accessible (which it isn't inside a plain `docker build`, one reason this
project moved away from that approach — see `docs/architecture.md`).

## GitHub Codespaces specifics

- **Egress allowlist:** see [Network egress](#network-egress-ghcrio--dockerio)
  above — this is the most common Codespaces-specific failure. Organization
  or personal Codespaces egress policies commonly restrict outbound traffic
  to an explicit allowlist that doesn't include `ghcr.io`/`docker.io` by
  default.
- **Docker-in-Docker:** Codespaces already runs a real Docker daemon inside
  the codespace container (not a nested/emulated one), so `kind create
  cluster` works the same way it does on a real Linux host or WSL2 — no
  special handling needed for that part.
- **Direct public-DNS queries from inside a container are blocked:**
  confirmed by running the quickstart against a real Codespace, twice, with
  two different results — sometimes a kind node's DNS works correctly by
  default there (via Docker's own per-container embedded resolver) and
  sometimes it genuinely doesn't, but either way a container querying a
  public resolver (`8.8.8.8`/`1.1.1.1`) directly over UDP/53 gets no
  response: Codespaces' actual working resolver is Azure's own internal
  one (`168.63.129.16`), reachable only from inside Azure's network. This
  only matters if you're troubleshooting DNS manually; `quickstart.sh`
  itself discovers and tries the host's real upstream resolver before ever
  falling back to a public one (see [DNS resolution inside
  kind](#dns-resolution-inside-kind)), so it isn't affected.

## Getting more diagnostic output

```sh
kubectl get events -A --sort-by=.lastTimestamp
kubectl logs -n sentinel5g-system deploy/sentinel5g-operator
kind export logs <output-dir> --name <cluster-name>
```

`.github/workflows/e2e.yml` runs this same set of commands automatically on
a failed CI run and uploads the output as a build artifact — useful as a
reference for what a healthy run's output looks like.
