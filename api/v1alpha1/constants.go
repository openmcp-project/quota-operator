package v1alpha1

import "fmt"

var (
	// LabelPrefix is the common label prefix for all labels used by the Quota Controller.
	LabelPrefix = fmt.Sprintf("quota.%s", GroupVersion.Group)

	// SingularQuotaIncreaseLabel is the label used to specify which QuotaIncrease to use in singular mode.
	// It is attached to the containing namespace.
	SingularQuotaIncreaseLabel = LabelPrefix + "/use"

	// BaseQuotaLabel specifies which quota definition from the configuration is applied to this namespace.
	BaseQuotaLabel = LabelPrefix + "/base"

	// EffectAnnotation is the annotation used to store the effect of a QuotaIncrease.
	EffectAnnotation = LabelPrefix + "/effect"

	// QuotaIncreaseOperationModeLabel is used to display the operation mode on QuotaIncreases and namespaces.
	QuotaIncreaseOperationModeLabel = LabelPrefix + "/mode"

	// ManagedByLabel is used to mark the ResourceQuotas created by the Quota Controller.
	ManagedByLabel = LabelPrefix + "/managed-by"

	// QuotaDefinitionLabel is used to mark the ResourceQuotas with the QuotaDefinition they are based on.
	QuotaDefinitionLabel = LabelPrefix + "/quota-definition"
)

const (
	// ActiveSingularQuotaIncreaseEffectPrefix is used to prefix the effect annotation of the active QuotaIncrease in singular mode.
	// It is set even if the QuotaIncrease does not have any effect.
	ActiveSingularQuotaIncreaseEffectPrefix = "[active]"
)
