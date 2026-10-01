//go:build linux

package ebpf

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cilium/ebpf"
	"golang.org/x/sys/unix"
)

// ErrPinPathNotBPFFS reports a PinPath that resolves onto something other
// than a bpf filesystem. Checked before anything is loaded, because
// bpf(BPF_OBJ_PIN) against (say) a container's overlayfs fails with a bare
// EINVAL at the very end of an otherwise successful load — an unhelpful
// place to meet a mount problem.
var ErrPinPathNotBPFFS = errors.New("pin path is not on a bpf filesystem (bpffs)")

// pinnedMapNames is exactly the ENFORCEMENT maps, and the omissions are the
// design rather than an oversight.
//
// These four are the ones whose contents are an operator decision: an entry
// exists because a mitigation put it there, and until a de-escalation or a
// finalizer removes it, it is meant to be in force. Without a pin, that
// state lived and died with the process — Loader.Close() detaches the XDP
// link and the whole collection goes with it, so a rollout, an OOM kill or
// a crash silently un-blocked everything the status subresource still
// claimed was blocked (ROADMAP.md Phase 4's first item). Pinning them means
// the next Attach reuses the same kernel objects, contents intact, from the
// instant the program is attached — before any userspace reconcile has had
// a chance to run.
//
// The *_rate and port_scan maps are deliberately NOT pinned. They are
// observation, not enforcement: each entry is a counter inside a 1-second
// window, and carrying one across a restart would hand the new process a
// window that started under the old one — a rate reading attributed to a
// period nobody was watching. Re-deriving them from live traffic costs one
// window and is always correct. The two ring buffers are not pinned either;
// a ring buffer's value is its consumer, and the consumer is the process
// that just died.
//
// This is the same enforcement-versus-observation split that decides HASH
// versus LRU_HASH in bpf/packet_filter.c, applied to a second question.
var pinnedMapNames = []string{
	blocklistMapName,
	blocklistV6MapName,
	tunnelBlocklistMapName,
	tunnelBlocklistV6MapName,
}

// ensurePinDir verifies that path is (or will be) on a bpf filesystem and
// creates it. The check walks up to the nearest existing ancestor, since
// path itself usually does not exist on a first run — it is the MOUNT that
// has to be bpffs, not the leaf directory.
func ensurePinDir(path string) error {
	existing := filepath.Clean(path)
	for {
		if _, err := os.Stat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return fmt.Errorf("resolve pin path %q: no existing ancestor directory", path)
		}
		existing = parent
	}

	var st unix.Statfs_t
	if err := unix.Statfs(existing, &st); err != nil {
		return fmt.Errorf("statfs %q: %w", existing, err)
	}
	// Compared against the UNTYPED unix.BPF_FS_MAGIC, deliberately, rather
	// than converting st.Type: Statfs_t.Type is int64 on the architectures
	// this is built for and 0xcafe4a11 does not fit a positive int32, so
	// truncating either side would report "not bpffs" for a filesystem that
	// is one. On a 32-bit Linux build this stops compiling instead, which
	// is the failure worth having.
	if st.Type != unix.BPF_FS_MAGIC {
		return fmt.Errorf("%w: %q (filesystem type %#x); mount one with `mount -t bpf bpffs /sys/fs/bpf`, "+
			"or mount the host's into the container (see docs/troubleshooting.md#ebpf)",
			ErrPinPathNotBPFFS, existing, st.Type)
	}

	// 0700: these pins are the operator's enforcement state. Anything that
	// can open them can delete a drop.
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create pin directory %q: %w", path, err)
	}
	return nil
}

// markPinnedMaps flags the enforcement maps in spec as PinByName, so
// NewCollectionWithOptions reuses an existing pin instead of creating a
// fresh map. Returns an error naming any map the object does not export —
// Attach already rejects such an object, but this runs first and silently
// pinning three of four maps would be worse than not pinning at all.
func markPinnedMaps(spec *ebpf.CollectionSpec) error {
	for _, name := range pinnedMapNames {
		m, ok := spec.Maps[name]
		if !ok {
			return fmt.Errorf("bpf object does not export map %q, so it cannot be pinned", name)
		}
		m.Pinning = ebpf.PinByName
	}
	return nil
}

// newPinnedCollection instantiates spec with the enforcement maps pinned at
// pinPath, reusing whatever is already pinned there.
//
// The second return value reports that the existing pins were INCOMPATIBLE
// with this object and had to be discarded — which happens when an upgrade
// changes a key, a value or max_entries on one of those maps. There is no
// good outcome available at that point, only a least-bad one, and the
// choice is deliberate: refusing to attach would leave the node with no
// enforcement at all, so the pins are removed, the maps are recreated empty,
// and the caller is told, loudly, that the kernel just lost every active
// drop. cmd/operator/main.go turns that into a warning Event; the periodic
// reconcile in pkg/controller.BlocklistReconciler then re-applies whatever
// the policies' status still claims, usually within a minute.
func newPinnedCollection(spec *ebpf.CollectionSpec, pinPath string) (*ebpf.Collection, bool, error) {
	opts := ebpf.CollectionOptions{Maps: ebpf.MapOptions{PinPath: pinPath}}

	coll, err := ebpf.NewCollectionWithOptions(spec, opts)
	if err == nil {
		return coll, false, nil
	}
	if !errors.Is(err, ebpf.ErrMapIncompatible) {
		return nil, false, err
	}

	if rmErr := RemovePins(pinPath); rmErr != nil {
		return nil, false, fmt.Errorf("pinned maps are incompatible with this object (%w) and removing the stale pins failed: %w", err, rmErr)
	}
	coll, retryErr := ebpf.NewCollectionWithOptions(spec, opts)
	if retryErr != nil {
		return nil, false, fmt.Errorf("pinned maps were incompatible with this object (%w) and reloading after removing them also failed: %w", err, retryErr)
	}
	return coll, true, nil
}

// RemovePins deletes the enforcement-map pins under pinPath. A pin is a
// reference, so this does not by itself undo anything: the maps stay alive
// while a loaded program still holds them. What it does is make the NEXT
// Attach start from empty maps.
//
// Not called anywhere on the operator's own shutdown path — that is the
// whole point of pinning — so this exists for an uninstall, or for an
// operator who has decided to clear the kernel state by hand. See
// docs/troubleshooting.md.
func RemovePins(pinPath string) error {
	var errs []error
	for _, name := range pinnedMapNames {
		if err := os.Remove(filepath.Join(pinPath, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove pin %q: %w", name, err))
		}
	}
	return errors.Join(errs...)
}
