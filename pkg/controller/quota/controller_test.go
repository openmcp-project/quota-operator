package quota_test

import (
	"fmt"
	"path"
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
	quotacontroller "github.com/openmcp-project/quota-operator/pkg/controller/quota"
	"github.com/openmcp-project/quota-operator/pkg/controller/quota/config"
)

func TestConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Quota Controller Test Suite")
}

const (
	clusterKey = "cluster"
)

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

func defaultTestSetup(mode config.QuotaIncreaseOperatingMode, deleteIneffectiveQuotas bool, testDataPathSegments ...string) *testutils.ComplexEnvironment {
	cfg, err := config.LoadConfig(path.Join(path.Join(testDataPathSegments...), "config"))
	Expect(err).ToNot(HaveOccurred())
	activeQuotaDefinitions := cfg.GetActiveQuotaDefinitions()
	sc := runtime.NewScheme()
	quotainstall.Install(sc)
	builder := testutils.NewComplexEnvironmentBuilder().WithInitObjectPath(clusterKey, testDataPathSegments...).WithFakeClient(clusterKey, sc)
	for _, qd := range cfg.Quotas {
		qd.Mode = mode
		qd.DeleteIneffectiveQuotas = deleteIneffectiveQuotas
		builder.WithReconcilerConstructor(qd.Name, func(c ...client.Client) reconcile.Reconciler {
			return quotacontroller.NewQuotaController(c[0], qd, activeQuotaDefinitions)
		}, clusterKey)
	}
	return builder.Build()
}

