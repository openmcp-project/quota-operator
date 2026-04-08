package app

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/openmcp-project/controller-utils/pkg/logging"
	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	openmcpconst "github.com/openmcp-project/openmcp-operator/api/constants"
	"github.com/openmcp-project/openmcp-operator/lib/clusteraccess"

	providerscheme "github.com/openmcp-project/quota-operator/api/install"
	quotav1alpha1 "github.com/openmcp-project/quota-operator/api/v1alpha1"
	"github.com/openmcp-project/quota-operator/internal/controller/quota"
)

var setupLog logging.Logger

func NewRunCommand(so *SharedOptions) *cobra.Command {
	opts := &RunOptions{
		SharedOptions: so,
	}
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the Platform Service Quota",
		Run: func(cmd *cobra.Command, args []string) {
			opts.PrintRawOptions(cmd)
			if err := opts.Complete(cmd.Context()); err != nil {
				panic(fmt.Errorf("error completing options: %w", err))
			}
			opts.PrintCompletedOptions(cmd)
			if opts.DryRun {
				cmd.Println("=== END OF DRY RUN ===")
				return
			}
			if err := opts.Run(cmd.Context()); err != nil {
				panic(err)
			}
		},
	}
	opts.AddFlags(cmd)

	return cmd
}

type RawRunOptions struct {
	// kubebuilder default flags
	MetricsAddr          string `json:"metrics-bind-address"`
	MetricsCertPath      string `json:"metrics-cert-path"`
	MetricsCertName      string `json:"metrics-cert-name"`
	MetricsCertKey       string `json:"metrics-cert-key"`
	WebhookCertPath      string `json:"webhook-cert-path"`
	WebhookCertName      string `json:"webhook-cert-name"`
	WebhookCertKey       string `json:"webhook-cert-key"`
	EnableLeaderElection bool   `json:"leader-elect"`
	ProbeAddr            string `json:"health-probe-bind-address"`
	PprofAddr            string `json:"pprof-bind-address"`
	SecureMetrics        bool   `json:"metrics-secure"`
	EnableHTTP2          bool   `json:"enable-http2"`

	Controllers []string `json:"controllers"`
}

type RunOptions struct {
	*SharedOptions
	RawRunOptions

	// fields filled in Complete()
	TLSOpts              []func(*tls.Config)
	WebhookTLSOpts       []func(*tls.Config)
	MetricsServerOptions metricsserver.Options
	MetricsCertWatcher   *certwatcher.CertWatcher
	WebhookCertWatcher   *certwatcher.CertWatcher
	ProviderNamespace    string
}

