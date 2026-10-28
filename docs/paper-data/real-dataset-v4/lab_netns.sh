#!/usr/bin/env bash
# Puts each UERANSIM UE in its own network namespace, so application traffic
# (TCP/TLS, not only ping) makes the full round trip through the GTP-U tunnel.
#
# Why: on the stock lab (../test-environment.md) the UEs' uesimtunN
# interfaces live in the host namespace, so the host OWNS 10.45.0.x. A reply
# from a server on ogstun (10.45.0.1) to a UE is then delivered locally and
# never re-enters the tunnel -- ping's uplink still shows up on N3, but a TCP
# handshake never completes. With each UE in a namespace the only route back
# to 10.45.0.x is ogstun -> UPF -> GTP-U -> gNB -> UE, the real path.
#
# Topology: namespace s5g-ueN with a veth (10.201.N.2/24) to the host
# (10.201.N.1/24). The gNB's radio-link-simulation address moves to
# 10.201.1.1, which every namespace reaches through its default route as a
# LOCAL host address (no forwarding, no firewall or NAT change). N2/N3 stay
# on loopback, so the N3 capture is the same `tcpdump -i lo 'udp port 2152'`
# as real-dataset-v2/v3. The stock gnb.yaml/ueN.yaml are copied, never edited.
#
#   sudo ./lab_netns.sh up   [LAB_DIR]   # default LAB_DIR=~/sentinel5g-lab
#   sudo ./lab_netns.sh down [LAB_DIR]
set -euo pipefail

ACTION="${1:?usage: lab_netns.sh up|down [LAB_DIR]}"
LAB="${2:-${SUDO_USER:+$(getent passwd "$SUDO_USER" | cut -d: -f6)}/sentinel5g-lab}"
UERANSIM="${UERANSIM:-$(dirname "$LAB")/UERANSIM/build}"
WORK="$LAB/netns"
UES=(1 2 3 4)

down() {
	pkill -x nr-ue || true
	pkill -x nr-gnb || true
	sleep 1
	for i in "${UES[@]}"; do
		ip netns del "s5g-ue$i" 2>/dev/null || true
		ip link del "s5g-v$i" 2>/dev/null || true
	done
}

up() {
	down
	mkdir -p "$WORK"
	sed -E 's/^linkIp:.*/linkIp: 10.201.1.1/' "$LAB/gnb.yaml" >"$WORK/gnb.yaml"
	for i in "${UES[@]}"; do
		ip netns add "s5g-ue$i"
		ip link add "s5g-v$i" type veth peer name eth0 netns "s5g-ue$i"
		ip addr add "10.201.$i.1/24" dev "s5g-v$i"
		ip link set "s5g-v$i" up
		ip -n "s5g-ue$i" addr add "10.201.$i.2/24" dev eth0
		ip -n "s5g-ue$i" link set lo up
		ip -n "s5g-ue$i" link set eth0 up
		ip -n "s5g-ue$i" route add default via "10.201.$i.1"
		awk '/^gnbSearchList:/{print; getline; print "  - 10.201.1.1"; next} {print}' \
			"$LAB/ue$i.yaml" >"$WORK/ue$i.yaml"
	done
	# 3h ceilings: the lab never outlives a forgotten session.
	nohup timeout 3h "$UERANSIM/nr-gnb" -c "$WORK/gnb.yaml" >"$WORK/gnb.log" 2>&1 &
	sleep 3
	for i in "${UES[@]}"; do
		nohup ip netns exec "s5g-ue$i" timeout 3h "$UERANSIM/nr-ue" -c "$WORK/ue$i.yaml" \
			>"$WORK/ue$i.log" 2>&1 &
		sleep 2
	done
	for _ in $(seq 20); do
		n=0
		for i in "${UES[@]}"; do
			ip -n "s5g-ue$i" -br addr show uesimtun0 2>/dev/null | grep -q 10.45 && n=$((n + 1))
		done
		[ "$n" -eq "${#UES[@]}" ] && break
		sleep 1
	done
	for i in "${UES[@]}"; do
		printf 's5g-ue%s: ' "$i"
		ip -n "s5g-ue$i" -br addr show uesimtun0 2>/dev/null || echo "NO PDU SESSION (see $WORK/ue$i.log)"
	done
}

case "$ACTION" in
up) up ;;
down) down ;;
*) echo "usage: lab_netns.sh up|down [LAB_DIR]" >&2 && exit 2 ;;
esac
