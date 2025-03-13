package quota

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

	colactrlutil "github.com/openmcp-project/controller-utils/pkg/controller"
	"github.com/openmcp-project/controller-utils/pkg/logging"

	openmcpv1alpha1 "github.com/openmcp-project/quota-operator/api/v1alpha1"
	"github.com/openmcp-project/quota-operator/pkg/controller/quota/config"
)

const ControllerName = "quota-controller"

// NewQuotaController creates a new QuotaController instance.
// The activeQuotaDefinitions set should contain the names of all QuotaDefinitions from all QuotaControllers running in the same cluster.
func NewQuotaController(c client.Client, cfg *config.QuotaDefinition, activeQuotaDefinitions sets.Set[string]) *QuotaController {
	return &QuotaController{
		Client:                 c,
		Config:                 cfg,
		ActiveQuotaDefinitions: activeQuotaDefinitions,
	}
}

// QuotaController actually reconciles namespaces, but it gets triggered by generation changes of
// - ResourceQuotas with an OwnerReference pointing to the namespace
// - QuotaIncreases in the namespace
type QuotaController struct {
	Client                 client.Client
	Config                 *config.QuotaDefinition
	ActiveQuotaDefinitions sets.Set[string]
}

// Reconcile contains the main logic of creating and updating a ResourceQuota based on the QuotaIncreases in the reconciled Namespace.
// The Namespace is registered as controller of the ResourceQuota and reacts on changes to QuotaIncreases within the namespace (even without owner reference), so this gets triggered if either is modified.
func (r *QuotaController) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := logging.FromContextOrPanic(ctx).WithName(ControllerName).WithName(r.Config.Name).WithValues("quotaDefinition", r.Config.Name)
	ctx = logging.NewContext(ctx, log)
	log.Debug("Reconcile triggered")

	// fetch Namespace
	ns := &corev1.Namespace{}
	if err := r.Client.Get(ctx, req.NamespacedName, ns); err != nil {
		if apierrors.IsNotFound(err) {
			log.Debug("Namespace not found")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("unable to fetch Namespace: %w", err)
	}

	if r.Config.Selector != nil {
		sel, err := metav1.LabelSelectorAsSelector(r.Config.Selector)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("error converting label selector: %w", err)
		}
		if !sel.Matches(labels.Set(ns.Labels)) {
			log.Debug("Skipping reconciliation because namespace labels do not match the configured selector")
			return ctrl.Result{}, nil
		}
	}

	if !ns.DeletionTimestamp.IsZero() {
		log.Debug("Namespace is being deleted, no action required")
		return ctrl.Result{}, nil
	}

	log.Info("Starting actual reconciliation logic")

	baseQuota, ok := colactrlutil.GetLabel(ns, openmcpv1alpha1.BaseQuotaLabel)
	if !ok {
		log.Debug("Adding base quota label to namespace", "label", openmcpv1alpha1.BaseQuotaLabel, "value", r.Config.Name)
		if err := colactrlutil.EnsureLabel(ctx, r.Client, ns, openmcpv1alpha1.BaseQuotaLabel, r.Config.Name, true); err != nil {
			return ctrl.Result{}, fmt.Errorf("error adding base quota label to namespace: %w", err)
		}
	} else if baseQuota != r.Config.Name {
		// check if this is an old label
		if r.ActiveQuotaDefinitions.Has(baseQuota) {
			// some other instance of QuotaController is already managing this namespace
			log.Info("Another quota definition is already used to manage this namespace, skipping it", "label", openmcpv1alpha1.BaseQuotaLabel, "conflictingQuotaDefinition", baseQuota)
			return ctrl.Result{}, nil
		}
		// namespace has the wrong base quota label, but no QuotaController instance is known with this base quota definition
		// => label probably outdated, overwrite it
		log.Info("Overwriting unknown base quota label on namespace", "label", openmcpv1alpha1.BaseQuotaLabel, "oldValue", baseQuota, "newValue", r.Config.Name)
		if err := colactrlutil.EnsureLabel(ctx, r.Client, ns, openmcpv1alpha1.BaseQuotaLabel, r.Config.Name, true, colactrlutil.OVERWRITE); err != nil {
			return ctrl.Result{}, fmt.Errorf("error overwriting base quota label on namespace: %w", err)
		}
	}

	// add operation mode label to namespace, if it doesn't exist yet
	if err := colactrlutil.EnsureLabel(ctx, r.Client, ns, openmcpv1alpha1.QuotaIncreaseOperationModeLabel, string(r.Config.Mode), true, colactrlutil.OVERWRITE); err != nil {
		return ctrl.Result{}, fmt.Errorf("error adding operation mode label to namespace: %w", err)
	}

	// list all QuotaIncreases in namespace
	qis := &openmcpv1alpha1.QuotaIncreaseList{}
	if err := r.Client.List(ctx, qis, client.InNamespace(ns.Name)); err != nil {
		return ctrl.Result{}, fmt.Errorf("error listing QuotaIncreases: %w", err)
	}

	// create/update ResourceQuota
	_, effects, err := r.createOrUpdateResourceQuota(ctx, ns, qis)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error creating/updating ResourceQuota: %w", err)
	}

	// ensure QuotaIncrease integrity
	if err := r.evaluateEffectiveness(ctx, ns, qis, effects); err != nil {
		return ctrl.Result{}, fmt.Errorf("error evaluating QuotaIncrease effectiveness: %w", err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *QuotaController) SetupWithManager(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr)
	if r.Config.Selector == nil {
		b.For(&corev1.Namespace{})
	} else {
		selectorPredicate, err := predicate.LabelSelectorPredicate(*r.Config.Selector)
		if err != nil {
			return fmt.Errorf("error constructing predicate from label selector: %w", err)
		}
		b.For(&corev1.Namespace{}, builder.WithPredicates(selectorPredicate))
	}
	rqLabelSelectorPredicate, err := predicate.LabelSelectorPredicate(metav1.LabelSelector{
		MatchLabels: map[string]string{
			openmcpv1alpha1.ManagedByLabel:       ControllerName,
			openmcpv1alpha1.QuotaDefinitionLabel: r.Config.Name,
		},
	})
	if err != nil {
		return fmt.Errorf("error constructing predicate from static label selector: %w", err)
	}
	return b.
		Owns(&corev1.ResourceQuota{}, builder.WithPredicates(predicate.And(predicate.GenerationChangedPredicate{}, rqLabelSelectorPredicate))).
		Watches(&openmcpv1alpha1.QuotaIncrease{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
			return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: o.GetNamespace()}}}
		}), builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Named(r.Config.Name).
		Complete(r)
}

