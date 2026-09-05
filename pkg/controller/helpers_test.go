package controller

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	securityv1alpha1 "github.com/sentinel5g/sentinel5g/api/v1alpha1"
)

// newScheme returns a runtime.Scheme with the built-in Kubernetes types
// (needed for corev1.Pod lookups in ThreatScoreWatcher) and the
// TelecomSecurityPolicy CRD registered, for use with the fake client in tests.
func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = securityv1alpha1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	return s
}

// nnFor returns the NamespacedName for a TelecomSecurityPolicy test fixture.
func nnFor(policy *securityv1alpha1.TelecomSecurityPolicy) types.NamespacedName {
	return types.NamespacedName{Namespace: policy.Namespace, Name: policy.Name}
}