func (o *RunOptions) AddFlags(cmd *cobra.Command) {
	// kubebuilder default flags
	cmd.Flags().StringVar(&o.MetricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	cmd.Flags().StringVar(&o.ProbeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	cmd.Flags().StringVar(&o.PprofAddr, "pprof-bind-address", "", "The address the pprof endpoint binds to. Expected format is ':<port>'. Leave empty to disable pprof endpoint.")
	cmd.Flags().BoolVar(&o.EnableLeaderElection, "leader-elect", false, "Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	cmd.Flags().BoolVar(&o.SecureMetrics, "metrics-secure", true, "If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	cmd.Flags().StringVar(&o.WebhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	cmd.Flags().StringVar(&o.WebhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	cmd.Flags().StringVar(&o.WebhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	cmd.Flags().StringVar(&o.MetricsCertPath, "metrics-cert-path", "", "The directory that contains the metrics server certificate.")
	cmd.Flags().StringVar(&o.MetricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	cmd.Flags().StringVar(&o.MetricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	cmd.Flags().BoolVar(&o.EnableHTTP2, "enable-http2", false, "If set, HTTP/2 will be enabled for the metrics and webhook servers")
}

func (o *RunOptions) Complete(ctx context.Context) error {
	if err := o.SharedOptions.Complete(); err != nil {
		return err
	}
	o.ProviderNamespace = os.Getenv(openmcpconst.EnvVariablePodNamespace)
	if o.ProviderNamespace == "" {
		return fmt.Errorf("environment variable '%s' must be set", openmcpconst.EnvVariablePodNamespace)
	}

	setupLog = o.Log.WithName("setup")
	ctrl.SetLogger(o.Log.Logr())

	// kubebuilder default stuff

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("Disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !o.EnableHTTP2 {
		o.TLSOpts = append(o.TLSOpts, disableHTTP2)
	}

	// Initial webhook TLS options
	o.WebhookTLSOpts = o.TLSOpts

	if len(o.WebhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates", "webhook-cert-path", o.WebhookCertPath, "webhook-cert-name", o.WebhookCertName, "webhook-cert-key", o.WebhookCertKey)

		var err error
		o.WebhookCertWatcher, err = certwatcher.New(
			filepath.Join(o.WebhookCertPath, o.WebhookCertName),
			filepath.Join(o.WebhookCertPath, o.WebhookCertKey),
		)
		if err != nil {
			return fmt.Errorf("failed to initialize webhook certificate watcher: %w", err)
		}

		o.WebhookTLSOpts = append(o.WebhookTLSOpts, func(config *tls.Config) {
			config.GetCertificate = o.WebhookCertWatcher.GetCertificate
		})
	}

	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.2/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	o.MetricsServerOptions = metricsserver.Options{
		BindAddress:   o.MetricsAddr,
		SecureServing: o.SecureMetrics,
		TLSOpts:       o.TLSOpts,
	}

	if o.SecureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.2/pkg/metrics/filters#WithAuthenticationAndAuthorization
		o.MetricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server. While convenient for development and testing,
	// this setup is not recommended for production.
	//
	// TODO(user): If you enable certManager, uncomment the following lines:
	// - [METRICS-WITH-CERTS] at config/default/kustomization.yaml to generate and use certificates
	// managed by cert-manager for the metrics server.
	// - [PROMETHEUS-WITH-CERTS] at config/prometheus/kustomization.yaml for TLS certification.
	if len(o.MetricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates", "metrics-cert-path", o.MetricsCertPath, "metrics-cert-name", o.MetricsCertName, "metrics-cert-key", o.MetricsCertKey)

		var err error
		o.MetricsCertWatcher, err = certwatcher.New(
			filepath.Join(o.MetricsCertPath, o.MetricsCertName),
			filepath.Join(o.MetricsCertPath, o.MetricsCertKey),
		)
		if err != nil {
			return fmt.Errorf("failed to initialize metrics certificate watcher: %w", err)
		}

		o.MetricsServerOptions.TLSOpts = append(o.MetricsServerOptions.TLSOpts, func(config *tls.Config) {
			config.GetCertificate = o.MetricsCertWatcher.GetCertificate
		})
	}

	return nil
}

func (o *RunOptions) Run(ctx context.Context) error {
	if err := o.PlatformCluster.InitializeClient(providerscheme.InstallOperatorAPIsPlatform(runtime.NewScheme())); err != nil {
		return err
	}

	setupLog = o.Log.WithName("setup")
	setupLog.Info("Environment", "value", o.Environment)
	setupLog.Info("ProviderName", "value", o.ProviderName)

	// verify QuotaServiceConfig existence
	// This also happens in the reconcile, but then the pod will look healthy while it is actually not able to reconcile anything.
	svcCfg := &quotav1alpha1.QuotaServiceConfig{}
	svcCfg.Name = o.ProviderName
	if err := o.PlatformCluster.Client().Get(ctx, client.ObjectKeyFromObject(svcCfg), svcCfg); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("QuotaServiceConfig '%s' not found: %w", svcCfg.Name, err)
		}
		return fmt.Errorf("error getting QuotaServiceConfig '%s': %w", svcCfg.Name, err)
	}
	if err := svcCfg.Spec.Validate(); err != nil {
		return fmt.Errorf("QuotaServiceConfig '%s' is invalid: %w", svcCfg.Name, err)
	}

	setupLog.Info("Getting access to the onboarding cluster")
	onboardingScheme := providerscheme.InstallOperatorAPIsOnboarding(runtime.NewScheme())

	providerSystemNamespace := os.Getenv(openmcpconst.EnvVariablePodNamespace)
	if providerSystemNamespace == "" {
		return fmt.Errorf("environment variable %s is not set", openmcpconst.EnvVariablePodNamespace)
	}

	clusterAccessManager := clusteraccess.NewClusterAccessManager(o.PlatformCluster.Client(), quota.ControllerName, providerSystemNamespace)
	clusterAccessManager.WithLogger(&setupLog).
		WithInterval(10 * time.Second).
		WithTimeout(30 * time.Minute)

	onboadingClusterPermissions := []clustersv1alpha1.PermissionsRequest{
		{
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{quotav1alpha1.GroupName},
					Resources: []string{"quotaincreases"},
					Verbs:     []string{"*"},
				},
				{
					APIGroups: []string{""},
					Resources: []string{"namespaces"},
					Verbs:     []string{"watch", "get", "list", "update", "patch"},
				},
				{
					APIGroups: []string{""},
					Resources: []string{"resourcequotas", "resourcequotas/status"},
					Verbs:     []string{"*"},
				},
			},
		},
	}
	onboardingCluster, err := clusterAccessManager.CreateAndWaitForCluster(ctx, clustersv1alpha1.PURPOSE_ONBOARDING, clustersv1alpha1.PURPOSE_ONBOARDING, onboardingScheme, onboadingClusterPermissions)
	if err != nil {
		return fmt.Errorf("error creating/updating onboarding cluster: %w", err)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: o.WebhookTLSOpts,
	})

	mgr, err := ctrl.NewManager(onboardingCluster.RESTConfig(), ctrl.Options{
		Scheme:                 onboardingCluster.Scheme(),
		Metrics:                o.MetricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: o.ProbeAddr,
		PprofBindAddress:       o.PprofAddr,
		LeaderElection:         o.EnableLeaderElection,
		LeaderElectionID:       "github.com/openmcp-project/platform-service-quota",
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
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		return fmt.Errorf("unable to create manager: %w", err)
	}

	// setup Quota reconciler
	if err := quota.NewQuotaController(o.PlatformCluster.Client(), onboardingCluster.Client(), o.ProviderName).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to add Quota reconciler to manager: %w", err)
	}

	if o.MetricsCertWatcher != nil {
		setupLog.Info("Adding metrics certificate watcher to manager")
		if err := mgr.Add(o.MetricsCertWatcher); err != nil {
			return fmt.Errorf("unable to add metrics certificate watcher to manager: %w", err)
		}
	}

	if o.WebhookCertWatcher != nil {
		setupLog.Info("Adding webhook certificate watcher to manager")
		if err := mgr.Add(o.WebhookCertWatcher); err != nil {
			return fmt.Errorf("unable to add webhook certificate watcher to manager: %w", err)
		}
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up ready check: %w", err)
	}

	setupLog.Info("Starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		return fmt.Errorf("problem running manager: %w", err)
	}

	return nil
}
