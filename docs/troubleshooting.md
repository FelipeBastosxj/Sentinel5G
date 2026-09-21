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

Sentinel5G's own images are published to both GHCR (`ghcr.io`, what the
charts and quickstart default to) and Docker Hub (`docker.io`); the
quickstart also pulls `natsio/nats-box` and the NATS server from
`docker.io`, and with `SENTINEL5G_AI_ENGINE=true` the model artifact
`ghcr.io/.../sentinel5g-model` as well. Either host being blocked breaks
the quickstart the same way. Common causes:

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

## A clone you can't write to (root-owned checkout, read-only mount)

**Symptom:** `quickstart.sh` fails with `Permission denied` from inside
`curl` or `kind` while installing a CLI or writing its kubeconfig, on a
clone you don't own — most often one unpacked with `sudo` (`sudo git
clone`, a tarball extracted as root, an image with the repo baked in), a
shared `/opt` or `/srv` path on a VM, or an NFS mount with `root_squash`.

The script writes exactly three things — its isolated kubeconfig, any CLI
it had to download, and helm's cache/config — and prefers to keep them
beside the clone. When the clone isn't writable it now falls back, in
order, to `$XDG_STATE_HOME` (or `$HOME/.local/state`) and then `$TMPDIR`
(`/tmp`), printing where it landed:

```
Repo directory isn't writable -- this run's kubeconfig and CLIs go in /home/you/.local/state/sentinel5g-quickstart
```

So this is no longer fatal, and no `chown` is required. If you'd rather
keep everything next to the clone, take ownership of it:

```sh
sudo chown -R "$(id -un):$(id -gn)" /path/to/Sentinel5G
```

Because the kubeconfig may not be in the repo, the script prints the
`export KUBECONFIG=...` line to paste before running `kubectl` against the
cluster it created. Note the cluster itself outlives that file: a re-run
re-exports the kubeconfig for an existing cluster rather than assuming it's
still there.

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

Every Sentinel5G image published from `v0.2.2` on — the operator, the AI
engine, the Falco bridge and the model artifact — is a multi-arch
(`linux/amd64`+`linux/arm64`) manifest on both GHCR and Docker Hub, and
`quickstart.sh` (with the `kubectl`/`kind`/`helm` binaries it downloads)
supports both. If you are pinned to an image older than that, Docker runs
the amd64 image under emulation on an ARM64 host (Apple Silicon, AWS
Graviton, Azure Ampere, some Codespaces machine types): slower, and
occasionally surfacing syscalls emulation doesn't support cleanly, rather
than failing outright — the first thing to suspect if something behaves
oddly on ARM64 specifically.

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
  Note the operator's Deployment doesn't set `hostNetwork: true` unless
  `daemonset.enabled` is, so "the interface" here is the pod's own veth,
  not the node's physical NIC — see `docs/integrations.md`.
- **"unrecognized reason"** (usually alongside a kernel-level error in
  `reason`) — most often an incompatible or very old kernel that doesn't
  support the BPF program/map types used. `bpf/packet_filter.c` doesn't rely
  on CO-RE relocations (see `docs/architecture.md`), but it does need a
  kernel with working XDP, ring buffers, and both LRU hash maps (the rate
  trackers) and plain hash maps (the two blocklists — see
  `docs/architecture.md` for why those deliberately aren't LRU).

Building `bpf/packet_filter.c` yourself (outside the operator's Dockerfile,
e.g. for local iteration) needs `clang`+`llvm`+`libbpf-dev` — nothing else.
It builds against `bpf/headers/vmlinux_min.h`, a small hand-maintained
header, not a `bpftool btf dump` of your own kernel's BTF, so there's no
dependency on `bpftool`, kernel headers, or `/sys/kernel/btf/vmlinux` being
accessible (which it isn't inside a plain `docker build`, one reason this
project moved away from that approach — see `docs/architecture.md`).

**"bpf object does not export map \"tunnel_rate\""** — or
`\"tunnel_blocklist\"`, or their `_v6` counterparts — at attach time means
the operator binary is newer than the `packet_filter.o` it was pointed at.
Per-TEID *tracking* added the rate maps and grew the ring-buffer record
from 24 to 32 bytes; per-TEID *mitigation* then added the
`tunnel_blocklist` maps. `pkg/ebpf.Attach` refuses an object missing any of
them rather than silently decoding every subsequent record at the wrong
length, or accepting an `ebpfBlockTunnel` action that could never take
effect. The published operator image bakes a matching object in; this only
happens when `--bpf-object` points at a stale copy of your own.

