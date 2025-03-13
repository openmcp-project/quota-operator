# Demo

This document aims to provide a short demo flow that can be used to get familiar with the quota operator or present it to people unfamiliar with it.

## Demo Flow

### Prerequisites

This demo requires access to a kubernetes cluster. It expects the `KUBECONFIG` environment variable to point to the kubeconfig for this cluster.

It will be helpful to have two terminal tabs with the `KUBECONFIG` environment variable configured - one for running the controller in and one for applying resources into the cluster while it is running.

### Create Namespaces

For this demo, we will work with three namespaces. They are used to demonstrate two things:
- That the quota operator is able to use different base quotas for different kinds of namespaces, controlled via labels on the namespaces.
  - To show this, one namespace will use quotas for secrets, one for configmaps and one for serviceaccounts.
- The three different operation modes the quota operator supports.
  - The three modes are `singular`, `maximum`, and `cumulative`. The differences will be explained below. The namespaces are named accordingly.

Create the namespaces:
```shell
cat << EOF | kubectl apply -f -
apiVersion: v1
kind: Namespace
metadata:
  labels:
    demo.quota.operator/id: singular
  name: singular
---
apiVersion: v1
kind: Namespace
metadata:
  labels:
    demo.quota.operator/id: maximum
  name: maximum
---
apiVersion: v1
kind: Namespace
metadata:
  labels:
    demo.quota.operator/id: cumulative
  name: cumulative
---
EOF
```

### Configure QuotaOperator

For this demo, we will configure the quota operator with three different quota definitions. Which will be applied depends on the value of the `demo.quota.operator/id` label.

```shell
quota_operator_config=$(mktemp)
cat << EOF > ${quota_operator_config}
quotas:
- name: "singular-quota"
  selector:
    matchLabels:
      demo.quota.operator/id: singular
  mode: singular
  template:
    spec:
      hard:
        count/secrets: 3
- name: "maximum-quota"
  selector:
    matchLabels:
      demo.quota.operator/id: maximum
  mode: maximum
  template:
    spec:
      hard:
        count/configmaps: 3
- name: "cumulative-quota"
  selector:
    matchLabels:
      demo.quota.operator/id: cumulative
  mode: cumulative
  template:
    spec:
      hard:
        count/serviceaccounts: 3
EOF
```

This config instructs the operator to run three instances of the quota controller. The first one reacts only to namespaces with the `demo.quota.operator/id: singular` label and by default grants a quota for three secrets. Instance two and three behave the same for the `maximum` and `cumulative` label value and configmap and serviceaccount quotas, respectively.

You might also notice that each instance is configured with a different value for `mode`. This value controls how the operator handles multiple competing `QuotaIncrease` resources in the same namespace. We will get to this later.

### Run QuotaOperator

Now run the quota operator:
```shell
go run ./cmd/quota-operator/main.go --kubeconfig ${KUBECONFIG} --config ${quota_operator_config} --cli # the --cli argument configures the logger for terminal-optimized output
```

### Check for ResourceQuotas

By now, the quota operator should have created a `ResourceQuota` in all of our three namespaces, based on the respective templates provided in the config.

Fetch the `ResourceQuota` resources:
```shell
kubectl get quota -A
```

The result should look roughly like this (assuming there were no other `ResourceQuota`s in the cluster before):
```
NAMESPACE        NAME               AGE   REQUEST                      LIMIT
cumulative       cumulative-quota   17m   count/serviceaccounts: 1/3
maximum          maximum-quota      17m   count/configmaps: 1/3
singular         singular-quota     17m   count/secrets: 0/3
```

As can be seen, the quota operator created different resource quotas based on the labels on the namespaces, as we have specified in the configuration. Namespaces without one of the configured labels (e.g. `default`) are ignored and did not get a `ResourceQuota`.

### Create QuotaIncreases

