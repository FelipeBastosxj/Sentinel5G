//go:build linux

package ebpf

import (
	"fmt"
	"net"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
)

const (
	blocklistMapName = "blocklist"
	xdpProgramName   = "xdp_packet_filter"
	blockedValue     = uint8(1)
)

// Loader attaches bpf/packet_filter.c (compiled to objPath by bpf/Makefile)
// as an XDP program on iface and exposes its blocklist map.
type Loader struct {
	collection *ebpf.Collection
	link       link.Link
	blocklist  *ebpf.Map
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

	return &Loader{collection: coll, link: xdpLink, blocklist: blocklist}, nil
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
