//go:build linux

package ebpf

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"golang.org/x/sys/unix"
)

const (
	blocklistMapName         = "blocklist"
	signalRateMapName        = "signal_rate"
	scanRateMapName          = "scan_rate"
	signalingEventsMapName   = "signaling_events"
	blocklistV6MapName       = "blocklist_v6"
	signalRateV6MapName      = "signal_rate_v6"
	scanRateV6MapName        = "scan_rate_v6"
	signalingEventsV6MapName = "signaling_events_v6"
	xdpProgramName           = "xdp_packet_filter"
	blockedValue             = uint8(1)
)

// Loader attaches bpf/packet_filter.c (compiled to objPath by bpf/Makefile)
// as an XDP program on iface and exposes its blocklist maps. IPv6 is a
// parallel set of maps (the *V6 fields), not a unified 128-bit-capable
// scheme on the IPv4 ones — see bpf/packet_filter.c's top comment for why.
type Loader struct {
	collection      *ebpf.Collection
	link            link.Link
	blocklist       *ebpf.Map
	signalRate      *ebpf.Map
	scanRate        *ebpf.Map
	signalingEvents *ebpf.Map

	blocklistV6       *ebpf.Map
	signalRateV6      *ebpf.Map
	scanRateV6        *ebpf.Map
	signalingEventsV6 *ebpf.Map

	// bootTime is wall-clock "now" minus CLOCK_MONOTONIC "now", read once at
	// attach time, so SignalingEvents can convert the kernel's monotonic
	// bpf_ktime_get_ns() timestamps into real time.Time values: bootTime +
	// monotonic_ns == wall-clock observation time. Never re-read per event —
	// it's a fixed offset for the life of this boot.
	bootTime time.Time
}

// Attach loads the compiled BPF object at objPath and attaches its XDP
// program to the network interface named iface.
func Attach(objPath, iface string) (*Loader, error) {
	spec, err := ebpf.LoadCollectionSpec(objPath)
	if err != nil {
		return nil, fmt.Errorf("load bpf collection spec from %q: %w", objPath, err)
	}

	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		return nil, fmt.Errorf("instantiate bpf collection: %w", err)
	}

	prog, ok := coll.Programs[xdpProgramName]
	if !ok {
		coll.Close()
		return nil, fmt.Errorf("bpf object %q does not export program %q", objPath, xdpProgramName)
	}

	blocklist, ok := coll.Maps[blocklistMapName]
	if !ok {
		coll.Close()
		return nil, fmt.Errorf("bpf object %q does not export map %q", objPath, blocklistMapName)
	}

	signalRate, ok := coll.Maps[signalRateMapName]
	if !ok {
		coll.Close()
		return nil, fmt.Errorf("bpf object %q does not export map %q", objPath, signalRateMapName)
	}

	scanRate, ok := coll.Maps[scanRateMapName]
	if !ok {
		coll.Close()
		return nil, fmt.Errorf("bpf object %q does not export map %q", objPath, scanRateMapName)
	}

	signalingEvents, ok := coll.Maps[signalingEventsMapName]
	if !ok {
		coll.Close()
		return nil, fmt.Errorf("bpf object %q does not export map %q", objPath, signalingEventsMapName)
	}

	blocklistV6, ok := coll.Maps[blocklistV6MapName]
	if !ok {
		coll.Close()
		return nil, fmt.Errorf("bpf object %q does not export map %q", objPath, blocklistV6MapName)
	}

	signalRateV6, ok := coll.Maps[signalRateV6MapName]
	if !ok {
		coll.Close()
		return nil, fmt.Errorf("bpf object %q does not export map %q", objPath, signalRateV6MapName)
	}

	scanRateV6, ok := coll.Maps[scanRateV6MapName]
	if !ok {
		coll.Close()
		return nil, fmt.Errorf("bpf object %q does not export map %q", objPath, scanRateV6MapName)
	}

	signalingEventsV6, ok := coll.Maps[signalingEventsV6MapName]
	if !ok {
		coll.Close()
		return nil, fmt.Errorf("bpf object %q does not export map %q", objPath, signalingEventsV6MapName)
	}

	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		coll.Close()
		return nil, fmt.Errorf("resolve interface %q: %w: %w", iface, ErrInterfaceNotFound, err)
	}

	xdpLink, err := link.AttachXDP(link.XDPOptions{
		Program:   prog,
		Interface: ifi.Index,
	})
	if err != nil {
		coll.Close()
		return nil, fmt.Errorf("attach xdp program to %q: %w", iface, err)
	}

	bootTime, err := monotonicToWallClockOffset()
	if err != nil {
		xdpLink.Close()
		coll.Close()
		return nil, fmt.Errorf("compute monotonic-to-wall-clock offset: %w", err)
	}

	return &Loader{
		collection:        coll,
		link:              xdpLink,
		blocklist:         blocklist,
		signalRate:        signalRate,
		scanRate:          scanRate,
		signalingEvents:   signalingEvents,
		blocklistV6:       blocklistV6,
		signalRateV6:      signalRateV6,
		scanRateV6:        scanRateV6,
		signalingEventsV6: signalingEventsV6,
		bootTime:          bootTime,
	}, nil
}

