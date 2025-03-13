package app

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/openmcp-project/controller-utils/pkg/logging"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlcfg "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	crdinstall "github.tools.sap/CoLa/quota-operator/api/crds"
	quotainstall "github.tools.sap/CoLa/quota-operator/api/install"
	quotacontroller "github.tools.sap/CoLa/quota-operator/pkg/controller/quota"
)

func NewQuotaOperatorCommand(ctx context.Context) *cobra.Command {
	options := NewOptions()

	cmd := &cobra.Command{
		Use:   "quota",
		Short: "quota manages ResourceQuota and QuotaIncrease objects in namespaces.",

		Run: func(cmd *cobra.Command, args []string) {
			if err := options.Complete(); err != nil {
				fmt.Print(err)
				os.Exit(1)
			}
			ctx = logging.NewContext(ctx, options.Log)
			if err := options.run(ctx); err != nil {
				options.Log.Error(err, "unable to run quota operator")
				os.Exit(1)
			}
		},
	}

	options.AddFlags(cmd.Flags())

	return cmd
}

func (o *Options) run(ctx context.Context) error {
	log := o.Log
	// ctx = logging.NewContext(ctx, log)
	setupLog := log.WithName("setup")

	if o.DryRun {
		setupLog.Info("Exiting now because this is a dry run")
		return nil
	}
	if len(o.Config.Quotas) == 0 {
		setupLog.Info("No quota definitions specified in config, nothing to do")
		return nil
	}

	setupLog.Info("Starting controllers")
	sc := runtime.NewScheme()
	quotainstall.Install(sc)
	mgr, err := ctrl.NewManager(o.ClusterConfig, ctrl.Options{
		Scheme: sc,
		Metrics: server.Options{
			BindAddress: o.MetricsAddr,
		},
		Controller: ctrlcfg.Controller{
			RecoverPanic: ptr.To(true),
		},
		HealthProbeBindAddress: o.ProbeAddr,
		LeaderElection:         o.EnableLeaderElection,
		LeaderElectionID:       "quota.openmcp.cloud",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		return fmt.Errorf("unable to start manager: %w", err)
	}

	// install CRDs if configured
	if !o.NoCRDs {
		setupScheme := runtime.NewScheme()
		quotainstall.Install(setupScheme)
		if err := apiextv1.AddToScheme(setupScheme); err != nil { // required for CRD installation
			panic(err)
		}
		setupClient, err := client.New(o.ClusterConfig, client.Options{Scheme: setupScheme})
		if err != nil {
			return fmt.Errorf("error building setup client: %w", err)
		}
		setupLog.Info("CRD installation configured, deploying CRDs ...")
		crds := crdinstall.CRDs()
		for _, crd := range crds {
			setupLog.Info("Deploying CRD", "name", crd.Name)
			desired := crd.DeepCopy()
			if _, err := ctrl.CreateOrUpdate(ctx, setupClient, crd, func() error {
				crd.Spec = desired.Spec
				return nil
			}); err != nil {
				return fmt.Errorf("error trying to apply CRD '%s' into cluster: %w", crd.Name, err)
			}
		}
	}

	// create set of active quota definitions
	activeQuotaDefinitions := o.Config.GetActiveQuotaDefinitions()

	// create controllers
	for _, qd := range o.Config.Quotas {
		if err := quotacontroller.NewQuotaController(mgr.GetClient(), qd, activeQuotaDefinitions).SetupWithManager(mgr); err != nil {
			return fmt.Errorf("error adding controller for quota definition '%s' to manager: %w", qd.Name, err)
		}
	}

	setupLog.Info("Starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}

	return nil
}
