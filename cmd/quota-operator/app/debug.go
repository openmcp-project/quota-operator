package app

import (
	"fmt"
	"strings"

	"sigs.k8s.io/yaml"
)

func (ro *rawOptions) String(includeHeader bool) (string, error) {
	sb := strings.Builder{}
	if includeHeader {
		sb.WriteString("########## RAW OPTIONS ##########\n")
	}
	printableRawOptions, err := yaml.Marshal(ro)
	if err != nil {
		return "", fmt.Errorf("unable to marshal raw options to yaml: %w", err)
	}
	sb.WriteString(string(printableRawOptions))
	if includeHeader {
		sb.WriteString("########## END RAW OPTIONS ##########\n")
	}
	return sb.String(), nil
}

func (o *Options) String(includeHeader bool, includeRawOptions bool) (string, error) {
	sb := strings.Builder{}
	if includeHeader {
		sb.WriteString("########## OPTIONS ##########\n")
	}
	if includeRawOptions {
		rawOpts, err := o.rawOptions.String(false)
		if err != nil {
			return "", err
		}
		sb.WriteString(rawOpts)
	}

	opts := map[string]any{}

	// clusters
	opts["clusterHost"] = nil
	if o.ClusterConfig != nil {
		opts["clusterHost"] = o.ClusterConfig.Host
	}

	// config
	opts["config"] = nil
	if o.Config != nil {
		opts["config"] = o.Config
	}

	// convert to yaml
	optsString, err := yaml.Marshal(opts)
	if err != nil {
		return "", fmt.Errorf("error converting options map to yaml: %w", err)
	}
	sb.WriteString(string(optsString))

	if includeHeader {
		sb.WriteString("########## END OPTIONS ##########\n")
	}
	return sb.String(), nil
}