// monotonicToWallClockOffset returns wall-clock "now" minus CLOCK_MONOTONIC
// "now", so that offset.Add(time.Duration(monotonicNs)) recovers the
// wall-clock time a monotonic reading (like bpf_ktime_get_ns()) corresponds
// to. Read once; the two clocks' relationship doesn't drift within a boot.
func monotonicToWallClockOffset() (time.Time, error) {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err != nil {
		return time.Time{}, fmt.Errorf("clock_gettime(CLOCK_MONOTONIC): %w", err)
	}
	monotonicNow := time.Duration(ts.Sec)*time.Second + time.Duration(ts.Nsec)
	return time.Now().UTC().Add(-monotonicNow), nil
}

// Block implements BlocklistUpdater.
func (l *Loader) Block(ip net.IP) error {
	m, key, err := l.blocklistMapAndKey(ip)
	if err != nil {
		return err
	}
	if err := m.Put(key, blockedValue); err != nil {
		return fmt.Errorf("insert %s into blocklist map: %w", ip, err)
	}
	return nil
}

// Unblock implements BlocklistUpdater.
func (l *Loader) Unblock(ip net.IP) error {
	m, key, err := l.blocklistMapAndKey(ip)
	if err != nil {
		return err
	}
	if err := m.Delete(key); err != nil {
		return fmt.Errorf("remove %s from blocklist map: %w", ip, err)
	}
	return nil
}

// blocklistMapAndKey picks blocklist vs blocklist_v6 and the matching key
// encoding based on ip's address family (see ipv4Key/ipv6Key).
func (l *Loader) blocklistMapAndKey(ip net.IP) (*ebpf.Map, []byte, error) {
	if ip.To4() != nil {
		key, err := ipv4Key(ip)
		return l.blocklist, key, err
	}
	key, err := ipv6Key(ip)
	return l.blocklistV6, key, err
}