For each of the three namespaces, let's create three `QuotaIncrease`s:
- A `small` one, increasing the already exising quota to 10.
- A `medium` one, increasing the already existing quota to 50 and additionally granting quota for 10 services.
- A `big` one, increasing the already existing quota to 100.

#### Mode: singular

```shell
cat << EOF | kubectl apply -f -
apiVersion: openmcp.cloud/v1alpha1
kind: QuotaIncrease
metadata:
  name: small
  namespace: singular
spec:
  hard:
    count/secrets: 10
---
apiVersion: openmcp.cloud/v1alpha1
kind: QuotaIncrease
metadata:
  name: medium
  namespace: singular
spec:
  hard:
    count/secrets: 50
    count/services: 10
---
apiVersion: openmcp.cloud/v1alpha1
kind: QuotaIncrease
metadata:
  name: big
  namespace: singular
spec:
  hard:
    count/secrets: 100
---
EOF
```

However, checking the `ResourceQuota`, we notice that is has not changed:
```
$ kubectl -n singular get quota singular-quota
NAME             AGE   REQUEST              LIMIT
singular-quota   4h    count/secrets: 0/3
```

The reason is simple: In `singular` mode, only one specific `QuotaIncrease` is taken into account and that one has to be referenced via a label on the containing namespace. Let's add the corresponding label, pointing to the `medium` `QuotaIncrease`:
```shell
kubectl label namespace singular quota.openmcp.cloud/use=medium
```

And now, the increased quotas have taken effect:
```
$ kubectl -n singular get quota singular-quota
NAME             AGE    REQUEST                                     LIMIT
singular-quota   4h2m   count/secrets: 0/50, count/services: 0/10
```

When listing the `QuotaIncrease`s with `-o wide`, it immediately becomes visible which one is the active one:
```
$ kubectl -n singular get qi -o wide
NAME     MODE       AGE    EFFECT
big      singular   167m
medium   singular   167m   [active] count/secrets: 50, count/services: 10
small    singular   167m
```


#### Mode: maximum

```shell
cat << EOF | kubectl apply -f -
apiVersion: openmcp.cloud/v1alpha1
kind: QuotaIncrease
metadata:
  name: small
  namespace: maximum
spec:
  hard:
    count/configmaps: 10
---
apiVersion: openmcp.cloud/v1alpha1
kind: QuotaIncrease
metadata:
  name: medium
  namespace: maximum
spec:
  hard:
    count/configmaps: 50
    count/services: 10
---
apiVersion: openmcp.cloud/v1alpha1
kind: QuotaIncrease
metadata:
  name: big
  namespace: maximum
spec:
  hard:
    count/configmaps: 100
---
EOF
```

As we can see, the quota operator immediately picked up the increases and modified the `ResourceQuota` accordingly:
```
kubectl -n maximum get quota
NAME            AGE     REQUEST                                         LIMIT
maximum-quota   3h23m   count/configmaps: 1/100, count/services: 0/10
```

Now, let's have a look at the `QuotaIncrease`s:
```
kubectl -n maximum get qi -o wide
NAME     MODE      AGE   EFFECT
big      maximum   83m   count/configmaps: 100
medium   maximum   83m   count/services: 10
small    maximum   83m
```

The `maximum` mode takes only the highest quantity for each resource into account. Therefore, the configmap quotas from `small` and `medium` are overshadowed by the one from `big` and not shown under the `effect` column. Note that the service quota from `medium` is still effective, because none of the other `QuotaIncrease`s grant quota for this resource.


#### Mode: cumulative

```shell
cat << EOF | kubectl apply -f -
apiVersion: openmcp.cloud/v1alpha1
kind: QuotaIncrease
metadata:
  name: small
  namespace: cumulative
spec:
  hard:
    count/serviceaccounts: 10
---
apiVersion: openmcp.cloud/v1alpha1
kind: QuotaIncrease
metadata:
  name: medium
  namespace: cumulative
spec:
  hard:
    count/serviceaccounts: 50
    count/services: 10
---
apiVersion: openmcp.cloud/v1alpha1
kind: QuotaIncrease
metadata:
  name: big
  namespace: cumulative
spec:
  hard:
    count/serviceaccounts: 100
---
EOF
```

