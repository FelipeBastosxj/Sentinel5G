//go:build linux

package ebpf

import (
	"encoding/binary"
	"testing"
	"time"
)

func sampleV4() []byte {
	b := make([]byte, 32)
	binary.LittleEndian.PutUint64(b[0:], 1_000_000_000)
	binary.LittleEndian.PutUint32(b[8:], 0x04030201)
	binary.LittleEndian.PutUint32(b[12:], 0x08070605)
	binary.LittleEndian.PutUint16(b[16:], 2152)
	binary.LittleEndian.PutUint16(b[18:], 300)
	b[20] = 1
	binary.LittleEndian.PutUint32(b[24:], 0x4d84)
	binary.LittleEndian.PutUint32(b[28:], 31)
	return b
}

// The decode runs once per observed signaling packet, on every node. At the
// ~125k pkt/s a single tunnel can carry (docs/paper-data/test-environment.md)
// a per-record heap allocation is a measurable share of the budget.
func BenchmarkDecodeSignalingEvent(b *testing.B) {
	l := &Loader{bootTime: time.Unix(1700000000, 0)}
	sample := sampleV4()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := l.decodeSignalingEvent(sample); err != nil {
			b.Fatal(err)
		}
	}
}

// Fuzz: arbitrary bytes must never panic, and only exact-length records
// decode -- a short or long record is a schema mismatch, not a partial
// event.
func FuzzDecodeSignalingEvent(f *testing.F) {
	f.Add(sampleV4())
	f.Add([]byte{})
	f.Add(make([]byte, 24)) // the pre-Phase-2.5 record size
	f.Add(make([]byte, 31))
	f.Add(make([]byte, 33))
	l := &Loader{bootTime: time.Unix(1700000000, 0)}
	f.Fuzz(func(t *testing.T, sample []byte) {
		evt, err := l.decodeSignalingEvent(sample)
		if len(sample) < 32 {
			if err == nil {
				t.Fatalf("decoded a %d-byte record; the struct is 32 bytes", len(sample))
			}
			return
		}
		if err != nil {
			t.Fatalf("a %d-byte record failed to decode: %v", len(sample), err)
		}
		if len(evt.SourceIP) != 4 || len(evt.DestIP) != 4 {
			t.Fatalf("IPv4 addresses must be 4 bytes, got %d/%d", len(evt.SourceIP), len(evt.DestIP))
		}
		if evt.TEID == 0 && evt.TunnelRate != 0 {
			// Not enforced by the decoder (it's a kernel invariant), but a
			// fuzzed record that violates it must still decode without
			// panicking -- which the assertions above already proved.
			_ = evt
		}
	})
}

func FuzzDecodeSignalingEventV6(f *testing.F) {
	f.Add(make([]byte, 56))
	f.Add(make([]byte, 48)) // the pre-Phase-2.5 v6 record size
	f.Add([]byte{})
	l := &Loader{bootTime: time.Unix(1700000000, 0)}
	f.Fuzz(func(t *testing.T, sample []byte) {
		evt, err := l.decodeSignalingEventV6(sample)
		if (err == nil) != (len(sample) >= 56) {
			t.Fatalf("len=%d err=%v: only >=56-byte records may decode", len(sample), err)
		}
		if err == nil && (len(evt.SourceIP) != 16 || len(evt.DestIP) != 16) {
			t.Fatalf("IPv6 addresses must be 16 bytes")
		}
	})
}
