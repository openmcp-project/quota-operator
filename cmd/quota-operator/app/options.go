package app

import (
	"fmt"

	openmcpctrlutil "github.com/openmcp-project/controller-utils/pkg/controller"
	"github.com/openmcp-project/controller-utils/pkg/logging"
	flag "github.com/spf13/pflag"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/openmcp-project/quota-operator/pkg/controller/quota/config"
)

// rawOptions contains the options specified directly via the command line.
// The Options struct then contains these as embedded struct and additionally some options that were derived from the raw options (e.g. by loading files or interpreting raw options).
type rawOptions struct {
	// controller-runtime stuff
	MetricsAddr          string `json:"metricsAddress"`
	EnableLeaderElection bool   `json:"enableLeaderElection"`
	ProbeAddr            string `json:"healthProbeAddress"`

	// raw options that need to be evaluated
	ConfigPath  string `json:"configPath"`
	ClusterPath string `json:"clusterConfigPath"`

	// raw options that are final
	NoCRDs bool `json:"noCRDs"`
	DryRun bool `json:"dryRun"`
}

// Options describes the options to configure the Landscaper controller.
type Options struct {
	rawOptions

	// logger
	Log logging.Logger

	// completed options from raw options
	ClusterConfig *rest.Config
	Config        *config.QuotaControllerConfig
}

func NewOptions() *Options {
	return &Options{}
}

func (o *Options) AddFlags(fs *flag.FlagSet) {
	// standard stuff
	fs.StringVar(&o.MetricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	fs.StringVar(&o.ProbeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	fs.BoolVar(&o.EnableLeaderElection, "leader-elect", false, "Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")

	// config
	fs.StringVar(&o.ConfigPath, "config", "", "Path to the QuotaController config file.")

	// cluster
	fs.StringVar(&o.ClusterPath, "kubeconfig", "", "Path to the cluster kubeconfig file or directory containing either a kubeconfig or host, token, and ca file. Leave empty to use in-cluster config.")

	// common
	fs.BoolVar(&o.DryRun, "dry-run", false, "If true, the CLI args are evaluated as usual, but the program exits before the controllers are started.")
	fs.BoolVar(&o.NoCRDs, "no-crds", false, "If true, the CRDs for QuotaIncreases are NOT deployed into the target cluster.")
	logging.InitFlags(fs)
}

// Complete parses all Options and flags and initializes the basic functions
func (o *Options) Complete() error {
	// build logger
	log, err := logging.GetLogger()
	if err != nil {
		return err
	}
	o.Log = log
	ctrl.SetLogger(o.Log.Logr())
	olog := log.WithName("options")

	// print raw options
	rawOptsString, err := o.rawOptions.String(true)
	if err != nil {
		olog.Error(err, "error computing raw options string for printing")
	} else {
		fmt.Print(rawOptsString)
	}

	// load kubeconfig
	o.ClusterConfig, err = openmcpctrlutil.LoadKubeconfig(o.ClusterPath)
	if err != nil {
		return fmt.Errorf("unable to load laas cluster kubeconfig: %w", err)
	}

	// load dataplane provider config
	if o.ConfigPath == "" {
		return fmt.Errorf("no (or empty) path to QuotaController config file given, please specify --config argument")
	}
	o.Config, err = config.LoadConfig(o.ConfigPath)
	if err != nil {
		return err
	}
	err = config.Validate(o.Config)
	if err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// print options
	optsString, err := o.String(true, false)
	if err != nil {
		olog.Error(err, "error computing options string for printing")
	} else {
		fmt.Print(optsString)
	}

	return nil
}
