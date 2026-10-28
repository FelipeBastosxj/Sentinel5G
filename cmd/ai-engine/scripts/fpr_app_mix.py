"""Measures false positives on a real APPLICATION-MIX benign capture
(docs/paper-data/real-dataset-v4/), per application and per direction --
the residual §2.10 left open: its 340,000 benign packets were ICMP, not the
web / video / bulk / voice mix a production N3 carries.

Three detectors are scored on the same capture, every one of which would
turn into a mitigation under `autoMitigate: true`:

  A. the autoencoder AS SHIPPED -- trained on the committed real normal data,
     exactly like fpr_large_benign.py (what an install gets out of the box);
  B. the autoencoder CALIBRATED LOCALLY -- trained and calibrated on the
     first half of this capture, scored on the second half: the procedure
     §2.8/§2.10 prescribe for a deployment. With --holdout, it is ALSO
     calibrated on the whole capture and scored on a second, independent
     session (different seed, different time): the stricter form;
  C. the deterministic rules (chart defaults unless overridden) -- the per-tunnel flood
     rule (pkg/detect.GTPUFloodDetector) and the per-peer source-flood rule
     (pkg/detect.GTPUSourceFloodDetector), mirrored packet by packet.

Every tunnel is attributed to its application through the inner IP header
and the generator's `<capture>.roles.json`. "uplink" is gNB -> UPF, the
direction an XDP probe on the UPF's N3 interface sees.

Usage (from cmd/ai-engine/):
  python scripts/fpr_app_mix.py --capture app_mix_a.pcap [--holdout app_mix_b.pcap]

Each capture's roles are read from `<capture>.roles.json` beside it when it
exists (the lab generator writes one); on an operator's own capture, pass
`--gnb-ip` (the peer whose packets are uplink) and leave the roles out.
"""

from __future__ import annotations

import argparse
import json
import random
import struct
from collections import defaultdict
from functools import partial
from pathlib import Path

import numpy as np
import torch

from scripts.build_real_dataset import (
    _REAL_DATASET_DIR,
    SIGNALING_RATE_WINDOW_S,
    capture_groups,
    events_from_capture,
)
from scripts.fpr_confidence import THRESHOLDS, clopper_pearson_upper
from scripts.pcap_gtpu import _ip_offset, read_udp_packets
from sentinel_ai.features import extract_features
from sentinel_ai.model import Autoencoder, reconstruction_error, train

# charts/sentinel5g-operator/values.yaml defaults (pkg/config). Keep in sync
# by hand. The pre-2026-10-28 defaults §2.11.5 measured are 1,000 / 20,000 /
# 256; pass them with --tunnel-flood-pps etc. to reproduce that table.
TUNNEL_FLOOD_PPS = 100_000
TUNNEL_FLOOD_COOLDOWN_S = 30.0
SOURCE_FLOOD_PPS = 500_000
SOURCE_FLOOD_DISTINCT_TUNNELS = 2048

GNB_N3_IP = "127.0.0.1"  # lab gNB gtpIp; packets from it are uplink
RATE_SCALE = 100_000  # report "wrong mitigations/s" at this event rate


def _tunnel_owners(path: Path, roles: dict[str, str]) -> dict[tuple[str, int], str]:
    """(outer source IP, TEID) -> application, read from the inner IP header:
    the UE address is the inner source on uplink and the inner destination
    on downlink."""
    raw = path.read_bytes()
    endian = "<" if struct.unpack_from("<I", raw, 0)[0] in (0xA1B2C3D4, 0xA1B23C4D) else ">"
    link_type = struct.unpack_from(endian + "I", raw, 20)[0]
    owners: dict[tuple[str, int], str] = {}
    off = 24
    while off + 16 <= len(raw):
        caplen = struct.unpack_from(endian + "I", raw, off + 8)[0]
        frame = raw[off + 16 : off + 16 + caplen]
        off += 16 + caplen
        ip = _ip_offset(link_type, frame)
        if ip is None or len(frame) < ip + 20 or frame[ip + 9] != 17:
            continue
        gtp = ip + (frame[ip] & 0x0F) * 4 + 8
        if len(frame) < gtp + 8 or frame[gtp + 1] != 0xFF:
            continue
        teid = struct.unpack_from(">I", frame, gtp + 4)[0]
        src = ".".join(str(b) for b in frame[ip + 12 : ip + 16])
        if (src, teid) in owners:
            continue
        inner = gtp + 8
        if frame[gtp] & 0x07:  # E/S/PN: 4 optional bytes, then extensions
            nxt, inner = frame[gtp + 11], gtp + 12
            while nxt and inner < len(frame):
                ext_len = frame[inner] * 4
                if ext_len == 0:
                    break
                nxt = frame[inner + ext_len - 1]
                inner += ext_len
        if len(frame) < inner + 20 or frame[inner] >> 4 != 4:
            continue
        isrc = ".".join(str(b) for b in frame[inner + 12 : inner + 16])
        idst = ".".join(str(b) for b in frame[inner + 16 : inner + 20])
        owner = roles.get(isrc) or roles.get(idst)
        if owner:
            owners[(src, teid)] = owner
    return owners


