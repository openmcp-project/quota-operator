package quota

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	"github.com/openmcp-project/controller-utils/pkg/logging"
	openapiconst "github.com/openmcp-project/openmcp-operator/api/constants"

	quotav1alpha1 "github.com/openmcp-project/quota-operator/api/v1alpha1"
)

const ControllerName = "quota"

// NewQuotaController creates a new QuotaController instance.
// The activeQuotaDefinitions set should contain the names of all QuotaDefinitions from all QuotaControllers running in the same cluster.
func NewQuotaController(platformCluster, onboardingCluster *clusters.Cluster, providerName string) *QuotaController {
	return &QuotaController{
		PlatformCluster:   platformCluster,
		OnboardingCluster: onboardingCluster,
		ProviderName:      providerName,
		cfgLock:           &sync.RWMutex{},
	}
}

// QuotaController actually reconciles namespaces, but it gets triggered by generation changes of
// - ResourceQuotas with an OwnerReference pointing to the namespace
// - QuotaIncreases in the namespace
type QuotaController struct {
	PlatformCluster   *clusters.Cluster
	OnboardingCluster *clusters.Cluster
	ProviderName      string
	Config            *quotav1alpha1.QuotaServiceConfig
	cfgLock           *sync.RWMutex
}

// Reconcile contains the main logic of creating and updating a ResourceQuota based on the QuotaIncreases in the reconciled Namespace.
// The Namespace is registered as controller of the ResourceQuota and reacts on changes to QuotaIncreases within the namespace (even without owner reference), so this gets triggered if either is modified.
func (r *QuotaController) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := logging.FromContextOrPanic(ctx).WithName(ControllerName)
	ctx = logging.NewContext(ctx, log)
	log.Debug("Reconcile triggered")

	// fetch and update internal config
	cfg := &quotav1alpha1.QuotaServiceConfig{}
	if err := r.PlatformCluster.Client().Get(ctx, types.NamespacedName{Name: r.ProviderName}, cfg); err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to fetch QuotaServiceConfig '%s': %w", r.ProviderName, err)
	}
	if err := cfg.Spec.Validate(); err != nil {
		return ctrl.Result{}, fmt.Errorf("invalid QuotaServiceConfig '%s': %w", r.ProviderName, err)
	}
	// if the config has a different generation than the known one, which indicates a spec change, update the internal config
	r.cfgLock.RLock()
	knownGeneration := int64(-1)
	if r.Config != nil {
		knownGeneration = r.Config.Generation
	}
	r.cfgLock.RUnlock()
	if cfg.Generation != knownGeneration {
		log.Info("Detected change in QuotaServiceConfig, updating internal config", "oldGeneration", knownGeneration, "newGeneration", cfg.Generation)
		r.cfgLock.Lock()
		r.Config = cfg
		r.cfgLock.Unlock()
	}

	// fetch Namespace
	ns := &corev1.Namespace{}
	if err := r.OnboardingCluster.Client().Get(ctx, req.NamespacedName, ns); err != nil {
		if apierrors.IsNotFound(err) {
			log.Debug("Namespace not found")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("unable to fetch Namespace: %w", err)
	}

	// identify responsible quota definition
	var qdef *quotav1alpha1.QuotaDefinition
	r.cfgLock.RLock()
	for _, qd := range r.Config.Spec.Quotas {
		if qd.Selector == nil {
			qdef = qd.DeepCopy()
			break
		} else {
			sel, err := metav1.LabelSelectorAsSelector(qd.Selector)
			if err != nil {
				r.cfgLock.RUnlock()
				return ctrl.Result{}, fmt.Errorf("error converting label selector for quota definition '%s': %w", qd.Name, err)
			}
			if sel.Matches(labels.Set(ns.Labels)) {
				qdef = qd.DeepCopy()
				break
			}
		}
	}
	r.cfgLock.RUnlock()
	if qdef == nil {
		log.Debug("No matching quota definition found for namespace, skipping reconciliation")
		return ctrl.Result{}, nil
	} else {
		log = log.WithName(qdef.Name).WithValues("quotaDefinition", qdef.Name)
		ctx = logging.NewContext(ctx, log)
		log.Debug("Found matching quota definition for namespace")
	}

	if !ns.DeletionTimestamp.IsZero() {
		log.Debug("Namespace is being deleted, no action required")
		return ctrl.Result{}, nil
	}

	log.Info("Starting actual reconciliation logic")

	// evaluate quota-managed-by label on namespace
	quotaManagedBy, ok := ctrlutils.GetLabel(ns, quotav1alpha1.ManagedByLabel)
	if ok && quotaManagedBy != r.ProviderName {
		log.Info("Namespace is managed by another instance of this platform service, skipping reconciliation", "providerName", quotaManagedBy)
		return ctrl.Result{}, nil
	}

	// ensure labels on namespace
	old := ns.DeepCopy()
	if err := ctrlutils.EnsureLabel(ctx, nil, ns, quotav1alpha1.ManagedByLabel, r.ProviderName, false); err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to set managed-by label '%s' to value '%s' on namespace: %w", quotav1alpha1.ManagedByLabel, r.ProviderName, err)
	}
	if err := ctrlutils.EnsureLabel(ctx, nil, ns, quotav1alpha1.BaseQuotaLabel, qdef.Name, false, ctrlutils.OVERWRITE); err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to set base quota label '%s' to value '%s' on namespace: %w", quotav1alpha1.BaseQuotaLabel, qdef.Name, err)
	}
	if err := ctrlutils.EnsureLabel(ctx, nil, ns, quotav1alpha1.QuotaIncreaseOperationModeLabel, string(qdef.Mode), false, ctrlutils.OVERWRITE); err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to set operation mode label '%s' to value '%s' on namespace: %w", quotav1alpha1.QuotaIncreaseOperationModeLabel, qdef.Mode, err)
	}
	if !maps.Equal(old.Labels, ns.Labels) {
		if err := r.OnboardingCluster.Client().Patch(ctx, ns, client.MergeFrom(old)); err != nil {
			return ctrl.Result{}, fmt.Errorf("error patching labels on namespace: %w", err)
		}
		log.Info("Updated labels on namespace", "oldLabels", old.Labels, "newLabels", ns.Labels)
	}

	// list all QuotaIncreases in namespace
	qis := &quotav1alpha1.QuotaIncreaseList{}
	if err := r.OnboardingCluster.Client().List(ctx, qis, client.InNamespace(ns.Name)); err != nil {
		return ctrl.Result{}, fmt.Errorf("error listing QuotaIncreases: %w", err)
	}

	// create/update ResourceQuota
	_, effects, err := r.createOrUpdateResourceQuota(ctx, ns, qdef, qis)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error creating/updating ResourceQuota: %w", err)
	}

	// ensure QuotaIncrease integrity
	if err := r.evaluateEffectiveness(ctx, ns, qdef, qis, effects); err != nil {
		return ctrl.Result{}, fmt.Errorf("error evaluating QuotaIncrease effectiveness: %w", err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *QuotaController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}, builder.WithPredicates(
			// Only reconcile namespaces that
			// 1. do not have the openmcp or quota 'ignore' annotation
			// 2. either have no managed-by label from the quota controller at all or have one whose value matches the provider name of this controller
			predicate.And(
				predicate.Not(
					predicate.Or(
						ctrlutils.HasAnnotationPredicate(openapiconst.OperationAnnotation, openapiconst.OperationAnnotationValueIgnore),
						ctrlutils.HasAnnotationPredicate(quotav1alpha1.QuotaOperationLabel, openapiconst.OperationAnnotationValueIgnore),
					),
				),
				predicate.Not(
					predicate.And(
						ctrlutils.HasLabelPredicate(quotav1alpha1.ManagedByLabel, ""),
						predicate.Not(
							ctrlutils.HasLabelPredicate(quotav1alpha1.ManagedByLabel, r.ProviderName),
						),
					),
				),
			),
		)).
		Owns(&corev1.ResourceQuota{}, builder.WithPredicates(
			predicate.And(
				predicate.GenerationChangedPredicate{},
				predicate.Not(
					predicate.And(
						ctrlutils.HasLabelPredicate(quotav1alpha1.ManagedByLabel, ""),
						predicate.Not(
							ctrlutils.HasLabelPredicate(quotav1alpha1.ManagedByLabel, r.ProviderName),
						),
					),
				),
			),
		)).
		Watches(&quotav1alpha1.QuotaIncrease{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
			return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: o.GetNamespace()}}}
		}), builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		WatchesRawSource(source.Kind(r.PlatformCluster.Cluster().GetCache(), &quotav1alpha1.QuotaServiceConfig{}, handler.TypedEnqueueRequestsFromMapFunc(func(ctx context.Context, cfg *quotav1alpha1.QuotaServiceConfig) []reconcile.Request {
			// simply reconcile all namespaces
			// We could optimize this by first fetching the changed config and then listing only those namespace which match a selector,
			// but this would cause duplicate code and significantly complicate this logic,
			// and since the main reconciliation loop anyway ignores namespaces that don't match a selector, the effect would be negligible.
			nsList := &corev1.NamespaceList{}
			if err := r.OnboardingCluster.Client().List(ctx, nsList); err != nil {
				logging.FromContextOrDiscard(ctx).Error(err, "Error listing namespaces for QuotaServiceConfig change")
				return nil
			}
			reqs := make([]reconcile.Request, len(nsList.Items))
			for i, ns := range nsList.Items {
				reqs[i] = reconcile.Request{NamespacedName: types.NamespacedName{Name: ns.Name}}
			}
			return reqs
		}), ctrlutils.ToTypedPredicate[*quotav1alpha1.QuotaServiceConfig](ctrlutils.ExactNamePredicate(r.ProviderName, "")), predicate.TypedGenerationChangedPredicate[*quotav1alpha1.QuotaServiceConfig]{})).
		Complete(r)
}

