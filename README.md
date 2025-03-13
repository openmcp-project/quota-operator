[![REUSE status](https://api.reuse.software/badge/github.com/openmcp-project/quota-operator)](https://api.reuse.software/info/github.com/openmcp-project/quota-operator)
# quota-operator

## About this project

A Kubernetes Operator for managing resource quota in namespaces.

## What it does

The quota operator takes one or more _quota definitions_ in its config. Each quota definition consists of a label selector (applied to namespaces) and a base `ResourceQuota` template. In each namespace matching the label selector, a `ResourceQuota` with the quotas from the template is created.

Whenever a `QuotaIncrease` resource is added to any of these namespaces, the operator adapts the generated `ResourceQuota` accordingly, depending on the operating mode configured in the quota definition:
- for `cumulative` mode, all quotas from all `QuotaIncrease`s in that namespace are summed up
- for `maximum` mode, only the highest quota for each resource takes effect
- for `singular` mode, only the `QuotaIncrease` which is referenced in the `quota.openmcp.cloud/use` label on the containing namespace is taken into account

When listing `QuotaIncrease`s with the `-o wide` option via `kubectl`, the effect that each quota increase has on the corresponding `ResourceQuota` is shown. The operator can also be configured to immediately delete `QuotaIncrease`s that don't have any effect.

### Limitations

The sets of namespaces returned by the label selectors of different quota definitions in the config must be disjunct. If the same namespace is matched by more than one quota definition's label selector, only the first quota definition to act on this namespace will take effect, all other ones will then ignore it. To identify already handled namespaces, the operator attaches a label to the namespace that contains the name of the quota definition that is responsible for it.

To allow renaming quota definitions in the config, these labels will be overwritten if their value does not match one of the currently configured quota definitions' name. If you are running multiple quota-operators for the same cluster, each needs to be given a list of all _other_ operators' quota definition names (under `externalQuotaDefinitionNames` in the configuration).

## Requirements and Setup

The operator is quite simple to run and only requires two arguments:
- `--config` must specify the path to the configuration file.
  - See [the docs](docs/config.md) for more information on what the config file has to look like.
- `--kubeconfig` specifies the path to the kubeconfig file for the cluster that should be watched by the quota operator.
  - When running within the cluster which should also be watched, this argument can be omitted.
  - Instead of pointing to a kubeconfig file, this argument may also point to a folder containing an OIDC trust configuration.

To deploy the quota operator into a cluster, best use the provided helm chart.

### Further documentation

- [Configuration](docs/config.md)
- A quick [demo flow](docs/demo.md) which can be used as a tutorial or for showcasing the quota operator
- A more thorough explanation of the different [operating modes](docs/modes.md)

## Support, Feedback, Contributing

This project is open to feature requests/suggestions, bug reports etc. via [GitHub issues](https://github.com/openmcp-project/quota-operator/issues). Contribution and feedback are encouraged and always welcome. For more information about how to contribute, the project structure, as well as additional contribution information, see our [Contribution Guidelines](CONTRIBUTING.md).

## Security / Disclosure
If you find any bug that may be a security problem, please follow our instructions at [in our security policy](https://github.com/openmcp-project/quota-operator/security/policy) on how to report it. Please do not create GitHub issues for security-related doubts or problems.

## Code of Conduct

We as members, contributors, and leaders pledge to make participation in our community a harassment-free experience for everyone. By participating in this project, you agree to abide by its [Code of Conduct](https://github.com/SAP/.github/blob/main/CODE_OF_CONDUCT.md) at all times.

## Licensing

Copyright 2025 SAP SE or an SAP affiliate company and quota-operator contributors. Please see our [LICENSE](LICENSE) for copyright and license information. Detailed information including third-party components and their licensing/copyright information is available [via the REUSE tool](https://api.reuse.software/info/github.com/openmcp-project/
