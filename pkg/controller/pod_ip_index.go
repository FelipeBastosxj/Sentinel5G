package controller

import "sync"

// PodRef identifies a Pod by namespace/name.
type PodRef struct {
	Namespace string
	Name      string
}

// PodIPIndex is a concurrency-safe, in-memory cache mapping a Pod's IP
// address to the Pod that owns it, kept in sync by PodIPIndexer. It exists
// so pkg/ingestion can resolve a kernel-observed source IP back to a
// namespace/Pod name without an API server round trip on every
// packet-derived event — the same "index instead of query" reasoning as
// PolicyIndex.
//
// Unlike PolicyIndex, entries are never explicitly removed on Pod deletion:
// a NotFound response to a deleted Pod's reconcile doesn't carry its last
// known IP, and tracking deletions precisely would need watching raw
// informer events instead of plain Reconcile requests. A stale entry is
// silently overwritten as soon as the (possibly different) Pod that reuses
// that IP is next reconciled; until then, an event from a reused IP may be
// briefly attributed to the wrong Pod. Acceptable for this index's purpose
// (best-effort attribution for AI scoring context, not a security
// decision), not acceptable if this index were ever used to decide who to
// block — it isn't.
type PodIPIndex struct {
	mu   sync.RWMutex
	byIP map[string]PodRef
}

// NewPodIPIndex returns an empty PodIPIndex.
func NewPodIPIndex() *PodIPIndex {
	return &PodIPIndex{byIP: make(map[string]PodRef)}
}

// Put records that ip belongs to ref, overwriting any previous owner.
func (idx *PodIPIndex) Put(ip string, ref PodRef) {
	if ip == "" {
		return
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.byIP[ip] = ref
}

// Lookup returns the Pod last observed owning ip, if any.
func (idx *PodIPIndex) Lookup(ip string) (PodRef, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	ref, ok := idx.byIP[ip]
	return ref, ok
}
