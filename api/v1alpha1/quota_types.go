package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// QuotaIncreaseSpec defines the quota increase for a specific resource.
type QuotaIncreaseSpec struct {
	// Hard maps the resource name to the quantity that should be added to the ResourceQuota.
	// This is the same format that is used in the ResourceQuota resource.
	Hard corev1.ResourceList `json:"hard"`
}

// QuotaIncrease is the Schema for the QuotaIncrease API
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:resource:shortName=qi
// +kubebuilder:printcolumn:name="Mode",type=string,JSONPath=`.metadata.labels['quota\.openmcp\.cloud\/mode']`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Effect",type=string,JSONPath=`.metadata.annotations['quota\.openmcp\.cloud\/effect']`,priority=1
type QuotaIncrease struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec QuotaIncreaseSpec `json:"spec,omitempty"`
}

// QuotaIncreaseList contains a list of QuotaIncrease
// +kubebuilder:object:root=true
type QuotaIncreaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []QuotaIncrease `json:"items"`
}

func init() {
	SchemeBuilder.Register(&QuotaIncrease{}, &QuotaIncreaseList{})
}