// Close implements BlocklistUpdater.
func (l *Loader) Close() error {
	var errs []error
	if err := l.link.Close(); err != nil {
		errs = append(errs, fmt.Errorf("detach xdp link: %w", err))
	}
	l.collection.Close()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

var _ BlocklistUpdater = (*Loader)(nil)
var _ EventSource = (*Loader)(nil)

// rawSignalingEvent is the byte-exact Go mirror of bpf/packet_filter.c's
// `struct signaling_event` — field order, sizes, and the trailing 2-byte pad
// MUST stay in sync by hand with that struct's `packed, aligned(8)` layout.
type rawSignalingEvent struct {
	TimestampNs uint64
	Saddr       uint32
	Daddr       uint32
	DestPort    uint16
	PayloadSize uint16
	Protocol    uint8
	Malformed   uint8
	VlanID      uint16
}

// rawSignalingEventV6 is the byte-exact Go mirror of bpf/packet_filter.c's
// `struct signaling_event_v6` — same hand-kept-in-sync convention as
// rawSignalingEvent above.
type rawSignalingEventV6 struct {
	TimestampNs uint64
	Saddr       [16]byte
	Daddr       [16]byte
	DestPort    uint16
	PayloadSize uint16
	Protocol    uint8
	Malformed   uint8
	VlanID      uint16
}

// SignalingEvents implements EventSource: starts two background goroutines
// — one reading bpf/packet_filter.c's signaling_events ring buffer, one
// reading signaling_events_v6 — decoding each record and merging both onto
// one channel, until ctx is done. Two separate ringbufs (not one shared,
// discriminator-tagged buffer) keep the IPv4 wire format and decode path
// above byte-for-byte unchanged; see struct signaling_event_v6's comment in
// packet_filter.c. The returned channel closes once both readers have
// stopped (context cancellation or a fatal read error, e.g. the ring
// buffers being closed by Close()).
func (l *Loader) SignalingEvents(ctx context.Context) (<-chan SignalingEvent, error) {
	reader, err := ringbuf.NewReader(l.signalingEvents)
	if err != nil {
		return nil, fmt.Errorf("open signaling_events ring buffer: %w", err)
	}

	readerV6, err := ringbuf.NewReader(l.signalingEventsV6)
	if err != nil {
		reader.Close()
		return nil, fmt.Errorf("open signaling_events_v6 ring buffer: %w", err)
	}

	out := make(chan SignalingEvent)

	go func() {
		<-ctx.Done()
		reader.Close() // Unblocks both readers' Read() calls below.
		readerV6.Close()
	}()

	var wg sync.WaitGroup
	wg.Add(2)
	go l.readSignalingEvents(ctx, reader, out, &wg)
	go l.readSignalingEventsV6(ctx, readerV6, out, &wg)
	go func() {
		wg.Wait()
		close(out)
	}()

	return out, nil
}

func (l *Loader) readSignalingEvents(ctx context.Context, reader *ringbuf.Reader, out chan<- SignalingEvent, wg *sync.WaitGroup) {
	defer wg.Done()
	defer reader.Close()

	for {
		record, err := reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) || ctx.Err() != nil {
				return
			}
			continue // Transient read error: skip this record, keep reading.
		}

		var raw rawSignalingEvent
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &raw); err != nil {
			continue // Short/corrupt record: skip rather than crash the reader.
		}

		evt := SignalingEvent{
			// raw.TimestampNs is nanoseconds since boot (bpf_ktime_get_ns());
			// converting to int64 only overflows past ~292 years of uptime.
			ObservedAt:  l.bootTime.Add(time.Duration(raw.TimestampNs)), // #nosec G115
			SourceIP:    ipv4FromU32(raw.Saddr),
			DestIP:      ipv4FromU32(raw.Daddr),
			DestPort:    raw.DestPort,
			PayloadSize: raw.PayloadSize,
			Protocol:    SignalProtocol(raw.Protocol),
			Malformed:   raw.Malformed != 0,
			VLANID:      raw.VlanID,
		}

		select {
		case out <- evt:
		case <-ctx.Done():
			return
		}
	}
}

func (l *Loader) readSignalingEventsV6(ctx context.Context, reader *ringbuf.Reader, out chan<- SignalingEvent, wg *sync.WaitGroup) {
	defer wg.Done()
	defer reader.Close()

	for {
		record, err := reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) || ctx.Err() != nil {
				return
			}
			continue // Transient read error: skip this record, keep reading.
		}

		var raw rawSignalingEventV6
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &raw); err != nil {
			continue // Short/corrupt record: skip rather than crash the reader.
		}

		evt := SignalingEvent{
			ObservedAt: l.bootTime.Add(time.Duration(raw.TimestampNs)), // #nosec G115
			// raw.Saddr/Daddr are already the raw 16 address bytes (not a
			// uint32 register value like rawSignalingEvent.Saddr), so no
			// byte-order round trip is needed the way ipv4FromU32 does one.
			SourceIP:    net.IP(raw.Saddr[:]),
			DestIP:      net.IP(raw.Daddr[:]),
			DestPort:    raw.DestPort,
			PayloadSize: raw.PayloadSize,
			Protocol:    SignalProtocol(raw.Protocol),
			Malformed:   raw.Malformed != 0,
			VLANID:      raw.VlanID,
		}

		select {
		case out <- evt:
		case <-ctx.Done():
			return
		}
	}
}

// signalRateEntry is the Go mirror of bpf/packet_filter.c's
// `struct signal_rate_entry` (window_start_ns + count), the shared value
// type for both signal_rate and scan_rate.
//
// The trailing `_ uint32` is load-bearing, not cosmetic: C naturally pads
// this struct to 16 bytes (8-byte alignment from window_start_ns), which is
// what the map's value size actually is (`bpftool map list` confirms
// "value 16B") -- but cilium/ebpf's key/value marshaling sums each Go
// field's own size via reflection (like encoding/binary.Size) rather than
// using Go's struct-level unsafe.Sizeof, so a two-field
// {uint64;uint32} version (12 bytes by that reflection-based sum) fails
// every real Lookup with "doesn't consume all data" the moment the map
// actually returns a hit -- found by real-testing this exact bug via
// `bpftool map dump` against a live kernel map, not by inspection: the
// pre-fix version of this struct existed, unchanged, since the original
// signal_rate map shipped, silently making Loader.SignalRate() return
// ok=false for every real entry, always -- see TestSignalRateEntryMatchesKernelValueSize.
type signalRateEntry struct {
	WindowStartNs uint64
	Count         uint32
	_             uint32
}

