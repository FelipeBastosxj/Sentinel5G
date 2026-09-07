#!/usr/bin/env python3
"""Real but tiny/garbage UDP packets to the N3 socket (127.0.0.7:2152),
payload sizes in the 0-8 byte range -- approximating the "malformed" shape
sentinel_ai's synthetic generator models (generate_synthetic_dataset.py's
_anomalous_event "malformed" branch: payload_size U(0,8) bytes). Real on-wire
packets, not synthetic feature vectors.
"""
import random
import socket
import time

DEST = ("127.0.0.7", 2152)
COUNT = 700
RATE = 20  # pkt/s, moderate -- not a flood, this dimension is payload size, not rate

sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
rng = random.Random(7)  # nosec B311 - reproducible test traffic generation
interval = 1.0 / RATE
for _ in range(COUNT):
    size = rng.randint(0, 8)
    sock.sendto(bytes(rng.getrandbits(8) for _ in range(size)), DEST)
    time.sleep(interval)
print(f"sent {COUNT} malformed-shaped packets")
