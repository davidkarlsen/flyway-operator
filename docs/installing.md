# Installing

The operator watches all namespaces. Choose one of the methods below, then continue with [usage](usage.md).
Metrics are on by default in every install path; see [Metrics](metrics.md) for scraping, RBAC and verification.

## Requirements

- Kubernetes 1.35 or later; on OpenShift, 4.22 or later. Migration Jobs mount the SQL image as an
  [image volume](https://kubernetes.io/docs/tasks/configure-pod-container/image-volumes/); the `ImageVolume`
  feature gate is on by default from 1.35 (beta) and GA in 1.36
  ([feature gates](https://kubernetes.io/docs/reference/command-line-tools-reference/feature-gates/), [KEP-4639](https://github.com/kubernetes/enhancements/tree/master/keps/sig-node/4639-oci-volume-source)).
- A container runtime with image volume support: CRI-O 1.31 or later, or containerd 2.1 or later
  ([Kubernetes blog](https://kubernetes.io/blog/2025/04/29/kubernetes-v1-33-image-volume-beta/), [containerd 2.1.0](https://github.com/containerd/containerd/releases/tag/v2.1.0)).
- On OpenShift the pod's SCC must allow the `image` volume type (`restricted-v2` does).

## OpenShift (OLM)

Each release is also submitted to OpenShift's Community Operators catalog
([community-operators-prod#11910](https://github.com/redhat-openshift-ecosystem/community-operators-prod/pull/11910)
is the first submission); once it is merged and released, the operator is listed in OperatorHub without the catalog source below.

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

2. Install the operator from **Operators → OperatorHub** in the console, or with a `Subscription`.
   Only the `AllNamespaces` install mode is supported, so subscribe in `openshift-operators`, which already has a
   global `OperatorGroup`:

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
   From v0.7.0, `stable-v0` is the package's default channel. A subscription without `channel` follows it; one with an
   explicit `channel` keeps it.

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

The chart installs the CRD. Configurable values (image, resources, node selector, tolerations, etc.) are in
[`values.yaml`](../config/helm-chart/flyway-operator/values.yaml).

### Upgrading the CRD

Helm creates the CRD on install but never updates it on `helm upgrade`. An old CRD schema silently drops fields it does
not know, for example `flywayConfiguration.resources` or `serviceAccountName`, and the migration runs without them.
After each upgrade that changes the API, apply the CRD of the release yourself. Server-side apply keeps the CRD out of
the 256 KiB last-applied annotation as it grows:

```shell
kubectl apply --server-side --force-conflicts -f https://raw.githubusercontent.com/davidkarlsen/flyway-operator/<tag>/config/crd/bases/flyway.davidkarlsen.com_migrations.yaml
```

Replace `<tag>` with the operator version prefixed with `v` (the chart's `appVersion`), for example `v0.6.0`. The
chart's own copy of the CRD (`config/helm-chart/flyway-operator/crds/`) is only updated by the chart release pull
request after the tag, so at the tag itself it is still the previous release's. OLM updates the CRD with the operator.

## From source

Mostly useful when developing the operator.

```shell
make docker-build docker-push IMG=<some-registry>/flyway-operator:tag
make deploy IMG=<some-registry>/flyway-operator:tag
```

Remove it again with `make undeploy` (controller) and `make uninstall` (CRDs).

## Upgrading to 0.7.0

- The `ReconcileSuccess` condition is gone. It was rewritten with a new timestamp on every reconcile and only meant that
  the reconcile loop ran; the operator removes it from an existing Migration at its next successful reconcile. Read
  `Applied`, `Progressing` and `Failed` instead (the [Argo CD health check](argocd.md) already does).
- `ReconcileError` is now removed again by the next successful reconcile; before, it stayed forever once set. Its
  `lastTransitionTime` only moves when the condition appears. Failures still emit a `ProcessingError` Warning event.
- The operator writes a Migration's status only when it changed, so idle Migrations no longer see a status update on
  every reconcile.

### Before you upgrade

- **Helm: apply the CRD first.** Helm does not update CRDs, and the new CRD adds `status.schemaVersion`, the Job pod
  fields and optional credentials; the API server drops them until you apply it. Use the command in
  [Upgrading the CRD](#upgrading-the-crd) with tag `v0.7.0`. OLM and kustomize update the CRD themselves.
- **RBAC.** The operator needs `pods: list` and `events: list` (to say why a pod does not start) and `pods/log: get`
  (to read Flyway's output). OLM and kustomize grant them. Helm gets them with chart 0.6.0; until then the Migrations
  still run, but the status shows plain messages, a never-started Job is waited for rather than replaced, and there is
  no `schemaVersion` and no Flyway error.
- **Alerts.** A `PrometheusRule` with four alerts is now installed by default with OLM and kustomize (it needs the
  Prometheus Operator CRDs), and is opt-in in Helm with chart 0.6.0. To turn it off, see
  [Shipped rules](metrics.md#shipped-rules); the alerts are explained in the [runbook](runbook.md).
- **Helm: values are validated.** Chart 0.6.0 ships a `values.schema.json`; a value of the wrong type or an unknown
  key fails `helm upgrade`, so check your values file against the chart's `values.yaml` first.
- **OLM channel.** `stable-v0` is now the default channel, see [OpenShift (OLM)](#openshift-olm).

### New in 0.7.0

- `database.username` and `credentials` are optional. With an empty username the Job sets no `FLYWAY_USER`; see
  [Passwordless authentication](usage.md#passwordless-authentication).
- Resources, node selector, affinity, tolerations, priority class and service account for the Job's pod; see
  [Resources, scheduling and service account](usage.md#resources-scheduling-and-service-account).
- `status.schemaVersion` and the `SCHEMA VERSION` column, and Flyway's error in the failure message; see
  [Status](usage.md#status).
- The `Progressing` message says why the pod does not start, and a Job that never started a container is replaced
  when the spec changes; see [When the pod does not start](usage.md#when-the-pod-does-not-start).

## Upgrading to 0.6.0

v0.6.0 requires Kubernetes 1.35 or later, see [Requirements](#requirements); v0.5.x is the last release for older
clusters. The SQL files are mounted at `/flyway/migration-source`, not `/flyway/sql`: update a `FLYWAY_LOCATIONS` you set
yourself. A location that does not exist fails the Job (`FLYWAY_FAIL_ON_MISSING_LOCATIONS=true`).

## Upgrading to 0.5.0

- Jobs no longer carry `app.kubernetes.io/name: flyway` and `app.kubernetes.io/instance: <migration>`. Selectors on
  those Job labels (e.g. `kubectl get jobs -l`) must switch to `flyway-operator.davidkarlsen.com/migration: <migration>`.
  Job pods now carry that label and `app.kubernetes.io/managed-by: flyway-operator` too, so NetworkPolicies and
  monitoring can select them; see [Job and pod metadata](usage.md#job-and-pod-metadata).
