//go:build linux

package ebpf

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"golang.org/x/sys/unix"
)

const (
	blocklistMapName       = "blocklist"
	signalRateMapName      = "signal_rate"
	signalingEventsMapName = "signaling_events"
	xdpProgramName         = "xdp_packet_filter"
	blockedValue           = uint8(1)
)

// Loader attaches bpf/packet_filter.c (compiled to objPath by bpf/Makefile)
// as an XDP program on iface and exposes its blocklist map.
type Loader struct {
	collection      *ebpf.Collection
	link            link.Link
	blocklist       *ebpf.Map
	signalRate      *ebpf.Map
	signalingEvents *ebpf.Map

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

	signalingEvents, ok := coll.Maps[signalingEventsMapName]
	if !ok {
		coll.Close()
		return nil, fmt.Errorf("bpf object %q does not export map %q", objPath, signalingEventsMapName)
	}

	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		coll.Close()
		return nil, fmt.Errorf("resolve interface %q: %w", iface, err)
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
		collection:      coll,
		link:            xdpLink,
		blocklist:       blocklist,
		signalRate:      signalRate,
		signalingEvents: signalingEvents,
		bootTime:        bootTime,
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
	key, err := ipv4Key(ip)
	if err != nil {
		return err
	}
	if err := l.blocklist.Put(key, blockedValue); err != nil {
		return fmt.Errorf("insert %s into blocklist map: %w", ip, err)
	}
	return nil
}

// Unblock implements BlocklistUpdater.
func (l *Loader) Unblock(ip net.IP) error {
	key, err := ipv4Key(ip)
	if err != nil {
		return err
	}
	if err := l.blocklist.Delete(key); err != nil {
		return fmt.Errorf("remove %s from blocklist map: %w", ip, err)
	}
	return nil
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
	_           [2]byte
}

// SignalingEvents implements EventSource: starts a background goroutine
// reading bpf/packet_filter.c's signaling_events ring buffer and decoding
// each record, until ctx is done. The returned channel closes when the
// reader stops (context cancellation or a fatal read error, e.g. the ring
// buffer being closed by Close()).
func (l *Loader) SignalingEvents(ctx context.Context) (<-chan SignalingEvent, error) {
	reader, err := ringbuf.NewReader(l.signalingEvents)
	if err != nil {
		return nil, fmt.Errorf("open signaling_events ring buffer: %w", err)
	}

	out := make(chan SignalingEvent)

	go func() {
		defer close(out)
		defer reader.Close()

		go func() {
			<-ctx.Done()
			reader.Close() // Unblocks the Read() below.
		}()

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
			}

			select {
			case out <- evt:
			case <-ctx.Done():
				return
			}
		}
	}()

	return out, nil
}

// SignalRate returns the current window's packet count for ip from the
// signal_rate map (see track_signal_rate() in packet_filter.c), and false
// if ip has no entry (no signaling traffic observed from it this window).
// Used by pkg/ingestion to populate NormalizedEvent.RatePerSecond.
func (l *Loader) SignalRate(ip net.IP) (count uint32, ok bool) {
	key, err := ipv4Key(ip)
	if err != nil {
		return 0, false
	}

	var entry struct {
		WindowStartNs uint64
		Count         uint32
	}
	if err := l.signalRate.Lookup(key, &entry); err != nil {
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