func (r *QuotaController) createOrUpdateResourceQuota(ctx context.Context, namespace *corev1.Namespace, qdef *quotav1alpha1.QuotaDefinition, qis *quotav1alpha1.QuotaIncreaseList) (*corev1.ResourceQuota, map[string]corev1.ResourceList, error) {
	log := logging.FromContextOrPanic(ctx)

	computedRq, effects := r.computeResourceQuota(ctx, namespace, qdef, qis)

	rq := &corev1.ResourceQuota{}
	rq.SetName(computedRq.Name)
	rq.SetNamespace(computedRq.Namespace)
	log.Info("Creating/Updating ResourceQuota", "resourceQuota", rq.Name)
	_, err := controllerutil.CreateOrUpdate(ctx, r.OnboardingCluster.Client(), rq, func() error {
		rq.Annotations = computedRq.Annotations
		rq.Labels = computedRq.Labels
		rq.Spec = computedRq.Spec

		return controllerutil.SetControllerReference(namespace, rq, r.OnboardingCluster.Scheme())
	})
	if err != nil {
		return nil, nil, err
	}
	return rq, effects, nil
}

// computeResourceQuota takes the base ResourceQuota from the config and returns it with the quotas adapted based on the QuotaIncreases in the namespace, respecting the configured mode.
func (r *QuotaController) computeResourceQuota(ctx context.Context, namespace *corev1.Namespace, qdef *quotav1alpha1.QuotaDefinition, qis *quotav1alpha1.QuotaIncreaseList) (*corev1.ResourceQuota, map[string]corev1.ResourceList) {
	log := logging.FromContextOrPanic(ctx)

	rq := qdef.BaseResourceQuota()
	rq.SetNamespace(namespace.Name)
	if rq.Labels == nil {
		rq.Labels = map[string]string{}
	}
	rq.Labels[quotav1alpha1.ManagedByLabel] = r.ProviderName
	rq.Labels[quotav1alpha1.QuotaDefinitionLabel] = qdef.Name

	effects := map[string]corev1.ResourceList{}

	switch qdef.Mode {
	case quotav1alpha1.SINGULAR:
		qiName, ok := ctrlutils.GetLabel(namespace, quotav1alpha1.SingularQuotaIncreaseLabel)
		if !ok {
			log.Info("No singular QuotaIncrease label found on namespace, ignoring QuotaIncreases", "label", quotav1alpha1.SingularQuotaIncreaseLabel)
			return rq, effects
		}
		for _, qi := range qis.Items {
			if qi.Name == qiName {
				effects[qi.Name] = corev1.ResourceList{}
				for name, quantity := range qi.Spec.Hard {
					if quantity.Cmp(rq.Spec.Hard[name]) > 0 {
						rq.Spec.Hard[name] = quantity
						effects[qi.Name][name] = quantity
					}
				}
				return rq, effects
			}
		}
		log.Info("Referenced QuotaIncrease not found in namespace", "label", quotav1alpha1.SingularQuotaIncreaseLabel, "QuotaIncrease", qiName)
	case quotav1alpha1.CUMULATIVE:
		for _, qi := range qis.Items {
			effects[qi.Name] = corev1.ResourceList{}
			for name, quantity := range qi.Spec.Hard {
				old, ok := rq.Spec.Hard[name]
				effects[qi.Name][name] = quantity
				if !ok {
					rq.Spec.Hard[name] = quantity
				} else {
					old.Add(quantity)
					rq.Spec.Hard[name] = old
				}
			}
		}
	case quotav1alpha1.MAXIMUM:
		maxQuotas := computeMaxQuotaMapping(rq.Spec.Hard, qis)
		for resource, qi := range maxQuotas {
			if _, ok := effects[qi.Name]; !ok {
				effects[qi.Name] = corev1.ResourceList{}
			}
			effects[qi.Name][resource] = qi.Spec.Hard[resource]
			rq.Spec.Hard[resource] = qi.Spec.Hard[resource]
		}
	}
	return rq, effects
}