def _score(model: Autoencoder, rows: np.ndarray, reference_error: float) -> np.ndarray:
    err = reconstruction_error(model, torch.from_numpy(rows)).numpy()
    return np.clip(err / (reference_error * 4.0), 0.0, 1.0)


def _fit(rows: np.ndarray, seed: int) -> tuple[Autoencoder, float]:
    model = Autoencoder(input_dim=rows.shape[1])
    torch.manual_seed(seed)
    train(model, torch.from_numpy(rows), epochs=200, lr=1e-2)
    ref = float(np.percentile(reconstruction_error(model, torch.from_numpy(rows)).numpy(), 99))
    return model, ref


def _fp_table(scores: np.ndarray, groups: np.ndarray, alpha: float) -> dict:
    """False positives per threshold tier, overall and per group."""
    out: dict = {}
    for tier, t in THRESHOLDS.items():
        flagged = scores >= t
        per = {}
        for g in sorted(set(groups.tolist())):
            mask = groups == g
            n, k = int(mask.sum()), int(flagged[mask].sum())
            per[g] = {
                "packets": n,
                "false_positives": k,
                "upper_bound_95": clopper_pearson_upper(k, n, alpha),
            }
        n, k = len(scores), int(flagged.sum())
        upper = clopper_pearson_upper(k, n, alpha)
        out[tier] = {
            "packets": n,
            "false_positives": k,
            "point_fpr": k / n if n else 0.0,
            "upper_bound_95": upper,
            f"wrong_mitigations_per_s_at_{RATE_SCALE}": round(upper * RATE_SCALE, 2),
            "by_group": per,
        }
    return out


def _rules(
    packets,
    tunnel_rates,
    owner_of,
    direction_of,
    tunnel_pps: int = TUNNEL_FLOOD_PPS,
    source_pps: int = SOURCE_FLOOD_PPS,
    distinct_tunnels: int = SOURCE_FLOOD_DISTINCT_TUNNELS,
) -> dict:
    """Mirrors the two Go rules packet by packet (chart defaults unless
    overridden)."""
    alerts: dict[str, int] = defaultdict(int)
    alerted_tunnels: dict[str, set] = defaultdict(set)
    last: dict[tuple[str, int], float] = {}
    peak: dict[str, int] = defaultdict(int)
    for p, rate in zip(packets, tunnel_rates):
        if p.teid == 0:
            continue
        key = (p.source_ip, p.teid)
        g = f"{owner_of.get(key, 'unattributed')}/{direction_of(p)}"
        peak[g] = max(peak[g], rate)
        if rate >= tunnel_pps:
            if key not in last or p.timestamp - last[key] >= TUNNEL_FLOOD_COOLDOWN_S:
                last[key] = p.timestamp
                alerts[g] += 1
                alerted_tunnels[g].add(key)

    # Source flood: one window per peer, packets and distinct TEIDs in it.
    src_peak_pps: dict[str, int] = defaultdict(int)
    src_peak_teids: dict[str, int] = defaultdict(int)
    win: dict[str, tuple[float, int, set]] = {}
    for p in packets:
        start, count, teids = win.get(p.source_ip, (None, 0, set()))
        if start is None or p.timestamp - start > SIGNALING_RATE_WINDOW_S:
            start, count, teids = p.timestamp, 0, set()
        count += 1
        if p.teid:
            teids.add(p.teid)
        win[p.source_ip] = (start, count, teids)
        src_peak_pps[p.source_ip] = max(src_peak_pps[p.source_ip], count)
        src_peak_teids[p.source_ip] = max(src_peak_teids[p.source_ip], len(teids))

    return {
        "tunnel_flood": {
            "threshold_pps": tunnel_pps,
            "cooldown_s": TUNNEL_FLOOD_COOLDOWN_S,
            "alerts": dict(sorted(alerts.items())),
            "tunnels_alerted": {g: len(v) for g, v in sorted(alerted_tunnels.items())},
            "peak_tunnel_pps": dict(sorted(peak.items())),
        },
        "source_flood": {
            "threshold_pps": source_pps,
            "threshold_distinct_tunnels": distinct_tunnels,
            "peak_pps": dict(src_peak_pps),
            "peak_distinct_tunnels": dict(src_peak_teids),
            "alerts": sum(
                1
                for s in src_peak_pps
                if (source_pps and src_peak_pps[s] >= source_pps)
                or (distinct_tunnels and src_peak_teids[s] >= distinct_tunnels)
            ),
        },
    }


