package ebpf

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
)

func TestClassifyAttachError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string // substring expected in the classification
	}{
		{
			name: "object file missing",
			err:  fmt.Errorf("load bpf collection spec from %q: %w", "/var/run/sentinel5g/packet_filter.o", fs.ErrNotExist),
			want: "was not found",
		},
		{
			name: "insufficient privileges",
			err:  fmt.Errorf("instantiate bpf collection: %w", fs.ErrPermission),
			want: "insufficient privileges",
		},
		{
			name: "interface not found",
			err:  fmt.Errorf("resolve interface %q: %w: %w", "eth9", ErrInterfaceNotFound, errors.New("no such network interface")),
			want: "does not exist on this node",
		},
		{
			name: "unrecognized",
			err:  errors.New("invalid argument"),
			want: "unrecognized reason",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyAttachError(tc.err)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("ClassifyAttachError(%v) = %q, want a string containing %q", tc.err, got, tc.want)
			}
			if !strings.Contains(got, "docs/troubleshooting.md#ebpf") {
				t.Fatalf("ClassifyAttachError(%v) = %q, want it to point at docs/troubleshooting.md#ebpf", tc.err, got)
			}
		})
	}

	if got := ClassifyAttachError(nil); got != "" {
		t.Fatalf("ClassifyAttachError(nil) = %q, want empty string", got)
	}
}
