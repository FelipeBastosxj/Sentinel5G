//go:build linux

package ebpf

import (
	"errors"
	"fmt"
	"testing"

	"golang.org/x/sys/unix"
)

// IsMapFull must recognise the kernel's E2BIG (how a plain HASH reports "no
// room, and I do not evict") through Go's error wrapping, and must not fire
// on anything else -- a false positive here would label an ordinary write
// failure as a capacity DoS and page someone for nothing.
func TestIsMapFull(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"bare E2BIG", unix.E2BIG, true},
		{"wrapped E2BIG (how the loader returns it)",
			fmt.Errorf("insert tunnel into map: update: key too big for map: %w", unix.E2BIG), true},
		{"a different errno", unix.ENOENT, false},
		{"an unrelated error", errors.New("connection refused"), false},
	}
	for _, tc := range cases {
		if got := IsMapFull(tc.err); got != tc.want {
			t.Errorf("%s: IsMapFull = %v, want %v", tc.name, got, tc.want)
		}
	}
}
