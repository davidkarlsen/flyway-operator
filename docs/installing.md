# Installing

The operator watches all namespaces. Choose one of the methods below, then continue with [usage](usage.md).

Requires Kubernetes 1.25 or later: the CRD validates `jobMetadata` and `podMetadata` with CEL rules.

## OpenShift (OLM)

1. Add the catalog source:

   ```yaml
   apiVersion: operators.coreos.com/v1alpha1
   kind: CatalogSource
   metadata:
     name: flyway-operator
     namespace: openshift-marketplace
   spec:
     displayName: Flyway operator
     publisher: davidkarlsen
     sourceType: grpc
     image: ghcr.io/davidkarlsen/flyway-operator-catalog:alpha
     updateStrategy:
       registryPoll:
         interval: 45m
   ```

2. Install the operator, either from **Operators → OperatorHub** in the console, or with a `Subscription`.
   The operator only supports the `AllNamespaces` install mode, so subscribe in `openshift-operators`,
   which already has a global `OperatorGroup`:

   ```yaml
   apiVersion: operators.coreos.com/v1alpha1
   kind: Subscription
   metadata:
     name: flyway-operator
     namespace: openshift-operators
   spec:
     name: flyway-operator
     channel: stable-v0
     source: flyway-operator
     sourceNamespace: openshift-marketplace
     installPlanApproval: Automatic
   ```

   `stable-v0` follows every 0.x release. To stay on one minor version, subscribe to `stable-v0.<minor>`
   (e.g. `stable-v0.5`) instead, and switch channels to upgrade.

3. Check that the install succeeded:

   ```shell
   oc get csv -n openshift-operators | grep flyway-operator
   ```

## Helm

```shell
helm repo add flyway-operator https://davidkarlsen.github.io/flyway-operator
helm repo update
kubectl create namespace flyway-operator
helm install flyway-operator flyway-operator/flyway-operator -n flyway-operator
```

Configurable values (image, resources, node selector, tolerations, etc.) are in the chart's
[`values.yaml`](../config/helm-chart/flyway-operator/values.yaml). The chart installs the CRD.

## From source

Mostly useful when developing the operator.

```shell
make docker-build docker-push IMG=<some-registry>/flyway-operator:tag
make deploy IMG=<some-registry>/flyway-operator:tag
```

Remove it again with `make undeploy` (controller) and `make uninstall` (CRDs).

## Upgrading to 0.5.0

- Jobs no longer carry `app.kubernetes.io/name: flyway` and `app.kubernetes.io/instance: <migration>`. Selectors on
  those Job labels (e.g. `kubectl get jobs -l`) must switch to `flyway-operator.davidkarlsen.com/migration: <migration>`.
  Job pods now carry that label and `app.kubernetes.io/managed-by: flyway-operator` too, so NetworkPolicies and
  monitoring can select them; see [Job and pod metadata](usage.md#job-and-pod-metadata).

Metrics are on by default in every install path; see [Metrics](metrics.md) for scraping, RBAC and verification.
