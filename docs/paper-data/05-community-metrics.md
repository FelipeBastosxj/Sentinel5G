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

**Not applicable yet.** `release.yml` publishes images to
`ghcr.io/felipebastosxj/sentinel5g-operator` and
`ghcr.io/felipebastosxj/sentinel5g-ai-engine`, but only triggers on a
`v*.*.*` tag push. Locally, `git tag` does list `v0.2.0`/`v0.3.0`/`v0.4.0`
— but those point at commits from the legacy "EventStream" platform this
repo was repurposed from (pre `2ecce23`, "chore: remove legacy
event-ingestion platform"), not at any Sentinel5G release, and
`git ls-remote --tags origin` confirms none of them were ever pushed to
`origin` — so `release.yml` has never actually run. No image has been
published to GHCR, and there is no Docker Hub target configured in the
workflow at all — pulls can't be measured because no image exists to pull.
If those stale tags aren't wanted for anything, they're local-only cleanup
(`git tag -d v0.2.0 v0.3.0 v0.4.0`); left alone here since deleting tags
wasn't asked for.

**Pending — next step:** once a first `v0.1.0`-style tag is cut, GHCR
exposes pull counts under the package's own page
(`github.com/FelipeBastosxj/Sentinel5G/pkgs/container/<name>`, or via `gh
api /orgs/.../packages` / `/users/.../packages` for a personal account) —
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
