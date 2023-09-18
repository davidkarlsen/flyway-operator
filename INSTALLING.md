# Installing

There are various ways to install the operator, outlined below.



## From OLM

If you are on OpenShift you will most likely want to install from OLM.

Add the catalog-source:

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

Then go to `operators -> operatorhub` in the console to install, or add a subscription through yaml.



## With helm

Add the helm repo:

```shell
helm repo add flyway-operator https://davidkarlsen.github.io/flyway-operator
```

then install:
```
# create namespace
kubectl create namespace flyway-operator

# install into namespace
helm install flyway-operator flyway-operator/flyway-operator -n flyway-operator
```


## From Source

This is mostly useful for developers of the operator.

1. Install Instances of Custom Resources:

```sh
kubectl apply -f config/samples/
```

2. Build and push your image to the location specified by `IMG`:

```sh
make docker-build docker-push IMG=<some-registry>/flyway-operator:tag
```

3. Deploy the controller to the cluster with the image specified by `IMG`:

```sh
make deploy IMG=<some-registry>/flyway-operator:tag
```

### Uninstall CRDs
To delete the CRDs from the cluster:

```sh
make uninstall
```

### Undeploy controller
UnDeploy the controller from the cluster:

```sh
make undeploy
```