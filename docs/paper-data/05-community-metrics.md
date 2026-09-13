# 5. Community Metrics (For the Immigration Dossier)

## 5.1 GitHub stats

**Measured** on 2026-09-06 via the public GitHub API
(`curl https://api.github.com/repos/FelipeBastosxj/Sentinel5G`) — no
authentication used, so this is exactly what any public visitor sees.

| Metric | Value | As of |
|---|---|---|
| Stars | 0 | 2026-09-06 |
| Forks | 0 | 2026-09-06 |
| Watchers/subscribers | 0 | 2026-09-06 |
| Open issues | 0 | 2026-09-06 |
| Repo created | 2026-09-05T20:20:42Z | — |
| Last push | 2026-09-06T13:10:37Z | — |

**Read honestly:** the repository is one day old at this snapshot (created
2026-09-05). Zero community-engagement numbers are the accurate, expected
state for a just-published repo, not a data gap — there is nothing to
under- or over-state here. Re-running the `curl` command above at
submission time will produce whatever the numbers have grown to by then;
this file should be regenerated close to when the dossier is actually
assembled, not reused from this early snapshot.

**Clone traffic** (`/traffic/clones`) is not included above: that endpoint
requires push-level authentication (repo admin token) and is not public —
it can only be pulled by the repo owner, e.g. via `gh api
repos/FelipeBastosxj/Sentinel5G/traffic/clones` while authenticated as
`FelipeBastosxj`, and GitHub itself only retains 14 days of history, so it
needs to be captured periodically (a recurring scheduled pull, or a manual
screenshot of the repo's Insights → Traffic page) rather than fetched once
at the end.

## 5.2 Container registry pulls (Docker Hub / GHCR)

**Superseded (2026-09-13).** An earlier draft of this section stated that
`release.yml` had never run and that no image had been published. That
was true at the 2026-09-06 snapshot and is not now: `v0.1.0`, `v0.2.0`,
`v0.2.1` and `v0.2.2` are Sentinel5G releases pushed to `origin` (see
`CHANGELOG.md`), `release.yml` publishes multi-arch images to both
`ghcr.io/felipebastosxj/*` and Docker Hub, and
`01-performance-benchmarks.md` §1.2 ran the `v0.1.0` images in-cluster.
The packages whose pull counts can be captured are:
`sentinel5g-operator`, `sentinel5g-ai-engine`, `sentinel5g-falco-bridge`,
the two OCI charts under `charts/`, and — from the first release after
`v0.2.2` — the trained model artifact `sentinel5g-model`.

**Pending — next step:** GHCR exposes pull counts under each package's own
page (`github.com/FelipeBastosxj/Sentinel5G/pkgs/container/<name>`, or via
`gh api /users/FelipeBastosxj/packages`); Docker Hub under the repository
page. Neither has been captured yet —
periodic screenshots or API pulls of that page are the mechanism, same
cadence consideration as the clone-traffic note above (GHCR's own download
counters are cumulative-since-publish, so no retention window problem
there, unlike git clone traffic).

## 5.3 Suggested recurring capture

Both 5.1 and 5.2 are point-in-time and decay in usefulness the longer
they're not refreshed before a dossier is actually filed. If this matters,
the lowest-effort real mechanism already available in this toolchain is a
scheduled pull of the public API endpoint above (stars/forks/watchers) plus
an authenticated `gh api .../traffic/clones` and `.../traffic/popular/referrers`
call, saved as dated JSON snapshots in this folder (e.g.
`05-community-metrics/2026-09-06.json`) rather than overwriting this file
each time — that preserves the actual growth curve, which is more
persuasive evidence than a single high-water-mark number.
