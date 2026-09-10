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
   is what `cmd/operator` does today.
2. **Cilium-native:** compile the same detection logic as a Cilium
   [custom BPF program](https://docs.cilium.io/) or export equivalent
   signals via Cilium's Hubble flow API instead of a second XDP attachment
   point on the same interface — two independent XDP programs cannot both
   own the same attachment point without an explicit multi-prog dispatcher.
   This integration is tracked in `ROADMAP.md` and not implemented in this
   scaffold.

Falco can run alongside Sentinel5G purely as a complementary
syscall-level detection signal; there is no code-level integration point
today since Falco alerts and Sentinel5G's `ThreatScoreEvent`s use different
schemas. Bridging them (e.g. a Falco gRPC output plugin that emits
`NormalizedEvent`s) is a natural roadmap item.

## Mesh isolation: Istio, Cilium

Both adapters share the same pattern: no vendored mesh-specific Go client
(`istio.io/client-go`, `github.com/cilium/cilium`'s API module) is required,
since each talks to its CRD via an unstructured `controller-runtime` client
— the operator works against any cluster running the relevant CRD,
regardless of that mesh's own Go client version. Both are idempotent: the
same selector always maps to the same policy object name (see
`pkg/mesh.quarantineName`, shared by both adapters), so repeated
`Quarantine` calls update in place rather than accumulating stale objects.
`buildMeshAdapter` (`cmd/operator/main.go`) checks the relevant CRD is
actually registered before building either adapter, falling back to
`mesh.NoopAdapter` (with a log line explaining why) if it isn't — the same
"degrade gracefully" rule `EbpfBlock` already follows when eBPF isn't
attached.

**`pkg/mesh.IstioAdapter`** quarantines a workload by creating a deny-all
`security.istio.io/v1` `AuthorizationPolicy` scoped to the policy's
`targetWorkloads` selector (`spec.action: DENY` with a single empty rule —
see that file's own comment for why an empty `rules: []` doesn't work,
found by testing against a real cluster).

**`pkg/mesh.CiliumAdapter`** (`MESH_ADAPTER=cilium`) quarantines a workload
by creating a `cilium.io/v2` `CiliumNetworkPolicy` with `ingressDeny`/
`egressDeny` rules using the `"all"` reserved entity — deliberately not an
empty `endpointSelector`/`ingress`/`egress` ("allow nothing"): Cilium's own
docs are explicit that "deny policies take precedence over allow policies,"
but merely adding no allow rules doesn't override an *existing* allow policy
for the same pod from elsewhere (e.g. a baseline "allow same-namespace"
policy many clusters run) — only an explicit deny does. `"all"` is used
rather than an empty selector for the same reason: an empty
`fromEndpoints`/`toEndpoints` only covers Cilium-managed endpoints inside
the cluster, while `"all"` is documented as covering the cluster **and**
`world` (external) traffic, matching `IstioAdapter`'s full deny-all scope
rather than a narrower intra-cluster-only one.

Clusters without either mesh should leave `MESH_ADAPTER` unset or set it to
`noop` (`mesh.adapter=noop` in `.env.example`, or the Helm chart's
`config.meshAdapter`), which makes `IsolatePod` actions no-ops.

Linkerd is not implemented — its policy model (`policy.linkerd.io`
`Server`/`AuthorizationPolicy`) is scoped per-port (`Server.spec.port` is
required, no wildcard), unlike Istio's and Cilium's workload-wide deny,
and `pkg/mesh.Adapter.Quarantine`'s signature carries no port information to
scope a `Server` by. A real implementation needs either extending `Adapter`
to be port-aware, or discovering the target Pods' declared container ports
at `Quarantine`-call time (itself an incomplete guarantee — a container's
declared `ports:` in its spec is documentation only, not enforced, so a
port the workload actually listens on but didn't declare would stay
reachable). `pkg/mesh.Adapter` is the extension point for this or any other
mesh — implement it and register it in `pkg/mesh.NewAdapter`.

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
  the cause shows up in the log instead of a bare error string.

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

`pkg/events.Connect` (Go) and `sentinel_ai/server.py`'s NATS worker (Python)
default to a plain, unauthenticated `nats://` connection — fine for local dev,
but anything that can reach that URL can publish a forged `NormalizedEvent`
or `ThreatScoreEvent` on it. Since `ThreatScoreWatcher.handle` trusts every
message on `NATS_THREATS_SUBJECT` unconditionally, an unauthenticated bus
reachable from outside the cluster is a real way to trigger a live mitigation
(`EbpfBlock`/`IsolatePod`) against any workload a policy protects, by
publishing a few lines of forged JSON.

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
