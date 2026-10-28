#!/usr/bin/env python3
"""Captures a realistic APPLICATION-MIX benign GTP-U dataset from the native
Open5GS + UERANSIM lab -- the residual real-dataset-v3/ left open: its
340,000 benign packets were ICMP through the tunnels, not the video / web /
messaging mix a production N3 carries.

Each UE plays one fixed role, concurrently, so every TEID in the capture
maps to exactly one application class:

  s5g-ue1  web browsing   DNS lookups, then an HTML page and 8-40 assets
                          over up to 6 parallel keep-alive TLS connections,
                          with exponential think time between pages
  s5g-ue2  video stream   DASH-style: 4 s segments at 2.5 Mbit/s, a
                          3-segment start-up burst, then one per segment
                          period; 2-6 min sessions with gaps between
  s5g-ue3  bulk download  20-60 MB transfers at full speed (an app update,
                          a file sync) every 45-150 s, small background
                          sync requests in between
  s5g-ue4  voice + chat   RTP-shaped calls (G.711: 50 pkt/s, 172 B each
                          way) of 1-4 min, chat messages (0.5-4 KB POSTs),
                          the occasional photo upload (1-3 MB), DNS

The servers run on the lab host, bound to the UPF's ogstun address
(10.45.0.1), so nothing leaves the machine and no firewall or NAT change is
needed: every byte still crosses gNB -> GTP-U -> UPF -> ogstun and back,
which is what the capture sees. The traffic is real kernel TCP/TLS (OpenSSL)
and UDP; the endpoints are local, so the round-trip time is the lab's, not
the internet's (see README.md, "Honest scope").

Each UE runs in its own network namespace (lab_netns.sh): with the UEs in
the host namespace the host owns their addresses, a server's reply never
re-enters the tunnel, and no TCP handshake completes. Inside its namespace
each client also binds to uesimtun0 (SO_BINDTODEVICE), so nothing can leak
out of the veth instead of the tunnel.

Run as root with the core up:

  sudo ./lab_netns.sh up
  sudo python3 gen_app_mix.py --minutes 30 --out /path/to/app_mix.pcap
  sudo ./lab_netns.sh down

The capture is `tcpdump -i lo 'udp port 2152'` (N3 on loopback) for the
whole run.
"""

from __future__ import annotations

import argparse
import http.client
import http.server
import json
import os
import random
import re
import socket
import socketserver
import ssl
import struct
import subprocess
import sys
import tempfile
import threading
import time
from pathlib import Path

SERVER_IP = "10.45.0.1"
HTTPS_PORT = 8443
RTP_PORT = 5004
DNS_PORT = 53

_BLOCK = os.urandom(1 << 20)  # served bodies are slices of this


# ---------------------------------------------------------------- servers --


class _Handler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"  # keep-alive, like every real client/CDN

    def log_message(self, *args) -> None:  # noqa: D102 - quiet
        pass

    def do_GET(self) -> None:  # noqa: N802 - http.server API
        m = re.match(r"^/b/(\d+)", self.path)
        n = int(m.group(1)) if m else 1024
        self.send_response(200)
        self.send_header("Content-Type", "application/octet-stream")
        self.send_header("Content-Length", str(n))
        self.end_headers()
        while n > 0:
            chunk = _BLOCK[: min(n, len(_BLOCK))]
            self.wfile.write(chunk)
            n -= len(chunk)

    def do_POST(self) -> None:  # noqa: N802 - http.server API
        length = int(self.headers.get("Content-Length", "0"))
        while length > 0:
            got = self.rfile.read(min(length, 65536))
            if not got:
                break
            length -= len(got)
        body = b'{"ok":true}'
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


class _TLSServer(socketserver.ThreadingMixIn, http.server.HTTPServer):
    daemon_threads = True
    allow_reuse_address = True


def start_https(certdir: Path) -> _TLSServer:
    cert, key = certdir / "cert.pem", certdir / "key.pem"
    subprocess.run(
        [
            "openssl",
            "req",
            "-x509",
            "-newkey",
            "rsa:2048",
            "-nodes",
            "-days",
            "1",
            "-subj",
            "/CN=sentinel5g-lab",
            "-keyout",
            str(key),
            "-out",
            str(cert),
        ],
        check=True,
        capture_output=True,
    )
    ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    ctx.load_cert_chain(cert, key)
    srv = _TLSServer((SERVER_IP, HTTPS_PORT), _Handler)
    srv.socket = ctx.wrap_socket(srv.socket, server_side=True)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv


def start_rtp_echo() -> socket.socket:
    """The far end of a call: echoes each RTP packet, so the call has the
    symmetric 50 pkt/s in each direction a real two-party call has."""
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    s.bind((SERVER_IP, RTP_PORT))

    def loop() -> None:
        while True:
            try:
                data, addr = s.recvfrom(2048)
                s.sendto(data, addr)
            except OSError:
                return

    threading.Thread(target=loop, daemon=True).start()
    return s


