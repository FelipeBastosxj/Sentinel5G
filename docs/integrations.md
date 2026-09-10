# Integrations

Sentinel5G composes with existing cloud-native security and networking
tooling rather than replacing it. This page covers the integration points
referenced from `docs/architecture.md`.

## Capture layer: Cilium / Falco

`bpf/packet_filter.c` is a standalone XDP program; it does not require
Cilium or Falco to run. If your cluster already runs Cilium as its CNI, you
have two options:

1. **Standalone (default):** attach `packet_filter.c` directly via
   `pkg/ebpf.Attach` on the node interfaces facing telecom workloads. This
   is what `cmd/operator` does today, and requires no Cilium/Hubble at all.
2. **Cilium-native (`pkg/hubble.Observer`, `HUBBLE_ADDR`):** instead of a
   second XDP attachment point on the same interface — two independent XDP
   programs cannot both own the same attachment point without an explicit
   multi-prog dispatcher — consume Cilium's already-running dataplane
   visibility via Hubble's gRPC Observer API (`GetFlows`, typically served
   by Hubble Relay, which aggregates every node's own Hubble agent into one
   stream). `Observer.Start` dials `HUBBLE_ADDR`, streams flows with
   `Follow: true`, and republishes matching ones (UDP to port 2152/5060,
   the same telecom signaling ports `bpf/packet_filter.c`'s
   `is_signaling_port()` recognizes) as `events.NormalizedEvent` on the
   same NATS subject the eBPF path uses — `pkg/hubble.FromFlow` does the
   mapping, reading `Namespace`/`PodName` directly off the flow's already-
   resolved `Source` endpoint (Cilium has already done the identity lookup
   `pkg/ingestion.FromSignalingEvent` otherwise has to do itself via
   `PodIPIndex`). Unlike `pkg/ingestion.Publisher` and `pkg/falco.Bridge`
   (both `NeedLeaderElection() == false`, since each needs to run on every
   node/replica to see its own local traffic), `Observer.
   NeedLeaderElection()` returns `true` — Hubble Relay has already
   aggregated every node's flows into one stream, so more than one live
   `Observer` against the same Relay would double-publish every flow.
   Empty `HUBBLE_ADDR` (the default) leaves this disabled entirely, same
   opt-in posture as `ebpf.enabled`/`daemonset.enabled`. A cluster running
   Cilium should use this *instead of* attaching `packet_filter.c`, not
   alongside it — running both against the same real traffic would
   double-count every signaling packet as two separate `NormalizedEvent`s.
   `HUBBLE_TLS_CA_FILE`/`HUBBLE_TLS_CERT_FILE`/`HUBBLE_TLS_KEY_FILE`
   configure mTLS to Hubble Relay, commonly deployed that way — see
   [Cilium's Hubble TLS docs](https://docs.cilium.io/en/stable/observability/hubble/configuration/#tls-configuration).

   **Verification note**: this integration was NOT verified against a live
   Hubble/Cilium deployment. This project's real WSL2 test cluster (see
   the persistent session memory referenced from `CLAUDE.md`) runs
   flannel, not Cilium, as its CNI, and installing Cilium there would risk
   breaking the Istio/NATS/Open5GS environment already relied on for other
   testing. What *was* verified instead: an in-process gRPC server
   implementing the real `observer.ObserverServer` interface
   (`google.golang.org/grpc/test/bufconn`, not a real network listener),
   exercised end-to-end through the actual generated
   `observer.ObserverClient` — dial, stream, `Recv` loop, the
   `GetFlowsResponse` oneof, graceful stream shutdown — with hand-built but
   schema-accurate `flow.Flow` messages (checked against
   `github.com/cilium/cilium/api/v1/flow`'s actual generated Go types, not
   assumed), publishing to a real NATS JetStream server and confirmed via a
   direct subscription. This is the same class of explicit, honest caveat
   this page already carries for the Falco bridge below and for
   `LinkerdAdapter`'s real-traffic-enforcement gap further down.

