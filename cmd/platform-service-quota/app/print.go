package app

import (
	"fmt"

	"sigs.k8s.io/yaml"

	"github.com/spf13/cobra"
)

func (o *SharedOptions) PrintRaw(cmd *cobra.Command) {
	data, err := yaml.Marshal(o.RawSharedOptions)
	if err != nil {
		cmd.Println(fmt.Errorf("error marshalling raw shared options: %w", err).Error())
		return
	}
	cmd.Print(string(data))
}

func (o *SharedOptions) PrintCompleted(cmd *cobra.Command) {
	raw := map[string]any{
		"platformCluster": o.PlatformCluster.APIServerEndpoint(),
		"providerName":    o.ProviderName,
	}
	data, err := yaml.Marshal(raw)
	if err != nil {
		cmd.Println(fmt.Errorf("error marshalling completed shared options: %w", err).Error())
		return
	}
	cmd.Print(string(data))
}

func (o *InitOptions) PrintRaw(cmd *cobra.Command) {}

func (o *InitOptions) PrintRawOptions(cmd *cobra.Command) {
	cmd.Println("########## RAW OPTIONS START ##########")
	o.SharedOptions.PrintRaw(cmd)
	o.PrintRaw(cmd)
	cmd.Println("########## RAW OPTIONS END ##########")
}

func (o *InitOptions) PrintCompleted(cmd *cobra.Command) {}

func (o *InitOptions) PrintCompletedOptions(cmd *cobra.Command) {
	cmd.Println("########## COMPLETED OPTIONS START ##########")
	o.SharedOptions.PrintCompleted(cmd)
	o.PrintCompleted(cmd)
	cmd.Println("########## COMPLETED OPTIONS END ##########")
}

func (o *RunOptions) PrintRaw(cmd *cobra.Command) {
	data, err := yaml.Marshal(o.RawRunOptions)
	if err != nil {
		cmd.Println(fmt.Errorf("error marshalling raw options: %w", err).Error())
		return
	}
	cmd.Print(string(data))
}

func (o *RunOptions) PrintRawOptions(cmd *cobra.Command) {
	cmd.Println("########## RAW OPTIONS START ##########")
	o.SharedOptions.PrintRaw(cmd)
	o.PrintRaw(cmd)
	cmd.Println("########## RAW OPTIONS END ##########")
}

func (o *RunOptions) PrintCompleted(cmd *cobra.Command) {
	raw := map[string]any{
		// TODO add options or remove
	}
	data, err := yaml.Marshal(raw)
	if err != nil {
		cmd.Println(fmt.Errorf("error marshalling completed options: %w", err).Error())
		return
	}
	cmd.Print(string(data))
}

func (o *RunOptions) PrintCompletedOptions(cmd *cobra.Command) {
	cmd.Println("########## COMPLETED OPTIONS START ##########")
	o.SharedOptions.PrintCompleted(cmd)
	o.PrintCompleted(cmd)
	cmd.Println("########## COMPLETED OPTIONS END ##########")
}
