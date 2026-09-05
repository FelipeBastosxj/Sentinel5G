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
  `--bpf-object`/`--bpf-interface` directly to `cmd/operator`) **and** add
  the required capabilities to the manager container's `securityContext`.
- `cmd/operator/main.go` logs (rather than fails) when the eBPF attach
  fails, so a misconfigured capability set degrades to mesh-only isolation
  instead of crash-looping the operator.

## Observability

See `docs/observability.md` for metrics and dashboards.
