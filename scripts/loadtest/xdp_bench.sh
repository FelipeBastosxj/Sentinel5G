#!/usr/bin/env bash
# Per-packet cost of bpf/packet_filter.c, measured in the kernel.
#
# Answers the first of docs/observability.md's SLOs (<0.2ms added per
# packet) and, via the sweep below, ROADMAP.md Phase 3's question about
# whether the signaling_events ring buffer holds up at telecom volume.
#
# Method: BPF_PROG_TEST_RUN (`bpftool prog run`) executes the real, loaded
# program against a real frame N times in the kernel and reports the mean.
# Read what that does and doesn't measure: hot caches, no NIC driver path,
# and -- critically -- no userspace consumer draining the ring buffer, so
# once the ring fills every subsequent submit fails fast and the reported
# cost DROPS. That is why the sweep reports several repeat counts instead
# of one number: the ring-limited figure is the honest per-packet cost, the
# post-saturation one is what the program costs when it has given up
# emitting.
#
# Needs root (bpftool prog load / run).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OBJ="${OBJ:-$REPO_ROOT/bpf/packet_filter.o}"
PIN="${PIN:-/sys/fs/bpf/sentinel5g_bench}"
PKT_DIR="${PKT_DIR:-/tmp/sentinel5g-loadtest}"
# 8192 = the ring buffer's 256KB / a 32-byte record: the point where a
# program with no consumer stops being able to emit.
REPEATS="${REPEATS:-1000 4000 8192 50000 1000000}"

[ "$(id -u)" -eq 0 ] || { echo "needs root (bpftool)" >&2; exit 1; }
[ -f "$OBJ" ] || { echo "$OBJ not found -- run 'make -C bpf' first" >&2; exit 1; }

"$(dirname "${BASH_SOURCE[0]}")/gen_packets.py" --out-dir "$PKT_DIR" >/dev/null

cleanup() { rm -f "$PIN"; }
trap cleanup EXIT

printf '%-22s %10s %12s %12s\n' packet repeat ns/packet "ring state"
for pkt in gtpu sip junk_on_gtpu_port off_port; do
  for n in $REPEATS; do
    # A fresh load per measurement, so each starts with an empty ring
    # buffer and empty maps rather than inheriting the previous run's.
    rm -f "$PIN"
    bpftool prog load "$OBJ" "$PIN"
    ns="$(bpftool prog run pinned "$PIN" data_in "$PKT_DIR/$pkt.bin" \
            data_out /dev/null repeat "$n" 2>&1 |
          grep -oE 'duration \(average\): [0-9]+' | grep -oE '[0-9]+$')"
    state=live
    [ "$n" -gt 8192 ] && state="saturated"
    printf '%-22s %10s %12s %12s\n' "$pkt" "$n" "${ns:-?}" "$state"
  done
done

rm -f "$PIN"
bpftool prog load "$OBJ" "$PIN"
echo
echo "map capacities and types (the LRU-vs-HASH question in ROADMAP.md Phase 3):"
# Only this program's maps: `bpftool map show` lists every map on the host,
# including any left pinned by another run, so the listing is filtered to
# the ids this load actually created.
ids="$(bpftool prog show pinned "$PIN" | grep -oE 'map_ids [0-9,]+' | cut -d' ' -f2 | tr ',' ' ')"
for id in $ids; do
  bpftool map show id "$id" | tr '\n' ' ' | sed 's/  */ /g; s/^/  /; s/$/\n/'
done
rm -f "$PIN"
