package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Sensitivity controls how aggressively the AI engine's threat score is
// interpreted for a given policy.
// +kubebuilder:validation:Enum=low;medium;high
type Sensitivity string

const (
	SensitivityLow    Sensitivity = "low"
	SensitivityMedium Sensitivity = "medium"
	SensitivityHigh   Sensitivity = "high"
)

// WorkloadSelector identifies the workloads a TelecomSecurityPolicy protects.
// At least one of the fields must be set; fields are ANDed together.
type WorkloadSelector struct {
	// App matches the "app" label on Pods/Deployments in the policy's namespace.
	// +optional
	App string `json:"app,omitempty"`

	// MatchLabels matches an arbitrary label set, ANDed with App if both are set.
	// +optional
	MatchLabels map[string]string `json:"matchLabels,omitempty"`
}

// ThreatDetectionSpec configures how the AI engine's anomaly scoring is
// consumed for the target workloads.
type ThreatDetectionSpec struct {
	// Sensitivity biases the effective threshold applied to the incoming
	// ThreatScoreEvent: high lowers it, low raises it.
	// +kubebuilder:default=medium
	Sensitivity Sensitivity `json:"sensitivity,omitempty"`

	// AutoMitigate, when true, allows the controller to trigger Actions
	// automatically once the effective threat score threshold is crossed.
	// When false, the controller only updates Status so a human can decide.
	// +kubebuilder:default=false
	AutoMitigate bool `json:"autoMitigate,omitempty"`
}

// ActionsSpec declares which mitigations the controller is allowed to take
// when a policy's threat threshold is crossed and AutoMitigate is enabled.
type ActionsSpec struct {
	// EbpfBlock instructs the operator to push the offending source IP(s)
	// into the eBPF blocklist map on the affected node(s).
	// +kubebuilder:default=false
	EbpfBlock bool `json:"ebpfBlock,omitempty"`

	// IsolatePod instructs the operator to apply a service-mesh quarantine
	// policy (see pkg/mesh) around the affected workload.
	// +kubebuilder:default=false
	IsolatePod bool `json:"isolatePod,omitempty"`
}

// TelecomSecurityPolicySpec defines the desired state of a TelecomSecurityPolicy.
type TelecomSecurityPolicySpec struct {
	// TargetWorkloads lists the workloads this policy protects.
	// +kubebuilder:validation:MinItems=1
	TargetWorkloads []WorkloadSelector `json:"targetWorkloads"`

	// ThreatDetection configures scoring interpretation for this policy.
	ThreatDetection ThreatDetectionSpec `json:"threatDetection,omitempty"`

	// Actions declares which automated mitigations are permitted.
	Actions ActionsSpec `json:"actions,omitempty"`
}

// PolicyPhase is a coarse-grained summary of a TelecomSecurityPolicy's state.
// +kubebuilder:validation:Enum=Pending;Monitoring;Alerting;Mitigating;Degraded
type PolicyPhase string

const (
	PolicyPhasePending    PolicyPhase = "Pending"
	PolicyPhaseMonitoring PolicyPhase = "Monitoring"
	// PolicyPhaseAlerting means the effective threat score threshold was
	// crossed but ThreatDetection.AutoMitigate is false, so the controller
	// withheld Actions by policy rather than by failure -- the detection-
	// only "shadow mode" pkg/controller.ThreatScoreWatcher.applyPolicy sets
	// this in, deliberately distinct from PolicyPhaseDegraded (an operator-
	// side failure), so a pilot rollout's false-positive rate reads as
	// "working as configured," not "broken."
	PolicyPhaseAlerting   PolicyPhase = "Alerting"
	PolicyPhaseMitigating PolicyPhase = "Mitigating"
	PolicyPhaseDegraded   PolicyPhase = "Degraded"
)

// TelecomSecurityPolicyStatus defines the observed state of a TelecomSecurityPolicy.
type TelecomSecurityPolicyStatus struct {
	// Phase summarizes the policy's current state.
	// +optional
	Phase PolicyPhase `json:"phase,omitempty"`

	// ObservedThreatScore is the most recent threat score (0.0-1.0) matched
	// against one of this policy's target workloads.
	// +optional
	ObservedThreatScore string `json:"observedThreatScore,omitempty"`

	// LastMitigationTime records when an automated mitigation was last applied.
	// +optional
	LastMitigationTime *metav1.Time `json:"lastMitigationTime,omitempty"`

	// BlockedSourceIPs lists source IPs this policy has pushed into the eBPF
	// blocklist, so the finalizer can Unblock each one when the policy is
	// deleted (see pkg/controller.Reconciler's finalize). Only populated when
	// Actions.EbpfBlock is enabled and has actually fired.
	// +optional
	// +listType=set
	BlockedSourceIPs []string `json:"blockedSourceIPs,omitempty"`

	// Conditions represent the latest available observations of the policy's state.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=tsp
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Score",type=string,JSONPath=`.status.observedThreatScore`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// TelecomSecurityPolicy is the Schema for the telecomsecuritypolicies API.
// It declares which telecom workloads Sentinel5G protects, how sensitive the
// AI-driven threat detection should be, and which automated mitigations the
// operator is allowed to take in a closed loop.
type TelecomSecurityPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TelecomSecurityPolicySpec   `json:"spec,omitempty"`
	Status TelecomSecurityPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TelecomSecurityPolicyList contains a list of TelecomSecurityPolicy.
type TelecomSecurityPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TelecomSecurityPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(GroupVersion, &TelecomSecurityPolicy{}, &TelecomSecurityPolicyList{})
		// Required alongside AddKnownTypes, not optional boilerplate:
		// registers the common meta/v1 types (CreateOptions, UpdateOptions,
		// ListOptions, WatchEvent, ...) for THIS GroupVersion specifically —
		// client-go's REST client needs them to encode/decode requests
		// against the CRD. controller-runtime's now-deprecated
		// pkg/scheme.Builder did this call automatically; migrating off it
		// means doing it by hand (see that package's Register, and its own
		// doc comment for the exact replacement pattern this follows).
		// Missing this doesn't fail to compile -- it fails at runtime on
		// the first real Create/Update/List/Watch call against a real API
		// server, with "CreateOptions is not suitable for converting to
		// ...scheme" -- caught by pkg/controller/envtest_test.go, not by
		// the fake-client-backed unit tests.
		metav1.AddToGroupVersion(s, GroupVersion)
		return nil
	})
}
