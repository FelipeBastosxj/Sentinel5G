package controller

import (
	"testing"

	"k8s.io/apimachinery/pkg/types"
)

func TestPodIPIndex_PutAndLookup(t *testing.T) {
	idx := NewPodIPIndex()

	if _, ok := idx.Lookup("10.42.0.7"); ok {
		t.Fatal("expected no entry before Put")
	}

	idx.Put("10.42.0.7", PodRef{Namespace: "telecom-core", Name: "amf-0"})

	ref, ok := idx.Lookup("10.42.0.7")
	if !ok {
		t.Fatal("expected an entry after Put")
	}
	if ref.Namespace != "telecom-core" || ref.Name != "amf-0" {
		t.Fatalf("expected telecom-core/amf-0, got %s/%s", ref.Namespace, ref.Name)
	}
}

func TestPodIPIndex_PutOverwritesOnIPReuse(t *testing.T) {
	idx := NewPodIPIndex()
	idx.Put("10.42.0.7", PodRef{Namespace: "telecom-core", Name: "amf-0"})
	idx.Put("10.42.0.7", PodRef{Namespace: "telecom-core", Name: "amf-1"})

	ref, ok := idx.Lookup("10.42.0.7")
	if !ok || ref.Name != "amf-1" {
		t.Fatalf("expected the later Put to win, got %+v (ok=%v)", ref, ok)
	}
}

func TestPodIPIndex_PutIgnoresEmptyIP(t *testing.T) {
	idx := NewPodIPIndex()
	idx.Put("", PodRef{Namespace: "telecom-core", Name: "amf-0"})

	if _, ok := idx.Lookup(""); ok {
		t.Fatal("expected an empty IP to never be recorded")
	}
}

func TestPodIPIndex_RemoveEvictsTheDeletedPodsEntry(t *testing.T) {
	idx := NewPodIPIndex()
	idx.Put("10.42.0.7", PodRef{Namespace: "telecom-core", Name: "amf-0"})

	idx.Remove(types.NamespacedName{Namespace: "telecom-core", Name: "amf-0"})

	if _, ok := idx.Lookup("10.42.0.7"); ok {
		t.Fatal("expected the entry to be evicted after Remove")
	}
}

func TestPodIPIndex_RemoveOnUnknownPodIsANoOp(t *testing.T) {
	idx := NewPodIPIndex()
	idx.Put("10.42.0.7", PodRef{Namespace: "telecom-core", Name: "amf-0"})

	idx.Remove(types.NamespacedName{Namespace: "telecom-core", Name: "never-indexed"})

	ref, ok := idx.Lookup("10.42.0.7")
	if !ok || ref.Name != "amf-0" {
		t.Fatalf("expected amf-0's entry to survive a Remove for an unrelated Pod, got %+v (ok=%v)", ref, ok)
	}
}

// Regression guard for the real race Remove's doc comment describes:
// Kubernetes doesn't serialize a deleted Pod's last reconcile against a new
// Pod's first one, so by the time a stale Remove for the old owner of an IP
// runs, a different Pod may have already reused that IP and been Put. That
// live mapping must survive.
func TestPodIPIndex_RemoveDoesNotEvictAReusedIPsNewOwner(t *testing.T) {
	idx := NewPodIPIndex()
	oldPod := PodRef{Namespace: "telecom-core", Name: "amf-0"}
	newPod := PodRef{Namespace: "telecom-core", Name: "amf-1"}

	idx.Put("10.42.0.7", oldPod)
	idx.Put("10.42.0.7", newPod) // IP reused by a different Pod before amf-0's deletion is processed.

	idx.Remove(types.NamespacedName{Namespace: oldPod.Namespace, Name: oldPod.Name})

	ref, ok := idx.Lookup("10.42.0.7")
	if !ok || ref != newPod {
		t.Fatalf("expected the reused IP's current owner %+v to survive the old owner's Remove, got %+v (ok=%v)", newPod, ref, ok)
	}
}

func TestPodIPIndex_PutMovesAPodsOwnEntryWhenItsIPChanges(t *testing.T) {
	idx := NewPodIPIndex()
	pod := PodRef{Namespace: "telecom-core", Name: "amf-0"}

	idx.Put("10.42.0.7", pod)
	idx.Put("10.42.0.9", pod) // Same Pod, e.g. re-scheduled with a new IP.

	if _, ok := idx.Lookup("10.42.0.7"); ok {
		t.Fatal("expected the stale old-IP entry to be dropped once the same Pod is Put under a new IP")
	}
	ref, ok := idx.Lookup("10.42.0.9")
	if !ok || ref != pod {
		t.Fatalf("expected the new-IP entry to resolve to %+v, got %+v (ok=%v)", pod, ref, ok)
	}

	// And Remove must still find it under the *new* IP, not the stale one.
	idx.Remove(types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name})
	if _, ok := idx.Lookup("10.42.0.9"); ok {
		t.Fatal("expected Remove to evict the entry under the Pod's current IP")
	}
}

// Regression guard for the same race TestPodIPIndex_RemoveDoesNotEvictAReusedIPsNewOwner
// covers, but on Put's own IP-change cleanup path instead of Remove: a stale
// Put (delivered late, e.g. a re-scheduled Pod's watch event arriving after
// a delay) must not evict a *different* Pod's live entry just because that
// different Pod happens to have reused the first Pod's old IP in the
// meantime.
func TestPodIPIndex_PutDoesNotEvictAReusedIPsNewOwnerWhenItsOwnIPChanges(t *testing.T) {
	idx := NewPodIPIndex()
	movedPod := PodRef{Namespace: "telecom-core", Name: "amf-0"}
	newOwnerOfOldIP := PodRef{Namespace: "telecom-core", Name: "amf-1"}

	idx.Put("10.42.0.7", movedPod)        // movedPod starts at .7.
	idx.Put("10.42.0.7", newOwnerOfOldIP) // .7 reused by a different Pod.
	idx.Put("10.42.0.9", movedPod)        // movedPod's own stale Put finally arrives, now at .9.

	ref, ok := idx.Lookup("10.42.0.7")
	if !ok || ref != newOwnerOfOldIP {
		t.Fatalf("expected the reused IP's current owner %+v to survive the moved Pod's stale Put, got %+v (ok=%v)", newOwnerOfOldIP, ref, ok)
	}
	ref, ok = idx.Lookup("10.42.0.9")
	if !ok || ref != movedPod {
		t.Fatalf("expected the moved Pod's new IP to resolve to %+v, got %+v (ok=%v)", movedPod, ref, ok)
	}
}