def start_dns() -> subprocess.Popen:
    """dnsmasq answering every name with the lab server -- a real DNS
    server, so the queries and responses are real DNS on the wire."""
    return subprocess.Popen(
        [
            "dnsmasq",
            "--no-daemon",
            "--no-resolv",
            "--no-hosts",
            "--bind-interfaces",
            f"--listen-address={SERVER_IP}",
            f"--port={DNS_PORT}",
            f"--address=/#/{SERVER_IP}",
            "--pid-file=",
            "--conf-file=/dev/null",
        ],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )


# ---------------------------------------------------------------- clients --


def _bound_socket(dev: str, kind: int) -> socket.socket:
    s = socket.socket(socket.AF_INET, kind)
    s.setsockopt(socket.SOL_SOCKET, socket.SO_BINDTODEVICE, dev.encode())
    return s


class _TunnelHTTPS(http.client.HTTPSConnection):
    """An HTTPS connection whose socket is bound to one UE's tunnel."""

    def __init__(self, dev: str) -> None:
        ctx = ssl.create_default_context()
        ctx.check_hostname = False
        ctx.verify_mode = ssl.CERT_NONE  # lab self-signed cert  # nosec B501
        super().__init__(SERVER_IP, HTTPS_PORT, context=ctx, timeout=60)
        self._dev = dev

    def connect(self) -> None:
        sock = _bound_socket(self._dev, socket.SOCK_STREAM)
        sock.settimeout(self.timeout)
        sock.connect((self.host, self.port))
        self.sock = self._context.wrap_socket(sock, server_hostname=self.host)


def _get(conn: _TunnelHTTPS, size: int) -> None:
    conn.request("GET", f"/b/{size}")
    resp = conn.getresponse()
    while resp.read(65536):
        pass


def _post(conn: _TunnelHTTPS, size: int) -> None:
    conn.request(
        "POST", "/up", body=os.urandom(size), headers={"Content-Type": "application/octet-stream"}
    )
    resp = conn.getresponse()
    resp.read()


_NAMES = [
    "www.example.com",
    "cdn.example.net",
    "api.example.org",
    "img.example.com",
    "fonts.example.net",
    "static.example.org",
    "chat.example.com",
]


def _dns_lookup(dev: str, rng: random.Random) -> None:
    name = rng.choice(_NAMES)
    qid = rng.randrange(1 << 16)
    q = struct.pack(">HHHHHH", qid, 0x0100, 1, 0, 0, 0)
    q += b"".join(bytes([len(p)]) + p.encode() for p in name.split(".")) + b"\x00"
    q += struct.pack(">HH", 1, 1)  # A, IN
    s = _bound_socket(dev, socket.SOCK_DGRAM)
    s.settimeout(2)
    try:
        s.sendto(q, (SERVER_IP, DNS_PORT))
        s.recvfrom(512)
    except OSError:
        pass
    finally:
        s.close()


def _lognormal_bytes(rng: random.Random, median: float, sigma: float, cap: int) -> int:
    return max(200, min(cap, int(rng.lognormvariate(0, sigma) * median)))


def web(dev: str, stop: float, rng: random.Random) -> None:
    while time.time() < stop:
        for _ in range(rng.randint(1, 3)):
            _dns_lookup(dev, rng)
        conns = [_TunnelHTTPS(dev) for _ in range(rng.randint(2, 6))]
        try:
            _get(conns[0], _lognormal_bytes(rng, 60_000, 0.6, 400_000))  # the HTML
            assets = [
                _lognormal_bytes(rng, 25_000, 1.2, 2_000_000) for _ in range(rng.randint(8, 40))
            ]
            lock = threading.Lock()

            def worker(c: _TunnelHTTPS) -> None:
                while True:
                    with lock:
                        if not assets:
                            return
                        size = assets.pop()
                    _get(c, size)

            ts = [threading.Thread(target=worker, args=(c,)) for c in conns]
            for t in ts:
                t.start()
            for t in ts:
                t.join()
        except OSError:
            pass
        finally:
            for c in conns:
                c.close()
        time.sleep(min(rng.expovariate(1 / 8.0), 45))


def video(dev: str, stop: float, rng: random.Random) -> None:
    seg_s, bitrate = 4.0, 2_500_000
    seg_bytes = int(seg_s * bitrate / 8)
    while time.time() < stop:
        session_end = min(stop, time.time() + rng.uniform(120, 360))
        conn = _TunnelHTTPS(dev)
        try:
            _get(conn, 30_000)  # manifest
            for _ in range(3):  # start-up burst fills the buffer
                _get(conn, seg_bytes)
            nxt = time.time()
            while time.time() < session_end:
                nxt += seg_s
                _get(conn, int(seg_bytes * rng.uniform(0.8, 1.2)))  # VBR
                time.sleep(max(0.0, nxt - time.time()))
        except OSError:
            pass
        finally:
            conn.close()
        time.sleep(rng.uniform(10, 40))


def bulk(dev: str, stop: float, rng: random.Random) -> None:
    while time.time() < stop:
        conn = _TunnelHTTPS(dev)
        try:
            _get(conn, rng.randint(20, 60) * 1_000_000)
            idle_end = time.time() + rng.uniform(45, 150)
            while time.time() < min(idle_end, stop):
                _get(conn, rng.randint(1_000, 20_000))  # background sync
                time.sleep(rng.uniform(5, 20))
        except OSError:
            pass
        finally:
            conn.close()


