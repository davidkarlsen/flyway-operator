# Installing

The operator watches all namespaces. Choose one of the methods below, then continue with [usage](usage.md).

## Requirements

**Breaking (v0.6.0):** requires Kubernetes 1.35 or later and containerd 2.1 or later / CRI-O 1.31 or later.
v0.5.x is the last release for older clusters.

- Kubernetes 1.35 or later. Migration Jobs mount the SQL image as an
  [image volume](https://kubernetes.io/docs/tasks/configure-pod-container/image-volumes/); the `ImageVolume`
  feature gate is on by default from 1.35 (beta) and GA in 1.36
  ([feature gates](https://kubernetes.io/docs/reference/command-line-tools-reference/feature-gates/), [KEP-4639](https://github.com/kubernetes/enhancements/tree/master/keps/sig-node/4639-oci-volume-source)).
- A container runtime with image volume support: CRI-O 1.31 or later, or containerd 2.1 or later
  ([Kubernetes blog](https://kubernetes.io/blog/2025/04/29/kubernetes-v1-33-image-volume-beta/), [containerd 2.1.0](https://github.com/containerd/containerd/releases/tag/v2.1.0)).
- On OpenShift the pod's SCC must allow the `image` volume type (`restricted-v2` does).

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

Metrics are on by default in every install path; see [Metrics](metrics.md) for scraping, RBAC and verification.
