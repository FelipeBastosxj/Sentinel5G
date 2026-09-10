"""`python -m sentinel_ai.healthcheck` — what the Dockerfile's HEALTHCHECK
instruction actually runs. A plain script, not an HTTP client library
import beyond urllib (stdlib-only), so it works the same regardless of
which extras happen to be installed in a given image build.

Mode-aware because AI_ENGINE_MODE is a runtime choice, not a build-time
one, and the two modes expose different things to check: HTTP mode has
`/healthz` on its own app; the NATS worker has no HTTP app at all (see
server.py's create_app/run_nats_worker split), only the dedicated
Prometheus /metrics server main() starts for that mode — reachability of
that is the closest analogue to "is this process alive and serving"
available in NATS mode.

Exits 0 on a 200 response, non-zero otherwise (an uncaught urllib
exception already exits non-zero on its own).
"""

from __future__ import annotations

import os
import sys
import urllib.request


def _target_url() -> str:
    mode = os.environ.get("AI_ENGINE_MODE", "http")
    if mode == "nats":
        addr = os.environ.get("AI_ENGINE_METRICS_ADDR", "0.0.0.0:9090")
        port = addr.rsplit(":", 1)[-1] or "9090"
        return f"http://127.0.0.1:{port}/metrics"

    addr = os.environ.get("AI_ENGINE_HTTP_ADDR", "0.0.0.0:8090")
    port = addr.rsplit(":", 1)[-1] or "8090"
    return f"http://127.0.0.1:{port}/healthz"


def main() -> None:
    url = _target_url()
    # nosec B310: url is built from a fixed http://127.0.0.1 template above, never from
    # untrusted input, so the usual urlopen-with-arbitrary-scheme concern doesn't apply.
    with urllib.request.urlopen(url, timeout=3) as resp:  # nosec B310
        if resp.status != 200:
            print(f"healthcheck: unexpected status {resp.status} from {url}", file=sys.stderr)
            sys.exit(1)


if __name__ == "__main__":
    main()
