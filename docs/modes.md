# Operating Modes

The basic idea of the quota operator is that each namespace gets one base `ResourceQuota`, whose limits then can be increased by adding `QuotaIncrease` resources into that namespace. The operator supports three different modes of how to handle multiple `QuotaIncrease` resources, which are explained below.

The quota operator supports a feature called 'deletion of ineffective QuotaIncreases', which will automatically remove all `QuotaIncrease`s that don't have any effect on the generated `ResourceQuota`. The 'Effectiveness of QuotaIncreases' paragraphs below explain which `QuotaIncrease`s are considered 'effective' in the respective modes. Note that this feature is turned off by default and has to be explicitly enabled per quota definition in the config.

All of the examples below assume the following base `ResourceQuota` spec
```yaml
spec:
  hard:
    count/secrets: "10"
```
and the following `QuotaIncrease` specs
```yaml
spec: # small
  count/secrets: "5"
---
spec: # medium
  count/secrets: "50"
  count/configmaps: "10"
---
spec: # big
  count/secrets: "100"
```

## Mode: cumulative

In `cumulative` mode, the quantities specified in all `QuotaIncrease`s in the namespace are added to the base quota.

So, for the example values from above, the resulting `ResourceQuota` would have this spec
```yaml
spec:
  count/secrets: "165"
  count/configmaps: "10"
```

### Effectiveness of QuotaIncreases

In `cumulative` mode, each single `QuotaIncrease` contributes to the final quota and is therefore considered effective.


## Mode: maximum

In `maximum` mode, for each resource only the highest quota - either from the base `ResourceQuota` or from any `QuotaIncrease` in that namespace - is taken into account.

The example values would result in this `ResourceQuota` spec:
```yaml
spec:
  count/secrets: "100"
  count/configmaps: "10"
```

### Effectiveness of QuotaIncreases

For the example, only the `small` `QuotaIncrease` is considered to be ineffective. The secrets quota from the `medium` one is overshadowed by the one from `big`, but as `medium` provides the highest quota for configmaps, it is still considered effective.

If another `QuotaIncrease` with a higher configmap quota was added, the `medium` one would become ineffective.

If another `QuotaIncrease` with the same configmap quota was added, only the one whose name comes first in alphabetical order would be considered effective (regarding configmap quota) and the other one would be considered ineffective, unless it provides the highest quota for some other resource.


## Mode: singular

In `singular`, one single `QuotaIncrease` must be referenced via the `quota.openmcp.cloud/use` label on the containing namespace. Only this `QuotaIncrease` will be taken into account. The quotas from the `QuotaIncrease` and the one from the base `ResourceQuota` are aggregated maximum-style and not accumulated.

Assuming the aforementioned label would point to the `medium` `QuotaIncrease`, the resulting `ResourceQuota` spec would be
```yaml
spec:
  count/secrets: "50"
  count/configmaps: "10"
```

### Effectiveness of QuotaIncreases

For `singular` mode, only the referenced `QuotaIncrease` is considered to be effective. That is even the case if all quotas it provides are smaller than the respective ones in the base `ResourceQuota`, although the `QuotaIncrease` actually does not have any influence on the generated `ResourceQuota` in this case. As an example, if the `small` `QuotaIncrease` from the examples was the referenced one, it would still not be deleted if deletion of ineffective `QuotaIncrease`s was turned on, despite its secrets quota of 5 being overshadowed by the base `ResourceQuota`'s secret quota of 10.
