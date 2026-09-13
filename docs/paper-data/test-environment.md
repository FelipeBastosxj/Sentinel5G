# Test environment

Every measurement under `docs/paper-data/` was taken against a real 5G core,
not a simulator of one. This file records which core, because the answer
changed and the earlier answer was not in the repository at all: several
files here referenced `memory/wsl2_real_test_environment.md`, a path that
never existed as a committed file. Those references now point here.

## Current: native Linux (2026-09-11 onwards)

Used for `real-dataset-v2/` and §2.6 of
[`02-ai-training-inference.md`](02-ai-training-inference.md).

| | |
|---|---|
| Host | Linux 6.14.0-37-generic, x86_64, Ubuntu 24.04 |
| Core | Open5GS 2.8.0 (`ppa:open5gs/latest`), all NFs on loopback |
| RAN/UE | UERANSIM, built from source at `48554b7` |
| Subscriber DB | MongoDB 7 in a container, `127.0.0.1:27017` |
| PLMN | MCC 999 / MNC 70, SST 1 |
| N2 (NGAP) | AMF `127.0.0.5:38412` |
| N3 (GTP-U) | gNB `127.0.0.1` → UPF `127.0.0.7`, port 2152 |
| UE subnet | `10.45.0.0/16`, gateway `10.45.0.1` (`ogstun`) |
| Subscribers | 4 (`999700000000001`–`4`), the UERANSIM test key/OPc |

The N3 addressing is deliberately identical to the WSL2 environment below,
so captures from the two are directly comparable rather than merely similar.

### Building it

```sh
sudo add-apt-repository -y ppa:open5gs/latest
sudo apt-get update && sudo apt-get install -y open5gs
# Open5GS's UDR/PCF need MongoDB, which Ubuntu 24.04 doesn't package.
docker run -d --name open5gs-mongo --restart unless-stopped \
  -p 127.0.0.1:27017:27017 mongo:7
sudo systemctl restart open5gs-udrd open5gs-pcfd

sudo apt-get install -y cmake make g++ libsctp-dev lksctp-tools
git clone https://github.com/aligungr/UERANSIM.git && make -C UERANSIM -j"$(nproc)"
```

Register four subscribers (all sharing UERANSIM's default test key
`465B5CE8B199B49FAA5F0A2EE238A6BC` / OPc
`E8ED289DEBA952E4283B54E88E6183CA`), then:

```sh
sudo UERANSIM/build/nr-gnb -c gnb.yaml            # from config/open5gs-gnb.yaml
for i in 1 2 3 4; do sudo UERANSIM/build/nr-ue -c "ue$i.yaml" & done
ip -o -4 addr show | grep uesimtun   # expect four tunnels, 10.45.0.2-.5
```

Four attached UEs give **four distinct TEIDs sharing one source IP**
(`127.0.0.1`, the gNB's N3 address). That is the property the whole
per-tunnel design exists for and the one the previous environment could not
produce.

### Two traps worth knowing before generating load

Both cost real time here, and both fail *silently* — the generator reports a
perfectly successful transfer while not a single packet enters the tunnel:

- **Binding a source address is not enough.** The UE's tunnel address and the
  UPF gateway are both local addresses on this host, so the kernel
  short-circuits traffic between them through loopback. `iperf3 -B 10.45.0.3`
  measures loopback, not GTP-U.
- **UERANSIM's `nr-binder` uses a relative `LD_PRELOAD`** (`./libdevbnd.so`),
  so it only takes effect when the caller's working directory happens to be
  UERANSIM's build directory. Anywhere else the preload quietly fails and the
  wrapped process runs unbound.

`real-dataset-v2/gen_tunnel_flood.py` uses `SO_BINDTODEVICE` instead, which
binds the socket to the TUN device itself and cannot be short-circuited.
`ping -I <tun>` is also reliable, which is why the well-behaved UEs use it.

### Measured throughput ceilings

Worth recording because an earlier caveat attributed a low ceiling to WSL2,
and that turned out to be wrong:

| Generator | Rate through one tunnel |
|---|---|
| `ping -f -I uesimtun0` | ~89 pkt/s |
| `iperf3 -u` over loopback (not the tunnel) | ~125,000 pkt/s |
| `gen_tunnel_flood.py --pps 3000` | 2,989 pkt/s, as requested |

`ping -f`'s ~89 pkt/s on native Linux is barely above the ~63 pkt/s measured
inside WSL2 — so **that ceiling was a property of flood-ping as a generator,
not of WSL2**. `docs/paper-data/02-ai-training-inference.md` §2.4 attributed
it to the environment and left "a non-WSL2, higher-throughput environment" as
open work; §2.6 corrects that. A real flood is not rate-limited anywhere near
flood-ping's number.

## Previous: WSL2 (2026-09-06 / 2026-09-07)

Used for `normal.pcap`/`storm.pcap`, `real-dataset/`, and
`01-performance-benchmarks.md`. Same Open5GS + UERANSIM topology and the same
N3 loopback addressing, running under WSL2 on Windows, with a **single UE**.
That single-subscriber limit is why every capture in `real-dataset/` carries
one TEID, which bounds what those captures can validate — see that
directory's README and §2.5.1.