func (r *QuotaController) createOrUpdateResourceQuota(ctx context.Context, namespace *corev1.Namespace, qis *openmcpv1alpha1.QuotaIncreaseList) (*corev1.ResourceQuota, map[string]corev1.ResourceList, error) {
	log := logging.FromContextOrPanic(ctx)

	computedRq, effects := r.computeResourceQuota(ctx, namespace, qis)

	rq := &corev1.ResourceQuota{}
	rq.SetName(computedRq.Name)
	rq.SetNamespace(computedRq.Namespace)
	log.Info("Creating/Updating ResourceQuota", "resourceQuota", rq.Name)
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, rq, func() error {
		rq.Annotations = computedRq.Annotations
		rq.Labels = computedRq.Labels
		rq.Spec = computedRq.Spec

		return controllerutil.SetControllerReference(namespace, rq, r.Client.Scheme())
	})
	if err != nil {
		return nil, nil, err
	}
	return rq, effects, nil
}

// computeResourceQuota takes the base ResourceQuota from the config and returns it with the quotas adapted based on the QuotaIncreases in the namespace, respecting the configured mode.
func (r *QuotaController) computeResourceQuota(ctx context.Context, namespace *corev1.Namespace, qis *openmcpv1alpha1.QuotaIncreaseList) (*corev1.ResourceQuota, map[string]corev1.ResourceList) {
	log := logging.FromContextOrPanic(ctx)

	rq := r.Config.BaseResourceQuota()
	rq.SetNamespace(namespace.Name)
	if rq.Labels == nil {
		rq.Labels = map[string]string{}
	}
	rq.Labels[openmcpv1alpha1.ManagedByLabel] = ControllerName
	rq.Labels[openmcpv1alpha1.QuotaDefinitionLabel] = r.Config.Name

	effects := map[string]corev1.ResourceList{}

	switch r.Config.Mode {
	case config.SINGULAR:
		qiName, ok := colactrlutil.GetLabel(namespace, openmcpv1alpha1.SingularQuotaIncreaseLabel)
		if !ok {
			log.Info("No singular QuotaIncrease label found on namespace, ignoring QuotaIncreases", "label", openmcpv1alpha1.SingularQuotaIncreaseLabel)
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
		log.Info("Referenced QuotaIncrease not found in namespace", "label", openmcpv1alpha1.SingularQuotaIncreaseLabel, "QuotaIncrease", qiName)
	case config.CUMULATIVE:
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
	case config.MAXIMUM:
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
func computeMaxQuotaMapping(base corev1.ResourceList, qis *openmcpv1alpha1.QuotaIncreaseList) map[corev1.ResourceName]*openmcpv1alpha1.QuotaIncrease {
	maxQuotas := map[corev1.ResourceName]*openmcpv1alpha1.QuotaIncrease{}
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
		sb.WriteString(fmt.Sprintf("%s: %s, ", resource.String(), quantity.String()))
	}
	res := sb.String()
	if len(res) > 0 {
		res = strings.TrimSuffix(res, ", ")
	}
	return res
}

// evaluateEffectiveness is responsible for setting the effect annotation on all QuotaIncrease resources.
// If deletion of ineffective QuotaIncreases is enabled, it will also delete QuotaIncreases that are no longer effective.
func (r *QuotaController) evaluateEffectiveness(ctx context.Context, namespace *corev1.Namespace, qis *openmcpv1alpha1.QuotaIncreaseList, effects map[string]corev1.ResourceList) error {
	log := logging.FromContextOrPanic(ctx)

	singularQIName := ""
	prefix := ""
	if r.Config.Mode == config.SINGULAR {
		singularQIName, _ = colactrlutil.GetLabel(namespace, openmcpv1alpha1.SingularQuotaIncreaseLabel)
		prefix = openmcpv1alpha1.ActiveSingularQuotaIncreaseEffectPrefix
	}

	var errs error
	for _, qi := range qis.Items {
		effect := effects[qi.Name]
		if !r.Config.DeleteIneffectiveQuotas || len(effect) > 0 {
			// patch effect annotation on QuotaIncrease
			effectString := effectAsString(effect)
			if r.Config.Mode == config.SINGULAR && qi.Name == singularQIName {
				if effectString == "" {
					effectString = prefix
				} else {
					effectString = fmt.Sprintf("%s %s", prefix, effectString)
				}
			}
			errs = errors.Join(errs, colactrlutil.EnsureAnnotation(ctx, r.Client, &qi, openmcpv1alpha1.EffectAnnotation, effectString, true, colactrlutil.OVERWRITE))
			errs = errors.Join(errs, colactrlutil.EnsureLabel(ctx, r.Client, &qi, openmcpv1alpha1.QuotaIncreaseOperationModeLabel, string(r.Config.Mode), true, colactrlutil.OVERWRITE))
		} else if !(r.Config.Mode == config.SINGULAR && qi.Name == singularQIName) {
			// delete QuotaIncrease
			log.Info("Deleting ineffective QuotaIncrease", "quotaIncrease", client.ObjectKeyFromObject(&qi).String())
			errs = errors.Join(errs, r.Client.Delete(ctx, &qi))
		}
	}

	return errs
}