// computeMaxQuotaMapping maps resources to the quota increases which provide the highest quantity for these resources, respectively.
// Note that resources for which the base definition already contains the highest quantity are not included in the mapping.
func computeMaxQuotaMapping(base corev1.ResourceList, qis *quotav1alpha1.QuotaIncreaseList) map[corev1.ResourceName]*quotav1alpha1.QuotaIncrease {
	maxQuotas := map[corev1.ResourceName]*quotav1alpha1.QuotaIncrease{}
	for _, qi := range qis.Items {
		for resource, quantity := range qi.Spec.Hard {
			maxQ, ok := maxQuotas[resource]
			if (!ok || quantity.Cmp(maxQ.Spec.Hard[resource]) > 0) && quantity.Cmp(base[resource]) > 0 {
				// quantity for current resource is higher than the default and higher than the highest quantity seen so far
				maxQuotas[resource] = &qi
			}
		}
	}
	return maxQuotas
}

// effectAsString returns a string representation of the given resource-to-quota mapping.
// The resources are listed in alphabetical order to ensure a deterministic output.
func effectAsString(data corev1.ResourceList) string {
	sb := strings.Builder{}
	keys := sets.List(sets.KeySet(data))
	for _, resource := range keys {
		quantity := data[resource]
		fmt.Fprintf(&sb, "%s: %s, ", resource.String(), quantity.String())
	}
	res := sb.String()
	if len(res) > 0 {
		res = strings.TrimSuffix(res, ", ")
	}
	return res
}