**Falco bridging: `cmd/falco-bridge`.** Falco's syscall-level alerts are
bridged into `events.NormalizedEvent` by a small, separate binary
(`pkg/falco.Bridge`) — not folded into `cmd/operator`, since it's a pure
HTTP-webhook-to-NATS translator with no need for the operator's K8s
RBAC/leader-election/CRD-watching machinery. Point Falco's own
[`http_output`](https://falco.org/docs/outputs/#http-output) at it:

```yaml
# Falco's own falco.yaml
http_output:
  enabled: true
  url: "http://sentinel5g-falco-bridge.sentinel5g-system.svc.cluster.local:8091/falco"
```

`Bridge.FromAlert` maps Falco's `output_fields` (`fd.sip`/`fd.rip` →
SourceIP, `fd.dip`/`fd.lip` → DestIP, `fd.dport`/`fd.lport` → DestPort,
`k8s.ns.name`/`k8s.pod.name` → Namespace/PodName) the same
best-effort-attribution way `pkg/ingestion.FromSignalingEvent` does for the
eBPF path: `output_fields` is an open map whose actual keys depend on which
`%fields` a given Falco rule's own `output:` format string references, so a
missing or wrong-typed field is treated as unknown, not an error. `DestPort`
is classified into a `Protocol` using the same two telecom signaling ports
`bpf/packet_filter.c`'s `is_signaling_port()` recognizes (2152 → GTP-U, 5060
→ SIP), so downstream feature extraction doesn't need to special-case which
Layer 1 source produced an event. `PayloadSize`, `RatePerSecond`,
`Malformed`, and `VLANID` stay at their zero value — Falco's syscall-level
view has no equivalent of a packet payload size, a windowed rate (Falco
emits one alert per triggering event), protocol-framing validation, or a
Layer 2 VLAN tag.

This is Falco's counterpart to `pkg/ingestion.Publisher` (the eBPF-sourced
bridge), not a replacement — the two are independent and not mutually
exclusive: a cluster can run both, publishing onto the same subject, since
`NormalizedEvent` exists specifically so Layer 3 doesn't need to know which
Layer 1 source produced an event. Like `Publisher`, `Bridge.
NeedLeaderElection()` returns `false` — Falco commonly runs as its own
DaemonSet, and every alert needs publishing regardless of which replica of
this bridge happens to be elected leader if run under a manager (it isn't,
here, but the same reasoning applies to a horizontally-scaled `Deployment`
behind the `Service` `deployments/quickstart/falco-bridge.yaml` renders).

Falco's `http_output` posts unauthenticated by default — set
`FALCO_BRIDGE_SHARED_SECRET` (and Falco's own matching
`http_output.headers: {"X-Sentinel5g-Shared-Secret": "..."}`) if the bridge
is reachable by anything other than Falco itself: the same "an
unauthenticated ingress is a real risk" posture this page already documents
for the NATS bus below — anything that can `POST /falco` can forge a
`NormalizedEvent`.

**Verification note**: this bridge was verified end to end against a real
NATS JetStream server (a synthetic Falco-shaped JSON payload POSTed to a
running `Bridge` produced a correctly-mapped `NormalizedEvent`, actually
delivered on the real subject, confirmed by directly subscribing to it) and
inside a real built `docker build`/`docker run` container (`HEALTHCHECK`
reports `"Status":"healthy"` against a live NATS connection). What was
*not* verified is a live Falco daemon's own alerts flowing through it —
this WSL2 environment's custom kernel doesn't reliably support Falco's own
kernel-module/eBPF probe. The HTTP contract itself (Falco's long-stable,
widely-integrated JSON output schema) is the part carrying the residual
risk; this is the same kind of explicit, honest caveat this page already
carries for `LinkerdAdapter`'s real-traffic-enforcement gap below.

## Mesh isolation: Istio, Cilium, Linkerd

