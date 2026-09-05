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

// Attach always fails on non-Linux platforms; there is nothing to attach to.
func Attach(objPath, iface string) (*Loader, error) {
	return nil, fmt.Errorf("ebpf: XDP attach is only supported on linux (requested object %q on interface %q)", objPath, iface)
}

// Block is a no-op on non-Linux platforms.
func (l *Loader) Block(ip net.IP) error { return nil }

// Unblock is a no-op on non-Linux platforms.
func (l *Loader) Unblock(ip net.IP) error { return nil }

// Close is a no-op on non-Linux platforms.
func (l *Loader) Close() error { return nil }

var _ BlocklistUpdater = (*Loader)(nil)
