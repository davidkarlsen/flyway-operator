# Argo CD

Argo CD has no built-in health check for `Migration` and does not read conditions of custom resources it doesn't know,
so a `Migration` has no health and Argo CD cannot wait for it. Add the check below to make it wait.

## The health check

It reads the `Applied` and `Failed` conditions ([status](usage.md#status)) whose `observedGeneration` matches the
current spec:

| `Migration` | Argo CD health |
| --- | --- |
| `Applied=True` for the current generation | Healthy |
| a Job is created, running or retried | Progressing |
| `Failed=True` (`retryLimit` exhausted) | Degraded |
| paused by annotation | Suspended |

With plain Argo CD, add it to `argocd-cm`:

```yaml
data:
  resource.customizations.health.flyway.davidkarlsen.com_Migration: |
    hs = {}
    if obj.metadata.annotations ~= nil and obj.metadata.annotations["flyway-operator.davidkarlsen.com/paused"] == "true" then
      hs.status = "Suspended"
      hs.message = "Migration is paused"
      return hs
    end
    local applied, failed
    if obj.status ~= nil and obj.status.conditions ~= nil then
      for _, c in ipairs(obj.status.conditions) do
        if c.observedGeneration == obj.metadata.generation then
          if c.type == "Applied" then applied = c end
          if c.type == "Failed" then failed = c end
        end
      end
    end
    if failed ~= nil and failed.status == "True" then
      hs.status = "Degraded"
      hs.message = failed.message
    elseif applied ~= nil and applied.status == "True" then
      hs.status = "Healthy"
      hs.message = applied.message
    else
      hs.status = "Progressing"
      hs.message = applied ~= nil and applied.message or "Waiting for the operator to observe the current spec"
    end
    return hs
```

With the Argo CD operator (OpenShift GitOps), which manages `argocd-cm`, set it in the `ArgoCD` resource instead:

```yaml
spec:
  resourceHealthChecks:
    - group: flyway.davidkarlsen.com
      kind: Migration
      check: |
        hs = {}
        if obj.metadata.annotations ~= nil and obj.metadata.annotations["flyway-operator.davidkarlsen.com/paused"] == "true" then
          hs.status = "Suspended"
          hs.message = "Migration is paused"
          return hs
        end
        local applied, failed
        if obj.status ~= nil and obj.status.conditions ~= nil then
          for _, c in ipairs(obj.status.conditions) do
            if c.observedGeneration == obj.metadata.generation then
              if c.type == "Applied" then applied = c end
              if c.type == "Failed" then failed = c end
            end
          end
        end
        if failed ~= nil and failed.status == "True" then
          hs.status = "Degraded"
          hs.message = failed.message
        elseif applied ~= nil and applied.status == "True" then
          hs.status = "Healthy"
          hs.message = applied.message
        else
          hs.status = "Progressing"
          hs.message = applied ~= nil and applied.message or "Waiting for the operator to observe the current spec"
        end
        return hs
```

## Apply the schema before the application

Put the `Migration` in an earlier [sync wave](https://argo-cd.readthedocs.io/en/stable/user-guide/sync-waves/) than
the application. Argo CD starts a later wave only when the earlier one is Healthy, so the application never starts on an
older schema and a failed migration stops the rollout:

```yaml
metadata:
  annotations:
    argocd.argoproj.io/sync-wave: "-1"
```

## Things to know

- Argo CD reads health checks only from its own configuration and its built-in scripts (the `resource_customizations`
  directory), not from ConfigMaps, labels or annotations.
- A check in the instance configuration overrides Argo CD's built-in checks, including those for Deployments and Pods.
  Wildcards are globs where `*` also crosses `/`, so never write one such as `*/*`. Keep it to the exact
  `flyway.davidkarlsen.com/Migration`.
