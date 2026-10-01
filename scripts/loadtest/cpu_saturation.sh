#!/usr/bin/env bash
# Measures bpf/packet_filter.c under a sustained, REAL packet load with a REAL
# consumer draining the ring -- the measurement ROADMAP.md Phase 4 asks for,
# and the one xdp_bench.sh's BPF_PROG_TEST_RUN method explicitly does not
# provide (hot caches, no consumer, no attach point).
#
# It builds a veth pair, attaches the XDP program to the RX end, starts the
# SignalingEvents consumer, and blasts valid GTP-U frames at it from the TX
# end for a few seconds, reporting the program's own per-packet CPU (from BPF
# run-time stats, attach-mode-independent), the node CPU over the run, and
# ring-buffer saturation (observations dropped vs frames sent).
#
# What it is and isn't: the RX path is a veth in generic (SKB) XDP mode -- a
# real driver RX path, unlike loopback, but NOT native XDP on a physical NIC.
# The program's run_time is faithful regardless (it is the BPF code's own
# on-CPU time); native XDP on real hardware would lower the program's
# per-packet cost and add driver/IRQ cost this path lacks, and a physical NIC
# is what a true line-rate (10/40/100 GbE) figure needs. The single-thread
# generator caps the offered load, not the program.
#
# Root only (attach + veth + AF_PACKET + bpf stats).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OBJ="${OBJ:-$REPO_ROOT/bpf/packet_filter.o}"
SECONDS_RUN="${SECONDS_RUN:-8}"

if [ "$(id -u)" -ne 0 ]; then
  echo "must run as root (sudo $0)" >&2
  exit 1
fi

[ -f "$OBJ" ] || { echo "no BPF object at $OBJ; run: make -C bpf" >&2; exit 1; }

# BPF run-time stats: the kernel then accumulates run_count/run_time per
# program, which the harness reads back. Left enabled is harmless but we
# restore it.
PRIOR="$(sysctl -n kernel.bpf_stats_enabled)"
sysctl -w kernel.bpf_stats_enabled=1 >/dev/null
trap 'sysctl -w kernel.bpf_stats_enabled="$PRIOR" >/dev/null 2>&1 || true' EXIT

BIN="$(mktemp)"
go build -o "$BIN" "$REPO_ROOT/scripts/loadtest/xdp_saturation"
trap 'rm -f "$BIN"; sysctl -w kernel.bpf_stats_enabled="$PRIOR" >/dev/null 2>&1 || true' EXIT

"$BIN" -obj "$OBJ" -seconds "$SECONDS_RUN"
