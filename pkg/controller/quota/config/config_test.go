package config_test

import (
	"path"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/openmcp-project/quota-operator/pkg/controller/quota/config"
)

func TestConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Config Test Suite")
}

var _ = Describe("Config", func() {

	Context("Loading", func() {

		It("should fail if the config file does not exist", func() {
			_, err := config.LoadConfig(path.Join("testdata", "nonexistent.yaml"))
			Expect(err).To(HaveOccurred())
		})

		It("should fail if the structure of the config file is invalid", func() {
			_, err := config.LoadConfig(path.Join("testdata", "config_invalid_structure.yaml"))
			Expect(err).To(HaveOccurred())
		})

		It("should correctly load a valid config file", func() {
			cfg, err := config.LoadConfig(path.Join("testdata", "config_valid.yaml"))
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg).To(PointTo(MatchFields(0, Fields{
				"Quotas": ContainElements(
					&config.QuotaDefinition{
						Name: "project",
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"foo.bar.baz/foobar": "asdf",
							},
						},
						Mode: config.CUMULATIVE,
						ResourceQuotaTemplate: &config.ResourceQuotaTemplate{
							Annotations: map[string]string{
								"foo.bar.baz/foobar": "asdf",
							},
							Spec: &corev1.ResourceQuotaSpec{
								Hard: corev1.ResourceList{
									corev1.ResourceName("count/secrets"): resource.MustParse("3"),
								},
							},
						},
						DeleteIneffectiveQuotas: true,
					},
					&config.QuotaDefinition{
						Name: "workspace",
						Selector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "openmcp.cloud/project",
									Operator: metav1.LabelSelectorOpExists,
								},
								{
									Key:      "openmcp.cloud/workspace",
									Operator: metav1.LabelSelectorOpExists,
								},
							},
						},
						Mode: config.MAXIMUM,
						ResourceQuotaTemplate: &config.ResourceQuotaTemplate{
							Labels: map[string]string{
								"foo.bar.baz/foobar": "asdf",
							},
							Spec: &corev1.ResourceQuotaSpec{
								Hard: corev1.ResourceList{
									corev1.ResourceName("count/configmaps"): resource.MustParse("3"),
								},
							},
						},
						DeleteIneffectiveQuotas: false,
					},
					&config.QuotaDefinition{
						Name:     "all",
						Selector: nil,
						Mode:     config.SINGULAR,
						ResourceQuotaTemplate: &config.ResourceQuotaTemplate{
							Spec: &corev1.ResourceQuotaSpec{
								Hard: corev1.ResourceList{
									corev1.ResourceName("count/serviceaccounts"): resource.MustParse("3"),
								},
							},
						},
						DeleteIneffectiveQuotas: false,
					},
				),
				"ExternalQuotaDefinitionNames": ConsistOf("foo", "bar"),
			})))
		})

	})

	Context("Validation", func() {

		It("should return no errors for a valid config", func() {
			cfg, err := config.LoadConfig(path.Join("testdata", "config_valid.yaml"))
			Expect(err).ToNot(HaveOccurred())
			Expect(config.Validate(cfg)).ToNot(HaveOccurred())
		})

		It("should detect duplicate, missing, and invalid names", func() {
			cfg, err := config.LoadConfig(path.Join("testdata", "config_invalid_name.yaml"))
			Expect(err).ToNot(HaveOccurred())
			errs := config.ValidateRaw(cfg)
			Expect(errs).To(ConsistOf(
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("quotas[0].name"),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("quotas[1].name"),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("quotas[2].name"),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("quotas[3].name"),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeDuplicate),
					"Field": Equal("quotas[5].name"),
				})),
			))
		})

		It("should detect invalid modes", func() {
			cfg, err := config.LoadConfig(path.Join("testdata", "config_invalid_mode.yaml"))
			Expect(err).ToNot(HaveOccurred())
			errs := config.ValidateRaw(cfg)
			Expect(errs).To(ConsistOf(
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("quotas[0].mode"),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeNotSupported),
					"Field": Equal("quotas[1].mode"),
				})),
			))
		})

	})

	Context("Auxiliary Functions", func() {

		Context("QuotaDefinition DeepCopy()", func() {

			It("should return a deep copy of the original QuotaDefinition", func() {
				base := &config.QuotaDefinition{
					Name: "foo",
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"foo.bar.baz/foobar": "asdf",
						},
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "openmcp.cloud/project",
								Operator: metav1.LabelSelectorOpExists,
							},
						},
					},
					Mode:                    config.CUMULATIVE,
					DeleteIneffectiveQuotas: true,
				}
				old := &config.QuotaDefinition{
					Name: "foo",
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"foo.bar.baz/foobar": "asdf",
						},
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "openmcp.cloud/project",
								Operator: metav1.LabelSelectorOpExists,
							},
						},
					},
					Mode:                    config.CUMULATIVE,
					DeleteIneffectiveQuotas: true,
				}
				Expect(old).To(Equal(base)) // sanity check
				new := old.DeepCopy()
				Expect(new).ToNot(BeIdenticalTo(old))
				Expect(new).To(Equal(old))
				new.Name = "bar"
				new.Selector.MatchLabels["foo.bar.baz/foobar"] = "fdsa"
				new.Selector.MatchExpressions[0].Key = "openmcp.cloud/workspace"
				new.Mode = config.MAXIMUM
				new.DeleteIneffectiveQuotas = false
				Expect(new).ToNot(Equal(old))
				Expect(new).ToNot(Equal(base))
				Expect(old).To(Equal(base))
			})

		})

		Context("ResourceQuotaTemplate DeepCopy()", func() {

			It("should return a deep copy of the original ResourceQuotaTemplate", func() {
				base := &config.ResourceQuotaTemplate{
					Annotations: map[string]string{
						"annKey1": "annVal1",
					},
					Labels: map[string]string{
						"labelKey1": "labelVal1",
					},
					Spec: &corev1.ResourceQuotaSpec{
						Hard: corev1.ResourceList{
							corev1.ResourceName("count/secrets"): resource.MustParse("3"),
						},
					},
				}
				old := &config.ResourceQuotaTemplate{
					Annotations: map[string]string{
						"annKey1": "annVal1",
					},
					Labels: map[string]string{
						"labelKey1": "labelVal1",
					},
					Spec: &corev1.ResourceQuotaSpec{
						Hard: corev1.ResourceList{
							corev1.ResourceName("count/secrets"): resource.MustParse("3"),
						},
					},
				}
				Expect(old).To(Equal(base)) // sanity check
				new := old.DeepCopy()
				Expect(new).ToNot(BeIdenticalTo(old))
				Expect(new).To(Equal(old))
				new.Annotations["annKey2"] = "annVal2"
				new.Labels["labelKey2"] = "labelVal2"
				new.Spec.Hard[corev1.ResourceName("count/configmaps")] = resource.MustParse("3")
				Expect(new).ToNot(Equal(old))
				Expect(new).ToNot(Equal(base))
				Expect(old).To(Equal(base))
			})

		})

		Context("BaseResourceQuota()", func() {

			It("should return a base ResourceQuota", func() {
				cfg, err := config.LoadConfig(path.Join("testdata", "config_valid.yaml"))
				Expect(err).ToNot(HaveOccurred())
				qd := cfg.Quotas[0]
				rq := qd.BaseResourceQuota()
				Expect(rq.Name).To(Equal(qd.Name))
				Expect(rq.Namespace).To(Equal(""))
				Expect(rq.Annotations).To(Equal(qd.ResourceQuotaTemplate.Annotations))
				Expect(rq.Labels).To(Equal(qd.ResourceQuotaTemplate.Labels))
				Expect(rq.Spec).To(Equal(*qd.ResourceQuotaTemplate.Spec))
			})

		})

	})

})
