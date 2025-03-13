package config

import (
	"regexp"
	"slices"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

var (
	k8sNameRegex = regexp.MustCompile("^[a-z0-9]([-.]*[a-z0-9])*$")
)

// Validate validates the QuotaController configuration.
// This is equivalent to ValidateRaw().ToAggregate().
func Validate(cfg *QuotaControllerConfig) error {
	return ValidateRaw(cfg).ToAggregate()
}

// ValidateRaw works like validate, but it returns a list of errors instead of an aggregated one.
func ValidateRaw(cfg *QuotaControllerConfig) field.ErrorList {
	allErrs := field.ErrorList{}

	if cfg == nil {
		allErrs = append(allErrs, field.Required(field.NewPath(""), "QuotaController configuration must not be empty"))
		return allErrs
	}

	if len(cfg.ExternalQuotaDefinitionNames) > 0 {
		for i, name := range cfg.ExternalQuotaDefinitionNames {
			if name == "" {
				allErrs = append(allErrs, field.Invalid(field.NewPath("externalQuotaDefinitionNames").Index(i), name, "external quota definition name must not be empty"))
			}
		}
	}

	knownNames := sets.New[string]()
	for i, qd := range cfg.Quotas {
		allErrs = append(allErrs, validateQuotaDefinition(qd, field.NewPath("quotas").Index(i), knownNames)...)
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
	} else if len(qd.Name) > 253 || !k8sNameRegex.MatchString(qd.Name) {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("name"), qd.Name, "Name must be a valid DNS subdomain name (only lowercase letters, digits, '-', and '.', max length 253 characters)"))
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