// evaluateEffectiveness is responsible for setting the effect annotation on all QuotaIncrease resources.
// If deletion of ineffective QuotaIncreases is enabled, it will also delete QuotaIncreases that are no longer effective.
func (r *QuotaController) evaluateEffectiveness(ctx context.Context, namespace *corev1.Namespace, qdef *quotav1alpha1.QuotaDefinition, qis *quotav1alpha1.QuotaIncreaseList, effects map[string]corev1.ResourceList) error {
	log := logging.FromContextOrPanic(ctx)

	singularQIName := ""
	prefix := ""
	if qdef.Mode == quotav1alpha1.SINGULAR {
		singularQIName, _ = ctrlutils.GetLabel(namespace, quotav1alpha1.SingularQuotaIncreaseLabel)
		prefix = quotav1alpha1.ActiveSingularQuotaIncreaseEffectPrefix
	}

	var errs error
	for _, qi := range qis.Items {
		effect := effects[qi.Name]
		if !qdef.DeleteIneffectiveQuotas || len(effect) > 0 {
			// patch effect annotation on QuotaIncrease
			effectString := effectAsString(effect)
			if qdef.Mode == quotav1alpha1.SINGULAR && qi.Name == singularQIName {
				if effectString == "" {
					effectString = prefix
				} else {
					effectString = fmt.Sprintf("%s %s", prefix, effectString)
				}
			}
			errs = errors.Join(errs, ctrlutils.EnsureAnnotation(ctx, r.OnboardingCluster.Client(), &qi, quotav1alpha1.EffectAnnotation, effectString, true, ctrlutils.OVERWRITE))
			errs = errors.Join(errs, ctrlutils.EnsureLabel(ctx, r.OnboardingCluster.Client(), &qi, quotav1alpha1.QuotaIncreaseOperationModeLabel, string(qdef.Mode), true, ctrlutils.OVERWRITE))
		} else if qdef.Mode != quotav1alpha1.SINGULAR || qi.Name != singularQIName {
			// delete QuotaIncrease, if it is not the selected 'singular' one
			log.Info("Deleting ineffective QuotaIncrease", "quotaIncrease", client.ObjectKeyFromObject(&qi).String())
			errs = errors.Join(errs, r.OnboardingCluster.Client().Delete(ctx, &qi))
		}
	}

	return errs
}
