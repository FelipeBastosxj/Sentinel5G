//go:build linux

// Command xdp_saturation measures bpf/packet_filter.c's cost under a
// sustained, REAL packet load with a REAL consumer draining the ring — the
// measurement ROADMAP.md Phase 4 asks for and §1.4's bpftool-prog-run method
// explicitly does not provide (it runs with hot caches and no consumer).
//
// It builds a veth pair, attaches the XDP program to the RX end, starts the
// SignalingEvents consumer, and blasts valid GTP-U frames at the interface
// from the TX end as fast as one thread can, for a fixed duration. With
// BPF run-time stats enabled it then reports:
//
//   - the program's own CPU time per packet (run_time / run_count), which is
//     attach-mode-independent and so faithful even on a veth;
//   - the node CPU consumed over the run (/proc/stat delta), which on this
//     virtual path includes the veth/generic-XDP overhead a physical NIC
//     would replace with its own driver/IRQ cost — stated, not hidden;
//   - ring-buffer saturation: events the consumer received versus GTP-U
//     frames sent, the difference being observations the ring dropped when
//     full (the packet is never dropped, only the observation).
//
// Root only. Run via scripts/loadtest/cpu_saturation.sh, which sets
// kernel.bpf_stats_enabled and tears the veth down afterwards.
package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync/atomic"
	"time"

	"github.com/cilium/ebpf"
	"golang.org/x/sys/unix"

	sentinelebpf "github.com/FelipeBastosxj/Sentinel5G/pkg/ebpf"
)

func main() {
	obj := flag.String("obj", "bpf/packet_filter.o", "compiled BPF object")
	rxName := flag.String("rx", "s5gsat-rx", "XDP-attached (receive) veth name")
	txName := flag.String("tx", "s5gsat-tx", "transmit veth name")
	seconds := flag.Float64("seconds", 10, "load duration")
	flag.Parse()

	if os.Geteuid() != 0 {
		fail("must run as root")
	}

	// BPF run-time stats: the kernel then accumulates run_count/run_time for
	// every program, which is how the per-packet CPU cost is read back.
	closer, err := ebpf.EnableStats(unix.BPF_STATS_RUN_TIME)
	if err != nil {
		fail("enable bpf stats: %v (need kernel.bpf_stats_enabled support)", err)
	}
	defer closer.Close()

	// A fresh veth pair, torn down on exit. The TX side sends, the RX side
	// runs XDP — a real driver RX path, unlike loopback.
	run("ip", "link", "del", *rxName) // best-effort cleanup of a prior run
	// G204: fixed `ip` subcommands in a root-only loadtest tool, names from
	// flags the operator controls -- not an injection surface.
	if addErr := exec.Command("ip", "link", "add", *rxName, "type", "veth", "peer", "name", *txName).Run(); addErr != nil { //nolint:gosec
		fail("create veth pair: %v", addErr)
	}
	defer func() { _ = exec.Command("ip", "link", "del", *rxName).Run() }() //nolint:gosec
	run("ip", "link", "set", *rxName, "up")
	run("ip", "link", "set", *txName, "up")

	loader, err := sentinelebpf.Attach(*obj, *rxName)
	if err != nil {
		fail("attach XDP to %s: %v", *rxName, err)
	}
	defer loader.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var received int64
	stream, err := loader.SignalingEvents(ctx)
	if err != nil {
		fail("start consumer: %v", err)
	}
	go func() {
		for range stream {
			atomic.AddInt64(&received, 1)
		}
	}()

	frame := gtpuFrameV4()
	sock, ifindex := openTxSocket(*txName)
	defer unix.Close(sock)

	var addr unix.SockaddrLinklayer
	addr.Ifindex = ifindex
	addr.Halen = 6

	startCount, startRun, _ := loader.ProgramRunStats()
	cpuStart := readProcStat()
	wallStart := time.Now()

	var sent int64
	deadline := time.Now().Add(time.Duration(*seconds * float64(time.Second)))
	for time.Now().Before(deadline) {
		for i := 0; i < 2000; i++ {
			if serr := unix.Sendto(sock, frame, 0, &addr); serr == nil {
				sent++
			}
		}
	}
	wall := time.Since(wallStart)

	// Let the consumer drain the last of the ring.
	time.Sleep(300 * time.Millisecond)
	cancel()

	endCount, endRun, err := loader.ProgramRunStats()
	if err != nil {
		fail("read program stats: %v", err)
	}
	cpuEnd := readProcStat()

	runs := endCount - startCount
	runTime := endRun - startRun
	got := atomic.LoadInt64(&received)

	pps := float64(sent) / wall.Seconds()
	var nsPerPkt float64
	if runs > 0 {
		nsPerPkt = float64(runTime.Nanoseconds()) / float64(runs)
	}
	progCPUpct := float64(runTime.Nanoseconds()) / float64(wall.Nanoseconds()) * 100
	nodeCPUpct := cpuBusyPercent(cpuStart, cpuEnd)

	fmt.Printf("duration            %.1fs\n", wall.Seconds())
	fmt.Printf("frames sent         %d\n", sent)
	fmt.Printf("send rate           %.0f pkt/s (one thread)\n", pps)
	fmt.Printf("program executions  %d\n", runs)
	fmt.Printf("events consumed     %d\n", got)
	if runs > 0 {
		dropped := int64(runs) - got //nolint:gosec // runs is a packet count, far below int64 range
		fmt.Printf("ring dropped obs.   %d (%.1f%% of executions, ring full under load)\n",
			dropped, 100*float64(dropped)/float64(runs))
	}
	fmt.Printf("program CPU/packet  %.0f ns (bpf run_time/run_count, attach-mode-independent)\n", nsPerPkt)
	fmt.Printf("program CPU share   %.2f%% of one core over the run\n", progCPUpct)
	fmt.Printf("node CPU (all cores)%.2f%% busy during the run (veth+generic-XDP path, not a physical NIC)\n", nodeCPUpct)
}