## Nothing ever scores: `SCORING False`, every policy stuck at `Monitoring`

**Symptom:** `kubectl get tsp -A` shows `SCORING False` for longer than
`SCORING_PIPELINE_GRACE` (default 10m), every policy sits at
`Phase: Monitoring` with an empty `SCORE`, and `kubectl describe tsp` shows
a `ScoringPipelineNotReady` warning Event. Nothing errors.

**Cause:** the operator is subscribed to the threat-score subject but no
`ThreatScoreEvent` has ever arrived. The AI engine is a separate deployment
(`charts/sentinel5g-ai-engine`) and needs a trained model; without it the
scoring half of the system simply doesn't exist, and until this condition
was added that looked identical to a healthy, quiet cluster. Check, in
order:

1. Is it running? `kubectl -n sentinel5g-system get pods -l
   app.kubernetes.io/name=sentinel5g-ai-engine`. Not there → install the
   chart (`docs/production-install.md` step 3).
2. `Init:` states → the model initContainer. `CreateContainerConfigError`
   with "runAsNonRoot and image will run as root" means a model image built
   without the `USER 65532` line `cmd/ai-engine/Dockerfile.model` carries;
   `Permission denied` in the init logs means the pod's `fsGroup` isn't
   set (the chart sets `podSecurityContext.fsGroup: 65532` for exactly
   this).
3. `CrashLoopBackOff` with `ValueError: model at ... expects a
   N-dimensional input but this build extracts 12 features` → the model
   was exported against an older feature vector. Re-export it (the error
   names the commands) or move to the published artifact.
4. Both running, still `False` → they're not on the same bus. The
   engine's `nats.url`/`eventsSubject`/`threatsSubject` must match the
   operator's; `kubectl -n sentinel5g-system logs deploy/sentinel5g-ai-engine`
   should end with `subscribed to sentinel5g.events.normalized ...
   publishing to sentinel5g.threats.scored`.

`sentinel5g_threat_scores_received_total` on the operator's `/metrics` is
the same signal as a counter — flat at zero means the same thing.

## `helm install sentinel5g-ai-engine` fails with "no model source configured"

**Symptom:** the render fails with `no model source configured: set either
model.image.repository ... or model.existingSecret ...`, or with
`model.image.repository and model.existingSecret are mutually exclusive`.

**Cause:** deliberate. The chart refuses to install something that cannot
score rather than come up healthy and silently never publish (the failure
the section above describes). Pick one source: leave `model.image` at its
default (a published artifact), or clear it and bring your own —
`--set model.image.repository=null --set model.existingSecret=<name>`
with a Secret holding `autoencoder.onnx`, `autoencoder.onnx.data` and
`autoencoder.norm.json`.

## `EBPFAttachFailed` / `ScoringPipelineNotReady` Events never appear

**Symptom:** the operator log shows the condition, but `kubectl get
events` has nothing, and the log carries `Server rejected event ...
events.events.k8s.io is forbidden`.

**Cause, fixed:** controller-runtime's event recorder writes through
`events.k8s.io/v1`, and the ClusterRole only granted the core-group
`events` resource — so every Event the operator ever recorded was
rejected, from the day `EBPFAttachFailed` was added. Upgrade the chart (or
re-apply `config/rbac/role.yaml`); an older ClusterRole from before this
fix reproduces it.

