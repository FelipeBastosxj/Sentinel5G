#!/usr/bin/env python3
"""Real UDP flood directly at the UPF's N3 socket (127.0.0.7:2152), bypassing
the UE tunnel. NOT protocol-conformant GTP-U (no real TEID/GTP header, just a
plain UDP payload sized to match the ~100-byte GTP-U packets in the existing
captures) -- included specifically for storm rate-range coverage, since the
in-tunnel ping flood (real_storm_pingflood.pcap) tops out around ~62 pkt/s on
this kernel, far below the synthetic training distribution's storm range
(800-4500/s, see generate_synthetic_dataset.py). These packets are real,
on-wire, hitting the real production port -- just not real GTP-U framing.
"""
import socket
import time

DEST = ("127.0.0.7", 2152)
PAYLOAD = b"\x00" * 92  # ~100 bytes on the wire incl. UDP/IP headers, matching real_normal.pcap
TIERS = [(500, 5), (2000, 5), (4000, 5)]  # (packets/sec, duration_s)

sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
for rate, duration in TIERS:
    interval = 1.0 / rate
    end = time.time() + duration
    sent = 0
    while time.time() < end:
        sock.sendto(PAYLOAD, DEST)
        sent += 1
        time.sleep(interval)
    print(f"tier rate={rate}/s target duration={duration}s sent={sent}")
