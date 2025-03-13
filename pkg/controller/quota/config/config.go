package config

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type QuotaControllerConfig struct {
	// ExternalQuotaDefinitionNames contains the names of the QuotaDefinitions that are managed by other QuotaControllers running in the same cluster.
	// This is used to make sure that only one controller tries to manage a given namespace.
	// If only one QuotaController is running in the cluster, this should be left empty.
	// +optional
	ExternalQuotaDefinitionNames []string `json:"externalQuotaDefinitionNames,omitempty"`

	// Quotas is a list of QuotaDefinitions.
	Quotas []*QuotaDefinition `json:"quotas"`
}

type QuotaDefinition struct {
	// Name is the identifier for this quota definition.
	Name string `json:"name"`
	// Selector is a label selector that specifies which namespaces this quota definition should be applied to.
	// If nil, all namespaces are selected.
	// +optional
	Selector *metav1.LabelSelector `json:"selector"`
	// ResourceQuotaTemplate is the template for the ResourceQuota that should be created for all namespaces which match the selector.
	ResourceQuotaTemplate *ResourceQuotaTemplate `json:"template"`
	// Mode is the mode in which the quota should be increased.
	// cumulative: multiple quota increases for the same resource will add up.
	// maximum: the highest quota increase for the same resource will be used.
	// singular: only one quota increase for the same resource will be used (specified via label on the namespace).
	Mode QuotaIncreaseOperatingMode `json:"mode"`
	// DeleteIneffectiveQuotas specifies whether ResourceQuotas that are no longer effective should be deleted automatically.
	// +optional
	DeleteIneffectiveQuotas bool `json:"deleteIneffectiveQuotas,omitempty"`
}

type ResourceQuotaTemplate struct {
	// Annotations are the annotations that should be added to the generated ResourceQuota.
	Annotations map[string]string `json:"annotations,omitempty"`
	// Labels are the labels that should be added to the generated ResourceQuota.
	Labels map[string]string `json:"labels,omitempty"`
	// Spec is the spec of the generated ResourceQuota.
	Spec *corev1.ResourceQuotaSpec `json:"spec"`
}

type QuotaIncreaseOperatingMode string

const (
	// CUMULATIVE means that multiple quota increases for the same resource will add up.
	CUMULATIVE QuotaIncreaseOperatingMode = "cumulative"
	// MAXIMUM means that the highest quota increase for the same resource will be used.
	MAXIMUM QuotaIncreaseOperatingMode = "maximum"
	// SINGULAR means that only one quota increase for the same resource will be used.
	SINGULAR QuotaIncreaseOperatingMode = "singular"
)

var (
	// SUPPORTED_OPERATING_MODES contains all supported operating modes. Used for validation.
	SUPPORTED_OPERATING_MODES = []QuotaIncreaseOperatingMode{CUMULATIVE, MAXIMUM, SINGULAR}
)

func (d *QuotaDefinition) DeepCopy() *QuotaDefinition {
	if d == nil {
		return nil
	}
	selector := d.Selector.DeepCopy()
	template := d.ResourceQuotaTemplate.DeepCopy()
	return &QuotaDefinition{
		Name:                    d.Name,
		Selector:                selector,
		ResourceQuotaTemplate:   template,
		Mode:                    d.Mode,
		DeleteIneffectiveQuotas: d.DeleteIneffectiveQuotas,
	}
}

func (t *ResourceQuotaTemplate) DeepCopy() *ResourceQuotaTemplate {
	if t == nil {
		return nil
	}
	annotations := make(map[string]string, len(t.Annotations))
	for k, v := range t.Annotations {
		annotations[k] = v
	}
	labels := make(map[string]string, len(t.Labels))
	for k, v := range t.Labels {
		labels[k] = v
	}
	spec := t.Spec.DeepCopy()
	return &ResourceQuotaTemplate{
		Annotations: annotations,
		Labels:      labels,
		Spec:        spec,
	}
}

// BaseResourceQuota returns a ResourceQuota object based on the configured template.
// A deep copy is returned, the returned object can be modified without affecting the original template.
// Note that the namespace is missing and has to be set afterwards.
func (d *QuotaDefinition) BaseResourceQuota() *corev1.ResourceQuota {
	res := &corev1.ResourceQuota{}
	res.SetName(d.Name)
	res.SetAnnotations(d.ResourceQuotaTemplate.Annotations)
	res.SetLabels(d.ResourceQuotaTemplate.Labels)
	res.Spec = *d.ResourceQuotaTemplate.Spec
	return res.DeepCopy()
}