## Every packet on port 2152 is `malformed: true`

**Symptom:** after upgrading, traffic to the GTP-U port that used to score
as ordinary GTP-U now arrives flagged `malformed`.

**Cause:** the kernel probe now validates GTP-U framing instead of trusting
the port. Traffic that isn't a real GTP-U T-PDU — a plain UDP flood aimed
at the N3 socket, a generator that never entered the tunnel (`iperf3 -B`
and UERANSIM's `nr-binder` both do this silently, see
`docs/paper-data/real-dataset-v2/README.md`), a GTPv0 or GTP' packet — is
reported as what it is. Genuine GTP-U from a real gNB carries a valid
version/PT and a TEID and is not affected.

## The first score after an operator restart never arrived

**Symptom:** a `ThreatScoreEvent` published while the operator was
restarting shows up in the AI engine's log as published, `SCORING` flips
to `True`, but the policy's `SCORE` stays empty and nothing mitigates.

**Cause, fixed:** JetStream redelivers everything the durable missed the
instant the operator resubscribes, which used to happen before
`PolicyIndex` had been populated — the buffered scores matched nothing,
were acked, and were gone. `ThreatScoreWatcher.Start` now seeds the index
from the cache before subscribing. If you see this on a current build,
`kubectl -n sentinel5g-system logs deploy/sentinel5g-operator | grep
"policy index warmed"` (debug level, `LOG_LEVEL=debug`) is the line that
should precede the subscription.

## A policy with `ebpfBlockTunnel` never drops anything

**Symptom:** the policy reaches `Phase: Mitigating`, `status.blockedTunnels`
stays empty, and traffic keeps flowing.

**Cause:** the scores reaching it carry no TEID, so the per-tunnel action
has nothing to key on. It does not fall back to blocking the whole source —
that would drop every subscriber behind the gNB, which is exactly what the
action exists to avoid — so it does nothing and says so:

```sh
kubectl -n sentinel5g-system exec deploy/sentinel5g-operator -- \
  wget -qO- localhost:8080/metrics | grep ebpf_block_tunnel
# ..._mitigations_total{action="ebpf_block_tunnel",result="no_teid"} 12
```

A non-zero `no_teid` means the capture path can't produce tunnel
identities. Hubble and Falco structurally cannot (`docs/integrations.md`);
only the XDP path parses GTP-U. If that path *is* the source, check the
traffic really is GTP-U T-PDUs — path-management messages and anything that
fails validation legitimately carry TEID 0.

## `BlockTunnel` fails with "key too big for map"

**Symptom:** a mitigation errors with `insert tunnel .../0x... into tunnel
blocklist map: update: key too big for map`, and
`sentinel5g_mitigations_total{result="error"}` climbs.

**Cause:** the map is full (16,384 tunnels, `MAX_TUNNEL_BLOCKLIST_ENTRIES`).
It is a plain HASH rather than an LRU on purpose — evicting an
operator-owned drop to make room would un-block traffic nobody asked to
un-block — so a full map refuses loudly instead. Either de-escalation isn't
running (check `DE_ESCALATION_DWELL` and that policies are reaching
`Monitoring` again), or you genuinely have more concurrent blocked
subscribers than the map holds, in which case rebuild the object with a
larger `MAX_TUNNEL_BLOCKLIST_ENTRIES`.

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
kubectl get telecomsecuritypolicies -A            # PHASE / SCORE / SCORING
kubectl logs -n sentinel5g-system deploy/sentinel5g-operator
kubectl logs -n sentinel5g-system deploy/sentinel5g-ai-engine --all-containers   # includes the model initContainer
kind export logs <output-dir> --name <cluster-name>
```

`.github/workflows/e2e.yml` runs this same set of commands automatically on
a failed CI run and uploads the output as a build artifact — useful as a
reference for what a healthy run's output looks like.
