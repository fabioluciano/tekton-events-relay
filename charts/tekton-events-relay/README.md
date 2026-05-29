# tekton-events-relay

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

CloudEvents receiver that reports pipeline execution status to multiple SCM providers

**Homepage:** <https://github.com/fabioluciano/tekton-events-relay>

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| fabioluciano |  | <https://github.com/fabioluciano> |

## Source Code

* <https://github.com/fabioluciano/tekton-events-relay>

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity.nodeAffinity.preferredDuringSchedulingIgnoredDuringExecution[0].preference.matchExpressions[0].key | string | `"node-role.kubernetes.io/worker"` |  |
| affinity.nodeAffinity.preferredDuringSchedulingIgnoredDuringExecution[0].preference.matchExpressions[0].operator | string | `"In"` |  |
| affinity.nodeAffinity.preferredDuringSchedulingIgnoredDuringExecution[0].preference.matchExpressions[0].values[0] | string | `"true"` |  |
| affinity.nodeAffinity.preferredDuringSchedulingIgnoredDuringExecution[0].weight | int | `50` |  |
| affinity.podAntiAffinity.preferredDuringSchedulingIgnoredDuringExecution[0].podAffinityTerm.labelSelector.matchExpressions[0].key | string | `"app.kubernetes.io/name"` |  |
| affinity.podAntiAffinity.preferredDuringSchedulingIgnoredDuringExecution[0].podAffinityTerm.labelSelector.matchExpressions[0].operator | string | `"In"` |  |
| affinity.podAntiAffinity.preferredDuringSchedulingIgnoredDuringExecution[0].podAffinityTerm.labelSelector.matchExpressions[0].values[0] | string | `"tekton-events-relay"` |  |
| affinity.podAntiAffinity.preferredDuringSchedulingIgnoredDuringExecution[0].podAffinityTerm.topologyKey | string | `"kubernetes.io/hostname"` |  |
| affinity.podAntiAffinity.preferredDuringSchedulingIgnoredDuringExecution[0].weight | int | `100` |  |
| autoscaling.enabled | bool | `false` |  |
| autoscaling.maxReplicas | int | `10` |  |
| autoscaling.minReplicas | int | `3` |  |
| autoscaling.targetCPUUtilizationPercentage | int | `75` |  |
| autoscaling.targetMemoryUtilizationPercentage | int | `80` |  |
| commonAnnotations | object | `{}` |  |
| commonLabels | object | `{}` |  |
| config.dashboard_url | string | `"https://tekton.company.example.com"` |  |
| config.dedupe_size | int | `10000` |  |
| config.filter.allow_pipelinerun | bool | `true` |  |
| config.filter.allow_taskrun | bool | `false` |  |
| config.filter.ignore_unknown | bool | `true` |  |
| config.providers.azure_devops.enabled | bool | `true` |  |
| config.providers.azure_devops.genre | string | `"tekton-ci"` |  |
| config.providers.bitbucket_cloud.enabled | bool | `true` |  |
| config.providers.bitbucket_server.base_url | string | `"https://bitbucket.company.example.com"` |  |
| config.providers.bitbucket_server.enabled | bool | `false` |  |
| config.providers.gitea.base_url | string | `"https://gitea.company.example.com"` |  |
| config.providers.gitea.enabled | bool | `false` |  |
| config.providers.github.enabled | bool | `true` |  |
| config.providers.gitlab_cloud.base_url | string | `"https://gitlab.com/api/v4"` |  |
| config.providers.gitlab_cloud.enabled | bool | `true` |  |
| config.providers.gitlab_server.base_url | string | `"https://gitlab.company.example.com/api/v4"` |  |
| config.providers.gitlab_server.enabled | bool | `false` |  |
| config.providers.sourcehut.enabled | bool | `false` |  |
| config.server.addr | string | `":8080"` |  |
| config.server.read_timeout_sec | int | `10` |  |
| config.server.write_timeout_sec | int | `10` |  |
| email | string | `"platform@example.com"` |  |
| existingSecret | string | `""` |  |
| fullnameOverride | string | `""` |  |
| image.pullPolicy | string | `"IfNotPresent"` |  |
| image.repository | string | `"ghcr.io/fabioluciano/tekton-events-relay"` |  |
| image.tag | string | `""` |  |
| imagePullSecrets | list | `[]` |  |
| livenessProbe.httpGet.path | string | `"/healthz"` |  |
| livenessProbe.httpGet.port | string | `"http"` |  |
| livenessProbe.initialDelaySeconds | int | `5` |  |
| livenessProbe.periodSeconds | int | `10` |  |
| nameOverride | string | `""` |  |
| nodeSelector | object | `{}` |  |
| owner | string | `"platform-team"` |  |
| podAnnotations | object | `{}` |  |
| podDisruptionBudget.minAvailable | int | `1` |  |
| podSecurityContext.fsGroup | int | `65532` |  |
| podSecurityContext.runAsGroup | int | `65532` |  |
| podSecurityContext.runAsNonRoot | bool | `true` |  |
| podSecurityContext.runAsUser | int | `65532` |  |
| podSecurityContext.seccompProfile.type | string | `"RuntimeDefault"` |  |
| readinessProbe.httpGet.path | string | `"/healthz"` |  |
| readinessProbe.httpGet.port | string | `"http"` |  |
| readinessProbe.periodSeconds | int | `5` |  |
| replicaCount | int | `2` |  |
| resources.limits.cpu | string | `"500m"` |  |
| resources.limits.ephemeral-storage | string | `"1Gi"` |  |
| resources.limits.memory | string | `"256Mi"` |  |
| resources.requests.cpu | string | `"50m"` |  |
| resources.requests.ephemeral-storage | string | `"100Mi"` |  |
| resources.requests.memory | string | `"64Mi"` |  |
| securityContext.allowPrivilegeEscalation | bool | `false` |  |
| securityContext.capabilities.drop[0] | string | `"ALL"` |  |
| securityContext.readOnlyRootFilesystem | bool | `true` |  |
| securityContext.runAsGroup | int | `65532` |  |
| securityContext.runAsNonRoot | bool | `true` |  |
| securityContext.runAsUser | int | `65532` |  |
| securityContext.seccompProfile.type | string | `"RuntimeDefault"` |  |
| service.annotations | object | `{}` |  |
| service.port | int | `80` |  |
| service.targetPort | int | `8080` |  |
| service.type | string | `"ClusterIP"` |  |
| serviceAccount.annotations | object | `{}` |  |
| serviceAccount.automountServiceAccountToken | bool | `false` |  |
| serviceAccount.create | bool | `true` |  |
| serviceAccount.name | string | `""` |  |
| startupProbe.failureThreshold | int | `30` |  |
| startupProbe.httpGet.path | string | `"/healthz"` |  |
| startupProbe.httpGet.port | string | `"http"` |  |
| startupProbe.initialDelaySeconds | int | `0` |  |
| startupProbe.periodSeconds | int | `5` |  |
| tolerations | list | `[]` |  |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.14.2](https://github.com/norwoodj/helm-docs/releases/v1.14.2)
