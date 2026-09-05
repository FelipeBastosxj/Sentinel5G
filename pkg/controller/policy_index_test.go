package controller

import (
	"testing"

	"k8s.io/apimachinery/pkg/types"

	securityv1alpha1 "github.com/sentinel5g/sentinel5g/api/v1alpha1"
)

func TestSelectorMatchesOne(t *testing.T) {
	tests := []struct {
		name      string
		selector  securityv1alpha1.WorkloadSelector
		podLabels map[string]string
		want      bool
	}{
		{
			name:      "app match",
			selector:  securityv1alpha1.WorkloadSelector{App: "amf-service"},
			podLabels: map[string]string{"app": "amf-service", "tier": "core"},
			want:      true,
		},
		{
			name:      "app mismatch",
			selector:  securityv1alpha1.WorkloadSelector{App: "amf-service"},
			podLabels: map[string]string{"app": "smf-service"},
			want:      false,
		},
		{
			name:      "matchLabels subset match",
			selector:  securityv1alpha1.WorkloadSelector{MatchLabels: map[string]string{"tier": "core"}},
			podLabels: map[string]string{"app": "amf-service", "tier": "core"},
			want:      true,
		},
		{
			name:      "matchLabels missing key",
			selector:  securityv1alpha1.WorkloadSelector{MatchLabels: map[string]string{"tier": "core"}},
			podLabels: map[string]string{"app": "amf-service"},
			want:      false,
		},
		{
			name:      "app and matchLabels both required (AND)",
			selector:  securityv1alpha1.WorkloadSelector{App: "amf-service", MatchLabels: map[string]string{"tier": "edge"}},
			podLabels: map[string]string{"app": "amf-service", "tier": "core"},
			want:      false,
		},
		{
			name:      "empty selector never matches",
			selector:  securityv1alpha1.WorkloadSelector{},
			podLabels: map[string]string{"app": "amf-service"},
			want:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := selectorMatchesOne(tc.selector, tc.podLabels)
			if got != tc.want {
				t.Errorf("selectorMatchesOne(%+v, %v) = %v, want %v", tc.selector, tc.podLabels, got, tc.want)
			}
		})
	}
}

func TestPolicyIndex_MatchingPolicies(t *testing.T) {
	idx := NewPolicyIndex()

	protectAMF := &securityv1alpha1.TelecomSecurityPolicy{}
	protectAMF.Namespace = "telecom-core"
	protectAMF.Name = "protect-amf-core"
	protectAMF.Spec.TargetWorkloads = []securityv1alpha1.WorkloadSelector{{App: "amf-service"}}
	idx.Put(protectAMF)

	protectSMF := &securityv1alpha1.TelecomSecurityPolicy{}
	protectSMF.Namespace = "telecom-core"
	protectSMF.Name = "protect-smf-core"
	protectSMF.Spec.TargetWorkloads = []securityv1alpha1.WorkloadSelector{{App: "smf-service"}}
	idx.Put(protectSMF)

	otherNamespace := &securityv1alpha1.TelecomSecurityPolicy{}
	otherNamespace.Namespace = "other-ns"
	otherNamespace.Name = "protect-amf-other"
	otherNamespace.Spec.TargetWorkloads = []securityv1alpha1.WorkloadSelector{{App: "amf-service"}}
	idx.Put(otherNamespace)

	matches := idx.MatchingPolicies("telecom-core", map[string]string{"app": "amf-service"})
	if len(matches) != 1 || matches[0].Name != "protect-amf-core" {
		t.Fatalf("expected exactly [protect-amf-core], got %+v", matches)
	}

	idx.Remove(types.NamespacedName{Namespace: "telecom-core", Name: "protect-amf-core"})

	matches = idx.MatchingPolicies("telecom-core", map[string]string{"app": "amf-service"})
	if len(matches) != 0 {
		t.Fatalf("expected no matches after Remove, got %+v", matches)
	}
}
