package controller

import (
	"sync"

	"k8s.io/apimachinery/pkg/types"
)

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
// Entries are evicted on Pod deletion via Remove, using byPod as a reverse
// index — a plain Reconcile NotFound response carries no IP to key off
// directly, see Remove's doc comment for how that's worked around. Until a
// deleted Pod's Remove is processed, a stale entry may briefly attribute an
// event to a Pod that's already gone, or (if its IP was already reused) get
// silently overwritten by the new owner first; both are acceptable for this
// index's purpose (best-effort attribution for AI scoring context, not a
// security decision), not acceptable if this index were ever used to decide
// who to block — it isn't.
type PodIPIndex struct {
	mu   sync.RWMutex
	byIP map[string]PodRef
	// byPod is the reverse index (Pod -> its last known IP). Required
	// because a Pod's deletion is observed as a bare NamespacedName (see
	// PodIPIndexer.Reconcile's NotFound branch) with no IP in it at all —
	// without this, Remove would have no way to find which byIP entry
	// belonged to the deleted Pod short of watching raw informer delete
	// events instead of plain Reconcile requests.
	byPod map[types.NamespacedName]string
}

// NewPodIPIndex returns an empty PodIPIndex.
func NewPodIPIndex() *PodIPIndex {
	return &PodIPIndex{
		byIP:  make(map[string]PodRef),
		byPod: make(map[types.NamespacedName]string),
	}
}

// Put records that ip belongs to ref, overwriting any previous owner.
func (idx *PodIPIndex) Put(ip string, ref PodRef) {
	if ip == "" {
		return
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()

	key := types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}
	if oldIP, ok := idx.byPod[key]; ok && oldIP != ip {
		// This Pod's own IP changed since it was last indexed (rare, but
		// keep the forward index accurate) -- byPod is about to only
		// remember the new IP, so the entry under the old one would
		// otherwise become unreachable from Remove and linger forever.
		delete(idx.byIP, oldIP)
	}
	idx.byIP[ip] = ref
	idx.byPod[key] = ip
}

// Lookup returns the Pod last observed owning ip, if any.
func (idx *PodIPIndex) Lookup(ip string) (PodRef, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	ref, ok := idx.byIP[ip]
	return ref, ok
}

// Remove evicts key's entry — called by PodIPIndexer when it observes the
// Pod's deletion.
//
// Deliberately does NOT delete byIP[ip] unconditionally once ip is found via
// byPod: Kubernetes doesn't serialize a deleted Pod's last reconcile against
// a new Pod's first one, so a different Pod may have already reused ip and
// been Put into byIP before this Remove runs for the old one. Removing
// byIP[ip] unconditionally in that case would evict a live, correct
// mapping for the new Pod. Only remove it if it still points at this exact
// Pod.
func (idx *PodIPIndex) Remove(key types.NamespacedName) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	ip, ok := idx.byPod[key]
	if !ok {
		return
	}
	delete(idx.byPod, key)

	if ref, ok := idx.byIP[ip]; ok && ref.Namespace == key.Namespace && ref.Name == key.Name {
		delete(idx.byIP, ip)
	}
}
