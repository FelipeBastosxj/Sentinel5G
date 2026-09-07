#!/usr/bin/env python3
"""Real UDP packets to random high ports (1024-65535, excluding 2152/5060)
on the UPF's real address 127.0.0.7 -- approximating an off-protocol
probing/scan pattern (see generate_synthetic_dataset.py's _anomalous_event
"scan" branch). Real on-wire packets to real random ports.
"""
import random
import socket
import time

DEST_HOST = "127.0.0.7"
COUNT = 700
RATE = 20  # pkt/s

sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
rng = random.Random(13)  # nosec B311 - reproducible test traffic generation
interval = 1.0 / RATE
excluded = {2152, 5060}
for _ in range(COUNT):
    port = rng.randint(1024, 65535)
    while port in excluded:
        port = rng.randint(1024, 65535)
    payload_size = rng.randint(0, 64)
    sock.sendto(bytes(rng.getrandbits(8) for _ in range(payload_size)), (DEST_HOST, port))
    time.sleep(interval)
print(f"sent {COUNT} scan-shaped packets")
