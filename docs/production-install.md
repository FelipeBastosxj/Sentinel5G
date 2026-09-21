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

See `ROADMAP.md`'s "Phase 2.5 — Production readiness" section for what it
closed and, more usefully, what it explicitly did *not* validate. The
in-tunnel-flood detection gap (`docs/paper-data/02-ai-training-inference.md`
§2.4) is closed — by a per-tunnel rate in the kernel and a deterministic
detector (§2.5), with the model now catching floods above ~1,000 pkt/s on
its own (§2.6) — but every threshold involved was calibrated on lab
captures, not a production N3, and the mitigation is still keyed by source
IP (step 5). Read §2.6 before deciding how much to trust
`autoMitigate: true` against real traffic.

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

The AI engine has its own chart, separate from the operator's — it is a
separate deployment with its own scaling and its own model:

```sh
helm install sentinel5g-ai-engine oci://ghcr.io/felipebastosxj/charts/sentinel5g-ai-engine \
  --version 0.2.2 -n sentinel5g-system \
  --set nats.url=nats://your-nats:4222 \
  --set nats.username=sentinel5g --set nats.existingPasswordSecret=nats-creds
```

It needs a trained model (`autoencoder.onnx` + `.onnx.data` + `.norm.json`),
and **the chart refuses to install without one** rather than coming up
healthy and silently never scoring. Two ways to supply it:

- **`model.image` (the default).** Each release publishes a signed model
  artifact trained from the committed real dataset, and the chart pulls it
  with an initContainer. Read what that model actually is before trusting it
  in production: two short lab sessions of GTP-U on one Open5GS core — a
  single UE for about an hour, then four UEs for a few minutes
  (`docs/paper-data/real-dataset/`, `real-dataset-v2/`,
  `test-environment.md` state the limits). It is a reproducible starting
  point that has been shown not to flag traffic from a different day; it
  is not a model of your network. The first release to publish it is the
  one after `v0.2.2`; `model.image.tag` must name a tag that exists.
- **`model.existingSecret`.** Your own model, trained against your own
  traffic — the right answer before relying on `autoMitigate: true`
  anywhere. See `docs/getting-started.md`'s "Training against real captures
  instead" section, then:

  ```sh
  kubectl create secret generic ai-engine-model -n sentinel5g-system \
    --from-file=autoencoder.onnx=cmd/ai-engine/models/autoencoder.onnx \
    --from-file=autoencoder.onnx.data=cmd/ai-engine/models/autoencoder.onnx.data \
    --from-file=autoencoder.norm.json=cmd/ai-engine/models/autoencoder.norm.json
  helm install ... --set model.image.repository=null \
    --set model.existingSecret=ai-engine-model
  ```

The two are mutually exclusive; setting both fails the render with a message
saying so.

The chart failing to render is only the first line of defence; an install
that reaches a model but can't reach the bus, or one done by hand without the
chart, still ends up publishing nothing. The operator reports that on the
policies themselves. Confirm the scoring half is actually running before
moving on:

```sh
kubectl get tsp -A
# SCORING must read True. False + NoThreatScoresReceived means the operator
# is subscribed but no score has ever arrived -- i.e. this step was skipped,
# or the AI engine is running without a model.
```

It reports `False`/`AwaitingFirstScore` for the first `SCORING_PIPELINE_GRACE`
(`config.scoringPipelineGrace`, default `10m`) after the operator starts,
which covers an install where the AI engine comes up second. `kubectl
describe tsp` carries the full reason and message.

The chart's `nats.*` values mirror the operator's one for one, including
`allowUnauthenticated` — which must stay `false` here for the same reason it
must there. Both halves have to point at the same bus and the same subjects,
or the engine scores events nobody consumes.

## 4. Start in detection-only mode

Set `threatDetection.autoMitigate: false` on your first real
`TelecomSecurityPolicy` and watch `.status.phase` move to `Alerting` (not
`Mitigating`) once the threat score crosses your threshold — this is the
closed-loop path minus the actual mitigation, so you can measure real
false-positive rate before anything automated acts on it. Only flip
`autoMitigate: true` per policy once you're satisfied.

Measure it rather than eyeballing `.status.phase`: every one of those
withheld mitigations increments
`sentinel5g_threshold_crossings_total{outcome="alerting"}` on the operator's
`/metrics`. See `docs/observability.md`'s "Measuring a detection-only
pilot's false-positive rate" for the queries.

Two sources feed that counter, and the pilot is the place to tune both:

- The autoencoder's score. Its thresholds (`THREAT_SCORE_THRESHOLD` ×
  the policy's `sensitivity`) were validated against lab captures, not
  your traffic — `docs/paper-data/02-ai-training-inference.md` is explicit
  about what those captures do and don't cover.
- The deterministic GTP-U tunnel-flood rule (`config.gtpuTunnelFlood`, on
  by default, `docs/integrations.md`). Its `packetsPerSecond` default of
  1,000 per *tunnel* is reasoned rather than measured against a production
  N3; `sentinel5g_threat_scores_received_total{source="rule"}` tells you
  how often it is the one firing, and the alerting counter tells you
  whether that would have been right.

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

**On an N3 interface, choose the right eBPF action.** There are two, and
they differ in blast radius:

- `actions.ebpfBlock` drops by **source address**. On N3 that address is
  the peer gNB's and every subscriber behind it shares it, so a mitigation
  triggered by one flooding subscriber drops them all.
- `actions.ebpfBlockTunnel` drops by **(source, TEID)** — the same key the
  detector measures — so exactly the subscriber that was identified is cut
  off and the others keep working. This is what you want on N3.

They are independent. Enabling only the tunnel action means a score that
carries no TEID results in no drop at all (deliberately: it will not
silently widen to the whole gNB), which you can watch as
`sentinel5g_mitigations_total{action="ebpf_block_tunnel",result="no_teid"}`.
Enable `ebpfBlock` alongside it only if a source-wide drop is a fallback
you actually want. Note the two capture paths that cannot produce a TEID at
all — Hubble and Falco (`docs/integrations.md`) — make the tunnel action
permanently inert on their own.

What is still blunter than the detection: `isolatePod` quarantines the
whole workload, because the mesh layer cannot see a GTP-U tunnel
(`ROADMAP.md` Phase 3).

## 6. Wire up observability

The metrics `Service` each chart renders is always there regardless of
`serviceMonitor.enabled`; set `serviceMonitor.enabled: true` (on both
charts — the AI engine's metrics port is `config.metricsAddr`, `:9090` in
NATS mode) only if Prometheus Operator is actually installed on this
cluster — if it isn't, the charts render no `ServiceMonitor` (with a warning
in the post-install NOTES) instead of failing the install outright, but you
still need a working scrape path either way. The one alert to wire before
anything else: `sum(rate(sentinel5g_threat_scores_received_total[15m])) == 0`
for longer than you'd tolerate, which is "the scoring half is dead" as a
number. See `docs/observability.md` for the metrics exposed
and the latency targets they're meant to validate against — two of the
three are measured (`docs/paper-data/01-performance-benchmarks.md` §1.4,
reproducible via `scripts/loadtest/`); the CPU-per-node target is not, and
is labelled as a target rather than a result.

## 7. Turn on automated mitigation, per policy

Once a policy's `Alerting` phase history from step 4 looks right, set
`threatDetection.autoMitigate: true` on it. There's no global switch — this
is deliberately per-`TelecomSecurityPolicy`, so you can roll it out one
protected workload at a time rather than flipping it cluster-wide.
