package quota_test

import (
	"fmt"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	gtypes "github.com/onsi/gomega/types"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	openmcpctrlutil "github.com/openmcp-project/controller-utils/pkg/controller"
	testutils "github.com/openmcp-project/controller-utils/pkg/testing"

	quotainstall "github.com/openmcp-project/quota-operator/api/install"
	quotav1alpha1 "github.com/openmcp-project/quota-operator/api/v1alpha1"
	quotacontroller "github.com/openmcp-project/quota-operator/internal/controller/quota"
)

const (
	providerName      = "quota"
	platformCluster   = "platform"
	onboardingCluster = "onboarding"
	rec               = providerName
)

func TestConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Quota Controller Test Suite")
}

func matchNumericQuantity(val int64) gtypes.GomegaMatcher {
	return WithTransform(func(q resource.Quantity) int64 {
		return q.Value()
	}, BeNumerically("==", val))
}

func matchQuantity(q2 resource.Quantity) gtypes.GomegaMatcher {
	return WithTransform(func(q resource.Quantity) bool {
		return q.Equal(q2)
	}, BeTrue())
}

func haveName(name string) gtypes.GomegaMatcher {
	return WithTransform(func(obj client.Object) string {
		return obj.GetName()
	}, Equal(name))
}

func withPointerizedSlice[T any](matcher gtypes.GomegaMatcher) gtypes.GomegaMatcher {
	return WithTransform(func(items []T) []*T {
		res := make([]*T, len(items))
		for i, item := range items {
			res[i] = &item
		}
		return res
	}, matcher)
}

func defaultTestSetup(mode quotav1alpha1.QuotaIncreaseOperatingMode, deleteIneffectiveQuotas bool, testDataPathSegments ...string) *testutils.ComplexEnvironment {
	env := testutils.NewComplexEnvironmentBuilder().
		WithInitObjectPath(platformCluster, filepath.Join(testDataPathSegments...), "platform").
		WithInitObjectPath(onboardingCluster, filepath.Join(testDataPathSegments...), "onboarding").
		WithFakeClient(platformCluster, quotainstall.InstallOperatorAPIsPlatform(runtime.NewScheme())).
		WithFakeClient(onboardingCluster, quotainstall.InstallOperatorAPIsOnboarding(runtime.NewScheme())).
		WithReconcilerConstructor(rec, func(c ...client.Client) reconcile.Reconciler {
			return quotacontroller.NewQuotaController(c[0], c[1], providerName)
		}, platformCluster, onboardingCluster).
		Build()

	cfg := &quotav1alpha1.QuotaServiceConfig{}
	cfg.SetName(providerName)
	ExpectWithOffset(1, env.Client(platformCluster).Get(env.Ctx, client.ObjectKeyFromObject(cfg), cfg)).To(Succeed())
	for i := range cfg.Spec.Quotas {
		cfg.Spec.Quotas[i].Mode = mode
		cfg.Spec.Quotas[i].DeleteIneffectiveQuotas = deleteIneffectiveQuotas
	}
	ExpectWithOffset(1, env.Client(platformCluster).Update(env.Ctx, cfg)).To(Succeed())

	return env
}