def _detection(model: Autoencoder, ref: float, groups) -> dict:
    """Share of each committed REAL attack capture the model flags, per tier.
    A locally calibrated model that flags no benign packet is only worth
    something if it still flags these."""
    out = {}
    for g in groups:
        if g.label != 1 or g.synthetic:
            continue
        rows = np.asarray([extract_features(e) for e in g.events], dtype=np.float32)
        scores = _score(model, rows, ref)
        out[g.name] = {"packets": len(g.events)} | {
            f"flagged_{tier}": round(float(np.mean(scores >= t)), 4)
            for tier, t in THRESHOLDS.items()
        }
    return out


def _direction(p, gnb_ip: str = GNB_N3_IP) -> str:
    return "uplink" if p.source_ip == gnb_ip else "downlink"


def _load(path: Path, rng: random.Random, gnb_ip: str = GNB_N3_IP):
    """One capture -> feature rows, per-packet group labels and the raw
    packets/events the rule mirror needs. Without a `<capture>.roles.json`
    (an operator's own capture) every tunnel is "unattributed" and the
    per-direction totals still hold."""
    roles_file = path.with_suffix(".roles.json")
    roles = json.loads(roles_file.read_text()) if roles_file.exists() else {}
    owner_of = _tunnel_owners(path, roles)
    packets = read_udp_packets(path)
    # Same treatment fpr_large_benign.py gives a scored capture: rates from
    # the real timestamps, absolute time-of-day spread (build_real_dataset.py
    # explains why), so a capture taken at one hour is not "anomalous" for it.
    events = events_from_capture(
        path, protocol="GTP-U", dest_port_override=2152, spread_time_of_day=rng
    )
    if len(events) != len(packets):
        raise RuntimeError(f"{path}: events and packets out of step")
    rows = np.asarray([extract_features(e) for e in events], dtype=np.float32)
    app = np.asarray([owner_of.get((p.source_ip, p.teid), "unattributed") for p in packets])
    direc = np.asarray([_direction(p, gnb_ip) for p in packets])
    group = np.char.add(np.char.add(app.astype(str), "/"), direc.astype(str))
    return rows, group, direc == "uplink", packets, events, owner_of