The `ResourceQuota` got adapted accordingly:
```
kubectl -n cumulative get quota
NAME               AGE     REQUEST                                              LIMIT
cumulative-quota   4h40m   count/serviceaccounts: 1/163, count/services: 0/10
```

As the name suggests, the `cumulative` mode simply adds up all quantities specified by all `QuotaIncrease`s for each given resource. This means that, opposed to the other two modes, here each `QuotaIncrease` always has an effect.

```
kubectl -n cumulative get qi -o wide
NAME     MODE         AGE   EFFECT
big      cumulative   66s   count/serviceaccounts: 100
medium   cumulative   66s   count/serviceaccounts: 50, count/services: 10
small    cumulative   66s   count/serviceaccounts: 10
```


### Deletion of ineffective QuotaIncreses

In `maximum` mode, there might be `QuotaIncrease`s which don't have any effect, because all quotas they provide are overshadowed by other `QuotaIncrease`s providing higher quotas for the same resources. The situation is even worse for `singular` mode, where only one `QuotaIncrease` is taken into account at all.

The quota operator can be instructed to automatically delete `QuotaIncrease` resources that don't have an effect on the generated `ResourceQuota`. Let's turn this feature on for all three of our quota definitions. For this, we have to stop the quota operator and update the config:
```shell
cat << EOF > ${quota_operator_config}
quotas:
- name: "singular-quota"
  selector:
    matchLabels:
      demo.quota.operator/id: singular
  mode: singular
  deleteIneffectiveQuotas: true # <<<<<<<<<<<<<<<<<<<<
  template:
    spec:
      hard:
        count/secrets: 3
- name: "maximum-quota"
  selector:
    matchLabels:
      demo.quota.operator/id: maximum
  mode: maximum
  deleteIneffectiveQuotas: true # <<<<<<<<<<<<<<<<<<<<
  template:
    spec:
      hard:
        count/configmaps: 3
- name: "cumulative-quota"
  selector:
    matchLabels:
      demo.quota.operator/id: cumulative
  mode: cumulative
  deleteIneffectiveQuotas: true # <<<<<<<<<<<<<<<<<<<<
  template:
    spec:
      hard:
        count/serviceaccounts: 3
EOF
```

Now, let's start the quota operator again. We will immediately see some log messages with `Deleting ineffective QuotaIncrease`.
A look at the `QuotaIncrease`s quickly shows the effect of this configuration:
```
kubectl get qi -A -o wide
NAMESPACE    NAME     MODE         AGE     EFFECT
cumulative   big      cumulative   19m     count/serviceaccounts: 100
cumulative   medium   cumulative   19m     count/serviceaccounts: 50, count/services: 10
cumulative   small    cumulative   19m     count/serviceaccounts: 10
maximum      big      maximum      108m    count/configmaps: 100
maximum      medium   maximum      108m    count/services: 10
singular     medium   singular     4h38m   [active] count/secrets: 50, count/services: 10
```

In short,
- all `QuotaIncrease`s with mode `cumulative` survived, because they are all summed up and therefore every single one influences the generated `ResourceQuota`.
- for `maximum` mode, only `big` and `medium` survived. `big` contains the highest quota for configmaps. The configmap quota from `medium` is overshadowed by the one from `big` (which you can see by this quota not being shown in the `effect` column), but `medium` is the only `QuotaIncrease` in this namespace which provides quota for services and therefore still has an effect.
- for `singular` mode, only the `QuotaIncrease` referenced in the namespace label survived, as this is the only one which is taken into account. Note that this `QuotaIncrease` will never be deleted, even if it didn't contain any quotas or all of them were lower than the ones from the base `ResourceQuota` (which would mean that the `QuotaIncrease` does not have any effect).