var _ = Describe("CO-1155 QuotaIncrease Controller", func() {
	Context("Independent of Operating Mode", func() {

		It("should ignore namespaces with a managed-by label belonging to another controller instance", func() {
			env := defaultTestSetup(quotav1alpha1.CUMULATIVE, false, "testdata", "test-01")

			ns := &corev1.Namespace{}
			ns.SetName("ns-normal")
			Expect(env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(ns), ns)).To(Succeed())
			Expect(openmcpctrlutil.EnsureLabel(env.Ctx, env.Client(onboardingCluster), ns, quotav1alpha1.ManagedByLabel, "foreign", true)).To(Succeed())

			// verify that no ResourceQuotas exist
			rql := &corev1.ResourceQuotaList{}
			Expect(env.Client(onboardingCluster).List(env.Ctx, rql, client.InNamespace(ns.Name))).To(Succeed())
			Expect(rql.Items).To(BeEmpty())

			env.ShouldReconcile(rec, testutils.RequestFromObject(ns))

			// verify that no ResourceQuota was created
			Expect(env.Client(onboardingCluster).List(env.Ctx, rql, client.InNamespace(ns.Name))).To(Succeed())
			Expect(rql.Items).To(BeEmpty())
		})

		It("should use the first matching quota definition", func() {
			env := defaultTestSetup(quotav1alpha1.CUMULATIVE, false, "testdata", "test-01")

			ns := &corev1.Namespace{}
			ns.SetName("ns-normal")
			Expect(env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(ns), ns)).To(Succeed())

			// verify that no ResourceQuotas exist
			rql := &corev1.ResourceQuotaList{}
			Expect(env.Client(onboardingCluster).List(env.Ctx, rql, client.InNamespace(ns.Name))).To(Succeed())
			Expect(rql.Items).To(BeEmpty())

			env.ShouldReconcile(rec, testutils.RequestFromObject(ns))

			// verify that no ResourceQuota was created
			Expect(env.Client(onboardingCluster).List(env.Ctx, rql, client.InNamespace(ns.Name))).To(Succeed())
			Expect(rql.Items).ToNot(BeEmpty())
			Expect(env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(ns), ns)).To(Succeed())
			Expect(ns.Labels).To(HaveKeyWithValue(quotav1alpha1.BaseQuotaLabel, "all"))
		})

	})

	Context(fmt.Sprintf("Operating Mode: %s", quotav1alpha1.CUMULATIVE), func() {

		It("should add the operating mode label to the namespace", func() {
			env := defaultTestSetup(quotav1alpha1.CUMULATIVE, false, "testdata", "test-01")

			ns := &corev1.Namespace{}
			ns.SetName("ns-project")
			Expect(env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(ns), ns)).To(Succeed())
			Expect(openmcpctrlutil.HasLabel(ns, quotav1alpha1.QuotaIncreaseOperationModeLabel)).To(BeFalse())
			env.ShouldReconcile(rec, testutils.RequestFromObject(ns))
			Expect(env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(ns), ns)).To(Succeed())
			Expect(ns.Labels).To(HaveKeyWithValue(quotav1alpha1.QuotaIncreaseOperationModeLabel, string(quotav1alpha1.CUMULATIVE)))
		})

		It("should apply quota increases correctly", func() {
			env := defaultTestSetup(quotav1alpha1.CUMULATIVE, false, "testdata", "test-01")

			// verify test setup
			ns := &corev1.Namespace{}
			ns.SetName("ns-project")
			Expect(env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(ns), ns)).To(Succeed())
			Expect(openmcpctrlutil.HasLabel(ns, quotav1alpha1.BaseQuotaLabel)).To(BeFalse())
			rq := &corev1.ResourceQuota{}
			rq.SetName("project")
			rq.SetNamespace(ns.Name)
			err := env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(rq), rq)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			env.ShouldReconcile(rec, testutils.RequestFromObject(ns))
			// namespace should have been labeled
			Expect(env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(ns), ns)).To(Succeed())
			Expect(ns.Labels).To(HaveKeyWithValue(quotav1alpha1.BaseQuotaLabel, "project"))
			// ResourceQuota should have been created
			Expect(env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(rq), rq)).To(Succeed())
			// ResourceQuota should have owner reference pointing to namespace
			Expect(rq.OwnerReferences).To(ConsistOf(MatchFields(IgnoreExtras, Fields{
				"Kind": Equal("Namespace"),
				"Name": Equal(ns.Name),
			})))

			// list all QuotaIncreases in project namespace to determine expected secret quantity
			secretQ := int64(3)
			var cmQ int64 = 0
			qis := &quotav1alpha1.QuotaIncreaseList{}
			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns.Name))).To(Succeed())
			for _, qi := range qis.Items {
				rawQ := qi.Spec.Hard["count/secrets"]
				secretQ += rawQ.Value()
				rawQ, ok := qi.Spec.Hard["count/configmaps"]
				if ok {
					cmQ += rawQ.Value()
				}
			}
			Expect(rq.Spec.Hard["count/secrets"]).To(matchNumericQuantity(secretQ))
			Expect(rq.Spec.Hard["count/configmaps"]).To(matchNumericQuantity(cmQ))

			// QuotaIncreases should have been annotated and labeled
			for _, qi := range qis.Items {
				value, ok := openmcpctrlutil.GetAnnotation(&qi, quotav1alpha1.EffectAnnotation)
				Expect(ok).To(BeTrue())
				for res, q := range qi.Spec.Hard {
					Expect(value).To(ContainSubstring(fmt.Sprintf("%s: %s", res, q.String())))
				}
				Expect(qi.Labels).To(HaveKeyWithValue(quotav1alpha1.QuotaIncreaseOperationModeLabel, string(quotav1alpha1.CUMULATIVE)))
			}
		})

		It("should not delete ineffective QuotaIncreases if deleteIneffectiveQuotas is false", func() {
			env := defaultTestSetup(quotav1alpha1.CUMULATIVE, false, "testdata", "test-01")

			ns_project := &corev1.Namespace{}
			ns_project.SetName("ns-project")

			// count QuotaIncreases
			qis := &quotav1alpha1.QuotaIncreaseList{}
			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			qiCountOld := len(qis.Items)
			Expect(qiCountOld).To(BeNumerically(">=", 3))
			Expect(qis.Items).To(withPointerizedSlice[quotav1alpha1.QuotaIncrease](ContainElement(haveName("qi-project-empty"))))

			env.ShouldReconcile(rec, testutils.RequestFromObject(ns_project))

			// for cumulative mode, only empty QuotaIncreases are considered ineffective
			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			Expect(qis.Items).To(HaveLen(qiCountOld))
			Expect(qis.Items).To(withPointerizedSlice[quotav1alpha1.QuotaIncrease](ContainElement(haveName("qi-project-empty"))))

			// check workspace namespace
			ns_workspace := &corev1.Namespace{}
			ns_workspace.SetName("ns-workspace")
			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			Expect(qis.Items).ToNot(BeEmpty())
			qiCountOld = len(qis.Items)

			// reconcile workspace namespace with workspace quota controller
			env.ShouldReconcile(rec, testutils.RequestFromObject(ns_workspace))

			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			Expect(qis.Items).To(HaveLen(qiCountOld))
		})

		It("should delete ineffective QuotaIncreases if deleteIneffectiveQuotas is true", func() {
			env := defaultTestSetup(quotav1alpha1.CUMULATIVE, true, "testdata", "test-01")

			ns_project := &corev1.Namespace{}
			ns_project.SetName("ns-project")

			// count QuotaIncreases
			qis := &quotav1alpha1.QuotaIncreaseList{}
			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			qiCountOld := len(qis.Items)
			Expect(qiCountOld).To(Equal(4))
			Expect(qis.Items).To(withPointerizedSlice[quotav1alpha1.QuotaIncrease](ContainElement(haveName("qi-project-empty"))))

			env.ShouldReconcile(rec, testutils.RequestFromObject(ns_project))

			// for cumulative mode, only empty QuotaIncreases are considered ineffective
			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			qiCountNew := len(qis.Items)
			Expect(qiCountNew).To(Equal(qiCountOld - 1))
			Expect(qis.Items).ToNot(withPointerizedSlice[quotav1alpha1.QuotaIncrease](ContainElement(haveName("qi-project-empty"))))

			// check workspace namespace
			ns_workspace := &corev1.Namespace{}
			ns_workspace.SetName("ns-workspace")
			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_workspace.Name))).To(Succeed())
			Expect(qis.Items).ToNot(BeEmpty())
			qiCountOld = len(qis.Items)

			// reconcile workspace namespace with workspace quota controller
			env.ShouldReconcile(rec, testutils.RequestFromObject(ns_workspace))

			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_workspace.Name))).To(Succeed())
			qiCountNew = len(qis.Items)
			Expect(qiCountNew).To(Equal(qiCountOld))
		})

	})

	Context(fmt.Sprintf("Operating Mode: %s", quotav1alpha1.MAXIMUM), func() {

		It("should add the operating mode label to the namespace", func() {
			env := defaultTestSetup(quotav1alpha1.MAXIMUM, false, "testdata", "test-01")

			ns_project := &corev1.Namespace{}
			ns_project.SetName("ns-project")
			Expect(env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(ns_project), ns_project)).To(Succeed())
			Expect(openmcpctrlutil.HasLabel(ns_project, quotav1alpha1.QuotaIncreaseOperationModeLabel)).To(BeFalse())
			env.ShouldReconcile(rec, testutils.RequestFromObject(ns_project))
			Expect(env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(ns_project), ns_project)).To(Succeed())
			Expect(ns_project.Labels).To(HaveKeyWithValue(quotav1alpha1.QuotaIncreaseOperationModeLabel, string(quotav1alpha1.MAXIMUM)))
		})

		It("should apply quota increases correctly", func() {
			env := defaultTestSetup(quotav1alpha1.MAXIMUM, false, "testdata", "test-01")

			// verify test setup
			ns_project := &corev1.Namespace{}
			ns_project.SetName("ns-project")
			Expect(env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(ns_project), ns_project)).To(Succeed())
			Expect(openmcpctrlutil.HasLabel(ns_project, quotav1alpha1.BaseQuotaLabel)).To(BeFalse())
			rq_project := &corev1.ResourceQuota{}
			rq_project.SetName("project")
			rq_project.SetNamespace(ns_project.Name)
			err := env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(rq_project), rq_project)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			env.ShouldReconcile(rec, testutils.RequestFromObject(ns_project))
			// namespace should have been labeled
			Expect(env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(ns_project), ns_project)).To(Succeed())
			Expect(ns_project.Labels).To(HaveKeyWithValue(quotav1alpha1.BaseQuotaLabel, "project"))
			// ResourceQuota should have been created
			Expect(env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(rq_project), rq_project)).To(Succeed())
			// ResourceQuota should have owner reference pointing to namespace
			Expect(rq_project.OwnerReferences).To(ConsistOf(MatchFields(IgnoreExtras, Fields{
				"Kind": Equal("Namespace"),
				"Name": Equal(ns_project.Name),
			})))

			// list all QuotaIncreases in project namespace to determine expected secret quantity
			cfg := &quotav1alpha1.QuotaServiceConfig{}
			cfg.SetName(providerName)
			Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKeyFromObject(cfg), cfg)).To(Succeed())
			qd := cfg.Spec.GetQuotaDefinitionForName("project")
			Expect(qd).ToNot(BeNil())
			secretQ := qd.ResourceQuotaTemplate.Spec.Hard["count/secrets"].DeepCopy()
			cmQ := qd.ResourceQuotaTemplate.Spec.Hard["count/configmaps"].DeepCopy()
			saQ := qd.ResourceQuotaTemplate.Spec.Hard["count/serviceaccounts"].DeepCopy()
			qis := &quotav1alpha1.QuotaIncreaseList{}
			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			for _, qi := range qis.Items {
				rawQ, ok := qi.Spec.Hard["count/secrets"]
				if ok && rawQ.Cmp(secretQ) > 0 {
					secretQ = rawQ.DeepCopy()
				}
				rawQ, ok = qi.Spec.Hard["count/configmaps"]
				if ok && rawQ.Cmp(cmQ) > 0 {
					cmQ = rawQ.DeepCopy()
				}
				rawQ, ok = qi.Spec.Hard["count/serviceaccounts"]
				if ok && rawQ.Cmp(saQ) > 0 {
					saQ = rawQ.DeepCopy()
				}
			}
			Expect(rq_project.Spec.Hard["count/secrets"]).To(matchQuantity(secretQ))
			Expect(rq_project.Spec.Hard["count/configmaps"]).To(matchQuantity(cmQ))
			Expect(rq_project.Spec.Hard["count/serviceaccounts"]).To(matchQuantity(saQ))

			// QuotaIncreases should have been annotated
			for _, qi := range qis.Items {
				value, ok := openmcpctrlutil.GetAnnotation(&qi, quotav1alpha1.EffectAnnotation)
				Expect(ok).To(BeTrue())
				switch qi.Name {
				case "qi-project-max":
					// highest quota increase for secrets and configmaps
					secQ := qi.Spec.Hard["count/secrets"]
					cmQ := qi.Spec.Hard["count/configmaps"]
					Expect(value).To(Equal(fmt.Sprintf("count/configmaps: %d, count/secrets: %d", cmQ.Value(), secQ.Value())))
				case "qi-project-sa":
					// highest quota increase for serviceaccounts
					q := qi.Spec.Hard["count/serviceaccounts"]
					Expect(value).To(Equal(fmt.Sprintf("count/serviceaccounts: %d", q.Value())))
				default:
					Expect(value).To(BeEmpty())
				}
				Expect(qi.Labels).To(HaveKeyWithValue(quotav1alpha1.QuotaIncreaseOperationModeLabel, string(quotav1alpha1.MAXIMUM)))
			}
		})

		It("should not delete ineffective QuotaIncreases if deleteIneffectiveQuotas is false", func() {
			env := defaultTestSetup(quotav1alpha1.MAXIMUM, false, "testdata", "test-01")

			ns_project := &corev1.Namespace{}
			ns_project.SetName("ns-project")

			// count QuotaIncreases
			qis := &quotav1alpha1.QuotaIncreaseList{}
			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			qiCountOld := len(qis.Items)
			Expect(qiCountOld).To(Equal(4))
			Expect(qis.Items).To(withPointerizedSlice[quotav1alpha1.QuotaIncrease](ContainElements(haveName("qi-project-med"), haveName("qi-project-empty"))))

			env.ShouldReconcile(rec, testutils.RequestFromObject(ns_project))

			// for maximum mode, qi-project-med and qi-project-empty are considered ineffective
			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			Expect(qis.Items).To(HaveLen(qiCountOld))
			Expect(qis.Items).To(withPointerizedSlice[quotav1alpha1.QuotaIncrease](ContainElements(haveName("qi-project-med"), haveName("qi-project-empty"))))

			// check workspace namespace
			ns_workspace := &corev1.Namespace{}
			ns_workspace.SetName("ns-workspace")
			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_workspace.Name))).To(Succeed())
			Expect(qis.Items).ToNot(BeEmpty())
			qiCountOld = len(qis.Items)

			// reconcile workspace namespace with workspace quota controller
			env.ShouldReconcile(rec, testutils.RequestFromObject(ns_workspace))

			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_workspace.Name))).To(Succeed())
			Expect(qis.Items).To(HaveLen(qiCountOld))
		})

		It("should delete ineffective QuotaIncreases if deleteIneffectiveQuotas is true", func() {
			env := defaultTestSetup(quotav1alpha1.MAXIMUM, true, "testdata", "test-01")

			ns_project := &corev1.Namespace{}
			ns_project.SetName("ns-project")

			// count QuotaIncreases
			qis := &quotav1alpha1.QuotaIncreaseList{}
			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			qiCountOld := len(qis.Items)
			Expect(qiCountOld).To(Equal(4))
			Expect(qis.Items).To(withPointerizedSlice[quotav1alpha1.QuotaIncrease](ContainElements(haveName("qi-project-med"), haveName("qi-project-empty"))))

			env.ShouldReconcile(rec, testutils.RequestFromObject(ns_project))

			// for maximum mode, qi-project-med and qi-project-empty are considered ineffective
			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			qiCountNew := len(qis.Items)
			Expect(qiCountNew).To(Equal(qiCountOld - 2))
			Expect(qis.Items).ToNot(withPointerizedSlice[quotav1alpha1.QuotaIncrease](Or(ContainElement(haveName("qi-project-med")), ContainElement(haveName("qi-project-empty")))))

			// check workspace namespace
			ns_workspace := &corev1.Namespace{}
			ns_workspace.SetName("ns-workspace")
			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_workspace.Name))).To(Succeed())
			Expect(qis.Items).ToNot(BeEmpty())

			// reconcile workspace namespace with workspace quota controller
			env.ShouldReconcile(rec, testutils.RequestFromObject(ns_workspace))

			// the only QuotaIncrease in the workspace namespace has a lower quantity than the base quota and is therefore ineffective
			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_workspace.Name))).To(Succeed())
			Expect(qis.Items).To(BeEmpty())
		})

		It("should determine effectiveness among identical QuotaIncreases deterministically", func() {
			env := defaultTestSetup(quotav1alpha1.MAXIMUM, true, "testdata", "test-02")

			ns_normal := &corev1.Namespace{}
			ns_normal.SetName("ns-normal")

			// count QuotaIncreases
			qis := &quotav1alpha1.QuotaIncreaseList{}
			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_normal.Name))).To(Succeed())
			Expect(qis.Items).To(HaveLen(4))

			// reconcile normal namespace with all quota controller
			env.ShouldReconcile(rec, testutils.RequestFromObject(ns_normal))

			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_normal.Name))).To(Succeed())
			Expect(qis.Items).To(HaveLen(1))
			Expect(qis.Items[0].Name).To(Equal("qi-normal-alpha"))
		})

	})

	Context(fmt.Sprintf("Operating Mode: %s", quotav1alpha1.SINGULAR), func() {

		It("should add the operating mode label to the namespace", func() {
			env := defaultTestSetup(quotav1alpha1.SINGULAR, false, "testdata", "test-01")

			ns_project := &corev1.Namespace{}
			ns_project.SetName("ns-project")
			Expect(env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(ns_project), ns_project)).To(Succeed())
			Expect(openmcpctrlutil.HasLabel(ns_project, quotav1alpha1.QuotaIncreaseOperationModeLabel)).To(BeFalse())
			env.ShouldReconcile(rec, testutils.RequestFromObject(ns_project))
			Expect(env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(ns_project), ns_project)).To(Succeed())
			Expect(ns_project.Labels).To(HaveKeyWithValue(quotav1alpha1.QuotaIncreaseOperationModeLabel, string(quotav1alpha1.SINGULAR)))
		})

		It("should apply quota increases correctly", func() {
			env := defaultTestSetup(quotav1alpha1.SINGULAR, false, "testdata", "test-01")

			// verify test setup
			ns_project := &corev1.Namespace{}
			ns_project.SetName("ns-project")
			Expect(env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(ns_project), ns_project)).To(Succeed())
			Expect(openmcpctrlutil.HasLabel(ns_project, quotav1alpha1.BaseQuotaLabel)).To(BeFalse())
			rq_project := &corev1.ResourceQuota{}
			rq_project.SetName("project")
			rq_project.SetNamespace(ns_project.Name)
			err := env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(rq_project), rq_project)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			env.ShouldReconcile(rec, testutils.RequestFromObject(ns_project))
			// namespace should have been labeled
			Expect(env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(ns_project), ns_project)).To(Succeed())
			Expect(ns_project.Labels).To(HaveKeyWithValue(quotav1alpha1.BaseQuotaLabel, "project"))
			// ResourceQuota should have been created
			Expect(env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(rq_project), rq_project)).To(Succeed())
			// ResourceQuota should have owner reference pointing to namespace
			Expect(rq_project.OwnerReferences).To(ConsistOf(MatchFields(IgnoreExtras, Fields{
				"Kind": Equal("Namespace"),
				"Name": Equal(ns_project.Name),
			})))

			// should just use default quota, if use label is missing on namespace
			cfg := &quotav1alpha1.QuotaServiceConfig{}
			cfg.SetName(providerName)
			Expect(env.Client(platformCluster).Get(env.Ctx, client.ObjectKeyFromObject(cfg), cfg)).To(Succeed())
			qd := cfg.Spec.GetQuotaDefinitionForName("project")
			Expect(qd).ToNot(BeNil())
			Expect(rq_project.Spec).To(Equal(qd.ResourceQuotaTemplate.Spec))

			// list all QuotaIncreases in project namespace to verify empty effect annotation
			qis := &quotav1alpha1.QuotaIncreaseList{}
			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			for _, qi := range qis.Items {
				Expect(qi.Annotations).To(HaveKeyWithValue(quotav1alpha1.EffectAnnotation, ""))
			}

			// add use label to namespace
			Expect(openmcpctrlutil.EnsureLabel(env.Ctx, env.Client(onboardingCluster), ns_project, quotav1alpha1.SingularQuotaIncreaseLabel, "qi-project-sa", true)).To(Succeed())
			env.ShouldReconcile(rec, testutils.RequestFromObject(ns_project))

			// verify that QuotaIncrease was applied correctly
			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			for _, qi := range qis.Items {
				if qi.Name == "qi-project-sa" {
					expectedPrefix := quotav1alpha1.ActiveSingularQuotaIncreaseEffectPrefix
					if len(expectedPrefix) > 0 {
						expectedPrefix += " "
					}
					Expect(qi.Annotations).To(HaveKeyWithValue(quotav1alpha1.EffectAnnotation, fmt.Sprintf("%scount/secrets: 10, count/serviceaccounts: 5", expectedPrefix)))
				} else {
					Expect(qi.Annotations).To(HaveKeyWithValue(quotav1alpha1.EffectAnnotation, ""))
				}
				Expect(qi.Labels).To(HaveKeyWithValue(quotav1alpha1.QuotaIncreaseOperationModeLabel, string(quotav1alpha1.SINGULAR)))
			}
			Expect(env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(rq_project), rq_project)).To(Succeed())
			Expect(rq_project.Spec.Hard["count/secrets"]).To(matchNumericQuantity(10))
			Expect(rq_project.Spec.Hard["count/serviceaccounts"]).To(matchNumericQuantity(5))

			// verify that the referenced QuotaIncrease does not reduce the base quota
			ns_workspace := &corev1.Namespace{}
			ns_workspace.SetName("ns-workspace")
			env.ShouldReconcile(rec, testutils.RequestFromObject(ns_workspace))
			rq_workspace := &corev1.ResourceQuota{}
			rq_workspace.SetName("workspace")
			rq_workspace.SetNamespace(ns_workspace.Name)
			Expect(env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(rq_workspace), rq_workspace)).To(Succeed())
			old := rq_workspace.DeepCopy()
			Expect(env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(ns_workspace), ns_workspace)).To(Succeed())
			Expect(openmcpctrlutil.EnsureLabel(env.Ctx, env.Client(onboardingCluster), ns_workspace, quotav1alpha1.SingularQuotaIncreaseLabel, "qi-workspace-min", true)).To(Succeed())
			env.ShouldReconcile(rec, testutils.RequestFromObject(ns_workspace))
			Expect(env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(rq_workspace), rq_workspace)).To(Succeed())
			Expect(rq_workspace.Spec).To(Equal(old.Spec))
			qi_workspace_min := &quotav1alpha1.QuotaIncrease{}
			qi_workspace_min.SetName("qi-workspace-min")
			qi_workspace_min.SetNamespace(ns_workspace.Name)
			Expect(env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(qi_workspace_min), qi_workspace_min)).To(Succeed())
			Expect(qi_workspace_min.Annotations).To(HaveKeyWithValue(quotav1alpha1.EffectAnnotation, quotav1alpha1.ActiveSingularQuotaIncreaseEffectPrefix))
		})

		It("should not delete ineffective QuotaIncreases if deleteIneffectiveQuotas is false", func() {
			env := defaultTestSetup(quotav1alpha1.SINGULAR, false, "testdata", "test-01")

			ns_project := &corev1.Namespace{}
			ns_project.SetName("ns-project")

			// count QuotaIncreases
			qis := &quotav1alpha1.QuotaIncreaseList{}
			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			qiCountOld := len(qis.Items)
			Expect(qiCountOld).To(Equal(4))

			env.ShouldReconcile(rec, testutils.RequestFromObject(ns_project))

			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			Expect(qis.Items).To(HaveLen(qiCountOld))
		})

		It("should delete all QuotaIncreases if deleteIneffectiveQuotas is true and no use label is set", func() {
			env := defaultTestSetup(quotav1alpha1.SINGULAR, true, "testdata", "test-01")

			ns_project := &corev1.Namespace{}
			ns_project.SetName("ns-project")

			// count QuotaIncreases
			qis := &quotav1alpha1.QuotaIncreaseList{}
			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			qiCountOld := len(qis.Items)
			Expect(qiCountOld).To(Equal(4))

			env.ShouldReconcile(rec, testutils.RequestFromObject(ns_project))

			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			Expect(qis.Items).To(BeEmpty())
		})

		It("should delete all other QuotaIncreases if deleteIneffectiveQuotas is true and a use label is set", func() {
			env := defaultTestSetup(quotav1alpha1.SINGULAR, true, "testdata", "test-01")

			ns_project := &corev1.Namespace{}
			ns_project.SetName("ns-project")
			Expect(env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(ns_project), ns_project)).To(Succeed())
			Expect(openmcpctrlutil.EnsureLabel(env.Ctx, env.Client(onboardingCluster), ns_project, quotav1alpha1.SingularQuotaIncreaseLabel, "qi-project-sa", true)).To(Succeed())

			// count QuotaIncreases
			qis := &quotav1alpha1.QuotaIncreaseList{}
			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			qiCountOld := len(qis.Items)
			Expect(qiCountOld).To(Equal(4))

			env.ShouldReconcile(rec, testutils.RequestFromObject(ns_project))

			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			Expect(qis.Items).To(HaveLen(1))
			Expect(qis.Items[0].Name).To(Equal("qi-project-sa"))
		})

		It("should never delete the referenced QuotaIncrease, even if it is ineffective", func() {
			env := defaultTestSetup(quotav1alpha1.SINGULAR, true, "testdata", "test-01")

			ns_workspace := &corev1.Namespace{}
			ns_workspace.SetName("ns-workspace")
			Expect(env.Client(onboardingCluster).Get(env.Ctx, client.ObjectKeyFromObject(ns_workspace), ns_workspace)).To(Succeed())
			Expect(openmcpctrlutil.EnsureLabel(env.Ctx, env.Client(onboardingCluster), ns_workspace, quotav1alpha1.SingularQuotaIncreaseLabel, "qi-workspace-min", true)).To(Succeed())

			env.ShouldReconcile(rec, testutils.RequestFromObject(ns_workspace))

			// count QuotaIncreases
			qis := &quotav1alpha1.QuotaIncreaseList{}
			Expect(env.Client(onboardingCluster).List(env.Ctx, qis, client.InNamespace(ns_workspace.Name))).To(Succeed())
			Expect(qis.Items).To(HaveLen(1))
			Expect(qis.Items[0].Name).To(Equal("qi-workspace-min"))
		})
	})
})