def main() -> None:
    ap = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    ap.add_argument("--capture", required=True, type=Path)
    ap.add_argument("--holdout", type=Path, help="independent session to score B against")
    ap.add_argument(
        "--gnb-ip", default=GNB_N3_IP, help="the gNB's N3 address; packets from it are uplink"
    )
    ap.add_argument("--tunnel-flood-pps", type=int, default=TUNNEL_FLOOD_PPS)
    ap.add_argument("--source-flood-pps", type=int, default=SOURCE_FLOOD_PPS)
    ap.add_argument(
        "--source-flood-distinct-tunnels", type=int, default=SOURCE_FLOOD_DISTINCT_TUNNELS
    )
    ap.add_argument("--alpha", type=float, default=0.05)
    ap.add_argument("--seed", type=int, default=42)
    ap.add_argument(
        "--calibration-rows",
        type=int,
        default=20_000,
        help="rows sampled from the first half to train/calibrate B",
    )
    args = ap.parse_args()

    rng = random.Random(args.seed)  # nosec B311
    uplink_of = partial(_direction, gnb_ip=args.gnb_ip)
    thresholds = {
        "tunnel_pps": args.tunnel_flood_pps,
        "source_pps": args.source_flood_pps,
        "distinct_tunnels": args.source_flood_distinct_tunnels,
    }
    rows, group, uplink, packets, events, owner_of = _load(args.capture, rng, args.gnb_ip)

    # A. As shipped.
    groups = capture_groups(_REAL_DATASET_DIR, args.seed)
    shipped_rows = np.asarray(
        [extract_features(e) for g in groups if g.label == 0 and not g.synthetic for e in g.events],
        dtype=np.float32,
    )
    model_a, ref_a = _fit(shipped_rows, args.seed)
    scores_a = _score(model_a, rows, ref_a)

    # B. Calibrated on the first half of this capture, scored on the second.
    half = len(rows) // 2
    np_rng = np.random.default_rng(args.seed)
    calib_idx = np_rng.choice(half, size=min(args.calibration_rows, half), replace=False)
    model_b, ref_b = _fit(rows[calib_idx], args.seed)
    scores_b = _score(model_b, rows[half:], ref_b)

    duration = packets[-1].timestamp - packets[0].timestamp if packets else 0.0
    out = {
        "detection_on_committed_attacks": {
            "A_shipped_model": _detection(model_a, ref_a, groups),
            "B_locally_calibrated": _detection(model_b, ref_b, groups),
        },
        "capture": args.capture.name,
        "packets": len(packets),
        "duration_s": round(duration, 1),
        "packets_by_group": {g: int((group == g).sum()) for g in sorted(set(group.tolist()))},
        "alpha": args.alpha,
        "A_shipped_model": {
            "trained_on_rows": int(len(shipped_rows)),
            "all": _fp_table(scores_a, group, args.alpha),
            "uplink": _fp_table(scores_a[uplink], group[uplink], args.alpha),
        },
        "B_locally_calibrated": {
            "trained_on_rows": int(len(calib_idx)),
            "scored_second_half_packets": int(len(rows) - half),
            "all": _fp_table(scores_b, group[half:], args.alpha),
            "uplink": _fp_table(scores_b[uplink[half:]], group[half:][uplink[half:]], args.alpha),
        },
        "C_rules": _rules(
            packets,
            [int(e.tunnel_rate_per_second) for e in events],
            owner_of,
            uplink_of,
            **thresholds,
        ),
    }
    if args.holdout:
        h_rows, h_group, h_up, h_packets, h_events, h_owner = _load(args.holdout, rng, args.gnb_ip)
        calib_idx = np_rng.choice(
            len(rows), size=min(args.calibration_rows, len(rows)), replace=False
        )
        model_c, ref_c = _fit(rows[calib_idx], args.seed)
        h_scores = _score(model_c, h_rows, ref_c)
        out["detection_on_committed_attacks"]["B_cross_session_model"] = _detection(
            model_c, ref_c, groups
        )
        out["B_cross_session"] = {
            "calibrated_on": args.capture.name,
            "trained_on_rows": int(len(calib_idx)),
            "scored": args.holdout.name,
            "scored_packets": int(len(h_rows)),
            "duration_s": round(h_packets[-1].timestamp - h_packets[0].timestamp, 1),
            "all": _fp_table(h_scores, h_group, args.alpha),
            "uplink": _fp_table(h_scores[h_up], h_group[h_up], args.alpha),
        }
        out["C_rules_on_holdout"] = _rules(
            h_packets,
            [int(e.tunnel_rate_per_second) for e in h_events],
            h_owner,
            uplink_of,
            **thresholds,
        )
    print(json.dumps(out, indent=2))


if __name__ == "__main__":
    main()
