#!/usr/bin/env bash
# Captures a LARGE benign GTP-U dataset from the native Open5GS+UERANSIM lab
# (docs/paper-data/test-environment.md) -- the "orders of magnitude more real
# benign traffic" §2.9 named as the only thing that tightens the
# false-positive-rate bound. Run with the core up and 4 UEs attached (see
# the lab notes). Two sessions: a large steady one for the FPR bound, and a
# diverse-payload one so the bound isn't measured only on one packet shape.
#
# The committed samples (benign_*_sample.pcap) are the first 15k packets of
# each; the full ~300k/40k captures are reproduced by raising the -c counts.
set -euo pipefail
OUT="${OUT:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)}"
GW="${GW:-10.45.0.1}"
STEADY="${STEADY:-300000}"
DIVERSE="${DIVERSE:-40000}"

tuns=(); while read -r t; do tuns+=("$t"); done < <(ip -o link show | grep -oE 'uesimtun[0-9]+' | sort -u)
[ "${#tuns[@]}" -ge 4 ] || { echo "need 4 attached UEs, found ${#tuns[@]}" >&2; exit 1; }

# Session 1: steady multi-subscriber load (normal active-data usage, 100 pkt/s
# per UE, 80-byte payload) -- the volume that drives the FPR bound.
echo "steady session ($STEADY packets)..."
for t in "${tuns[@]}"; do ( ping -I "$t" -i 0.01 -s 80 "$GW" >/dev/null 2>&1 & ); done
tcpdump -i lo -n -s 128 -c "$STEADY" -w "$OUT/benign_steady.pcap" 'udp port 2152'
pkill -x ping || true; sleep 1

# Session 2: diverse payloads and rates across the four UEs -- so the bound
# isn't measured on a single packet shape.
echo "diverse session ($DIVERSE packets)..."
ping -I "${tuns[0]}" -i 0.015 -s 512  "$GW" >/dev/null 2>&1 &
ping -I "${tuns[1]}" -i 0.03  -s 1200 "$GW" >/dev/null 2>&1 &
ping -I "${tuns[2]}" -i 0.05  -s 200  "$GW" >/dev/null 2>&1 &
ping -I "${tuns[3]}" -i 0.01  -s 900  "$GW" >/dev/null 2>&1 &
tcpdump -i lo -n -s 256 -c "$DIVERSE" -w "$OUT/benign_diverse.pcap" 'udp port 2152'
pkill -x ping || true
echo "done: $OUT/benign_steady.pcap, $OUT/benign_diverse.pcap"