def voice_chat(dev: str, stop: float, rng: random.Random) -> None:
    def call() -> None:
        s = _bound_socket(dev, socket.SOCK_DGRAM)
        s.settimeout(0.001)
        seq, ts = rng.randrange(1 << 16), rng.randrange(1 << 32)
        ssrc = rng.randrange(1 << 32)
        end = time.time() + rng.uniform(60, 240)
        nxt = time.time()
        while time.time() < min(end, stop):
            hdr = struct.pack(">BBHII", 0x80, 0, seq & 0xFFFF, ts & 0xFFFFFFFF, ssrc)
            s.sendto(hdr + os.urandom(160), (SERVER_IP, RTP_PORT))  # 20 ms G.711
            seq, ts = seq + 1, ts + 160
            try:
                s.recvfrom(2048)
            except OSError:
                pass
            nxt += 0.02
            time.sleep(max(0.0, nxt - time.time()))
        s.close()

    def chat(until: float) -> None:
        conn = _TunnelHTTPS(dev)
        try:
            while time.time() < min(until, stop):
                if rng.random() < 0.05:
                    _post(conn, rng.randint(1_000_000, 3_000_000))  # a photo
                else:
                    _post(conn, rng.randint(500, 4_000))
                if rng.random() < 0.2:
                    _dns_lookup(dev, rng)
                time.sleep(rng.uniform(3, 20))
        except OSError:
            pass
        finally:
            conn.close()

    while time.time() < stop:
        gap_end = time.time() + rng.uniform(20, 60)
        chat(gap_end)
        call()


ROLES = {"web": web, "video": video, "bulk": bulk, "voice_chat": voice_chat}
NAMESPACES = {"s5g-ue1": "web", "s5g-ue2": "video", "s5g-ue3": "bulk", "s5g-ue4": "voice_chat"}
TUN = "uesimtun0"  # the first (only) tunnel inside each UE namespace


def main() -> None:
    ap = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    ap.add_argument("--minutes", type=float, default=30)
    ap.add_argument("--out", type=Path)
    ap.add_argument("--seed", type=int, default=42)
    ap.add_argument("--role", choices=ROLES, help=argparse.SUPPRESS)  # child mode
    ap.add_argument("--until", type=float, help=argparse.SUPPRESS)
    args = ap.parse_args()

    if os.geteuid() != 0:
        sys.exit("run as root: the namespaces, SO_BINDTODEVICE and the capture need it")

    if args.role:  # one UE's client, already inside its namespace
        ROLES[args.role](TUN, args.until, random.Random(args.seed))
        return

    if args.out is None:
        sys.exit("--out is required")
    have = subprocess.run(["ip", "netns", "list"], capture_output=True, text=True).stdout
    missing = [ns for ns in NAMESPACES if ns not in have]
    if missing:
        sys.exit(f"UE namespaces missing {missing}: run ./lab_netns.sh up first")

    # Which UE address played which role: the analysis attributes every
    # tunnel to an application through the inner IP header.
    roles = {}
    for ns, role in NAMESPACES.items():
        out = subprocess.run(
            ["ip", "-n", ns, "-4", "-o", "addr", "show", TUN], capture_output=True, text=True
        ).stdout
        m = re.search(r"inet (\d+\.\d+\.\d+\.\d+)/", out)
        if not m:
            sys.exit(f"{ns} has no PDU session on {TUN}")
        roles[m.group(1)] = role
    args.out.with_suffix(".roles.json").write_text(json.dumps(roles, indent=2) + "\n")

    seconds = int(args.minutes * 60)
    with tempfile.TemporaryDirectory() as tmp:
        start_https(Path(tmp))
        rtp = start_rtp_echo()
        dns = start_dns()
        time.sleep(1)
        # -G/-W 1: tcpdump ends itself after `seconds`. Never rely on
        # signalling it -- its AppArmor profile can refuse the signal.
        cap = subprocess.Popen(
            [
                "tcpdump",
                "-i",
                "lo",
                "-n",
                "-s",
                "160",
                "-G",
                str(seconds),
                "-W",
                "1",
                "-w",
                str(args.out),
                "udp port 2152",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        time.sleep(2)
        until = time.time() + seconds + 15  # traffic outlasts the capture window
        children = [
            subprocess.Popen(
                [
                    "ip",
                    "netns",
                    "exec",
                    ns,
                    sys.executable,
                    "-I",
                    os.path.abspath(__file__),
                    "--role",
                    role,
                    "--until",
                    str(until),
                    "--seed",
                    str(args.seed + i),
                ]
            )
            for i, (ns, role) in enumerate(NAMESPACES.items())
        ]
        print(f"generating for {args.minutes} min -> {args.out}", flush=True)
        cap.wait()
        for c in children:
            try:
                c.wait(timeout=max(0.0, until - time.time()) + 30)
            except subprocess.TimeoutExpired:
                c.kill()
        dns.terminate()
        rtp.close()
    print("done", flush=True)


if __name__ == "__main__":
    main()