var _ = Describe("CO-1155 QuotaIncrease Controller", func() {
	Context("Independent of Operating Mode", func() {

		It("should ignore namespaces with a base quota label belonging to another active QuotaDefinition", func() {
			env := defaultTestSetup(config.CUMULATIVE, false, "testdata", "test-01")

			ns_project := &corev1.Namespace{}
			ns_project.SetName("ns-project")
			Expect(env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(ns_project), ns_project)).To(Succeed())
			Expect(openmcpctrlutil.EnsureLabel(env.Ctx, env.Client(clusterKey), ns_project, quotav1alpha1.BaseQuotaLabel, "project", true)).To(Succeed())

			// verify that no ResourceQuotas exist
			rql := &corev1.ResourceQuotaList{}
			Expect(env.Client(clusterKey).List(env.Ctx, rql, client.InNamespace(ns_project.Name))).To(Succeed())
			Expect(rql.Items).To(BeEmpty())

			// reconcile project namespace with 'all' quota controller
			env.ShouldReconcile("all", testutils.RequestFromObject(ns_project))

			// verify that no ResourceQuota was created
			Expect(env.Client(clusterKey).List(env.Ctx, rql, client.InNamespace(ns_project.Name))).To(Succeed())
			Expect(rql.Items).To(BeEmpty())
		})

		It("should overwrite base quota labels belonging to unknown QuotaDefinitions", func() {
			env := defaultTestSetup(config.CUMULATIVE, false, "testdata", "test-01")

			ns_project := &corev1.Namespace{}
			ns_project.SetName("ns-project")
			Expect(env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(ns_project), ns_project)).To(Succeed())
			Expect(openmcpctrlutil.EnsureLabel(env.Ctx, env.Client(clusterKey), ns_project, quotav1alpha1.BaseQuotaLabel, "unknown", true)).To(Succeed())

			// verify that no ResourceQuotas exist
			rql := &corev1.ResourceQuotaList{}
			Expect(env.Client(clusterKey).List(env.Ctx, rql, client.InNamespace(ns_project.Name))).To(Succeed())
			Expect(rql.Items).To(BeEmpty())

			// reconcile project namespace with 'all' quota controller
			env.ShouldReconcile("all", testutils.RequestFromObject(ns_project))

			// verify that ResourceQuota was created
			Expect(env.Client(clusterKey).List(env.Ctx, rql, client.InNamespace(ns_project.Name))).To(Succeed())
			Expect(rql.Items).To(HaveLen(1))
			Expect(rql.Items[0].Name).To(Equal("all"))

			// verify that base quota label on namespace was overwritten
			Expect(env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(ns_project), ns_project)).To(Succeed())
			Expect(ns_project.Labels).To(HaveKeyWithValue(quotav1alpha1.BaseQuotaLabel, "all"))
		})

	})

	Context(fmt.Sprintf("Operating Mode: %s", config.CUMULATIVE), func() {

		It("should add the operating mode label to the namespace", func() {
			env := defaultTestSetup(config.CUMULATIVE, false, "testdata", "test-01")

			ns_project := &corev1.Namespace{}
			ns_project.SetName("ns-project")
			Expect(env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(ns_project), ns_project)).To(Succeed())
			Expect(openmcpctrlutil.HasLabel(ns_project, quotav1alpha1.QuotaIncreaseOperationModeLabel)).To(BeFalse())
			env.ShouldReconcile("project", testutils.RequestFromObject(ns_project))
			Expect(env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(ns_project), ns_project)).To(Succeed())
			Expect(ns_project.Labels).To(HaveKeyWithValue(quotav1alpha1.QuotaIncreaseOperationModeLabel, string(config.CUMULATIVE)))
		})

		It("should apply quota increases correctly", func() {
			env := defaultTestSetup(config.CUMULATIVE, false, "testdata", "test-01")

			// verify test setup
			ns_project := &corev1.Namespace{}
			ns_project.SetName("ns-project")
			Expect(env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(ns_project), ns_project)).To(Succeed())
			Expect(openmcpctrlutil.HasLabel(ns_project, quotav1alpha1.BaseQuotaLabel)).To(BeFalse())
			rq_project := &corev1.ResourceQuota{}
			rq_project.SetName("project")
			rq_project.SetNamespace(ns_project.Name)
			err := env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(rq_project), rq_project)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			// reconcile project namespace with project quota controller
			env.ShouldReconcile("project", testutils.RequestFromObject(ns_project))
			// namespace should have been labeled
			Expect(env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(ns_project), ns_project)).To(Succeed())
			Expect(ns_project.Labels).To(HaveKeyWithValue(quotav1alpha1.BaseQuotaLabel, "project"))
			// ResourceQuota should have been created
			Expect(env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(rq_project), rq_project)).To(Succeed())
			// ResourceQuota should have owner reference pointing to namespace
			Expect(rq_project.OwnerReferences).To(ConsistOf(MatchFields(IgnoreExtras, Fields{
				"Kind": Equal("Namespace"),
				"Name": Equal(ns_project.Name),
			})))

			// list all QuotaIncreases in project namespace to determine expected secret quantity
			rawQ := env.Reconciler("project").(*quotacontroller.QuotaController).Config.ResourceQuotaTemplate.Spec.Hard["count/secrets"]
			secretQ := rawQ.Value()
			var cmQ int64 = 0
			qis := &quotav1alpha1.QuotaIncreaseList{}
			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			for _, qi := range qis.Items {
				rawQ := qi.Spec.Hard["count/secrets"]
				secretQ += rawQ.Value()
				rawQ, ok := qi.Spec.Hard["count/configmaps"]
				if ok {
					cmQ += rawQ.Value()
				}
			}
			Expect(rq_project.Spec.Hard["count/secrets"]).To(matchNumericQuantity(secretQ))
			Expect(rq_project.Spec.Hard["count/configmaps"]).To(matchNumericQuantity(cmQ))

			// QuotaIncreases should have been annotated and labeled
			for _, qi := range qis.Items {
				value, ok := openmcpctrlutil.GetAnnotation(&qi, quotav1alpha1.EffectAnnotation)
				Expect(ok).To(BeTrue())
				for res, q := range qi.Spec.Hard {
					Expect(value).To(ContainSubstring(fmt.Sprintf("%s: %s", res, q.String())))
				}
				Expect(qi.Labels).To(HaveKeyWithValue(quotav1alpha1.QuotaIncreaseOperationModeLabel, string(config.CUMULATIVE)))
			}
		})

		It("should not delete ineffective QuotaIncreases if deleteIneffectiveQuotas is false", func() {
			env := defaultTestSetup(config.CUMULATIVE, false, "testdata", "test-01")

			ns_project := &corev1.Namespace{}
			ns_project.SetName("ns-project")

			// count QuotaIncreases
			qis := &quotav1alpha1.QuotaIncreaseList{}
			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			qiCountOld := len(qis.Items)
			Expect(qiCountOld).To(BeNumerically(">=", 3))
			Expect(qis.Items).To(withPointerizedSlice[quotav1alpha1.QuotaIncrease](ContainElement(haveName("qi-project-empty"))))

			// reconcile project namespace with project quota controller
			env.ShouldReconcile("project", testutils.RequestFromObject(ns_project))

			// for cumulative mode, only empty QuotaIncreases are considered ineffective
			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			Expect(qis.Items).To(HaveLen(qiCountOld))
			Expect(qis.Items).To(withPointerizedSlice[quotav1alpha1.QuotaIncrease](ContainElement(haveName("qi-project-empty"))))

			// check workspace namespace
			ns_workspace := &corev1.Namespace{}
			ns_workspace.SetName("ns-workspace")
			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			Expect(qis.Items).ToNot(BeEmpty())
			qiCountOld = len(qis.Items)

			// reconcile workspace namespace with workspace quota controller
			env.ShouldReconcile("workspace", testutils.RequestFromObject(ns_workspace))

			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			Expect(qis.Items).To(HaveLen(qiCountOld))
		})

		It("should delete ineffective QuotaIncreases if deleteIneffectiveQuotas is true", func() {
			env := defaultTestSetup(config.CUMULATIVE, true, "testdata", "test-01")

			ns_project := &corev1.Namespace{}
			ns_project.SetName("ns-project")

			// count QuotaIncreases
			qis := &quotav1alpha1.QuotaIncreaseList{}
			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			qiCountOld := len(qis.Items)
			Expect(qiCountOld).To(Equal(4))
			Expect(qis.Items).To(withPointerizedSlice[quotav1alpha1.QuotaIncrease](ContainElement(haveName("qi-project-empty"))))

			// reconcile project namespace with project quota controller
			env.ShouldReconcile("project", testutils.RequestFromObject(ns_project))

			// for cumulative mode, only empty QuotaIncreases are considered ineffective
			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			qiCountNew := len(qis.Items)
			Expect(qiCountNew).To(Equal(qiCountOld - 1))
			Expect(qis.Items).ToNot(withPointerizedSlice[quotav1alpha1.QuotaIncrease](ContainElement(haveName("qi-project-empty"))))

			// check workspace namespace
			ns_workspace := &corev1.Namespace{}
			ns_workspace.SetName("ns-workspace")
			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_workspace.Name))).To(Succeed())
			Expect(qis.Items).ToNot(BeEmpty())
			qiCountOld = len(qis.Items)

			// reconcile workspace namespace with workspace quota controller
			env.ShouldReconcile("workspace", testutils.RequestFromObject(ns_workspace))

			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_workspace.Name))).To(Succeed())
			qiCountNew = len(qis.Items)
			Expect(qiCountNew).To(Equal(qiCountOld))
		})

	})

	Context(fmt.Sprintf("Operating Mode: %s", config.MAXIMUM), func() {

		It("should add the operating mode label to the namespace", func() {
			env := defaultTestSetup(config.MAXIMUM, false, "testdata", "test-01")

			ns_project := &corev1.Namespace{}
			ns_project.SetName("ns-project")
			Expect(env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(ns_project), ns_project)).To(Succeed())
			Expect(openmcpctrlutil.HasLabel(ns_project, quotav1alpha1.QuotaIncreaseOperationModeLabel)).To(BeFalse())
			env.ShouldReconcile("project", testutils.RequestFromObject(ns_project))
			Expect(env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(ns_project), ns_project)).To(Succeed())
			Expect(ns_project.Labels).To(HaveKeyWithValue(quotav1alpha1.QuotaIncreaseOperationModeLabel, string(config.MAXIMUM)))
		})

		It("should apply quota increases correctly", func() {
			env := defaultTestSetup(config.MAXIMUM, false, "testdata", "test-01")

			// verify test setup
			ns_project := &corev1.Namespace{}
			ns_project.SetName("ns-project")
			Expect(env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(ns_project), ns_project)).To(Succeed())
			Expect(openmcpctrlutil.HasLabel(ns_project, quotav1alpha1.BaseQuotaLabel)).To(BeFalse())
			rq_project := &corev1.ResourceQuota{}
			rq_project.SetName("project")
			rq_project.SetNamespace(ns_project.Name)
			err := env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(rq_project), rq_project)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			// reconcile project namespace with project quota controller
			env.ShouldReconcile("project", testutils.RequestFromObject(ns_project))
			// namespace should have been labeled
			Expect(env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(ns_project), ns_project)).To(Succeed())
			Expect(ns_project.Labels).To(HaveKeyWithValue(quotav1alpha1.BaseQuotaLabel, "project"))
			// ResourceQuota should have been created
			Expect(env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(rq_project), rq_project)).To(Succeed())
			// ResourceQuota should have owner reference pointing to namespace
			Expect(rq_project.OwnerReferences).To(ConsistOf(MatchFields(IgnoreExtras, Fields{
				"Kind": Equal("Namespace"),
				"Name": Equal(ns_project.Name),
			})))

			// list all QuotaIncreases in project namespace to determine expected secret quantity
			secretQ := env.Reconciler("project").(*quotacontroller.QuotaController).Config.ResourceQuotaTemplate.Spec.Hard["count/secrets"].DeepCopy()
			cmQ := env.Reconciler("project").(*quotacontroller.QuotaController).Config.ResourceQuotaTemplate.Spec.Hard["count/configmaps"].DeepCopy()
			saQ := env.Reconciler("project").(*quotacontroller.QuotaController).Config.ResourceQuotaTemplate.Spec.Hard["count/serviceaccounts"].DeepCopy()
			qis := &quotav1alpha1.QuotaIncreaseList{}
			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
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
				Expect(qi.Labels).To(HaveKeyWithValue(quotav1alpha1.QuotaIncreaseOperationModeLabel, string(config.MAXIMUM)))
			}
		})

		It("should not delete ineffective QuotaIncreases if deleteIneffectiveQuotas is false", func() {
			env := defaultTestSetup(config.MAXIMUM, false, "testdata", "test-01")

			ns_project := &corev1.Namespace{}
			ns_project.SetName("ns-project")

			// count QuotaIncreases
			qis := &quotav1alpha1.QuotaIncreaseList{}
			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			qiCountOld := len(qis.Items)
			Expect(qiCountOld).To(Equal(4))
			Expect(qis.Items).To(withPointerizedSlice[quotav1alpha1.QuotaIncrease](ContainElements(haveName("qi-project-med"), haveName("qi-project-empty"))))

			// reconcile project namespace with project quota controller
			env.ShouldReconcile("project", testutils.RequestFromObject(ns_project))

			// for maximum mode, qi-project-med and qi-project-empty are considered ineffective
			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			Expect(qis.Items).To(HaveLen(qiCountOld))
			Expect(qis.Items).To(withPointerizedSlice[quotav1alpha1.QuotaIncrease](ContainElements(haveName("qi-project-med"), haveName("qi-project-empty"))))

			// check workspace namespace
			ns_workspace := &corev1.Namespace{}
			ns_workspace.SetName("ns-workspace")
			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_workspace.Name))).To(Succeed())
			Expect(qis.Items).ToNot(BeEmpty())
			qiCountOld = len(qis.Items)

			// reconcile workspace namespace with workspace quota controller
			env.ShouldReconcile("workspace", testutils.RequestFromObject(ns_workspace))

			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_workspace.Name))).To(Succeed())
			Expect(qis.Items).To(HaveLen(qiCountOld))
		})

		It("should delete ineffective QuotaIncreases if deleteIneffectiveQuotas is true", func() {
			env := defaultTestSetup(config.MAXIMUM, true, "testdata", "test-01")

			ns_project := &corev1.Namespace{}
			ns_project.SetName("ns-project")

			// count QuotaIncreases
			qis := &quotav1alpha1.QuotaIncreaseList{}
			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			qiCountOld := len(qis.Items)
			Expect(qiCountOld).To(Equal(4))
			Expect(qis.Items).To(withPointerizedSlice[quotav1alpha1.QuotaIncrease](ContainElements(haveName("qi-project-med"), haveName("qi-project-empty"))))

			// reconcile project namespace with project quota controller
			env.ShouldReconcile("project", testutils.RequestFromObject(ns_project))

			// for maximum mode, qi-project-med and qi-project-empty are considered ineffective
			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			qiCountNew := len(qis.Items)
			Expect(qiCountNew).To(Equal(qiCountOld - 2))
			Expect(qis.Items).ToNot(withPointerizedSlice[quotav1alpha1.QuotaIncrease](Or(ContainElement(haveName("qi-project-med")), ContainElement(haveName("qi-project-empty")))))

			// check workspace namespace
			ns_workspace := &corev1.Namespace{}
			ns_workspace.SetName("ns-workspace")
			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_workspace.Name))).To(Succeed())
			Expect(qis.Items).ToNot(BeEmpty())

			// reconcile workspace namespace with workspace quota controller
			env.ShouldReconcile("workspace", testutils.RequestFromObject(ns_workspace))

			// the only QuotaIncrease in the workspace namespace has a lower quantity than the base quota and is therefore ineffective
			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_workspace.Name))).To(Succeed())
			Expect(qis.Items).To(BeEmpty())
		})

		It("should determine effectiveness among identical QuotaIncreases deterministically", func() {
			env := defaultTestSetup(config.MAXIMUM, true, "testdata", "test-02")

			ns_normal := &corev1.Namespace{}
			ns_normal.SetName("ns-normal")

			// count QuotaIncreases
			qis := &quotav1alpha1.QuotaIncreaseList{}
			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_normal.Name))).To(Succeed())
			Expect(qis.Items).To(HaveLen(4))

			// reconcile normal namespace with all quota controller
			env.ShouldReconcile("all", testutils.RequestFromObject(ns_normal))

			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_normal.Name))).To(Succeed())
			Expect(qis.Items).To(HaveLen(1))
			Expect(qis.Items[0].Name).To(Equal("qi-normal-alpha"))
		})

	})

	Context(fmt.Sprintf("Operating Mode: %s", config.SINGULAR), func() {

		It("should add the operating mode label to the namespace", func() {
			env := defaultTestSetup(config.SINGULAR, false, "testdata", "test-01")

			ns_project := &corev1.Namespace{}
			ns_project.SetName("ns-project")
			Expect(env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(ns_project), ns_project)).To(Succeed())
			Expect(openmcpctrlutil.HasLabel(ns_project, quotav1alpha1.QuotaIncreaseOperationModeLabel)).To(BeFalse())
			env.ShouldReconcile("project", testutils.RequestFromObject(ns_project))
			Expect(env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(ns_project), ns_project)).To(Succeed())
			Expect(ns_project.Labels).To(HaveKeyWithValue(quotav1alpha1.QuotaIncreaseOperationModeLabel, string(config.SINGULAR)))
		})

		It("should apply quota increases correctly", func() {
			env := defaultTestSetup(config.SINGULAR, false, "testdata", "test-01")

			// verify test setup
			ns_project := &corev1.Namespace{}
			ns_project.SetName("ns-project")
			Expect(env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(ns_project), ns_project)).To(Succeed())
			Expect(openmcpctrlutil.HasLabel(ns_project, quotav1alpha1.BaseQuotaLabel)).To(BeFalse())
			rq_project := &corev1.ResourceQuota{}
			rq_project.SetName("project")
			rq_project.SetNamespace(ns_project.Name)
			err := env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(rq_project), rq_project)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			// reconcile project namespace with project quota controller
			env.ShouldReconcile("project", testutils.RequestFromObject(ns_project))
			// namespace should have been labeled
			Expect(env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(ns_project), ns_project)).To(Succeed())
			Expect(ns_project.Labels).To(HaveKeyWithValue(quotav1alpha1.BaseQuotaLabel, "project"))
			// ResourceQuota should have been created
			Expect(env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(rq_project), rq_project)).To(Succeed())
			// ResourceQuota should have owner reference pointing to namespace
			Expect(rq_project.OwnerReferences).To(ConsistOf(MatchFields(IgnoreExtras, Fields{
				"Kind": Equal("Namespace"),
				"Name": Equal(ns_project.Name),
			})))

			// should just use default quota, if use label is missing on namespace
			cfg := env.Reconciler("project").(*quotacontroller.QuotaController).Config
			Expect(rq_project.Spec).To(Equal(*cfg.ResourceQuotaTemplate.Spec))

			// list all QuotaIncreases in project namespace to verify empty effect annotation
			qis := &quotav1alpha1.QuotaIncreaseList{}
			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			for _, qi := range qis.Items {
				Expect(qi.Annotations).To(HaveKeyWithValue(quotav1alpha1.EffectAnnotation, ""))
			}

			// add use label to namespace
			Expect(openmcpctrlutil.EnsureLabel(env.Ctx, env.Client(clusterKey), ns_project, quotav1alpha1.SingularQuotaIncreaseLabel, "qi-project-sa", true)).To(Succeed())
			env.ShouldReconcile("project", testutils.RequestFromObject(ns_project))

			// verify that QuotaIncrease was applied correctly
			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
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
				Expect(qi.Labels).To(HaveKeyWithValue(quotav1alpha1.QuotaIncreaseOperationModeLabel, string(config.SINGULAR)))
			}
			Expect(env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(rq_project), rq_project)).To(Succeed())
			Expect(rq_project.Spec.Hard["count/secrets"]).To(matchNumericQuantity(10))
			Expect(rq_project.Spec.Hard["count/serviceaccounts"]).To(matchNumericQuantity(5))

			// verify that the referenced QuotaIncrease does not reduce the base quota
			ns_workspace := &corev1.Namespace{}
			ns_workspace.SetName("ns-workspace")
			env.ShouldReconcile("workspace", testutils.RequestFromObject(ns_workspace))
			rq_workspace := &corev1.ResourceQuota{}
			rq_workspace.SetName("workspace")
			rq_workspace.SetNamespace(ns_workspace.Name)
			Expect(env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(rq_workspace), rq_workspace)).To(Succeed())
			old := rq_workspace.DeepCopy()
			Expect(env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(ns_workspace), ns_workspace)).To(Succeed())
			Expect(openmcpctrlutil.EnsureLabel(env.Ctx, env.Client(clusterKey), ns_workspace, quotav1alpha1.SingularQuotaIncreaseLabel, "qi-workspace-min", true)).To(Succeed())
			env.ShouldReconcile("workspace", testutils.RequestFromObject(ns_workspace))
			Expect(env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(rq_workspace), rq_workspace)).To(Succeed())
			Expect(rq_workspace.Spec).To(Equal(old.Spec))
			qi_workspace_min := &quotav1alpha1.QuotaIncrease{}
			qi_workspace_min.SetName("qi-workspace-min")
			qi_workspace_min.SetNamespace(ns_workspace.Name)
			Expect(env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(qi_workspace_min), qi_workspace_min)).To(Succeed())
			Expect(qi_workspace_min.Annotations).To(HaveKeyWithValue(quotav1alpha1.EffectAnnotation, quotav1alpha1.ActiveSingularQuotaIncreaseEffectPrefix))
		})

		It("should not delete ineffective QuotaIncreases if deleteIneffectiveQuotas is false", func() {
			env := defaultTestSetup(config.SINGULAR, false, "testdata", "test-01")

			ns_project := &corev1.Namespace{}
			ns_project.SetName("ns-project")

			// count QuotaIncreases
			qis := &quotav1alpha1.QuotaIncreaseList{}
			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			qiCountOld := len(qis.Items)
			Expect(qiCountOld).To(Equal(4))

			// reconcile project namespace with project quota controller
			env.ShouldReconcile("project", testutils.RequestFromObject(ns_project))

			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			Expect(qis.Items).To(HaveLen(qiCountOld))
		})

		It("should delete all QuotaIncreases if deleteIneffectiveQuotas is true and no use label is set", func() {
			env := defaultTestSetup(config.SINGULAR, true, "testdata", "test-01")

			ns_project := &corev1.Namespace{}
			ns_project.SetName("ns-project")

			// count QuotaIncreases
			qis := &quotav1alpha1.QuotaIncreaseList{}
			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			qiCountOld := len(qis.Items)
			Expect(qiCountOld).To(Equal(4))

			// reconcile project namespace with project quota controller
			env.ShouldReconcile("project", testutils.RequestFromObject(ns_project))

			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			Expect(qis.Items).To(BeEmpty())
		})

		It("should delete all other QuotaIncreases if deleteIneffectiveQuotas is true and a use label is set", func() {
			env := defaultTestSetup(config.SINGULAR, true, "testdata", "test-01")

			ns_project := &corev1.Namespace{}
			ns_project.SetName("ns-project")
			Expect(env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(ns_project), ns_project)).To(Succeed())
			Expect(openmcpctrlutil.EnsureLabel(env.Ctx, env.Client(clusterKey), ns_project, quotav1alpha1.SingularQuotaIncreaseLabel, "qi-project-sa", true)).To(Succeed())

			// count QuotaIncreases
			qis := &quotav1alpha1.QuotaIncreaseList{}
			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			qiCountOld := len(qis.Items)
			Expect(qiCountOld).To(Equal(4))

			// reconcile project namespace with project quota controller
			env.ShouldReconcile("project", testutils.RequestFromObject(ns_project))

			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_project.Name))).To(Succeed())
			Expect(qis.Items).To(HaveLen(1))
			Expect(qis.Items[0].Name).To(Equal("qi-project-sa"))
		})

		It("should never delete the referenced QuotaIncrease, even if it is ineffective", func() {
			env := defaultTestSetup(config.SINGULAR, true, "testdata", "test-01")

			ns_workspace := &corev1.Namespace{}
			ns_workspace.SetName("ns-workspace")
			Expect(env.Client(clusterKey).Get(env.Ctx, client.ObjectKeyFromObject(ns_workspace), ns_workspace)).To(Succeed())
			Expect(openmcpctrlutil.EnsureLabel(env.Ctx, env.Client(clusterKey), ns_workspace, quotav1alpha1.SingularQuotaIncreaseLabel, "qi-workspace-min", true)).To(Succeed())

			// reconcile project namespace with project quota controller
			env.ShouldReconcile("project", testutils.RequestFromObject(ns_workspace))

			// count QuotaIncreases
			qis := &quotav1alpha1.QuotaIncreaseList{}
			Expect(env.Client(clusterKey).List(env.Ctx, qis, client.InNamespace(ns_workspace.Name))).To(Succeed())
			Expect(qis.Items).To(HaveLen(1))
			Expect(qis.Items[0].Name).To(Equal("qi-workspace-min"))
		})
	})
})