// gtpuFrameV4 builds an Ethernet+IPv4+UDP(2152)+GTP-U T-PDU frame matching
// scripts/loadtest/gen_packets.py (flags 0x34, one extension header).
func gtpuFrameV4() []byte {
	inner := 64
	gtp := []byte{0x34, 0xFF}
	l := make([]byte, 2)
	binary.BigEndian.PutUint16(l, uint16(4+4+inner))
	gtp = append(gtp, l...)
	t := make([]byte, 4)
	binary.BigEndian.PutUint32(t, 0x4D84)
	gtp = append(gtp, t...)
	gtp = append(gtp, 0x00, 0x00, 0x00, 0x85, 0x01, 0x00, 0x00, 0x00)
	for i := 0; i < inner; i++ {
		gtp = append(gtp, 0xA5)
	}

	udp := make([]byte, 8)
	binary.BigEndian.PutUint16(udp[0:], 2152)
	binary.BigEndian.PutUint16(udp[2:], 2152)
	binary.BigEndian.PutUint16(udp[4:], uint16(8+len(gtp))) //nolint:gosec // frame is a few hundred bytes
	udp = append(udp, gtp...)

	ip := make([]byte, 20)
	ip[0] = 0x45
	binary.BigEndian.PutUint16(ip[2:], uint16(20+len(udp))) //nolint:gosec // frame is a few hundred bytes
	ip[8] = 64
	ip[9] = 17
	copy(ip[12:16], []byte{127, 0, 0, 1})
	copy(ip[16:20], []byte{127, 0, 0, 7})
	binary.BigEndian.PutUint16(ip[10:], ipChecksum(ip))
	ip = append(ip, udp...)

	eth := make([]byte, 14)
	copy(eth[0:6], []byte{0x02, 0, 0, 0, 0, 0x01})
	copy(eth[6:12], []byte{0x02, 0, 0, 0, 0, 0x02})
	binary.BigEndian.PutUint16(eth[12:], 0x0800)
	return append(eth, ip...)
}

func ipChecksum(h []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(h); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(h[i:]))
	}
	for sum>>16 != 0 {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}
	return ^uint16(sum)
}

func openTxSocket(name string) (int, int) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		fail("resolve %s: %v", name, err)
	}
	sock, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, int(htons(unix.ETH_P_ALL)))
	if err != nil {
		fail("AF_PACKET socket: %v", err)
	}
	return sock, iface.Index
}

func htons(v uint16) uint16 { return (v<<8)&0xFF00 | v>>8 }

// --- /proc/stat CPU accounting ---

type cpuTimes struct{ idle, total uint64 }

func readProcStat() cpuTimes {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuTimes{}
	}
	var fields [10]uint64
	var cpu string
	n, _ := fmt.Sscanf(string(data), "%s %d %d %d %d %d %d %d %d %d %d",
		&cpu, &fields[0], &fields[1], &fields[2], &fields[3], &fields[4],
		&fields[5], &fields[6], &fields[7], &fields[8], &fields[9])
	_ = n
	var total uint64
	for _, f := range fields {
		total += f
	}
	return cpuTimes{idle: fields[3], total: total}
}

func cpuBusyPercent(a, b cpuTimes) float64 {
	dt := b.total - a.total
	di := b.idle - a.idle
	if dt == 0 {
		return 0
	}
	return float64(dt-di) / float64(dt) * 100
}

func run(args ...string) { _ = exec.Command(args[0], args[1:]...).Run() } //nolint:gosec // fixed `ip` commands, root-only tool

func fail(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, "xdp_saturation: "+format+"\n", a...)
	os.Exit(1)
}
