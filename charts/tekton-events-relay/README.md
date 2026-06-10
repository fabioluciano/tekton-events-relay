# tekton-events-relay

![Version: 0.0.0](https://img.shields.io/badge/Version-0.0.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.0.0](https://img.shields.io/badge/AppVersion-0.0.0-informational?style=flat-square)

CloudEvents receiver that reports pipeline execution status to multiple SCM providers

**Homepage:** <https://github.com/fabioluciano/tekton-events-relay>

## Installation

```bash
helm install tekton-events-relay \
  oci://ghcr.io/fabioluciano/charts/tekton-events-relay \
  --version 0.0.0
```

### Signature Verification

Docker images and Helm charts are signed with [Cosign](https://github.com/sigstore/cosign) using keyless OIDC signing.

**Verify Docker image:**
```bash
cosign verify \
  --certificate-identity-regexp='https://github.com/fabioluciano/tekton-events-relay' \
  --certificate-oidc-issuer='https://token.actions.githubusercontent.com' \
  ghcr.io/fabioluciano/tekton-events-relay:0.0.0
```

**Verify Helm chart:**
```bash
cosign verify \
  --certificate-identity-regexp='https://github.com/fabioluciano/tekton-events-relay' \
  --certificate-oidc-issuer='https://token.actions.githubusercontent.com' \
  oci://ghcr.io/fabioluciano/charts/tekton-events-relay:0.0.0
```

Signatures are stored in the [Sigstore Rekor transparency log](https://rekor.sigstore.dev/).

## Configuration

The chart supports multiple SCM providers and notification channels. Enable and configure them through the `config` section in your values.yaml:

### Supported SCM Providers
- GitHub (commit status, check runs, PR comments, issue comments, discussion comments, labels, deployments)
- GitLab (commit status, MR notes, issue comments, labels)
- Bitbucket (Cloud and Server variants, commit status)
- Azure DevOps (commit status, PR labels)
- Gitea (commit status, PR comments, issue comments, labels)
- SourceHut (commit status)

### Supported Notifiers
- Slack
- Discord
- Microsoft Teams
- Datadog
- PagerDuty
- Generic Webhooks

### Quick Start Example

```yaml
config:
  dashboard_url: "https://tekton.your-domain.com"

  scm:
    github:
      - name: default
        enabled: true
        base_url: "https://api.github.com"
        actions:
          - name: commit-status
            type: commit_status
            enabled: true
            when: 'event.Resource == "pipelinerun"'

  notifiers:
    slack:
      - name: main-channel
        enabled: true
        channel: "#ci-notifications"
        username: "Tekton CI"
        when: 'event.State == "failure" || event.State == "error"'
```

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| fabioluciano | <hi@fabioluciano.com> | <https://fabioluciano.com> |

## Source Code

* <https://github.com/fabioluciano/tekton-events-relay>

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
| config.filter | object | `{"allow_customrun":false,"allow_eventlistener":false,"allow_pipelinerun":true,"allow_taskrun":true,"ignore_unknown":true}` | Event filtering configuration |
| config.filter.allow_customrun | bool | `false` | Process CustomRun events |
| config.filter.allow_eventlistener | bool | `false` | Process EventListener events |
| config.filter.allow_pipelinerun | bool | `true` | Process PipelineRun events |
| config.filter.allow_taskrun | bool | `true` | Process TaskRun events |
| config.filter.ignore_unknown | bool | `true` | Ignore unknown event types |
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
| config.notifiers.pagerduty | list | `[{"enabled":false,"name":"main-service","severity":"critical","when":"event.State == \"failure\" || event.State == \"error\""}]` | PagerDuty incident services configuration. Supports multiple services with independent integration keys. |
| config.notifiers.pagerduty[0] | object | `{"enabled":false,"name":"main-service","severity":"critical","when":"event.State == \"failure\" || event.State == \"error\""}` | Unique identifier for this PagerDuty service configuration |
| config.notifiers.pagerduty[0].enabled | bool | `false` | Enable or disable this PagerDuty notifier instance |
| config.notifiers.pagerduty[0].severity | string | `"critical"` | Incident severity level (critical, error, warning, info) |
| config.notifiers.pagerduty[0].when | string | `"event.State == \"failure\" || event.State == \"error\""` | CEL expression to filter which events trigger incidents |
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
| config.scm.azure_devops | list | `[{"actions":[{"enabled":false,"name":"commit-status","type":"commit_status","when":""},{"enabled":false,"failure_label":"build-failed","name":"label","success_label":"build-passed","type":"label","when":""}],"base_url":"https://dev.azure.com","enabled":false,"genre":"tekton-ci","insecure_skip_verify":false,"name":"default"}]` | Azure DevOps SCM provider configuration |
| config.scm.azure_devops[0].actions | list | `[{"enabled":false,"name":"commit-status","type":"commit_status","when":""},{"enabled":false,"failure_label":"build-failed","name":"label","success_label":"build-passed","type":"label","when":""}]` | Actions to perform when events are received |
| config.scm.azure_devops[0].actions[0].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.azure_devops[0].actions[0].type | string | `"commit_status"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.azure_devops[0].actions[1].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.azure_devops[0].actions[1].failure_label | string | `"build-failed"` | Label to add when pipeline fails |
| config.scm.azure_devops[0].actions[1].success_label | string | `"build-passed"` | Label to add when pipeline succeeds |
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
| config.scm.gitea | list | `[{"actions":[{"enabled":false,"name":"commit-status","type":"commit_status","when":""},{"enabled":false,"name":"pr-comment","template":"## Pipeline {{.State}}\n**Run:** {{.RunName}}\n**Commit:** {{.CommitSHA}}\n{{if .TargetURL}}[View Results]({{.TargetURL}}){{end}}\n","type":"pr_comment","when":"stateIn(\"success\", \"failure\")"},{"enabled":false,"name":"issue-comment","template":"Pipeline {{.Context}} finished: **{{.State}}**\n{{if .TargetURL}}[Results]({{.TargetURL}}){{end}}\n","type":"issue_comment","when":"stateIn(\"failure\", \"error\")"},{"enabled":false,"failure_label":"ci:failed","name":"label","success_label":"ci:passed","type":"label","when":""}],"base_url":"https://gitea.company.example.com","enabled":false,"name":"default"}]` | Gitea SCM provider configuration |
| config.scm.gitea[0].actions | list | `[{"enabled":false,"name":"commit-status","type":"commit_status","when":""},{"enabled":false,"name":"pr-comment","template":"## Pipeline {{.State}}\n**Run:** {{.RunName}}\n**Commit:** {{.CommitSHA}}\n{{if .TargetURL}}[View Results]({{.TargetURL}}){{end}}\n","type":"pr_comment","when":"stateIn(\"success\", \"failure\")"},{"enabled":false,"name":"issue-comment","template":"Pipeline {{.Context}} finished: **{{.State}}**\n{{if .TargetURL}}[Results]({{.TargetURL}}){{end}}\n","type":"issue_comment","when":"stateIn(\"failure\", \"error\")"},{"enabled":false,"failure_label":"ci:failed","name":"label","success_label":"ci:passed","type":"label","when":""}]` | Actions to perform when events are received |
| config.scm.gitea[0].actions[0].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitea[0].actions[0].type | string | `"commit_status"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.gitea[0].actions[1].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitea[0].actions[1].type | string | `"pr_comment"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.gitea[0].actions[2].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitea[0].actions[2].type | string | `"issue_comment"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.gitea[0].actions[3].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitea[0].actions[3].failure_label | string | `"ci:failed"` | Label to add when pipeline fails |
| config.scm.gitea[0].actions[3].success_label | string | `"ci:passed"` | Label to add when pipeline succeeds |
| config.scm.gitea[0].actions[3].type | string | `"label"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.gitea[0].base_url | string | `"https://gitea.company.example.com"` | API base URL for the SCM provider |
| config.scm.gitea[0].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.github | list | `[{"actions":[{"enabled":false,"name":"commit-status","type":"commit_status","when":"event.Resource == \"pipelinerun\""},{"enabled":false,"name":"check-run","template":"## Build Result: {{.State}}\n**Run:** {{.RunName}}\n**Namespace:** {{.Namespace}}\n{{if .TargetURL}}[View Logs]({{.TargetURL}}){{end}}\n","type":"check_run","when":"stateIn(\"success\", \"failure\", \"error\")"},{"enabled":false,"name":"deployment-status","type":"deployment_status","when":"stateIn(\"success\", \"failure\")"},{"enabled":false,"name":"pr-comment","template":"## Pipeline {{.State}}\n**Run:** {{.RunName}}\n**Commit:** {{.CommitSHA}}\n{{if .TargetURL}}[View Results]({{.TargetURL}}){{end}}\n","type":"pr_comment","when":"stateIn(\"success\", \"failure\")"},{"enabled":false,"name":"issue-comment","template":"Pipeline {{.Context}} finished with state: **{{.State}}**\n{{if .TargetURL}}[View Results]({{.TargetURL}}){{end}}\n","type":"issue_comment","when":"stateIn(\"failure\", \"error\")"},{"enabled":false,"name":"discussion-comment","template":"## Pipeline {{.State}}\n**Run:** {{.RunName}}\n**Commit:** {{.CommitSHA}}\n{{if .TargetURL}}[View Results]({{.TargetURL}}){{end}}\n","type":"discussion_comment","when":"stateIn(\"success\", \"failure\")"},{"enabled":false,"failure_label":"ci:failed","name":"label","success_label":"ci:passed","type":"label","when":""}],"auth":{"secret_name":"github-default-token"},"base_url":"https://api.github.com","enabled":false,"name":"default"}]` | GitHub SCM provider configuration |
| config.scm.github[0].actions[0].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.github[0].actions[0].type | string | `"commit_status"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.github[0].actions[1].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.github[0].actions[1].type | string | `"check_run"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.github[0].actions[2].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.github[0].actions[2].type | string | `"deployment_status"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.github[0].actions[3].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.github[0].actions[3].type | string | `"pr_comment"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.github[0].actions[4].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.github[0].actions[4].type | string | `"issue_comment"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.github[0].actions[5].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.github[0].actions[5].type | string | `"discussion_comment"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.github[0].actions[6].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.github[0].actions[6].failure_label | string | `"ci:failed"` | Label to add when pipeline fails |
| config.scm.github[0].actions[6].success_label | string | `"ci:passed"` | Label to add when pipeline succeeds |
| config.scm.github[0].actions[6].type | string | `"label"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.github[0].auth | object | `{"secret_name":"github-default-token"}` | Webhook authentication configuration |
| config.scm.github[0].auth.secret_name | string | `"github-default-token"` | Kubernetes Secret containing credentials |
| config.scm.github[0].base_url | string | `"https://api.github.com"` | API base URL for the SCM provider |
| config.scm.github[0].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitlab | list | `[{"actions":[{"enabled":false,"name":"commit-status","type":"commit_status","when":""},{"enabled":false,"name":"pr-comment","template":"## Pipeline {{.State}}\n**Run:** {{.RunName}}\n**Commit:** {{.CommitSHA}}\n{{if .TargetURL}}[View Results]({{.TargetURL}}){{end}}\n","type":"pr_comment","when":"stateIn(\"failure\", \"error\")"},{"enabled":false,"name":"issue-comment","template":"Pipeline {{.Context}} finished with state: **{{.State}}**\n{{if .TargetURL}}[View Results]({{.TargetURL}}){{end}}\n","type":"issue_comment","when":"stateIn(\"failure\", \"error\")"},{"enabled":false,"failure_label":"pipeline::failed","name":"label","success_label":"pipeline::success","type":"label","when":""}],"base_url":"https://gitlab.com/api/v4","enabled":false,"name":"cloud","variant":"saas"},{"actions":[{"enabled":false,"name":"commit-status","type":"commit_status","when":""},{"enabled":false,"name":"pr-comment","template":"## Pipeline {{.State}}\n**Run:** {{.RunName}}\n**Commit:** {{.CommitSHA}}\n{{if .TargetURL}}[View Results]({{.TargetURL}}){{end}}\n","type":"pr_comment","when":"stateIn(\"failure\", \"error\")"},{"enabled":false,"name":"issue-comment","template":"Pipeline {{.Context}} finished with state: **{{.State}}**\n{{if .TargetURL}}[View Results]({{.TargetURL}}){{end}}\n","type":"issue_comment","when":"stateIn(\"failure\", \"error\")"},{"enabled":false,"failure_label":"pipeline::failed","name":"label","success_label":"pipeline::success","type":"label","when":""}],"base_url":"https://gitlab.company.example.com/api/v4","enabled":false,"name":"server","variant":"self-managed"}]` | GitLab SCM provider configuration |
| config.scm.gitlab[0].actions | list | `[{"enabled":false,"name":"commit-status","type":"commit_status","when":""},{"enabled":false,"name":"pr-comment","template":"## Pipeline {{.State}}\n**Run:** {{.RunName}}\n**Commit:** {{.CommitSHA}}\n{{if .TargetURL}}[View Results]({{.TargetURL}}){{end}}\n","type":"pr_comment","when":"stateIn(\"failure\", \"error\")"},{"enabled":false,"name":"issue-comment","template":"Pipeline {{.Context}} finished with state: **{{.State}}**\n{{if .TargetURL}}[View Results]({{.TargetURL}}){{end}}\n","type":"issue_comment","when":"stateIn(\"failure\", \"error\")"},{"enabled":false,"failure_label":"pipeline::failed","name":"label","success_label":"pipeline::success","type":"label","when":""}]` | Actions to perform when events are received |
| config.scm.gitlab[0].actions[0].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitlab[0].actions[0].type | string | `"commit_status"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.gitlab[0].actions[1].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitlab[0].actions[1].type | string | `"pr_comment"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.gitlab[0].actions[2].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitlab[0].actions[2].type | string | `"issue_comment"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.gitlab[0].actions[3].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitlab[0].actions[3].failure_label | string | `"pipeline::failed"` | Label to add when pipeline fails |
| config.scm.gitlab[0].actions[3].success_label | string | `"pipeline::success"` | Label to add when pipeline succeeds |
| config.scm.gitlab[0].actions[3].type | string | `"label"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.gitlab[0].base_url | string | `"https://gitlab.com/api/v4"` | API base URL for the SCM provider |
| config.scm.gitlab[0].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitlab[0].variant | string | `"saas"` | Deployment model: 'saas' for gitlab.com, 'self-managed' for self-hosted. This field is required when instance is enabled. Currently metadata-only but enables future SaaS-specific behaviors (rate limiting, feature detection). |
| config.scm.gitlab[1].actions | list | `[{"enabled":false,"name":"commit-status","type":"commit_status","when":""},{"enabled":false,"name":"pr-comment","template":"## Pipeline {{.State}}\n**Run:** {{.RunName}}\n**Commit:** {{.CommitSHA}}\n{{if .TargetURL}}[View Results]({{.TargetURL}}){{end}}\n","type":"pr_comment","when":"stateIn(\"failure\", \"error\")"},{"enabled":false,"name":"issue-comment","template":"Pipeline {{.Context}} finished with state: **{{.State}}**\n{{if .TargetURL}}[View Results]({{.TargetURL}}){{end}}\n","type":"issue_comment","when":"stateIn(\"failure\", \"error\")"},{"enabled":false,"failure_label":"pipeline::failed","name":"label","success_label":"pipeline::success","type":"label","when":""}]` | Actions to perform when events are received |
| config.scm.gitlab[1].actions[0].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitlab[1].actions[0].type | string | `"commit_status"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.gitlab[1].actions[1].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitlab[1].actions[1].type | string | `"pr_comment"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.gitlab[1].actions[2].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitlab[1].actions[2].type | string | `"issue_comment"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.gitlab[1].actions[3].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitlab[1].actions[3].failure_label | string | `"pipeline::failed"` | Label to add when pipeline fails |
| config.scm.gitlab[1].actions[3].success_label | string | `"pipeline::success"` | Label to add when pipeline succeeds |
| config.scm.gitlab[1].actions[3].type | string | `"label"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.gitlab[1].base_url | string | `"https://gitlab.company.example.com/api/v4"` | API base URL for the SCM provider |
| config.scm.gitlab[1].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.gitlab[1].variant | string | `"self-managed"` | Deployment model: 'saas' for gitlab.com, 'self-managed' for self-hosted |
| config.scm.sourcehut | list | `[{"actions":[{"enabled":false,"name":"commit-status","type":"commit_status","when":""}],"enabled":false,"name":"default"}]` | SourceHut SCM provider configuration |
| config.scm.sourcehut[0].actions | list | `[{"enabled":false,"name":"commit-status","type":"commit_status","when":""}]` | Actions to perform when events are received |
| config.scm.sourcehut[0].actions[0].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.scm.sourcehut[0].actions[0].type | string | `"commit_status"` | Action type (commit_status, check_run, pr_comment, issue_comment, discussion_comment, label, deployment_status) |
| config.scm.sourcehut[0].enabled | bool | `false` | Enable or disable this SCM provider instance |
| config.server | object | `{"addr":":8080","auth":{"enabled":false,"secret_file":"${WEBHOOK_SECRET}","type":"hmac-sha256"},"max_body_size":1048576,"metrics_addr":"","rate_limit":{"burst":200,"enabled":false,"requests_per_second":100},"read_timeout_sec":10,"shutdown_timeout_sec":30,"write_timeout_sec":10}` | HTTP server configuration |
| config.server.addr | string | `":8080"` | Server listen address and port |
| config.server.auth | object | `{"enabled":false,"secret_file":"${WEBHOOK_SECRET}","type":"hmac-sha256"}` | Webhook authentication configuration |
| config.server.auth.secret_file | string | `"${WEBHOOK_SECRET}"` | Reference to webhook secret for HMAC validation |
| config.server.max_body_size | int | `1048576` | Maximum request body size in bytes |
| config.server.metrics_addr | string | `""` | Metrics server address (empty to use main server) |
| config.server.rate_limit | object | `{"burst":200,"enabled":false,"requests_per_second":100}` | Rate limiting configuration |
| config.server.rate_limit.burst | int | `200` | Maximum burst size for rate limiter |
| config.server.rate_limit.requests_per_second | float | `100` | Maximum requests per second |
| config.server.read_timeout_sec | int | `10` | HTTP read timeout in seconds |
| config.server.shutdown_timeout_sec | int | `30` | Graceful shutdown timeout in seconds |
| config.server.write_timeout_sec | int | `10` | HTTP write timeout in seconds |
| config.tracing | object | `{"endpoint":"","service_name":"tekton-events-relay"}` | OpenTelemetry tracing configuration |
| config.tracing.endpoint | string | `""` | OTLP endpoint for trace export (e.g., "otel-collector:4318"). Empty = tracing disabled. |
| config.tracing.service_name | string | `"tekton-events-relay"` | Service name reported in traces |
| dnsConfig.options[0].name | string | `"ndots"` |  |
| dnsConfig.options[0].value | string | `"2"` |  |
| dnsPolicy | string | `"ClusterFirst"` |  |
| email | string | `"platform@example.com"` |  |
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
| nodeSelector | object | `{}` |  |
| owner | string | `"platform-team"` |  |
| podAnnotations | object | `{}` |  |
| podDisruptionBudget.minAvailable | int | `1` |  |
| podSecurityContext.appArmorProfile.type | string | `"RuntimeDefault"` |  |
| podSecurityContext.fsGroup | int | `65532` |  |
| podSecurityContext.runAsGroup | int | `65532` |  |
| podSecurityContext.runAsNonRoot | bool | `true` |  |
| podSecurityContext.runAsUser | int | `65532` |  |
| podSecurityContext.seccompProfile.type | string | `"RuntimeDefault"` |  |
| readinessProbe.httpGet.path | string | `"/readyz"` |  |
| readinessProbe.httpGet.port | string | `"http"` |  |
| readinessProbe.periodSeconds | int | `5` |  |
| replicaCount | int | `2` |  |
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
| templates.enabled | bool | `false` |  |
| tolerations | list | `[]` |  |
