// Package falco bridges Falco's syscall-level alerts into
// events.NormalizedEvent, Falco's counterpart to pkg/ingestion (the
// eBPF-sourced Layer 1 -> Layer 2 bridge). See Bridge's doc comment in
// bridge.go for the full picture.
package falco

import (
	"strconv"
	"time"
)

// Alert is the Go mirror of Falco's JSON output payload (the "json_output"
// format Falco's http_output/program_output produce) — top-level fields
// (rule, priority, output, time, hostname, source, tags, output_fields)
// match Falco's long-stable, widely-integrated JSON schema.
//
// output_fields is parsed as an open map, not a fixed struct: its actual
// keys depend entirely on which %fields a given Falco rule's own
// `output:` format string references — a rule that never references
// fd.sip simply won't have that key present at all. fieldString/fieldPort
// below treat a missing or wrong-typed field as "unknown" (zero value),
// not an error, since NormalizedEvent's own fields are already documented
// as best-effort (see pkg/ingestion.FromSignalingEvent's identical
// tolerance for an unattributed source).
type Alert struct {
	Output       string                 `json:"output"`
	Priority     string                 `json:"priority"`
	Rule         string                 `json:"rule"`
	Time         time.Time              `json:"time"`
	Source       string                 `json:"source"`
	Tags         []string               `json:"tags"`
	Hostname     string                 `json:"hostname"`
	OutputFields map[string]interface{} `json:"output_fields"`
}

// fieldString returns the first non-empty string value found in fields for
// any of keys, checked in order. Some Falco rules populate fd.sip/fd.dip
// for classic socket syscalls, others populate fd.rip/fd.lip for
// connect-family rules -- callers pass both spellings, most-specific first.
func fieldString(fields map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := fields[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// fieldPort returns the first key in fields (checked in order) that
// decodes as a uint16 port number. encoding/json decodes a bare JSON
// number into float64 by default, but some Falco output configurations
// emit numeric fields as strings -- both are handled.
func fieldPort(fields map[string]interface{}, keys ...string) uint16 {
	for _, k := range keys {
		v, ok := fields[k]
		if !ok {
			continue
		}
		switch n := v.(type) {
		case float64:
			if n >= 0 && n <= 65535 {
				return uint16(n)
			}
		case string:
			if p, err := strconv.ParseUint(n, 10, 16); err == nil {
				return uint16(p)
			}
		}
	}
	return 0
}
