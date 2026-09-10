# Production install

`scripts/quickstart.sh` and README's "Quick Start" are a one-command demo:
they create their own `kind` cluster, bundle a NATS instance with no auth,
and apply a sample `TelecomSecurityPolicy` with `autoMitigate: true` +
`isolatePod: true` so a mitigation fires within seconds. None of that is
appropriate on a cluster carrying real traffic, and what a real install
needs instead is scattered across `docs/integrations.md`,
`charts/sentinel5g-operator/values.yaml`'s own comments, and
`docs/troubleshooting.md`. This page is the one ordered checklist, explicitly
**not** a script to paste — each step below links to where the actual detail
lives, and expects you to make a real decision at several of them.

See `ROADMAP.md`'s "Phase 2.5 — Production readiness" section for what's
still an open gap at this stage, in particular **the in-tunnel-flood
detection gap** (`docs/paper-data/02-ai-training-inference.md` §2.4) — read
that before deciding how much to trust `autoMitigate: true` against real
traffic.

## 1. Bring your own NATS JetStream, with auth or TLS

The Helm chart deliberately does not bundle NATS (`values.yaml`'s `nats.url`
comment) — bring a real one (the official [`nats` Helm
chart](https://github.com/nats-io/k8s/tree/main/helm/charts/nats) is a
reasonable default) with **JetStream enabled** and persistence configured
for your durability needs.

Configure at least one of `nats.credentialsFile` / `nats.username` +
`nats.existingPasswordSecret` / `nats.tlsCertFile`+`nats.tlsKeyFile` in
`values.yaml`. Both the operator and the AI engine now refuse to start
against a NATS bus with none of those set, unless `nats.allowUnauthenticated:
true` is explicitly passed — do not pass that flag here; it exists for the
demo cluster `scripts/quickstart.sh` owns end to end, not for a real
install. See `docs/integrations.md`'s "Securing the NATS message bus" for
why: anything able to reach an unauthenticated `NATS_URL` can forge a
`ThreatScoreEvent` and trigger a real mitigation against a real workload.

## 2. Install the operator (Helm)

Either from the repo (`charts/sentinel5g-operator`) or, without cloning it,
straight from the published OCI artifact:

```sh
helm install sentinel5g oci://ghcr.io/felipebastosxj/charts/sentinel5g-operator \
  --version 0.2.2 \
  --namespace sentinel5g-system --create-namespace \
  --set nats.url=nats://<your-nats-service>:4222 \
  --set nats.credentialsFile=/etc/nats/creds/sentinel5g.creds   # or username/existingPasswordSecret/tls*
```

`crds.keep` defaults to `true`: `helm uninstall` leaves the CRD (and every
`TelecomSecurityPolicy` riding on it) in place rather than deleting them
along with the release. If you genuinely want the CRD gone on uninstall,
set `crds.keep: false` — know that this takes every policy with it.

The operator starts and reconciles policies even before NATS is reachable
(it retries with backoff in the background); check `kubectl get pods -n
sentinel5g-system` and `kubectl describe pod` if it stays `0/1 Ready` for
longer than a minute or two — the `nats` readyz check reports the reason.

## 3. Deploy the AI engine, with a real model

`deployments/quickstart/ai-engine.yaml` needs a trained model
(`autoencoder.onnx` + `.onnx.data` + `.norm.json`) delivered via a Secret you
create yourself — these are deliberately not committed to the repo. Train
one against your own traffic, or at minimum the real dataset pipeline in
`docs/getting-started.md`'s "Train (or retrain) against the real dataset"
section, before relying on `autoMitigate: true` anywhere. Skipping this step
doesn't fail loudly: every policy just sits at `Phase: Monitoring` forever
with nothing publishing a `ThreatScoreEvent`.

Set the same NATS auth/TLS env vars here as step 1 — `NATS_ALLOW_UNAUTHENTICATED`
must not be set to `true` in a production deployment either.

## 4. Start in detection-only mode

Set `threatDetection.autoMitigate: false` on your first real
`TelecomSecurityPolicy` and watch `.status.phase` move to `Alerting` (not
`Mitigating`) once the threat score crosses your threshold — this is the
closed-loop path minus the actual mitigation, so you can measure real
false-positive rate before anything automated acts on it. Only flip
`autoMitigate: true` per policy once you're satisfied.

## 5. Opt into eBPF enforcement only after its preflight passes

`ebpf.enabled: true` needs **all** of: the right `ebpf.interface` for the
node's real NIC, `daemonset.enabled: true` on a multi-node cluster for real
node-wide coverage (a plain `Deployment` with `replicaCount > 1` instead
needs `podAntiAffinity` to keep replicas on distinct nodes, or a second
replica landing on the same node silently replaces the first's XDP attach —
see `docs/integrations.md`'s "eBPF blocklist enforcement" section for the
exact YAML), and a kernel new enough for the requested capabilities
(`ebpf.capabilities`, default `CAP_BPF`+`CAP_NET_ADMIN`, needs 5.8+; older
kernels need `CAP_SYS_ADMIN` instead). A failed attach is logged and
recorded as a `EBPFAttachFailed` Kubernetes Event against the operator's own
Pod (`kubectl describe pod`/`kubectl get events -n sentinel5g-system`) — it
degrades to mesh-isolation-only rather than crash-looping, but check for
that event rather than assuming enforcement is active.

## 6. Wire up observability

The metrics `Service` this chart renders is always there regardless of
`serviceMonitor.enabled`; set `serviceMonitor.enabled: true` only if
Prometheus Operator is actually installed on this cluster — if it isn't,
the chart now renders no `ServiceMonitor` (with a warning in the post-install
NOTES) instead of failing the install outright, but you still need a working
scrape path either way. See `docs/observability.md` for the metrics exposed
and the (currently unmeasured — see `ROADMAP.md` Phase 3) latency targets
they're meant to validate against.

## 7. Turn on automated mitigation, per policy

Once a policy's `Alerting` phase history from step 4 looks right, set
`threatDetection.autoMitigate: true` on it. There's no global switch — this
is deliberately per-`TelecomSecurityPolicy`, so you can roll it out one
protected workload at a time rather than flipping it cluster-wide.
