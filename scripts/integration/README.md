# Integration checks against real daemons

These run the capture-path components against the **real** external daemons
they translate, which unit tests (bufconn, hand-built fixtures) cannot cover.
They are manual lab exercises — not CI — because they stand up Falco and a
Cilium/Hubble cluster. Recorded results are in
`docs/paper-data/04-validation-testing-logs.md` §4.11.

| Script | What it proves |
|---|---|
| `falco_bridge_e2e.sh` | A real Falco daemon's alert, over its own `http_output`, becomes a `NormalizedEvent` on NATS via `cmd/falco-bridge`. |
| `hubble_observer_e2e.sh` | Real Cilium flows, streamed from a real Hubble Relay, become `NormalizedEvent`s via `pkg/hubble.Observer` (and non-GTP-U/SIP flows are correctly ignored). |

Both need Docker, a recent kernel with BTF (Falco's modern eBPF), `kind`,
`helm`, and a local NATS on `localhost:4222`. They create throwaway
containers / a throwaway `kind` cluster and tear them down.
