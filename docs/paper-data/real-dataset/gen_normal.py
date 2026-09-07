#!/usr/bin/env python3
"""Real varied baseline GTP-U traffic through the live UE PDU session
(docs/paper-data/real-dataset/real_normal.pcap). Cycles ping interval and
payload size so the capture has real shape variety instead of one constant
rate, unlike the original docs/paper-data/normal.pcap (~1 pkt/s only).
"""
import subprocess
import time

DEST = "10.45.0.1"
INTERVALS = [0.05, 0.1, 0.2, 0.5, 1.0]
SIZES = [8, 32, 64, 128, 256]
DURATION_S = 360

end = time.time() + DURATION_S
while time.time() < end:
    for interval in INTERVALS:
        for size in SIZES:
            count = max(3, round(2.0 / interval))
            subprocess.run(
                [
                    # -W 1: the tunnel's default gateway never replies (known
                    # WSL2-kernel TUN quirk, see memory/wsl2_real_test_
                    # environment.md) -- without a short wait timeout, ping's
                    # default ~10s post-send linger for a reply that will
                    # never come dominates the run, starving packet volume.
                    "ping", "-I", "uesimtun0", "-c", str(count),
                    "-i", str(interval), "-s", str(size), "-W", "1", "-q", DEST,
                ],
                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
            )
            if time.time() >= end:
                raise SystemExit
