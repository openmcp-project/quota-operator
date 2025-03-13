package config

import (
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/yaml"
)

// LoadConfig reads the configuration file from a given path and parses it into a QuotaControllerConfig object.
func LoadConfig(path string) (*QuotaControllerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading config file: %w", err)
	}
	cfg := &QuotaControllerConfig{}
	err = yaml.Unmarshal(data, cfg)
	if err != nil {
		return nil, fmt.Errorf("error parsing config file: %w", err)
	}
	return cfg, nil
}

// GetActiveQuotaDefinitions returns the set of known active QuotaDefinitions.
func (cfg *QuotaControllerConfig) GetActiveQuotaDefinitions() sets.Set[string] {
	// create set of active quota definitions
	activeQuotaDefinitions := sets.New[string](cfg.ExternalQuotaDefinitionNames...)
	for _, qd := range cfg.Quotas {
		activeQuotaDefinitions.Insert(qd.Name)
	}
	return activeQuotaDefinitions
}
