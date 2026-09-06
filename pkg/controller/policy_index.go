package controller

import (
	"sync"

	"k8s.io/apimachinery/pkg/types"

	securityv1alpha1 "github.com/FelipeBastosxj/Sentinel5G/api/v1alpha1"
)

// PolicyIndex is a concurrency-safe, in-memory cache of TelecomSecurityPolicy
// objects, kept in sync by Reconciler and queried by ThreatScoreWatcher on
// every incoming ThreatScoreEvent. Re-querying the API server on every scored
// event would not scale to per-packet-derived threat scores, so Reconcile
// pushes updates here instead.
//
// Indexed by namespace first (byNamespace[namespace][name]) rather than a
// single flat map, so MatchingPolicies only scans the policies that could
// possibly match instead of every policy across every namespace in the
// cluster on every single scored event.
type PolicyIndex struct {
	mu          sync.RWMutex
	byNamespace map[string]map[string]*securityv1alpha1.TelecomSecurityPolicy
}

// NewPolicyIndex returns an empty PolicyIndex.
func NewPolicyIndex() *PolicyIndex {
	return &PolicyIndex{byNamespace: make(map[string]map[string]*securityv1alpha1.TelecomSecurityPolicy)}
}

// Put inserts or replaces the cached copy of policy.
func (idx *PolicyIndex) Put(policy *securityv1alpha1.TelecomSecurityPolicy) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	byName, ok := idx.byNamespace[policy.Namespace]
	if !ok {
		byName = make(map[string]*securityv1alpha1.TelecomSecurityPolicy)
		idx.byNamespace[policy.Namespace] = byName
	}
	byName[policy.Name] = policy.DeepCopy()
}

// Remove evicts key from the index (called when the reconciler observes a delete).
func (idx *PolicyIndex) Remove(key types.NamespacedName) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	byName, ok := idx.byNamespace[key.Namespace]
	if !ok {
		return
	}
	delete(byName, key.Name)
	if len(byName) == 0 {
		delete(idx.byNamespace, key.Namespace)
	}
}

// MatchingPolicies returns every cached policy in namespace whose
// TargetWorkloads selects a workload carrying podLabels.
func (idx *PolicyIndex) MatchingPolicies(namespace string, podLabels map[string]string) []*securityv1alpha1.TelecomSecurityPolicy {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var matches []*securityv1alpha1.TelecomSecurityPolicy
	for _, policy := range idx.byNamespace[namespace] {
		if selectorMatches(policy.Spec.TargetWorkloads, podLabels) {
			matches = append(matches, policy)
		}
	}
	return matches
}

func selectorMatches(selectors []securityv1alpha1.WorkloadSelector, podLabels map[string]string) bool {
	for _, sel := range selectors {
		if selectorMatchesOne(sel, podLabels) {
			return true
		}
	}
	return false
}

func selectorMatchesOne(sel securityv1alpha1.WorkloadSelector, podLabels map[string]string) bool {
	if sel.App == "" && len(sel.MatchLabels) == 0 {
		return false
	}
	if sel.App != "" && podLabels["app"] != sel.App {
		return false
	}
	for k, v := range sel.MatchLabels {
		if podLabels[k] != v {
			return false
		}
	}
	return true
}
