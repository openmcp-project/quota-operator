package v1alpha1

import (
	"slices"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// QuotaServiceConfig is the Schema for the QuotaServiceConfig API
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=qcfg
type QuotaServiceConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec QuotaServiceConfigSpec `json:"spec,omitempty"`
}

type QuotaServiceConfigSpec struct {
	// Quotas is a list of QuotaDefinitions.
	Quotas []*QuotaDefinition `json:"quotas"`
}

type QuotaDefinition struct {
	// Name is the identifier for this quota definition.
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-.]*[a-z0-9])*$`
	Name string `json:"name"`
	// Selector is a label selector that specifies which namespaces this quota definition should be applied to.
	// If nil, all namespaces are selected.
	// +optional
	Selector *metav1.LabelSelector `json:"selector"`
	// ResourceQuotaTemplate is the template for the ResourceQuota that should be created for all namespaces which match the selector.
	// +kubebuilder:validation:Required
	ResourceQuotaTemplate *ResourceQuotaTemplate `json:"template"`
	// Mode is the mode in which the quota should be increased.
	// cumulative: multiple quota increases for the same resource will add up.
	// maximum: the highest quota increase for the same resource will be used.
	// singular: only one quota increase for the same resource will be used (specified via label on the namespace).
	// +kubebuilder:validation:Enum=cumulative;maximum;singular
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
	Spec corev1.ResourceQuotaSpec `json:"spec"`
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

// QuotaServiceConfigList contains a list of QuotaServiceConfig
// +kubebuilder:object:root=true
type QuotaServiceConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []QuotaServiceConfig `json:"items"`
}

// BaseResourceQuota returns a ResourceQuota object based on the configured template.
// A deep copy is returned, the returned object can be modified without affecting the original template.
// Note that the namespace is missing and has to be set afterwards.
func (d *QuotaDefinition) BaseResourceQuota() *corev1.ResourceQuota {
	res := &corev1.ResourceQuota{}
	res.SetName(d.Name)
	res.SetAnnotations(d.ResourceQuotaTemplate.Annotations)
	res.SetLabels(d.ResourceQuotaTemplate.Labels)
	res.Spec = d.ResourceQuotaTemplate.Spec
	return res.DeepCopy()
}

func init() {
	SchemeBuilder.Register(&QuotaServiceConfig{}, &QuotaServiceConfigList{})
}

// Validate validates the QuotaController configuration.
// This is equivalent to ValidateRaw().ToAggregate().
func (spec QuotaServiceConfigSpec) Validate() error {
	return spec.ValidateRaw().ToAggregate()
}

// ValidateRaw works like validate, but it returns a list of errors instead of an aggregated one.
func (spec QuotaServiceConfigSpec) ValidateRaw() field.ErrorList {
	allErrs := field.ErrorList{}

	fldPath := field.NewPath("spec")

	knownNames := sets.New[string]()
	for i, qd := range spec.Quotas {
		allErrs = append(allErrs, validateQuotaDefinition(qd, fldPath.Child("quotas").Index(i), knownNames)...)
	}

	return allErrs
}

func validateQuotaDefinition(qd *QuotaDefinition, fldPath *field.Path, knownNames sets.Set[string]) field.ErrorList {
	allErrs := field.ErrorList{}

	if qd == nil {
		allErrs = append(allErrs, field.Required(fldPath, "QuotaDefinition must not be empty"))
		return allErrs
	}

	if qd.Name == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("name"), "Name must not be empty"))
	} else if knownNames.Has(qd.Name) {
		allErrs = append(allErrs, field.Duplicate(fldPath.Child("name"), qd.Name))
	} else {
		knownNames.Insert(qd.Name)
	}

	if qd.ResourceQuotaTemplate == nil {
		allErrs = append(allErrs, field.Required(fldPath.Child("template"), "ResourceQuotaTemplate must not be empty"))
	}

	if qd.Mode == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("mode"), "Mode must not be empty"))
	} else if !slices.Contains(SUPPORTED_OPERATING_MODES, qd.Mode) {
		allErrs = append(allErrs, field.NotSupported(fldPath.Child("mode"), qd.Mode, SUPPORTED_OPERATING_MODES))
	}

	return allErrs
}

// GetQuotaDefinitionForName returns the QuotaDefinition with the given name, or nil if no such QuotaDefinition exists.
func (spec QuotaServiceConfigSpec) GetQuotaDefinitionForName(name string) *QuotaDefinition {
	for _, qd := range spec.Quotas {
		if qd.Name == name {
			return qd
		}
	}
	return nil
}
