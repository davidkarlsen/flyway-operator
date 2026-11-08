#!/usr/bin/env bash
# Renders the chart with values that must pass and values that must fail the values.schema.json validation.
# Usage: config/helm-chart/test-values.sh (run from anywhere; needs helm).
set -uo pipefail

chart="$(cd "$(dirname "$0")" && pwd)/flyway-operator"
kube=(--kube-version 1.35.0)
digest="sha256:0000000000000000000000000000000000000000000000000000000000000000"
failed=0

render() {
  helm template t "$chart" "${kube[@]}" "$@" 2>&1
}

pass() {
  local out
  if out=$(render "$@"); then
    echo "ok   (renders)  $*"
  else
    echo "FAIL (expected to render) $*"; echo "$out"; failed=1
  fi
}

fail() {
  local out
  if out=$(render "$@"); then
    echo "FAIL (expected a schema error) $*"; failed=1
  elif grep -q "values don't meet the specifications of the schema" <<<"$out"; then
    echo "ok   (rejected) $*"
  else
    echo "FAIL (failed, but not on the schema) $*"; echo "$out"; failed=1
  fi
}

pass
pass --set prometheusRule.enabled=true --api-versions monitoring.coreos.com/v1/PrometheusRule
pass --set grafana.dashboards.enabled=true --api-versions grafana.integreatly.org/v1beta1
pass --set grafana.dashboards.enabled=true --set grafana.dashboards.labels.grafana_dashboard=null
pass --set metrics.serviceMonitor.enabled=true --api-versions monitoring.coreos.com/v1/ServiceMonitor
pass --set image.digest="$digest"
pass --set image.tag=2
pass --set-string service.port=80
pass --set global.imageRegistry=registry.example.com
pass --set resources.limits.cpu=1 --set 'nodeSelector.kubernetes\.io/os=linux' --set podSecurityContext.fsGroup=2000 \
  --set-json 'tolerations=[{"key":"a","operator":"Exists"}]' --set-json 'imagePullSecrets=[{"name":"x"}]'

fail --set prometheusRule.enabled=yes
fail --set image.pullPolicy=Sometimes
fail --set metrics.serviceMonitr.enabled=true
fail --set nodeSelector=foo
fail --set unknownKey=true

# The image reference must follow the values above.
helm template t "$chart" "${kube[@]}" --set image.digest="$digest" | grep -q "flyway-operator@$digest" \
  || { echo "FAIL image.digest is not used in the image reference"; failed=1; }
helm template t "$chart" "${kube[@]}" --set image.tag=2 | grep -q "flyway-operator:2$" \
  || { echo "FAIL numeric image.tag is not used in the image reference"; failed=1; }

exit "$failed"
