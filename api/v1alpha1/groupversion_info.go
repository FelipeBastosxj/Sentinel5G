// Package v1alpha1 contains API Schema definitions for the security v1alpha1 API group.
// +kubebuilder:object:generate=true
// +groupName=security.sentinel5g.io
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is group version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: "security.sentinel5g.io", Version: "v1alpha1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
	// A plain runtime.SchemeBuilder, not controller-runtime's pkg/scheme.Builder
	// convenience wrapper -- the latter is deprecated (staticcheck SA1019) for
	// exactly this kind of api package, which is meant to depend on the
	// standard library, k8s.io/apimachinery, and other api packages only, not
	// controller-runtime. See telecomsecuritypolicy_types.go's init() for how
	// this shifts Register's call site: runtime.SchemeBuilder.Register takes
	// scheme-setup functions, not runtime.Object instances directly.
	SchemeBuilder = &runtime.SchemeBuilder{}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
