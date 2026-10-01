//go:build !linux

package ebpf

import (
	"fmt"
	"net"
)

// Loader is a no-op stand-in used when building outside Linux (eBPF/XDP is a
// Linux kernel facility). It lets the rest of the module — and unit tests
// for pkg/controller — build and run on macOS/Windows dev machines without
// pulling in cilium/ebpf's Linux-only syscalls.
type Loader struct{}

// Options is the non-Linux counterpart of the Linux build's Options. Kept
// so cmd/operator/main.go compiles unchanged on a macOS/Windows dev
// machine; every field is ignored, because Attach never succeeds here.
type Options struct {
	PinPath string
}

// Attach always fails on non-Linux platforms; there is nothing to attach to.
func Attach(objPath, iface string) (*Loader, error) {
	return AttachWithOptions(objPath, iface, Options{})
}

// AttachWithOptions always fails on non-Linux platforms.
func AttachWithOptions(objPath, iface string, _ Options) (*Loader, error) {
	return nil, fmt.Errorf("ebpf: XDP attach is only supported on linux (requested object %q on interface %q)", objPath, iface)
}

// RemovePins is a no-op on non-Linux platforms: bpffs is a Linux facility,
// so there is never anything pinned to remove.
func RemovePins(pinPath string) error { return nil }

// Block is a no-op on non-Linux platforms.
func (l *Loader) Block(ip net.IP) error { return nil }

// Unblock is a no-op on non-Linux platforms.
func (l *Loader) Unblock(ip net.IP) error { return nil }

// BlockTunnel is a no-op on non-Linux platforms.
func (l *Loader) BlockTunnel(ip net.IP, teid uint32) error { return nil }

// UnblockTunnel is a no-op on non-Linux platforms.
func (l *Loader) UnblockTunnel(ip net.IP, teid uint32) error { return nil }

// Close is a no-op on non-Linux platforms.
func (l *Loader) Close() error { return nil }

// Deliberately does not implement BlocklistInspector either, for the same
// reason as EventSource below: a Loader that can never attach enforces
// nothing, so there is no actual kernel state for a drift reconcile to
// compare against, and pkg/controller.BlocklistReconciler skips it cleanly
// instead of comparing the policies' desired state against a permanently
// empty answer and "correcting" it on every tick.
//
// Deliberately does not implement EventSource (pkg/ebpf/blocklist.go): there
// is no ring buffer to read on a platform Attach() always fails on, so
// callers' type assertion for EventSource fails cleanly here rather than
// needing a "not supported" error path of its own. That also means methods
// added to EventSource -- SignalingEvents, SignalRate, TunnelRate -- never
// need a counterpart here; the omission is the design, not an oversight.
var _ BlocklistUpdater = (*Loader)(nil)