All three adapters share the same pattern: no vendored mesh-specific Go
client (`istio.io/client-go`, `github.com/cilium/cilium`'s API module,
Linkerd's) is required, since each talks to its CRD via an unstructured
`controller-runtime` client — the operator works against any cluster
running the relevant CRD, regardless of that mesh's own Go client version.
`buildMeshAdapter` (`cmd/operator/main.go`) checks the relevant CRD is
actually registered before building any of them, falling back to
`mesh.NoopAdapter` (with a log line explaining why) if it isn't — the same
"degrade gracefully" rule `EbpfBlock` already follows when eBPF isn't
attached. Clusters without any of the three should leave `MESH_ADAPTER`
unset or set it to `noop` (`mesh.adapter=noop` in `.env.example`, or the
Helm chart's `config.meshAdapter`), which makes `IsolatePod` actions no-ops.

**`pkg/mesh.IstioAdapter`** and **`pkg/mesh.CiliumAdapter`**
(`MESH_ADAPTER=cilium`) give an equivalent, full guarantee: one object
denies **all** traffic to/from the target workload, regardless of port.
Both are idempotent via `pkg/mesh.quarantineName` (a deterministic name
derived from the selector, shared by every adapter — the same selector
always maps to the same object name, so repeated `Quarantine` calls update
in place rather than accumulating stale objects) and the shared
`createOrUpdateUnstructured`/`deleteUnstructuredIfExists` helpers in
`pkg/mesh/adapter.go`.

- **Istio**: a deny-all `security.istio.io/v1` `AuthorizationPolicy` scoped
  to the policy's `targetWorkloads` selector (`spec.action: DENY` with a
  single empty rule — see that file's own comment for why an empty
  `rules: []` doesn't work, found by testing against a real cluster).
- **Cilium**: a `cilium.io/v2` `CiliumNetworkPolicy` with `ingressDeny`/
  `egressDeny` rules using the `"all"` reserved entity — deliberately not
  an empty `endpointSelector`/`ingress`/`egress` ("allow nothing"): Cilium's
  own docs are explicit that "deny policies take precedence over allow
  policies," but merely adding no allow rules doesn't override an
  *existing* allow policy for the same pod from elsewhere (e.g. a baseline
  "allow same-namespace" policy many clusters run) — only an explicit deny
  does. `"all"` is used rather than an empty selector for the same reason:
  an empty `fromEndpoints`/`toEndpoints` only covers Cilium-managed
  endpoints inside the cluster, while `"all"` is documented as covering the
  cluster **and** `world` (external) traffic.

**`pkg/mesh.LinkerdAdapter`** (`MESH_ADAPTER=linkerd`) gives a materially
**weaker** guarantee — read this before choosing it. Linkerd's
`policy.linkerd.io/v1beta3` `Server` resource (the current storage
version; `v1beta1`/`v1beta2` are deprecated in Linkerd's own CRD in favor
of it, confirmed directly from `linkerd2`'s CRD source rather than assumed
from docs) is scoped to one specific port — `spec.port` is required, no
wildcard, unlike Istio's/Cilium's workload-wide deny. `Quarantine`
compensates by listing the target Pods and creating one deny-by-default
`Server` (`spec.accessPolicy: deny`, and no accompanying
`AuthorizationPolicy` — Linkerd denies everything to a `Server`'d port with
none) per **declared** `containerPort` found across them. This is a
best-effort quarantine, not a complete one: a container's declared
`ports:` in its Pod spec is documentation, not enforcement — a port the
workload actually listens on but never declared in its spec stays
reachable after `Quarantine`. `Quarantine` returns an error (rather than
silently "succeeding" with zero effect) if it discovers no ports at all.
`Release` deletes every `Server` it finds labeled for that selector's
quarantine, not by re-discovering the Pods' current ports (which may have
already changed, or the Pods may already be gone by the time `Release`
runs) — so nothing it creates can leak past cleanup.

Prefer Istio or Cilium when the mesh choice is yours to make; use Linkerd's
adapter when Linkerd is the only mesh available and a declared-ports-only
best-effort quarantine is an acceptable trade-off for your workloads. A
stronger Linkerd guarantee would need extending `pkg/mesh.Adapter.
Quarantine`'s signature to carry port information explicitly, a larger
change affecting every adapter and every call site in `pkg/controller`.

## eBPF blocklist enforcement

Attaching `bpf/packet_filter.c`'s XDP program needs `CAP_BPF` +
`CAP_NET_ADMIN` (or `CAP_SYS_ADMIN` on older kernels). The operator's
default `securityContext` (both `config/manager/manager.yaml` and the Helm
chart's `values.yaml`) drops all Linux capabilities, so:

- `EbpfBlock` actions silently no-op until you opt in.
- To opt in: set `ebpf.enabled: true` in the Helm chart's values (or pass
  `--bpf-object`/`--bpf-interface` directly to `cmd/operator`). The chart
  now also adds `ebpf.capabilities` (default `["BPF", "NET_ADMIN"]`) to the
  manager container's `securityContext` automatically when `ebpf.enabled` is
  true — override `ebpf.capabilities` to `["SYS_ADMIN", "NET_ADMIN"]` on
  older kernels instead of editing `securityContext` by hand.
- The compiled `bpf/packet_filter.o` is baked into the operator image at
  build time (see the `Dockerfile`'s `bpf-builder` stage) — no manual step
  needed to get it onto the node/container.
- `cmd/operator/main.go` logs (rather than fails) when the eBPF attach
  fails, classifying *why* (missing object, insufficient capability, unknown
  interface, or unrecognized) via `pkg/ebpf.ClassifyAttachError` — see
  `docs/troubleshooting.md#ebpf` — so a misconfigured capability set
  degrades to mesh-only isolation instead of crash-looping the operator, and
  the cause shows up in the log instead of a bare error string. It's also
  recorded as an `EBPFAttachFailed` Kubernetes Event against the operator's
  own Pod (via the `POD_NAME`/`POD_NAMESPACE` Downward API env vars the
  chart sets), so `kubectl describe pod`/`kubectl get events` surface it
  directly instead of only the log. `pkg/ebpf.Attach` also checks
  `--bpf-interface` resolves to a real interface *before* loading anything
  into the kernel, so a typo'd interface name fails fast with that same
  classification rather than after a full (wasted) collection load.

**Real multi-node coverage: `daemonset.enabled` (recommended with eBPF
enabled).** The Helm chart can render the operator as a `DaemonSet` instead
of a `Deployment` — this guarantees exactly one Pod per matching node (no
`podAntiAffinity` needed, `replicaCount` is ignored), so `pkg/ingestion.
Publisher` (the per-node ring-buffer reader, which deliberately runs on
*every* Pod, not just the leader — `--bpf-interface` is attached per-pod, so
each Pod only ever sees its own node's traffic) actually covers the whole
cluster instead of however many *distinct* nodes `replicaCount` Pods happen
to land on.

This also implies `hostNetwork: true` (and `dnsPolicy:
ClusterFirstWithHostNet`, required for cluster-internal DNS names to keep
resolving under it) — **and this part isn't optional if you want XDP to see
real node traffic**: without a Pod's own network namespace sharing the
node's, `--bpf-interface` attaches to that Pod's own veth interface instead
of the node's real one, which only ever carries that Pod's own traffic, never
the telecom workloads eBPF is meant to protect. `hostNetwork: true` is a real
security trade-off (the Pod can see and bind any port on the host, not just
its own), which is exactly why `daemonset.enabled` is its own explicit
opt-in rather than folded into `ebpf.enabled`.

**Without `daemonset.enabled`** (plain `Deployment`, e.g. if the
`hostNetwork` trade-off above isn't acceptable for your cluster): scaling
`replicaCount` only gives you coverage on however many distinct nodes those
replicas land on, not the whole cluster — and `pkg/ebpf.Loader`'s
`AttachXDP` has no guard against a second attach on the same interface, so
if two replicas land on the *same* node, the second Pod's XDP attach
silently replaces the first's, breaking that node's event stream. Use
`podAntiAffinity` to keep replicas on distinct nodes in this case:
  ```yaml
  affinity:
    podAntiAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        - labelSelector:
            matchLabels:
              app.kubernetes.io/name: sentinel5g-operator
          topologyKey: kubernetes.io/hostname
  ```

## Securing the NATS message bus

Anything that can reach the NATS URL can publish a forged `NormalizedEvent`
or `ThreatScoreEvent` on it. Since `ThreatScoreWatcher.handle` trusts every
message on `NATS_THREATS_SUBJECT` unconditionally, an unauthenticated bus
reachable from outside the cluster is a real way to trigger a live mitigation
(`EbpfBlock`/`IsolatePod`) against any workload a policy protects, by
publishing a few lines of forged JSON.

`pkg/config.Load()` (the operator) and `sentinel_ai/config.py`'s
`require_nats_credentials_if_unauthenticated_disallowed` (the AI engine,
checked in `AI_ENGINE_MODE=nats` before connecting) both refuse to start
against a NATS bus with none of the auth/TLS knobs below set, unless
`NATS_ALLOW_UNAUTHENTICATED=true` is set explicitly — that flag exists for
`scripts/quickstart.sh`'s throwaway demo cluster, not for a real install;
see `docs/production-install.md`.

Both sides support the same optional auth/TLS knobs, documented in
`.env.example`:

- `NATS_CREDENTIALS_FILE` — an NKey/JWT `.creds` file, the recommended option
  for a real NATS deployment (see the [NATS auth docs](https://docs.nats.io/running-a-nats-service/configuration/securing_nats)).
- `NATS_USERNAME` / `NATS_PASSWORD` — simple username/password auth.
- `NATS_TLS_CA_FILE`, `NATS_TLS_CERT_FILE`, `NATS_TLS_KEY_FILE` — TLS
  transport, with optional mTLS client certs.

At minimum outside of local dev, restrict who can reach the NATS port with a
NetworkPolicy even if you don't configure the above — the operator and the AI
engine are the only two clients that should ever be able to. The Helm chart
has an opt-in one built in: `networkPolicy.nats.enabled: true` plus
`networkPolicy.nats.podSelector` (matching NATS's actual Pods — this chart
doesn't deploy NATS itself, see `nats.url`'s comment in `values.yaml`) renders
a `NetworkPolicy` allowing ingress on `networkPolicy.nats.port` only from the
operator's own Pods and whatever's listed in
`networkPolicy.nats.additionalClients` (e.g. the AI engine's selector).
Rendering refuses with an explicit error if `enabled: true` is set without a
`podSelector` — an empty one would match every Pod in NATS's namespace, not
just NATS.

## Observability

See `docs/observability.md` for metrics and dashboards.
