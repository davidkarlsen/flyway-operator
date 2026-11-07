#!/usr/bin/env bash
# Creates (idempotently) the kind cluster and local registry used by `make test-e2e`.
# Follows https://kind.sigs.k8s.io/docs/user/local-registry/.
set -euo pipefail

: "${KIND:=kind}"
: "${KIND_CLUSTER:=flyway-operator-e2e}"
: "${KIND_KUBECONFIG:?KIND_KUBECONFIG must be set}"
: "${E2E_REGISTRY_NAME:=flyway-operator-e2e-registry}"
: "${E2E_REGISTRY_PORT:=5001}"

dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [ "$(docker inspect -f '{{.State.Running}}' "${E2E_REGISTRY_NAME}" 2>/dev/null || true)" != true ]; then
  docker rm -f "${E2E_REGISTRY_NAME}" >/dev/null 2>&1 || true
  docker run -d --restart=always -p "127.0.0.1:${E2E_REGISTRY_PORT}:5000" --network bridge \
    --name "${E2E_REGISTRY_NAME}" docker.io/library/registry:2
fi

if ! "${KIND}" get clusters | grep -qx "${KIND_CLUSTER}"; then
  "${KIND}" create cluster --name "${KIND_CLUSTER}" --config "${dir}/kind-config.yaml" \
    --kubeconfig "${KIND_KUBECONFIG}" --wait 120s
fi
"${KIND}" get kubeconfig --name "${KIND_CLUSTER}" > "${KIND_KUBECONFIG}"

registry_dir="/etc/containerd/certs.d/localhost:${E2E_REGISTRY_PORT}"
for node in $("${KIND}" get nodes --name "${KIND_CLUSTER}"); do
  docker exec "${node}" mkdir -p "${registry_dir}"
  printf '[host."http://%s:5000"]\n' "${E2E_REGISTRY_NAME}" | docker exec -i "${node}" cp /dev/stdin "${registry_dir}/hosts.toml"
done

if [ "$(docker inspect -f '{{json .NetworkSettings.Networks.kind}}' "${E2E_REGISTRY_NAME}")" = null ]; then
  docker network connect kind "${E2E_REGISTRY_NAME}"
fi
