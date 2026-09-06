package controller

import "testing"

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
