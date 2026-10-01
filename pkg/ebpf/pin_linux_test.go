//go:build linux

package ebpf

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// The check that makes a mount problem legible. Without it, pinning into a
// container's overlayfs fails as a bare EINVAL from bpf(BPF_OBJ_PIN), at
// the very end of an otherwise successful load -- an unhelpful place to
// meet a missing volume mount.
func TestEnsurePinDir_RejectsAPathThatIsNotOnBPFFS(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sentinel5g")
	err := ensurePinDir(dir)
	if err == nil {
		t.Fatal("expected ensurePinDir to reject a path on an ordinary filesystem")
	}
	if !errors.Is(err, ErrPinPathNotBPFFS) {
		t.Fatalf("expected ErrPinPathNotBPFFS, got %v", err)
	}
	if _, statErr := os.Stat(dir); statErr == nil {
		t.Fatal("ensurePinDir created the directory despite rejecting the filesystem")
	}
}

// The check walks up to the nearest EXISTING ancestor, because the leaf
// almost never exists on a first run. Asserting it here keeps a future
// simplification from statfs-ing the leaf and reporting ENOENT instead of
// the real problem.
func TestEnsurePinDir_ChecksTheMountNotTheLeaf(t *testing.T) {
	deep := filepath.Join(t.TempDir(), "a", "b", "c")
	err := ensurePinDir(deep)
	if !errors.Is(err, ErrPinPathNotBPFFS) {
		t.Fatalf("expected the filesystem check to reach the existing ancestor, got %v", err)
	}
}

// Which maps get pinned is a security decision, not a convenience one: an
// enforcement map must survive the process (otherwise a restart is a silent
// unblock), and an observation map must NOT (otherwise a fresh process
// inherits a rate window that began under a dead one). This asserts the
// split in both directions, so adding a map to pinnedMapNames by reflex is
// a test failure rather than a quiet change of meaning.
func TestPinnedMapsAreTheEnforcementMapsOnly(t *testing.T) {
	want := map[string]bool{
		blocklistMapName:         true,
		blocklistV6MapName:       true,
		tunnelBlocklistMapName:   true,
		tunnelBlocklistV6MapName: true,
	}
	got := map[string]bool{}
	for _, name := range pinnedMapNames {
		got[name] = true
	}

	for name := range want {
		if !got[name] {
			t.Errorf("enforcement map %q is not pinned: a restart would silently drop every mitigation it holds", name)
		}
	}
	for _, name := range []string{
		signalRateMapName, signalRateV6MapName,
		scanRateMapName, scanRateV6MapName,
		tunnelRateMapName, tunnelRateV6MapName,
		signalingEventsMapName, signalingEventsV6MapName,
	} {
		if got[name] {
			t.Errorf("observation map %q is pinned: a new process would inherit rate windows it never observed", name)
		}
	}
	if len(pinnedMapNames) != len(want) {
		t.Errorf("pinnedMapNames has %d entries, want exactly the %d enforcement maps", len(pinnedMapNames), len(want))
	}
}

// RemovePins runs on paths that may legitimately hold nothing (a first
// install, a node where pinning was never enabled), and an uninstall path
// that errors on "already clean" is an uninstall path nobody runs.
func TestRemovePins_IsIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := RemovePins(dir); err != nil {
		t.Fatalf("RemovePins on an empty directory: %v", err)
	}
	if err := RemovePins(filepath.Join(dir, "does-not-exist")); err != nil {
		t.Fatalf("RemovePins on a missing directory: %v", err)
	}
}

// RemovePins must remove exactly the enforcement pins and leave anything
// else in that directory alone -- /sys/fs/bpf is shared with every other
// BPF tool on the node.
func TestRemovePins_TouchesOnlyItsOwnPins(t *testing.T) {
	dir := t.TempDir()
	for _, name := range append(append([]string{}, pinnedMapNames...), "someone-elses-program") {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := RemovePins(dir); err != nil {
		t.Fatalf("RemovePins: %v", err)
	}
	for _, name := range pinnedMapNames {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("pin %q survived RemovePins", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "someone-elses-program")); err != nil {
		t.Errorf("RemovePins deleted an unrelated pin: %v", err)
	}
}