// portScanEntry is the Go mirror of bpf/packet_filter.c's
// `struct port_scan_entry` — nothing in pkg/ebpf currently reads the
// port_scan map directly (its results surface through the existing
// signaling_events ringbuf as a SignalProtoPortScan record instead), so
// this type isn't wired into Loader/Attach; it exists purely so
// TestPortScanEntryMatchesKernelValueSize can guard, ahead of any real
// reader ever being written, that this file's understanding of the C
// struct's packed+aligned(8) layout (window_start_ns + distinct_count +
// emitted + 1 byte pad + 15 ports, rounded up to a multiple of 8) stays
// correct — the same class of size mismatch that silently broke
// signalRateEntry once.
type portScanEntry struct {
	WindowStartNs uint64
	DistinctCount uint16
	Emitted       uint8
	_             uint8
	Ports         [15]uint16 // MULTIPORT_SCAN_THRESHOLD, bpf/headers/common.h.
	_             [6]byte    // Trailing padding to the real 48-byte kernel size.
}

// scanRateKey is the byte-exact Go mirror of bpf/packet_filter.c's
// `struct scan_key` — saddr (4 bytes) + dest_port (2 bytes, host byte order)
// + 2 bytes of explicit padding, `packed, aligned(4)`. Field order and sizes
// MUST stay in sync by hand with that struct, the same convention as
// rawSignalingEvent below.
type scanRateKey struct {
	Saddr    uint32
	DestPort uint16
	_        uint16
}

// scanRateKeyV6 is the byte-exact Go mirror of bpf/packet_filter.c's
// `struct scan_key_v6` — 16-byte saddr + dest_port (2 bytes, host byte
// order) + 2 bytes of the C struct's own explicit padding, then 4 more
// trailing bytes so binary.Size (what cilium/ebpf's marshaling actually
// uses) reaches the real 24-byte `packed, aligned(8)` kernel size
// confirmed via `bpftool map list` — the same class of mismatch
// signalRateEntry's doc comment explains in detail; a naive 20-byte
// {[16]byte; uint16; uint16} version fails every real Lookup.
type scanRateKeyV6 struct {
	Saddr    [16]byte
	DestPort uint16
	_        uint16
	_        [4]byte
}

// SignalRate returns the current window's packet count for (ip, destPort),
// and false if there's no entry this window. destPort selects which kernel
// map to consult: GTPU_PORT/SIP_PORT traffic is tracked in signal_rate
// (keyed by source IP only — track_signal_rate() in packet_filter.c), any
// other port in scan_rate (keyed by source IP *and* port — see
// track_scan_rate()'s comment there for why scan_rate can't get away with
// signal_rate's coarser source-only keying). Used by pkg/ingestion to
// populate NormalizedEvent.RatePerSecond.
func (l *Loader) SignalRate(ip net.IP, destPort uint16) (count uint32, ok bool) {
	var entry signalRateEntry
	signaling := destPort == gtpuPort || destPort == sipPort

	if ip.To4() != nil {
		if signaling {
			key, err := ipv4Key(ip)
			if err != nil {
				return 0, false
			}
			if err := l.signalRate.Lookup(key, &entry); err != nil {
				return 0, false
			}
			return entry.Count, true
		}

		v4, err := ipv4Key(ip)
		if err != nil {
			return 0, false
		}
		key := scanRateKey{Saddr: binary.LittleEndian.Uint32(v4), DestPort: destPort}
		if err := l.scanRate.Lookup(&key, &entry); err != nil {
			return 0, false
		}
		return entry.Count, true
	}

	v6, err := ipv6Key(ip)
	if err != nil {
		return 0, false
	}

	if signaling {
		if err := l.signalRateV6.Lookup(v6, &entry); err != nil {
			return 0, false
		}
		return entry.Count, true
	}

	key := scanRateKeyV6{DestPort: destPort}
	copy(key.Saddr[:], v6)
	if err := l.scanRateV6.Lookup(&key, &entry); err != nil {
		return 0, false
	}
	return entry.Count, true
}

// ipv4FromU32 reverses the little-endian round trip introduced by decoding
// the ring buffer record as a uint32 (see rawSignalingEvent): raw holds the
// same numeric value the kernel's __u32 saddr/daddr register held, so
// re-encoding it little-endian recovers the original opaque wire-order
// bytes — the same bytes ipv4Key would produce from the resulting net.IP,
// and the same byte-order-agnostic treatment described there.
func ipv4FromU32(raw uint32) net.IP {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, raw)
	return net.IP(b)
}
