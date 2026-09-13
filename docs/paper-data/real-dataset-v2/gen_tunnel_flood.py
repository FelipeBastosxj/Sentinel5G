#!/usr/bin/env python3
"""Floods one UE's GTP-U tunnel at a controlled packet rate.

Why this exists rather than `iperf3 -B <ue-addr>` or UERANSIM's own
nr-binder, both of which were tried first and both of which failed
*silently*:

- `iperf3 -B 10.45.0.3` binds a source ADDRESS. The UE's tunnel address and
  the UPF gateway are both local addresses on this host, so the kernel
  short-circuits the traffic straight through loopback. iperf3 reports a
  perfectly successful 5 Mbit/s transfer and not one packet enters the GTP-U
  tunnel.
- `nr-binder` sets `LD_PRELOAD=./libdevbnd.so` with a RELATIVE path, so it
  only takes effect when the caller's working directory happens to be
  UERANSIM's build directory. Run from anywhere else the preload fails, the
  wrapped process runs unbound, and you get the same silent loopback result.

Both failure modes look identical to success in the generator's own output;
the only way to catch them is to parse the capture and notice the flooding
UE's TEID is absent. This script uses SO_BINDTODEVICE instead, which binds
the socket to the TUN device itself and cannot be short-circuited. It needs
root for that.

Usage:
  sudo ./gen_tunnel_flood.py --device uesimtun0 --dest 10.45.0.1 \
      --pps 3000 --seconds 12
"""

from __future__ import annotations

import argparse
import socket
import time


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--device", required=True, help="UE TUN device, e.g. uesimtun0")
    parser.add_argument("--dest", default="10.45.0.1", help="destination IP (the UPF gateway)")
    parser.add_argument("--port", type=int, default=9999)
    parser.add_argument("--pps", type=int, default=3000, help="target packets per second")
    parser.add_argument("--seconds", type=float, default=12.0)
    parser.add_argument(
        "--size",
        type=int,
        default=200,
        help="UDP payload bytes, before the ~50 bytes of GTP-U/UDP/IP encapsulation",
    )
    args = parser.parse_args()

    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    # The whole point. Without it the kernel routes by destination and the
    # traffic never reaches the tunnel -- see this module's docstring.
    sock.setsockopt(socket.SOL_SOCKET, socket.SO_BINDTODEVICE, args.device.encode())

    payload = b"\xa5" * args.size
    interval = 1.0 / args.pps
    deadline = time.monotonic() + args.seconds
    sent = 0
    next_send = time.monotonic()

    while time.monotonic() < deadline:
        try:
            sock.sendto(payload, (args.dest, args.port))
            sent += 1
        except OSError:
            # A full socket buffer is expected at high rates and is not a
            # reason to stop -- the capture is the measurement, not this
            # counter.
            pass
        next_send += interval
        delay = next_send - time.monotonic()
        if delay > 0:
            time.sleep(delay)
        else:
            # Behind schedule: don't accumulate debt, just keep going.
            next_send = time.monotonic()

    sock.close()
    print(f"sent {sent} packets on {args.device} over {args.seconds:.0f}s")


if __name__ == "__main__":
    main()
