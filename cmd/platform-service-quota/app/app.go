package app

import (
	"fmt"
	"os"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/spf13/cobra"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	"github.com/openmcp-project/controller-utils/pkg/logging"
)

func NewPlatformServiceQuotaCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "platform-service-quota",
		Short: "platform-service-quota manages resource quotas",
	}
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)

	so := &SharedOptions{
		RawSharedOptions: &RawSharedOptions{},
		PlatformCluster:  clusters.New("platform"),
	}
	so.AddPersistentFlags(cmd)
	cmd.AddCommand(NewInitCommand(so))
	cmd.AddCommand(NewRunCommand(so))

	return cmd
}

type RawSharedOptions struct {
	Environment  string `json:"environment"`
	ProviderName string `json:"provider-name"`
	DryRun       bool   `json:"dry-run"`
}

type SharedOptions struct {
	*RawSharedOptions
	PlatformCluster *clusters.Cluster

	// fields filled in Complete()
	Log logging.Logger
}

func (o *SharedOptions) AddPersistentFlags(cmd *cobra.Command) {
	// logging
	logging.InitFlags(cmd.PersistentFlags())
	// clusters
	o.PlatformCluster.RegisterSingleConfigPathFlag(cmd.PersistentFlags())
	// environment
	cmd.PersistentFlags().StringVar(&o.Environment, "environment", "", "Environment name. Required. This is used to distinguish between different environments that are watching the same Onboarding cluster. Must be globally unique.")
	// provider name
	cmd.PersistentFlags().StringVar(&o.ProviderName, "provider-name", "", "Name of the provider resource.")
	cmd.PersistentFlags().BoolVar(&o.DryRun, "dry-run", false, "If set, the command aborts after evaluation of the given flags.")
}

func (o *SharedOptions) Complete() error {
	if o.Environment == "" {
		return fmt.Errorf("environment must not be empty")
	}
	if o.ProviderName == "" {
		return fmt.Errorf("provider-name must not be empty")
	}

	// build logger
	log, err := logging.GetLogger()
	if err != nil {
		return err
	}
	o.Log = log
	ctrl.SetLogger(o.Log.Logr())

	if err := o.PlatformCluster.InitializeRESTConfig(); err != nil {
		return err
	}

	return nil
}
