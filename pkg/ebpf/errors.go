package ebpf

import (
	"errors"
	"io/fs"
)

// ErrInterfaceNotFound wraps a failure to resolve --bpf-interface, so
// ClassifyAttachError (below) can tell it apart from a missing object file
// or an insufficient-privilege failure. loader_linux.go's Attach() wraps it
// alongside (not instead of) the underlying net.InterfaceByName error.
var ErrInterfaceNotFound = errors.New("network interface not found")

// ClassifyAttachError turns an Attach() failure into a short, actionable
// cause. It deliberately relies only on the standard library's fs.ErrExist/
// fs.ErrPermission sentinels (which syscall.Errno.Is already maps ENOENT/
// EACCES/EPERM to on every OS Go supports — no golang.org/x/sys/unix import
// needed here), so this file has no build tag and stays buildable on the
// non-Linux dev machines loader_other.go already supports.
//
// Callers should log this alongside err.Error() (see
// cmd/operator/main.go's attachBlocklist), not in place of it: this adds a
// likely cause on top of the original detail, it doesn't replace it — a
// wrong guess here should never hide the real error text.
func ClassifyAttachError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, fs.ErrNotExist):
		return "the compiled bpf/packet_filter.o was not found at the configured --bpf-object path " +
			"(the operator image bakes it in at build time -- see the Dockerfile's bpf-builder stage " +
			"-- so this usually means the path was overridden); see docs/troubleshooting.md#ebpf"
	case errors.Is(err, fs.ErrPermission):
		return "insufficient privileges to attach the XDP program -- the container needs " +
			"CAP_BPF+CAP_NET_ADMIN (set ebpf.enabled: true in the Helm chart's values, which wires " +
			"these up automatically via ebpf.capabilities); see docs/troubleshooting.md#ebpf"
	case errors.Is(err, ErrInterfaceNotFound):
		return "the configured --bpf-interface does not exist on this node; see docs/troubleshooting.md#ebpf"
	default:
		return "eBPF attach failed for an unrecognized reason, possibly an incompatible kernel; " +
			"see docs/troubleshooting.md#ebpf"
	}
}
