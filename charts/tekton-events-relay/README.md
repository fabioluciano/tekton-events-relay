# tekton-events-relay

![Version: 0.7.4-rc.2](https://img.shields.io/badge/Version-0.7.4--rc.2-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.7.4-rc.2](https://img.shields.io/badge/AppVersion-0.7.4--rc.2-informational?style=flat-square)

**Your pipelines run. Your platforms get updated. You write zero notification code.**

Tekton Events Relay turns the CloudEvents your Tekton pipelines already emit into commit statuses, PR comments, labels, deployments and alerts — across **6 SCM platforms** (GitHub, GitLab, Gitea, Bitbucket, Azure DevOps, SourceHut) and **8 notification channels** (Slack, Teams, Discord, PagerDuty, Datadog, Grafana, Sentry, webhooks). Routing is declared with CEL expressions and a one-time set of annotations on your `PipelineRun`s — **your pipelines never change**.

**Homepage:** <https://github.com/fabioluciano/tekton-events-relay>

📖 **[Full documentation](https://github.com/fabioluciano/tekton-events-relay/wiki)** — [Quickstart](https://github.com/fabioluciano/tekton-events-relay/wiki/Quickstart) · [Annotations contract](https://github.com/fabioluciano/tekton-events-relay/wiki/Annotations) · [Configuration reference](https://github.com/fabioluciano/tekton-events-relay/wiki/Configuration-Reference) · [Operations](https://github.com/fabioluciano/tekton-events-relay/wiki/Operations)

## Highlights

- **Self-updating PR comments** (`mode: upsert`) — one comment per run, edited in place; idempotent across retries, restarts and replicas.
- **Granular required checks** (`context_per_task`) — one commit status per Tekton task, ready for branch protection rules.
- **Declarative labels** — add/remove lists with Go templates, gated by a CEL `when`.
- **Deployment tracking** — GitHub/GitLab Environments pages, Grafana deploy markers, Sentry releases.
- **Reliability built in** — backoff+jitter retries honoring `Retry-After`, HTTP 503 back-pressure, dead letter queue with replay API, per-handler timeouts.
- **Multi-replica correctness** — pluggable state backends: in-memory, **Valkey**, or **embedded Olric** (no extra deployment; this chart wires the gossip Service and NetworkPolicy automatically).
- **Operations friendly** — hot config reload (ConfigMap edits apply without restart), 20+ Prometheus metrics + ServiceMonitor, OpenTelemetry tracing, diagnostic `/readyz`.

## Installation

```bash
helm install tekton-events-relay \
  oci://ghcr.io/fabioluciano/charts/tekton-events-relay \
  --version 0.7.4-rc.2 \
  --namespace tekton-events-relay --create-namespace \
  -f values.yaml
```

### Minimal working example

```yaml
# values.yaml
replicaCount: 1        # >1 requires config.store.backend: valkey|olric — see the wiki

config:
  scm:
    github:
      - name: github                       # matched by the scm.provider annotation
        enabled: true
        auth:
          secret_name: github-token        # Secret with key "token"
        actions:
          - name: ci-status
            type: commit_status
            enabled: true
          - name: pr-summary
            type: pr_comment
            enabled: true
            mode: upsert
            when: 'isPipelineRun() && stateIn("success", "failure")'

  notifiers:
    slack:
      - name: prod-alerts
        enabled: true
        secret_name: slack-webhook         # Secret with key "webhook_url"
        channel: "#prod-alerts"
        when: 'event.Namespace == "production" && stateIn("failure", "error")'
```

```bash
kubectl create secret generic github-token -n tekton-events-relay \
  --from-literal=token="ghp_..."
```

Then point Tekton at the relay (ConfigMap `config-defaults` in `tekton-pipelines`):

```yaml
data:
  default-cloud-events-sink: http://tekton-events-relay.tekton-events-relay.svc.cluster.local
```

…and [annotate your PipelineRuns](https://github.com/fabioluciano/tekton-events-relay/wiki/Annotations) in your TriggerTemplate. That's the whole integration.

## Prerequisites

- Kubernetes **1.24+**, Helm **3.8+**
- Tekton Pipelines **v0.40+** with CloudEvents enabled

## Signature verification

Images and charts are signed with [Cosign](https://github.com/sigstore/cosign) (keyless OIDC; signatures in the [Rekor](https://rekor.sigstore.dev/) transparency log):

```bash
cosign verify \
  --certificate-identity-regexp='https://github.com/fabioluciano/tekton-events-relay' \
  --certificate-oidc-issuer='https://token.actions.githubusercontent.com' \
  ghcr.io/fabioluciano/tekton-events-relay:0.7.4-rc.2

cosign verify \
  --certificate-identity-regexp='https://github.com/fabioluciano/tekton-events-relay' \
  --certificate-oidc-issuer='https://token.actions.githubusercontent.com' \
  oci://ghcr.io/fabioluciano/charts/tekton-events-relay:0.7.4-rc.2
```

## Scaling note

The default in-memory state backend is per-pod: run **one replica**, or set `config.store.backend` to `valkey`/`olric` before scaling out or enabling the HPA — details in [Operations → State backends](https://github.com/fabioluciano/tekton-events-relay/wiki/Operations#state-backends).

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| fabioluciano | <hi@fabioluciano.com> | <https://fabioluciano.com> |

## Source Code

* <https://github.com/fabioluciano/tekton-events-relay>

## Requirements

| Repository | Name | Version |
|------------|------|---------|
| https://valkey.io/valkey-helm/ | valkey | 0.9.4 |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity.mode | string | `"soft"` |  |
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
| config.accumulator | object | `{"enabled":false,"max_size":100,"ttl":"30s"}` | Event accumulation configuration (batch multiple events) |
| config.accumulator.max_size | int | `100` | Maximum number of events to accumulate |
| config.accumulator.ttl | string | `"30s"` | Time to live for accumulated events |
| config.dashboard_url | string | `"https://tekton.company.example.com"` | Base URL for the Tekton dashboard (used in notification links) |
| config.dedupe_size | int | `10000` | Size of the deduplication cache to prevent duplicate notifications |
| config.dlq | object | `{"enabled":false,"max_size_bytes":10485760,"path":"/var/lib/tekton-events-relay/dlq.jsonl","volumeSizeLimit":"32Mi"}` | Dead letter queue configuration |
| config.dlq.enabled | bool | `false` | Enable the dead letter queue |
| config.dlq.max_size_bytes | int | `10485760` | Maximum file size in bytes before oldest entries are dropped |
| config.dlq.path | string | `"/var/lib/tekton-events-relay/dlq.jsonl"` | JSONL file path (mounted as a writable emptyDir by the chart) |
| config.dlq.volumeSizeLimit | string | `"32Mi"` | Size limit of the emptyDir volume backing the DLQ |
| config.filter | object | `{"allow_customrun":false,"allow_eventlistener":false,"allow_pipelinerun":true,"allow_taskrun":true,"ignore_unknown":true}` | Event filtering configuration |
| config.filter.allow_customrun | bool | `false` | Process CustomRun events |
| config.filter.allow_eventlistener | bool | `false` | Process EventListener events |
| config.filter.allow_pipelinerun | bool | `true` | Process PipelineRun events |
| config.filter.allow_taskrun | bool | `true` | Process TaskRun events |
| config.filter.ignore_unknown | bool | `true` | Ignore unknown event types |
| config.handler_timeout | string | `"10s"` | Per-handler execution deadline; one slow provider cannot stall the whole event dispatch (Go duration format) |
| config.jira | list | `[{"actions":[{"enabled":true,"name":"result-comment","type":"comment","when":"event.Resource == \"pipelinerun\" && stateIn(\"success\", \"failure\", \"error\")"},{"enabled":false,"name":"mark-done","transition":"Done","type":"transition","when":"event.Resource == \"pipelinerun\" && event.State == \"success\""}],"auth":{},"base_url":"https://yourorg.atlassian.net","enabled":false,"name":"default"}]` | Jira integration instances (Cloud or Data Center) |
| config.jira[0] | object | `{"actions":[{"enabled":true,"name":"result-comment","type":"comment","when":"event.Resource == \"pipelinerun\" && stateIn(\"success\", \"failure\", \"error\")"},{"enabled":false,"name":"mark-done","transition":"Done","type":"transition","when":"event.Resource == \"pipelinerun\" && event.State == \"success\""}],"auth":{},"base_url":"https://yourorg.atlassian.net","enabled":false,"name":"default"}` | Unique identifier for this Jira instance |
| config.jira[0].actions | list | `[{"enabled":true,"name":"result-comment","type":"comment","when":"event.Resource == \"pipelinerun\" && stateIn(\"success\", \"failure\", \"error\")"},{"enabled":false,"name":"mark-done","transition":"Done","type":"transition","when":"event.Resource == \"pipelinerun\" && event.State == \"success\""}]` | Skip TLS verification (self-hosted Data Center only) insecure_skip_verify: false |
| config.jira[0].actions[0] | object | `{"enabled":true,"name":"result-comment","type":"comment","when":"event.Resource == \"pipelinerun\" && stateIn(\"success\", \"failure\", \"error\")"}` | Comment on the linked issue when the pipeline finishes |
| config.jira[0].actions[1] | object | `{"enabled":false,"name":"mark-done","transition":"Done","type":"transition","when":"event.Resource == \"pipelinerun\" && event.State == \"success\""}` | Move the card when the pipeline succeeds (disabled by default) |
| config.jira[0].base_url | string | `"https://yourorg.atlassian.net"` | Jira base URL (Cloud: https://yourorg.atlassian.net) |
| config.jira[0].enabled | bool | `false` | Enable or disable this Jira instance |
| config.logging | object | `{"level":"info","verbose":{"caller":false,"http_calls":false,"payloads":false}}` | Logging configuration |
| config.logging.level | string | `"info"` | Log level (debug, info, warn, error) |
| config.logging.verbose | object | `{"caller":false,"http_calls":false,"payloads":false}` | Verbose logging options |
| config.logging.verbose.caller | bool | `false` | Include caller information in logs |
| config.logging.verbose.http_calls | bool | `false` | Log HTTP calls |
| config.logging.verbose.payloads | bool | `false` | Log event payloads |
| config.notifiers.datadog | list | `[{"enabled":false,"name":"main-instance","site":"datadoghq.com","tags":["env:production","team:platform","service:tekton-events-relay"],"when":"event.State == \"success\" || event.State == \"failure\" || event.State == \"error\""}]` | Datadog event tracking configuration. Supports multiple instances with independent API keys and sites. |
| config.notifiers.datadog[0] | object | `{"enabled":false,"name":"main-instance","site":"datadoghq.com","tags":["env:production","team:platform","service:tekton-events-relay"],"when":"event.State == \"success\" || event.State == \"failure\" || event.State == \"error\""}` | Unique identifier for this Datadog instance configuration |
| config.notifiers.datadog[0].enabled | bool | `false` | Enable or disable this Datadog notifier instance |
| config.notifiers.datadog[0].site | string | `"datadoghq.com"` | Datadog site (datadoghq.com, datadoghq.eu, us3.datadoghq.com, etc.) |
| config.notifiers.datadog[0].tags | list | `["env:production","team:platform","service:tekton-events-relay"]` | Custom tags appended to auto-generated tags (state, context, namespace are added automatically) |
| config.notifiers.datadog[0].when | string | `"event.State == \"success\" || event.State == \"failure\" || event.State == \"error\""` | CEL expression to filter which events are sent to Datadog |
| config.notifiers.discord | list | `[{"enabled":false,"name":"main-channel","template":"**Pipeline {{.State}}** in `{{.Namespace}}`\n```\nRun:    {{.RunName}}\nCommit: {{.CommitSHA}}\n```\n{{if .TargetURL}}[View in Dashboard]({{.TargetURL}}){{end}}\n","username":"Tekton CI","when":"event.Namespace == \"staging\" || event.Namespace == \"production\""}]` | Discord notification channels configuration. Supports multiple channels with independent webhook secrets and templates. |
| config.notifiers.discord[0] | object | `{"enabled":false,"name":"main-channel","template":"**Pipeline {{.State}}** in `{{.Namespace}}`\n```\nRun:    {{.RunName}}\nCommit: {{.CommitSHA}}\n```\n{{if .TargetURL}}[View in Dashboard]({{.TargetURL}}){{end}}\n","username":"Tekton CI","when":"event.Namespace == \"staging\" || event.Namespace == \"production\""}` | Unique identifier for this Discord channel configuration |
| config.notifiers.discord[0].enabled | bool | `false` | Enable or disable this Discord notifier instance |
| config.notifiers.discord[0].template | string | `"**Pipeline {{.State}}** in `{{.Namespace}}`\n```\nRun:    {{.RunName}}\nCommit: {{.CommitSHA}}\n```\n{{if .TargetURL}}[View in Dashboard]({{.TargetURL}}){{end}}\n"` | Go template for the Discord message body. Available variables: .State, .RunName, .Namespace, .CommitSHA, .TargetURL |
| config.notifiers.discord[0].username | string | `"Tekton CI"` | Display name for the bot posting messages |
| config.notifiers.discord[0].when | string | `"event.Namespace == \"staging\" || event.Namespace == \"production\""` | CEL expression to filter which events trigger notifications |
| config.notifiers.email | list | `[{"enabled":false,"encryption":"starttls","from":"tekton@example.com","host":"smtp.example.com","html":false,"name":"default","port":587,"to":["platform-team@example.com"],"when":"event.Resource == \"pipelinerun\" && stateIn(\"failure\", \"error\")"}]` | SMTP email notifier instances. The most universal channel: any relay (corporate SMTP, SES, Mailgun, in-cluster Postfix) works. |
| config.notifiers.email[0] | object | `{"enabled":false,"encryption":"starttls","from":"tekton@example.com","host":"smtp.example.com","html":false,"name":"default","port":587,"to":["platform-team@example.com"],"when":"event.Resource == \"pipelinerun\" && stateIn(\"failure\", \"error\")"}` | Unique identifier for this email instance |
| config.notifiers.email[0].enabled | bool | `false` | Enable or disable this email notifier instance |
| config.notifiers.email[0].encryption | string | `"starttls"` | Connection security: starttls (default), tls or none. NOTE: with "none", AUTH is only attempted against localhost relays. |
| config.notifiers.email[0].from | string | `"tekton@example.com"` | Sender address |
| config.notifiers.email[0].host | string | `"smtp.example.com"` | SMTP server hostname |
| config.notifiers.email[0].html | bool | `false` | Send body as text/html instead of text/plain |
| config.notifiers.email[0].port | int | `587` | SMTP port (587 STARTTLS, 465 implicit TLS, 25 plain relays) |
| config.notifiers.email[0].to | list | `["platform-team@example.com"]` | Recipient list |
| config.notifiers.email[0].when | string | `"event.Resource == \"pipelinerun\" && stateIn(\"failure\", \"error\")"` | CEL expression to filter which events are emailed |
| config.notifiers.grafana | list | `[]` | ----------------------------------------------------------------------- |
| config.notifiers.pagerduty | list | `[{"enabled":false,"name":"main-service","severity":"critical","when":"event.State == \"failure\" || event.State == \"error\""}]` | PagerDuty incident services configuration. Supports multiple services with independent integration keys. |
| config.notifiers.pagerduty[0] | object | `{"enabled":false,"name":"main-service","severity":"critical","when":"event.State == \"failure\" || event.State == \"error\""}` | Unique identifier for this PagerDuty service configuration |
| config.notifiers.pagerduty[0].enabled | bool | `false` | Enable or disable this PagerDuty notifier instance |
| config.notifiers.pagerduty[0].severity | string | `"critical"` | Incident severity level (critical, error, warning, info) |
| config.notifiers.pagerduty[0].when | string | `"event.State == \"failure\" || event.State == \"error\""` | CEL expression to filter which events trigger incidents |
| config.notifiers.sentry | list | `[]` | ----------------------------------------------------------------------- |
| config.notifiers.slack | list | `[{"channel":"#ci-notifications","enabled":false,"icon_emoji":":robot_face:","name":"main-channel","template":":warning: *Pipeline {{.State}}*\n*Run:* `{{.RunName}}`\n*Commit:* `{{.CommitSHA}}`\n{{if .TargetURL}}<{{.TargetURL}}|View in Dashboard>{{end}}\n","username":"Tekton CI","when":"event.State == \"failure\" || event.State == \"error\""}]` | Slack notification channels configuration. Supports multiple channels with independent webhook secrets and templates. |
| config.notifiers.slack[0] | object | `{"channel":"#ci-notifications","enabled":false,"icon_emoji":":robot_face:","name":"main-channel","template":":warning: *Pipeline {{.State}}*\n*Run:* `{{.RunName}}`\n*Commit:* `{{.CommitSHA}}`\n{{if .TargetURL}}<{{.TargetURL}}|View in Dashboard>{{end}}\n","username":"Tekton CI","when":"event.State == \"failure\" || event.State == \"error\""}` | Unique identifier for this Slack channel configuration |
| config.notifiers.slack[0].channel | string | `"#ci-notifications"` | Slack channel to post notifications (e.g., #ci-notifications, @username) |
| config.notifiers.slack[0].enabled | bool | `false` | Enable or disable this Slack notifier instance |
| config.notifiers.slack[0].icon_emoji | string | `":robot_face:"` | Emoji icon for the bot (e.g., :robot_face:, :warning:) |
| config.notifiers.slack[0].template | string | `":warning: *Pipeline {{.State}}*\n*Run:* `{{.RunName}}`\n*Commit:* `{{.CommitSHA}}`\n{{if .TargetURL}}<{{.TargetURL}}|View in Dashboard>{{end}}\n"` | Go template for the Slack message body. Available variables: .State, .RunName, .CommitSHA, .TargetURL |
| config.notifiers.slack[0].username | string | `"Tekton CI"` | Display name for the bot posting messages |
| config.notifiers.slack[0].when | string | `"event.State == \"failure\" || event.State == \"error\""` | CEL expression to filter which events trigger notifications |
| config.notifiers.teams | list | `[{"enabled":false,"name":"main-channel","template":"**Pipeline {{.State}}**\n| Field | Value |\n|-------|-------|\n| Run | {{.RunName}} |\n| Namespace | {{.Namespace}} |\n| Commit | {{.CommitSHA}} |\n{{if .TargetURL}}[View in Dashboard]({{.TargetURL}}){{end}}\n","when":"event.State == \"success\" || event.State == \"failure\" || event.State == \"error\""}]` | Microsoft Teams notification channels configuration. Supports multiple channels with independent webhook secrets and templates. |
| config.notifiers.teams[0] | object | `{"enabled":false,"name":"main-channel","template":"**Pipeline {{.State}}**\n| Field | Value |\n|-------|-------|\n| Run | {{.RunName}} |\n| Namespace | {{.Namespace}} |\n| Commit | {{.CommitSHA}} |\n{{if .TargetURL}}[View in Dashboard]({{.TargetURL}}){{end}}\n","when":"event.State == \"success\" || event.State == \"failure\" || event.State == \"error\""}` | Unique identifier for this Teams channel configuration |
| config.notifiers.teams[0].enabled | bool | `false` | Enable or disable this Teams notifier instance |
| config.notifiers.teams[0].template | string | `"**Pipeline {{.State}}**\n| Field | Value |\n|-------|-------|\n| Run | {{.RunName}} |\n| Namespace | {{.Namespace}} |\n| Commit | {{.CommitSHA}} |\n{{if .TargetURL}}[View in Dashboard]({{.TargetURL}}){{end}}\n"` | Go template for the Teams message body. Available variables: .State, .RunName, .Namespace, .CommitSHA, .TargetURL |
| config.notifiers.teams[0].when | string | `"event.State == \"success\" || event.State == \"failure\" || event.State == \"error\""` | CEL expression to filter which events trigger notifications |
| config.notifiers.webhook | list | `[{"enabled":false,"headers":{"X-Source":"tekton-events-relay"},"name":"main-webhook","when":"event.State == \"success\" || event.State == \"failure\" || event.State == \"error\""}]` | Generic webhook endpoints configuration. Supports multiple webhooks with custom headers and payloads. |
| config.notifiers.webhook[0] | object | `{"enabled":false,"headers":{"X-Source":"tekton-events-relay"},"name":"main-webhook","when":"event.State == \"success\" || event.State == \"failure\" || event.State == \"error\""}` | Unique identifier for this webhook configuration |
| config.notifiers.webhook[0].enabled | bool | `false` | Enable or disable this webhook notifier instance |
| config.notifiers.webhook[0].headers | object | `{"X-Source":"tekton-events-relay"}` | Custom HTTP headers sent with the webhook request (e.g., authentication tokens, routing keys) |
| config.notifiers.webhook[0].when | string | `"event.State == \"success\" || event.State == \"failure\" || event.State == \"error\""` | CEL expression to filter which events trigger webhook calls |
| config.retry | object | `{"initial_backoff":"250ms","max_attempts":4,"max_backoff":"30s"}` | Outbound HTTP retry configuration |
| config.retry.initial_backoff | string | `"250ms"` | First backoff delay (Go duration format) |
| config.retry.max_attempts | int | `4` | Total attempts including the first request |
| config.retry.max_backoff | string | `"30s"` | Backoff ceiling (Go duration format) |
| config.scm.azure_devops | list | `[{"actions":[{"enabled":false,"name":"commit-status","type":"commit_status","when":""},{"enabled":false,"labels":{"add":["{{ if eq .State \"success\" }}build-passed{{ else }}build-failed{{ end }}"],"remove":["{{ if eq .State \"success\" }}build-failed{{ else }}build-passed{{ end }}"]},"name":"label","type":"label","when":""}],"base_url":"https://dev.azure.com","enabled":false,"genre":"tekton-ci","insecure_skip_verify":false,"name":"default"}]` | Azure DevOps SCM provider configuration |
| config.scm.azure_devops[0].actions | list | `[{"enabled":false,"name":"commit-status","type":"commit_status","when":""},{"enabled":false,"labels":{"add":["{{ if eq .State \"success\" }}build-passed{{ else }}build-failed{{ end }}"],"remove":["{{ if eq .State \"success\" }}build-failed{{ else }}build-passed{{ end }}"]},"name":"label","type":"label","when":""}]` | Actions to perform when events are received |
| config.scm.azure_devops[0].actions[0].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.azure_devops[0].actions[0].type | string | `"commit_status"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.azure_devops[0].actions[1].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.azure_devops[0].actions[1].labels | object | `{"add":["{{ if eq .State \"success\" }}build-passed{{ else }}build-failed{{ end }}"],"remove":["{{ if eq .State \"success\" }}build-failed{{ else }}build-passed{{ end }}"]}` | Declarative label effect: add/remove lists, Go-templated against the event |
| config.scm.azure_devops[0].actions[1].type | string | `"label"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.azure_devops[0].base_url | string | `"https://dev.azure.com"` | API base URL for Azure DevOps |
| config.scm.azure_devops[0].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.azure_devops[0].genre | string | `"tekton-ci"` | Build status context/genre identifier |
| config.scm.azure_devops[0].insecure_skip_verify | bool | `false` | Skip TLS verification (not recommended for production) |
| config.scm.bitbucket | list | `[{"actions":[{"enabled":false,"name":"commit-status","type":"commit_status","when":""}],"enabled":false,"name":"cloud","variant":"cloud"},{"actions":[{"enabled":false,"name":"commit-status","type":"commit_status","when":""}],"base_url":"https://bitbucket.company.example.com","enabled":false,"name":"server","variant":"server"}]` | Bitbucket SCM provider configuration |
| config.scm.bitbucket[0].actions | list | `[{"enabled":false,"name":"commit-status","type":"commit_status","when":""}]` | Actions to perform when events are received |
| config.scm.bitbucket[0].actions[0].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.bitbucket[0].actions[0].type | string | `"commit_status"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.bitbucket[0].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.bitbucket[0].variant | string | `"cloud"` | SCM variant (cloud or server) |
| config.scm.bitbucket[1].actions | list | `[{"enabled":false,"name":"commit-status","type":"commit_status","when":""}]` | Actions to perform when events are received |
| config.scm.bitbucket[1].actions[0].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.bitbucket[1].actions[0].type | string | `"commit_status"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.bitbucket[1].base_url | string | `"https://bitbucket.company.example.com"` | API base URL for the SCM provider |
| config.scm.bitbucket[1].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.bitbucket[1].variant | string | `"server"` | SCM variant (cloud or server) |
| config.scm.gitea | list | `[{"actions":[{"enabled":false,"name":"commit-status","type":"commit_status","when":""},{"enabled":false,"name":"pr-comment","template":"## Pipeline {{.State}}\n**Run:** {{.RunName}}\n**Commit:** {{.CommitSHA}}\n{{if .TargetURL}}[View Results]({{.TargetURL}}){{end}}\n","type":"pr_comment","when":"stateIn(\"success\", \"failure\")"},{"enabled":false,"name":"issue-comment","template":"Pipeline {{.Context}} finished: **{{.State}}**\n{{if .TargetURL}}[Results]({{.TargetURL}}){{end}}\n","type":"issue_comment","when":"stateIn(\"failure\", \"error\")"},{"enabled":false,"labels":{"add":["{{ if eq .State \"success\" }}ci:passed{{ else }}ci:failed{{ end }}"],"remove":["{{ if eq .State \"success\" }}ci:failed{{ else }}ci:passed{{ end }}"]},"name":"label","type":"label","when":""}],"base_url":"https://gitea.company.example.com","enabled":false,"name":"default"}]` | Gitea SCM provider configuration |
| config.scm.gitea[0].actions | list | `[{"enabled":false,"name":"commit-status","type":"commit_status","when":""},{"enabled":false,"name":"pr-comment","template":"## Pipeline {{.State}}\n**Run:** {{.RunName}}\n**Commit:** {{.CommitSHA}}\n{{if .TargetURL}}[View Results]({{.TargetURL}}){{end}}\n","type":"pr_comment","when":"stateIn(\"success\", \"failure\")"},{"enabled":false,"name":"issue-comment","template":"Pipeline {{.Context}} finished: **{{.State}}**\n{{if .TargetURL}}[Results]({{.TargetURL}}){{end}}\n","type":"issue_comment","when":"stateIn(\"failure\", \"error\")"},{"enabled":false,"labels":{"add":["{{ if eq .State \"success\" }}ci:passed{{ else }}ci:failed{{ end }}"],"remove":["{{ if eq .State \"success\" }}ci:failed{{ else }}ci:passed{{ end }}"]},"name":"label","type":"label","when":""}]` | Actions to perform when events are received |
| config.scm.gitea[0].actions[0].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitea[0].actions[0].type | string | `"commit_status"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.gitea[0].actions[1].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitea[0].actions[1].type | string | `"pr_comment"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.gitea[0].actions[2].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitea[0].actions[2].type | string | `"issue_comment"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.gitea[0].actions[3].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitea[0].actions[3].labels | object | `{"add":["{{ if eq .State \"success\" }}ci:passed{{ else }}ci:failed{{ end }}"],"remove":["{{ if eq .State \"success\" }}ci:failed{{ else }}ci:passed{{ end }}"]}` | Declarative label effect: add/remove lists, Go-templated against the event |
| config.scm.gitea[0].actions[3].type | string | `"label"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.gitea[0].base_url | string | `"https://gitea.company.example.com"` | API base URL for the SCM provider |
| config.scm.gitea[0].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.github | list | `[{"actions":[{"enabled":false,"name":"commit-status","type":"commit_status","when":"event.Resource == \"pipelinerun\""},{"enabled":false,"name":"check-run","template":"## Build Result: {{.State}}\n**Run:** {{.RunName}}\n**Namespace:** {{.Namespace}}\n{{if .TargetURL}}[View Logs]({{.TargetURL}}){{end}}\n","type":"check_run","when":"stateIn(\"success\", \"failure\", \"error\")"},{"enabled":false,"name":"deployment-status","type":"deployment_status","when":"stateIn(\"success\", \"failure\")"},{"enabled":false,"mode":"create","name":"pr-comment","template":"## Pipeline {{.State}}\n**Run:** {{.RunName}}\n**Commit:** {{.CommitSHA}}\n{{if .TargetURL}}[View Results]({{.TargetURL}}){{end}}\n","type":"pr_comment","when":"stateIn(\"success\", \"failure\")"},{"enabled":false,"name":"issue-comment","template":"Pipeline {{.Context}} finished with state: **{{.State}}**\n{{if .TargetURL}}[View Results]({{.TargetURL}}){{end}}\n","type":"issue_comment","when":"stateIn(\"failure\", \"error\")"},{"enabled":false,"name":"discussion-comment","template":"## Pipeline {{.State}}\n**Run:** {{.RunName}}\n**Commit:** {{.CommitSHA}}\n{{if .TargetURL}}[View Results]({{.TargetURL}}){{end}}\n","type":"discussion_comment","when":"stateIn(\"success\", \"failure\")"},{"enabled":false,"labels":{"add":[{"color":"0e8a16","name":"ci:passed"},{"name":"ready-to-merge"}],"remove":[{"name":"ci:failed"}]},"name":"label","type":"label","when":"event.Resource == \"pipelinerun\""}],"auth":{"secretRef":{"key":"token","name":"github-default-token"}},"base_url":"https://api.github.com","enabled":false,"name":"default"}]` | GitHub SCM provider configuration |
| config.scm.github[0].actions[0].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.github[0].actions[0].type | string | `"commit_status"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.github[0].actions[1].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.github[0].actions[1].type | string | `"check_run"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.github[0].actions[2].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.github[0].actions[2].type | string | `"deployment_status"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.github[0].actions[3].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.github[0].actions[3].mode | string | `"create"` | Comment mode: "create" posts a new comment per event; "upsert" embeds an invisible marker and edits the existing comment for the same run, making the action idempotent across retries, restarts and multiple replicas. Supported by GitHub, Gitea and Bitbucket Cloud. |
| config.scm.github[0].actions[3].type | string | `"pr_comment"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.github[0].actions[4].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.github[0].actions[4].type | string | `"issue_comment"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.github[0].actions[5].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.github[0].actions[5].type | string | `"discussion_comment"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.github[0].actions[6].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.github[0].actions[6].labels | object | `{"add":[{"color":"0e8a16","name":"ci:passed"},{"name":"ready-to-merge"}],"remove":[{"name":"ci:failed"}]}` | Declarative label effect: add/remove lists with optional colors (hex without #) Supports both old string format and new object format with color field |
| config.scm.github[0].actions[6].type | string | `"label"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.github[0].auth | object | `{"secretRef":{"key":"token","name":"github-default-token"}}` | Webhook authentication configuration |
| config.scm.github[0].auth.secretRef | object | `{"key":"token","name":"github-default-token"}` | Kubernetes Secret containing credentials |
| config.scm.github[0].base_url | string | `"https://api.github.com"` | API base URL for the SCM provider |
| config.scm.github[0].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitlab | list | `[{"actions":[{"enabled":false,"name":"commit-status","type":"commit_status","when":""},{"enabled":false,"name":"pr-comment","template":"## Pipeline {{.State}}\n**Run:** {{.RunName}}\n**Commit:** {{.CommitSHA}}\n{{if .TargetURL}}[View Results]({{.TargetURL}}){{end}}\n","type":"pr_comment","when":"stateIn(\"failure\", \"error\")"},{"enabled":false,"name":"issue-comment","template":"Pipeline {{.Context}} finished with state: **{{.State}}**\n{{if .TargetURL}}[View Results]({{.TargetURL}}){{end}}\n","type":"issue_comment","when":"stateIn(\"failure\", \"error\")"},{"enabled":false,"labels":{"add":["{{ if eq .State \"success\" }}pipeline::success{{ else }}pipeline::failed{{ end }}"],"remove":["{{ if eq .State \"success\" }}pipeline::failed{{ else }}pipeline::success{{ end }}"]},"name":"label","type":"label","when":""}],"base_url":"https://gitlab.com/api/v4","enabled":false,"name":"cloud","variant":"saas"},{"actions":[{"enabled":false,"name":"commit-status","type":"commit_status","when":""},{"enabled":false,"name":"pr-comment","template":"## Pipeline {{.State}}\n**Run:** {{.RunName}}\n**Commit:** {{.CommitSHA}}\n{{if .TargetURL}}[View Results]({{.TargetURL}}){{end}}\n","type":"pr_comment","when":"stateIn(\"failure\", \"error\")"},{"enabled":false,"name":"issue-comment","template":"Pipeline {{.Context}} finished with state: **{{.State}}**\n{{if .TargetURL}}[View Results]({{.TargetURL}}){{end}}\n","type":"issue_comment","when":"stateIn(\"failure\", \"error\")"},{"enabled":false,"labels":{"add":["{{ if eq .State \"success\" }}pipeline::success{{ else }}pipeline::failed{{ end }}"],"remove":["{{ if eq .State \"success\" }}pipeline::failed{{ else }}pipeline::success{{ end }}"]},"name":"label","type":"label","when":""}],"base_url":"https://gitlab.company.example.com/api/v4","enabled":false,"name":"server","variant":"self-managed"}]` | GitLab SCM provider configuration |
| config.scm.gitlab[0].actions | list | `[{"enabled":false,"name":"commit-status","type":"commit_status","when":""},{"enabled":false,"name":"pr-comment","template":"## Pipeline {{.State}}\n**Run:** {{.RunName}}\n**Commit:** {{.CommitSHA}}\n{{if .TargetURL}}[View Results]({{.TargetURL}}){{end}}\n","type":"pr_comment","when":"stateIn(\"failure\", \"error\")"},{"enabled":false,"name":"issue-comment","template":"Pipeline {{.Context}} finished with state: **{{.State}}**\n{{if .TargetURL}}[View Results]({{.TargetURL}}){{end}}\n","type":"issue_comment","when":"stateIn(\"failure\", \"error\")"},{"enabled":false,"labels":{"add":["{{ if eq .State \"success\" }}pipeline::success{{ else }}pipeline::failed{{ end }}"],"remove":["{{ if eq .State \"success\" }}pipeline::failed{{ else }}pipeline::success{{ end }}"]},"name":"label","type":"label","when":""}]` | Actions to perform when events are received |
| config.scm.gitlab[0].actions[0].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitlab[0].actions[0].type | string | `"commit_status"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.gitlab[0].actions[1].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitlab[0].actions[1].type | string | `"pr_comment"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.gitlab[0].actions[2].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitlab[0].actions[2].type | string | `"issue_comment"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.gitlab[0].actions[3].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitlab[0].actions[3].labels | object | `{"add":["{{ if eq .State \"success\" }}pipeline::success{{ else }}pipeline::failed{{ end }}"],"remove":["{{ if eq .State \"success\" }}pipeline::failed{{ else }}pipeline::success{{ end }}"]}` | Declarative label effect: add/remove lists, Go-templated against the event |
| config.scm.gitlab[0].actions[3].type | string | `"label"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.gitlab[0].base_url | string | `"https://gitlab.com/api/v4"` | API base URL for the SCM provider |
| config.scm.gitlab[0].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitlab[0].variant | string | `"saas"` | Deployment model: 'saas' for gitlab.com, 'self-managed' for self-hosted. This field is required when instance is enabled. Currently metadata-only but enables future SaaS-specific behaviors (rate limiting, feature detection). |
| config.scm.gitlab[1].actions | list | `[{"enabled":false,"name":"commit-status","type":"commit_status","when":""},{"enabled":false,"name":"pr-comment","template":"## Pipeline {{.State}}\n**Run:** {{.RunName}}\n**Commit:** {{.CommitSHA}}\n{{if .TargetURL}}[View Results]({{.TargetURL}}){{end}}\n","type":"pr_comment","when":"stateIn(\"failure\", \"error\")"},{"enabled":false,"name":"issue-comment","template":"Pipeline {{.Context}} finished with state: **{{.State}}**\n{{if .TargetURL}}[View Results]({{.TargetURL}}){{end}}\n","type":"issue_comment","when":"stateIn(\"failure\", \"error\")"},{"enabled":false,"labels":{"add":["{{ if eq .State \"success\" }}pipeline::success{{ else }}pipeline::failed{{ end }}"],"remove":["{{ if eq .State \"success\" }}pipeline::failed{{ else }}pipeline::success{{ end }}"]},"name":"label","type":"label","when":""}]` | Actions to perform when events are received |
| config.scm.gitlab[1].actions[0].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitlab[1].actions[0].type | string | `"commit_status"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.gitlab[1].actions[1].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitlab[1].actions[1].type | string | `"pr_comment"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.gitlab[1].actions[2].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitlab[1].actions[2].type | string | `"issue_comment"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.gitlab[1].actions[3].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitlab[1].actions[3].labels | object | `{"add":["{{ if eq .State \"success\" }}pipeline::success{{ else }}pipeline::failed{{ end }}"],"remove":["{{ if eq .State \"success\" }}pipeline::failed{{ else }}pipeline::success{{ end }}"]}` | Declarative label effect: add/remove lists, Go-templated against the event |
| config.scm.gitlab[1].actions[3].type | string | `"label"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.gitlab[1].base_url | string | `"https://gitlab.company.example.com/api/v4"` | API base URL for the SCM provider |
| config.scm.gitlab[1].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitlab[1].variant | string | `"self-managed"` | Deployment model: 'saas' for gitlab.com, 'self-managed' for self-hosted |
| config.scm.sourcehut | list | `[{"actions":[{"enabled":false,"name":"commit-status","type":"commit_status","when":""}],"enabled":false,"name":"default"}]` | SourceHut SCM provider configuration |
| config.scm.sourcehut[0].actions | list | `[{"enabled":false,"name":"commit-status","type":"commit_status","when":""}]` | Actions to perform when events are received |
| config.scm.sourcehut[0].actions[0].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.sourcehut[0].actions[0].type | string | `"commit_status"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.sourcehut[0].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.server | object | `{"addr":":8080","auth":{"enabled":false,"secret":{"secretRef":{"key":"hmac-key","name":"webhook-secret"}},"timestamp_tolerance":"5m","type":"hmac-sha256","validate_timestamp":false},"max_body_size":1048576,"metrics_addr":"","rate_limit":{"burst":200,"enabled":false,"requests_per_second":100},"read_timeout_sec":10,"shutdown_timeout_sec":30,"tls":{"cert_file":"","key_file":""},"write_timeout_sec":10}` | HTTP server configuration |
| config.server.addr | string | `":8080"` | Server listen address and port |
| config.server.auth | object | `{"enabled":false,"secret":{"secretRef":{"key":"hmac-key","name":"webhook-secret"}},"timestamp_tolerance":"5m","type":"hmac-sha256","validate_timestamp":false}` | Webhook authentication configuration |
| config.server.auth.secret | object | `{"secretRef":{"key":"hmac-key","name":"webhook-secret"}}` | Reference to webhook secret for HMAC validation Use secretRef for K8s Secret mount |
| config.server.auth.timestamp_tolerance | string | `"5m"` | Accepted clock skew for replay protection (Go duration format) |
| config.server.auth.validate_timestamp | bool | `false` | Replay protection: require an X-Webhook-Timestamp header (unix seconds) within timestamp_tolerance of the server clock |
| config.server.max_body_size | int | `1048576` | Maximum request body size in bytes |
| config.server.metrics_addr | string | `""` | Metrics server address (empty to use main server) |
| config.server.rate_limit | object | `{"burst":200,"enabled":false,"requests_per_second":100}` | Rate limiting configuration |
| config.server.rate_limit.burst | int | `200` | Maximum burst size for rate limiter |
| config.server.rate_limit.requests_per_second | float | `100` | Maximum requests per second |
| config.server.read_timeout_sec | int | `10` | HTTP read timeout in seconds |
| config.server.shutdown_timeout_sec | int | `30` | Graceful shutdown timeout in seconds |
| config.server.tls | object | `{"cert_file":"","key_file":""}` | Native TLS configuration for the receiver endpoint |
| config.server.tls.cert_file | string | `""` | Path to the PEM certificate (empty = plain HTTP) |
| config.server.tls.key_file | string | `""` | Path to the PEM private key (empty = plain HTTP) |
| config.server.write_timeout_sec | int | `10` | HTTP write timeout in seconds |
| config.store | object | `{"backend":"memory","olric":{"bind_port":3320,"memberlist_port":3322,"peers":[]},"ttl":"1h","valkey":{"address":"","db":0,"embedded":{"enabled":false},"key_prefix":"tekton-events-relay","password":{"secretRef":{"key":"password","name":""}}}}` | State backend configuration (dedupe + accumulator) |
| config.store.backend | string | `"memory"` | Backend: memory, valkey or olric |
| config.store.olric | object | `{"bind_port":3320,"memberlist_port":3322,"peers":[]}` | Olric embedded backend settings (backend: olric) |
| config.store.olric.bind_port | int | `3320` | Port for olric data traffic between relay pods |
| config.store.olric.memberlist_port | int | `3322` | Port for memberlist gossip between relay pods (TCP+UDP) |
| config.store.olric.peers | list | `[]` | Peer list override; defaults to the chart's headless gossip service |
| config.store.ttl | string | `"1h"` | Entry lifetime on remote backends (Go duration format) |
| config.store.valkey | object | `{"address":"","db":0,"embedded":{"enabled":false},"key_prefix":"tekton-events-relay","password":{"secretRef":{"key":"password","name":""}}}` | Valkey backend settings (backend: valkey) |
| config.store.valkey.address | string | `""` | Valkey server address (host:port); required when backend=valkey and embedded is disabled. When embedded.enabled is true, this is auto-populated with the embedded service address. |
| config.store.valkey.db | int | `0` | Valkey logical database |
| config.store.valkey.embedded | object | `{"enabled":false}` | Deploy an embedded Valkey instance as a subchart (disabled by default; use this for single-cluster dev/test setups; for production, use an external Valkey/KeyDB and set address directly). IMPORTANT: when enabling embedded valkey, you must first add the Valkey Helm repo: helm repo add valkey https://valkey.io/valkey-helm/ |
| config.store.valkey.key_prefix | string | `"tekton-events-relay"` | Prefix applied to all keys |
| config.store.valkey.password | object | `{"secretRef":{"key":"password","name":""}}` | Reference to a Kubernetes Secret containing the Valkey password (optional) |
| config.tracing | object | `{"endpoint":"","insecure":false,"service_name":"tekton-events-relay"}` | OpenTelemetry tracing configuration |
| config.tracing.endpoint | string | `""` | OTLP endpoint for trace export (e.g., "otel-collector:4318"). Empty = tracing disabled. |
| config.tracing.insecure | bool | `false` | When false, uses HTTPS for OTLP export. Set to true for plaintext HTTP. |
| config.tracing.service_name | string | `"tekton-events-relay"` | Service name reported in traces |
| dnsConfig.options[0].name | string | `"ndots"` |  |
| dnsConfig.options[0].value | string | `"2"` |  |
| dnsPolicy | string | `"ClusterFirst"` |  |
| email | string | `"platform@example.com"` |  |
| extraEnv | list | `[]` | Extra environment variables for the receiver container |
| extraVolumeMounts | list | `[]` | Extra volume mounts for the receiver container |
| extraVolumes | list | `[]` | Extra volumes (e.g. TLS certificates for config.server.tls) |
| fullnameOverride | string | `""` |  |
| image.pullPolicy | string | `"IfNotPresent"` |  |
| image.repository | string | `"ghcr.io/fabioluciano/tekton-events-relay"` |  |
| image.tag | string | `""` |  |
| imagePullSecrets | list | `[]` |  |
| livenessProbe.httpGet.path | string | `"/healthz"` |  |
| livenessProbe.httpGet.port | string | `"http"` |  |
| livenessProbe.initialDelaySeconds | int | `5` |  |
| livenessProbe.periodSeconds | int | `10` |  |
| monitoring.serviceMonitor.additionalLabels | object | `{}` |  |
| monitoring.serviceMonitor.enabled | bool | `false` |  |
| monitoring.serviceMonitor.interval | string | `"30s"` |  |
| monitoring.serviceMonitor.path | string | `"/metrics"` |  |
| monitoring.serviceMonitor.scrapeTimeout | string | `"10s"` |  |
| nameOverride | string | `""` |  |
| networkPolicy.enabled | bool | `true` |  |
| networkPolicy.extraEgress | list | `[]` | Additional egress rules appended verbatim (e.g. restrict 443 to SCM CIDRs) |
| networkPolicy.extraIngress | list | `[]` | Additional ingress rules appended verbatim |
| networkPolicy.metricsFrom | list | `[]` | Restrict who may scrape metrics (empty = any source); standard `from` peers |
| networkPolicy.metricsPort | int | `9090` | Metrics port opened for scraping when config.server.metrics_addr is set |
| networkPolicy.valkeyPort | int | `6379` | Valkey port allowed for egress when config.store.backend=valkey |
| nodeSelector | object | `{}` |  |
| owner | string | `"platform-team"` |  |
| podAnnotations | object | `{}` |  |
| podDisruptionBudget.enabled | bool | `true` | Create a PodDisruptionBudget |
| podDisruptionBudget.maxUnavailable | int | `1` | Maximum pods that may be unavailable during voluntary disruptions |
| podDisruptionBudget.minAvailable | string | `""` | Alternative to maxUnavailable (leave empty to use maxUnavailable) |
| podSecurityContext.appArmorProfile.type | string | `"RuntimeDefault"` |  |
| podSecurityContext.fsGroup | int | `65532` |  |
| podSecurityContext.runAsGroup | int | `65532` |  |
| podSecurityContext.runAsNonRoot | bool | `true` |  |
| podSecurityContext.runAsUser | int | `65532` |  |
| podSecurityContext.seccompProfile.type | string | `"RuntimeDefault"` |  |
| priorityClassName | string | `""` | PriorityClass for the relay pods (empty = cluster default) |
| readinessProbe.httpGet.path | string | `"/readyz"` |  |
| readinessProbe.httpGet.port | string | `"http"` |  |
| readinessProbe.periodSeconds | int | `5` |  |
| replicaCount | int | `1` |  |
| resources.limits.cpu | string | `"500m"` |  |
| resources.limits.ephemeral-storage | string | `"256Mi"` |  |
| resources.limits.memory | string | `"256Mi"` |  |
| resources.requests.cpu | string | `"100m"` |  |
| resources.requests.ephemeral-storage | string | `"100Mi"` |  |
| resources.requests.memory | string | `"64Mi"` |  |
| securityContext.allowPrivilegeEscalation | bool | `false` |  |
| securityContext.appArmorProfile.type | string | `"RuntimeDefault"` |  |
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
| terminationGracePeriodSeconds | int | `40` | Seconds allowed for graceful shutdown; keep above config.server.shutdown_timeout_sec |
| tolerations | list | `[]` |  |
| topologySpreadConstraints | list | `[]` | Topology spread constraints (e.g. spread across zones) |
| unsafe.allowMemoryStoreWithMultipleReplicas | bool | `false` | Allow multiple replicas with the per-pod memory store (NOT recommended) |
