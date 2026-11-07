# Installing

The operator watches all namespaces. Choose one of the methods below, then continue with [usage](usage.md).

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
     channel: alpha
     source: flyway-operator
     sourceNamespace: openshift-marketplace
     installPlanApproval: Automatic
   ```

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

## Metrics

The operator serves Prometheus metrics on port 8443 over HTTPS (self-signed certificate) at `/metrics`.
Every request is authenticated and authorized against the Kubernetes API, so the scraper needs a bearer token
for a ServiceAccount that may `get` the `/metrics` non-resource URL.

- **OLM and `make deploy`**: metrics are on, and the install includes a `ServiceMonitor`.
  `make deploy` always applies it, so it needs the Prometheus Operator CRDs (`monitoring.coreos.com/v1`);
  without them the apply fails with `no matches for kind "ServiceMonitor"`. On such clusters, remove
  `- ../prometheus` from `config/default/kustomization.yaml` first.
- **Helm**: metrics are on (`metrics.enabled: true`). The `ServiceMonitor` is opt-in:
  `--set metrics.serviceMonitor.enabled=true`. Set `metrics.enabled=false` to turn metrics off.

In both cases, bind the operator's `metrics-reader` ClusterRole to the ServiceAccount Prometheus runs as,
otherwise scrapes get 403. For OLM and `make deploy`:

```shell
kubectl create clusterrolebinding flyway-operator-metrics-reader \
  --clusterrole=flyway-operator-metrics-reader \
  --serviceaccount=<prometheus-namespace>:<prometheus-serviceaccount>
```

For Helm, the ClusterRole is `<release-fullname>-metrics-reader`; `helm install` prints the exact command
(see `helm get notes <release>`), with a binding name unique to the release.
