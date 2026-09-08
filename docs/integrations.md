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

## Mesh isolation: Istio

`pkg/mesh.IstioAdapter` quarantines a workload by creating a deny-all
`security.istio.io/v1` `AuthorizationPolicy` scoped to the policy's
`targetWorkloads` selector — no `istio.io/client-go` dependency is required,
since the adapter talks to the CRD via an unstructured `controller-runtime`
client. This means:

- The operator works against any cluster running Istio's
  `AuthorizationPolicy` CRD, regardless of Istio's Go client version.
- Quarantine is idempotent: the same selector always maps to the same
  policy name (see `pkg/mesh.quarantineName`), so repeated `Quarantine`
  calls update in place rather than accumulating stale objects.
- Clusters without Istio should set `MESH_ADAPTER=noop`
  (`mesh.adapter=noop` in `.env.example`, or the Helm chart's
  `config.meshAdapter`), which makes `IsolatePod` actions no-ops.

Other service meshes (Linkerd, Cilium's own mesh mode) are not implemented;
`pkg/mesh.Adapter` is the extension point — implement it and register it in
`pkg/mesh.NewAdapter`.

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

**Scaling `replicaCount` with eBPF enabled — two things to know:**

- `pkg/ingestion.Publisher` (the per-node ring-buffer reader) deliberately
  runs on *every* replica, not just the leader — `--bpf-interface` is
  attached per-pod, so each replica only ever sees its own node's traffic,
  and the operator needs every replica publishing for full coverage. This
  also means a plain `Deployment` gives you coverage on however many
  *distinct* nodes your replicas happen to land on — not the whole cluster
  the way a `DaemonSet` would guarantee (tracked as a real gap in
  `ROADMAP.md`, not yet implemented).
- `pkg/ebpf.Loader`'s `AttachXDP` has no guard against a second attach on
  the same interface: if two replicas land on the *same* node, the second
  pod's XDP attach silently replaces the first's, breaking that node's
  event stream. Use `podAntiAffinity` to keep replicas on distinct nodes,
  e.g. in the Helm chart's `values.yaml`:
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
engine are the only two clients that should ever be able to.

## Observability

See `docs/observability.md` for metrics and dashboards.
