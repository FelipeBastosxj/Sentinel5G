package falco

import "testing"

func TestFieldString_PrefersFirstPresentKey(t *testing.T) {
	fields := map[string]interface{}{
		"fd.rip": "198.51.100.9",
	}
	got := fieldString(fields, "fd.sip", "fd.rip")
	if got != "198.51.100.9" {
		t.Fatalf("expected fallback key fd.rip to be used, got %q", got)
	}
}

func TestFieldString_MissingKeyReturnsEmpty(t *testing.T) {
	fields := map[string]interface{}{}
	if got := fieldString(fields, "fd.sip", "fd.rip"); got != "" {
		t.Fatalf("expected empty string for a missing field, got %q", got)
	}
}

func TestFieldString_WrongTypeTreatedAsMissing(t *testing.T) {
	// A rule that emits a numeric field under a string-typed key (or any
	// other type mismatch) must not panic or return a garbage value --
	// output_fields is Falco-rule-defined, not schema-enforced.
	fields := map[string]interface{}{
		"fd.sip": float64(12345),
	}
	if got := fieldString(fields, "fd.sip"); got != "" {
		t.Fatalf("expected wrong-typed field to be treated as missing, got %q", got)
	}
}

func TestFieldString_EmptyStringValueSkipped(t *testing.T) {
	fields := map[string]interface{}{
		"fd.sip": "",
		"fd.rip": "198.51.100.9",
	}
	got := fieldString(fields, "fd.sip", "fd.rip")
	if got != "198.51.100.9" {
		t.Fatalf("expected empty-string fd.sip to be skipped in favor of fd.rip, got %q", got)
	}
}

func TestFieldPort_DecodesFloat64(t *testing.T) {
	fields := map[string]interface{}{"fd.dport": float64(2152)}
	if got := fieldPort(fields, "fd.dport"); got != 2152 {
		t.Fatalf("expected port 2152, got %d", got)
	}
}

func TestFieldPort_DecodesStringNumber(t *testing.T) {
	fields := map[string]interface{}{"fd.lport": "5060"}
	if got := fieldPort(fields, "fd.lport"); got != 5060 {
		t.Fatalf("expected port 5060, got %d", got)
	}
}

func TestFieldPort_OutOfRangeTreatedAsMissing(t *testing.T) {
	fields := map[string]interface{}{"fd.dport": float64(70000)}
	if got := fieldPort(fields, "fd.dport"); got != 0 {
		t.Fatalf("expected out-of-range port to be treated as missing (0), got %d", got)
	}
}

func TestFieldPort_MissingKeyReturnsZero(t *testing.T) {
	fields := map[string]interface{}{}
	if got := fieldPort(fields, "fd.dport", "fd.lport"); got != 0 {
		t.Fatalf("expected 0 for a missing port field, got %d", got)
	}
}
