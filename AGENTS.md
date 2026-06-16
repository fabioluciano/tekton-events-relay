# AGENTS.md

## What this is

Go 1.26 CloudEvents relay that receives Tekton pipeline events and dispatches to 6 SCM platforms + 9 notification channels. Single binary at `cmd/receiver`. Module: `github.com/fabioluciano/tekton-events-relay`.

## Developer commands

```bash
make build              # CGO_ENABLED=0 → bin/tekton-events-relay
make test               # go test -race -cover ./...
make vet                # go vet ./...
make fmt                # gofmt -s -w .
make run                # build + run with wiki/examples/config.yaml
```

Or via mise (preferred — pins tool versions):

```bash
mise run build
mise run test           # go test -race -coverprofile=coverage.txt -covermode=atomic ./...
mise run lint           # golangci-lint run --timeout=5m
mise run fmt            # gofmt -w .
```

CI runs: `go test -race -count=1 -timeout=5m -short -coverprofile=coverage.txt -covermode=atomic ./...`

**Single test/package**: `go test -run TestName ./internal/pipeline/...`

**Config validation**: `./tekton-events-relay --config path/to/config.yaml --validate`

## Lint & format

- **golangci-lint v2** config at `.golangci.yml` (5m timeout). Run: `golangci-lint run --timeout=5m`
- **goimports** required with `-local github.com/fabioluciano/tekton-events-relay` — local imports grouped separately.
- **gofmt -s** (simplify) is enforced.
- CI checks gofmt, goimports, go vet, golangci-lint, go mod tidy (must be clean).

## Pre-commit hooks

```bash
pre-commit install && pre-commit install --hook-type commit-msg
pre-commit run --all-files   # manual run
```

Hooks: go-fmt, go-vet, go-build, go-test (`-race -count=1 -timeout=5m -short`), go-mod-tidy, golangci-lint, go-deadcode, goimports, conventional-pre-commit, gitleaks, yamllint, helmlint, helm security suite, markdownlint, hadolint.

## Commits

**Conventional Commits enforced** (semantic-release). Types: `feat`, `fix`, `perf`, `refactor`, `docs`, `test`, `chore`, `ci`, `build`, `style`. Breaking: `feat!:` or `BREAKING CHANGE:` in body.

Commit template: `git config commit.template .gitmessage`

## Architecture

### Event flow

```
Tekton → HTTP POST (CloudEvent)
  → cehttp.FromRequest()                // internal/cehttp/
  → event.Registry.Find(type)           // resolves decoder by CloudEvent type
  → decoder.Decode(raw)                 // tekton decoders extract annotations → domain.Event
  → Pipeline chain:                     // internal/pipeline/chain.go
      Validator  → checks required fields (provider, runName)
      Filter     → drops by resource type (taskrun/pipelinerun/customrun/eventlistener)
      Deduper    → deduplicates by CloudEvent ID (fails open if store unavailable)
      Enricher   → fills derived fields (dashboard URL)
      Dispatcher → fans out to all matched ActionHandlers concurrently (errgroup)
  → Each ActionHandler:
      middleware.CEL    → evaluates `when` expression (skip if false)
      middleware.Filter → applies allow/deny lists per action
      handler.Handle() → calls SCM API / sends webhook / etc.
```

### Core types

`domain.Event` (`internal/domain/status.go`) is the central type all decoders produce and all notifiers consume:

- **Routing**: `Provider` (github/gitlab/etc), `Resource` (taskrun/pipelinerun/customrun/eventlistener), `APIBaseURL`
- **Pipeline identity**: `RunName`, `RunID`, `Namespace`, `TaskName`, `PipelineName`, `PipelineTaskName`, `EventListenerName`, `TriggerName`
- **Display**: `TaskDisplayName`, `PipelineDisplayName`, `IsFinallyTask`, `SCMEventType`, `TaskCount`
- **State**: `State` (pending/running/success/failure/error/canceled/done), `Context`, `Description`, `TargetURL`
- **SCM**: `CommitSHA`, `Repo` (Owner/Name/ID/Workspace/Project/Org)
- **Linking**: `IssueNumber`, `PRNumber`, `DiscussionNumber` (all `*int`, nil if absent)
- **Results**: `Results` ([]Result with Name/Value)
- **Timing**: `StartedAt`, `FinishedAt`

Other key types:

| Type | Package | Purpose |
|------|---------|---------|
| `event.Envelope` | `internal/event/` | Wraps Event + CloudEvent metadata (ID, type, source) |
| `event.Decoder` | `internal/event/` | Strategy interface: `CanHandle(eventType)`, `Decode(RawEvent)` |
| `notifier.ActionHandler` | `internal/notifier/` | Strategy interface: `Name()`, `Type()`, `Handle(ctx, Event)` |
| `notifier.ActionType` | `internal/notifier/` | 9 types: commit_status, commit_comment, pr_comment, issue_comment, label, check_run, discussion_comment, deployment_status, notify |
| `pipeline.Handler` | `internal/pipeline/` | Chain of Responsibility: `Handle(ctx, *Envelope)`, `SetNext(Handler)` |
| `factory.HandlerFactory[C]` | `internal/factory/` | Generic factory: `Build(cfg C, log) ([]ActionHandler, error)` |
| `store.Store` | `internal/store/` | State backend: `Dedupe()`, `RunBuffer()`, `Backend()`, `Close()` |
| `store.DedupeStore` | `internal/store/` | `FirstSeen(ctx, id) (bool, error)` |
| `dlq.Queue` | `internal/dlq/` | Dead letter queue: `Enqueue()`, `List()`, `Remove()` |
| `cel.Program` | `internal/cel/` | Compiled CEL: `Eval(Event) (bool, error)` |

### Tekton annotations

Read from Tekton resource metadata by decoders (`internal/event/event.go`):

```
tekton.dev/tekton-events-relay.scm.provider         # required: github, gitlab, gitea, bitbucket, azure_devops, sourcehut
tekton.dev/tekton-events-relay.scm.repo-owner       # required for most providers
tekton.dev/tekton-events-relay.scm.repo-name        # required for most providers
tekton.dev/tekton-events-relay.scm.repo-id          # optional: GitLab numeric project ID
tekton.dev/tekton-events-relay.scm.repo-workspace   # optional: Bitbucket Cloud workspace
tekton.dev/tekton-events-relay.scm.repo-project     # optional: Bitbucket Server / Azure DevOps project
tekton.dev/tekton-events-relay.scm.repo-org         # optional: Azure DevOps organization
tekton.dev/tekton-events-relay.scm.commit-sha       # optional: commit SHA (required for commit_status, check_run)
tekton.dev/tekton-events-relay.scm.api-base-url     # optional: self-hosted SCM base URL override
tekton.dev/tekton-events-relay.scm.context          # optional: logical check name (default: "tekton/build")
tekton.dev/tekton-events-relay.scm.issue-number     # optional: issue number for linking
tekton.dev/tekton-events-relay.scm.pr-number        # optional: PR number for linking
tekton.dev/tekton-events-relay.scm.discussion-number # optional: discussion number for linking
```

### Tekton CloudEvent types

Handled by decoders in `internal/event/tekton/`:

- `dev.tekton.event.taskrun.v1.{queued,started,running,unknown,failed,succeeded,cancelled}` — TaskRunDecoder
- `dev.tekton.event.pipelinerun.v1.{queued,started,running,unknown,failed,succeeded,cancelled}` — PipelineRunDecoder
- `dev.tekton.event.customrun.v1.{queued,started,running,unknown,failed,succeeded,cancelled}` — CustomRunDecoder
- `dev.tekton.event.eventlistener.v1.{started,successful,failed,done}` — EventListenerDecoder

Event suffix maps to `domain.State`: queued→pending, started→running, running→running, succeeded/failed/cancelled→matching, unknown→running.

### CEL macros

Available in `when` expressions (`internal/cel/cel.go`):

- `isPR()` — true if `PRNumber != nil`
- `isDiscussion()` — true if `DiscussionNumber != nil`
- `isIssue()` — true if `IssueNumber != nil`
- `isTaskRun()` — true if `Resource == "taskrun"`
- `isPipelineRun()` — true if `Resource == "pipelinerun"`
- `isCustomRun()` — true if `Resource == "customrun"`
- `isEventListener()` — true if `Resource == "eventlistener"`
- `isFinallyTask()` — true if `IsFinallyTask == true`
- `isIssueEvent()` / `isPREvent()` / `isCommentEvent()` / `isPushEvent()` — check `SCMEventType`
- `stateIn("a", "b", ...)` — vararg state membership check

Expression must return `bool`. Example: `'isPipelineRun() && stateIn("running", "success", "failure")'`

### Handler construction (factory pattern)

`cmd/receiver/main.go` calls `factory.BuildAll(cfg, log, opts...)` in `internal/factory/registry.go`:

1. **Build order**: SCM handlers → notifier handlers → accumulator (accumulator needs to look up already-registered providers)
2. For each provider type, `BuildAndRegister[C]()` iterates instances and delegates to the provider-specific factory
3. Each factory's `Build()` method calls `buildActionsWithMiddleware()` which:
   - Skips disabled actions (`action.Enabled == false`)
   - Builds the concrete handler via provider-specific `buildFn`
   - Wraps with `middleware.WrapWithCEL()` — compiles `when` expression, wraps with `ConditionalHandler`
   - Wraps with `middleware.WrapWithFilter()` — allow/deny lists per action
   - For `commit_status` actions, wraps with `middleware.WrapWithContextPerTask()`
4. All handlers registered in `notifier.Registry`
5. On config reload (`cmd/receiver/reload.go`): new registry + chain built atomically; the store survives rebuilds
6. `ErrUnsupportedActionType` silently skipped (allows partial action type support per provider)

### Handler dispatch

`Dispatcher.Handle()` in `internal/pipeline/dispatcher.go`:
- Filters handlers by provider name (SCM handlers only match if `env.Report.Provider == handler.Name()`)
- Notifier handlers (`ActionNotify`) always match (no provider gate)
- Fans out concurrently via `errgroup` with `maxConcurrency` limit
- Per-handler timeout via `handlerTimeout` config
- Errors are collected but don't stop other handlers; returns `errors.Join(errs...)`

### Store abstraction

`internal/store/store.go` — interface shared by deduper and accumulator:
- `memory` (default) — per-pod LRU, state lost on restart, single-replica only
- `valkey` — any RESP-compatible server (Redis/Valkey), shared across replicas
- `olric` — embedded distributed cache via memberlist gossip, zero extra deployments

### Config

`internal/config/config.go` — YAML loaded directly. Secrets use file-based resolution (`_file` suffix fields, e.g., `auth.secret_file`). `secrets.ResolveOrInfer()` resolves explicit path or infers `/etc/secrets/{provider}/{instance}/{key}`. Validated at load time; hot-reload validates before atomic swap.

Config structure (top-level keys):

```yaml
server:           # addr, metrics_addr, timeouts, max_body_size, rate_limit, auth, tls
dashboard_url:    # Tekton Dashboard base URL for TargetURL generation
filter:           # allow_taskrun, allow_pipelinerun, allow_customrun, allow_eventlistener, ignore_unknown
dedupe_size:      # LRU cache size (default 10000)
max_concurrency:  # dispatcher parallelism (default 100)
handler_timeout:  # per-handler deadline (default 10s)
retry:            # max_attempts (4), initial_backoff (250ms), max_backoff (30s)
store:            # backend: memory|valkey|olric, ttl, valkey{}, olric{}
dlq:              # enabled, path, max_size_bytes
accumulator:      # enabled, ttl, max_size, template, provider
scm:              # github[], gitlab[], gitea[], azure_devops[], bitbucket[], sourcehut[]
notifiers:        # slack[], teams[], discord[], pagerduty[], datadog[], webhook[], grafana[], sentry[], email[]
logging:          # level (info), verbose (caller, http_calls, payloads)
tracing:          # endpoint, service_name, insecure
```

### Hot reload

`cmd/receiver/reload.go` uses fsnotify to watch the config file. On change:
1. Load and validate new config
2. Build new handler registry + pipeline chain
3. Atomic pointer swap (registry, chain)
4. Store survives rebuilds (dedupe state preserved)
5. Metrics counter: `config_reloads_total`

Immutable sections (require restart): `server`, `store`, `dlq`, `logging`, `tracing`.

### HTTP endpoints

Exposed by `internal/http/server.go`:

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/` | POST | CloudEvents receiver (Tekton sink) |
| `/healthz` | GET | Liveness probe (always 200) |
| `/readyz` | GET | Readiness probe (JSON with per-handler status) |
| `/metrics` | GET | Prometheus metrics |
| `/api/v1/dlq` | GET | List dead letter queue entries (if DLQ enabled) |
| `/api/v1/dlq/replay` | POST | Replay a DLQ entry through the pipeline (if DLQ enabled) |

### HTTP client & retry

`internal/httpx/` provides a shared HTTP client with retry:
- Exponential backoff + jitter (upper half randomized)
- Honors `Retry-After` header
- Default: 4 attempts, 250ms initial, 30s max backoff
- Retries on: 408, 429, 5xx
- Shared connection pool: 100 max idle conns per host
- Debug transport redacts sensitive headers (Authorization, X-API-Key, etc.)

### Error handling

- `errors.RetryableError` (`internal/errors/`) → 503 back-pressure (Tekton retransmits)
- Permanent errors → DLQ (if enabled, file-based JSONL at `internal/dlq/`)
- Deduper fails open (processes event if store unavailable)
- DLQ: atomic writes via tmp+rename, oldest entries dropped when file exceeds `MaxSizeBytes` (default 10MB)

### Accumulator (F3)

`internal/accumulator/` — aggregates per-task TaskRun events into a single pipeline summary PR comment:
- Accumulates TaskRuns keyed by PipelineRun UID
- Flushes on terminal PipelineRun states (success, failure, canceled, error)
- Posts markdown summary via the registered SCM provider's PR comment handler
- Buffer: in-memory LRU (default) or shared via store.RunBuffer (valkey/olric)
- Custom templates supported via `accumulator.template` config field
- Registered last in `BuildAll()` so it can find already-registered SCM providers

### Metrics

`internal/metrics/registry.go` — 23 Prometheus collectors:
- Events: `events_received`, `events_processed`, `events_filtered`, `events_backpressure`, `events_unsupported_type`
- Handlers: `handler_duration`, `handler_timeouts`, `notifier_latency`, `handlers_registered`
- Deduper: `deduper_hits`, `dedupe_cache_size`, `deduper_evictions`
- Pipeline: `chain_duration`, `pipeline_errors`, `errors_permanent`
- HTTP: `http_request_duration`, `http_requests_total`, `http_requests_in_flight`
- Store: `store_errors`
- DLQ: `dlq_size`, `dlq_enqueued`
- Retry: `notifier_retries`, `notifier_rate_limit_hits`
- Config: `config_reloads`

### Tracing

`internal/tracing/` — OpenTelemetry OTLP HTTP exporter:
- Configured via `tracing.endpoint` (empty = disabled)
- Root span per HTTP request with method/target attributes
- Handler spans with handler name/type attributes
- Errors recorded on spans

### Secrets resolution

`internal/secrets/` — file-based secret resolution (Kubernetes secret mounts):
- `secrets.Resolve(filePath)` — reads file, trims whitespace
- `secrets.ResolveOrInfer(explicitPath, provider, instance, key)` — uses explicit path or infers `/etc/secrets/{provider}/{instance}/{key}`
- Path traversal protection via `sanitizePath()`
- All `_file` config fields use this pattern

## Adding a new notifier

1. Config struct in `internal/config/config.go` — add to `NotifiersConfig`
2. Factory in `internal/factory/<name>.go` — implement `HandlerFactory[C]`
3. Register in `internal/factory/registry.go` `buildNotifierHandlers()`
4. Handler in `internal/notifier/<name>/notifier.go` — implement `ActionHandler` with `Type() = ActionNotify`
5. Tests alongside source

## Adding a new SCM provider

1. Config struct in `internal/config/config.go` — add to `SCMConfig`
2. Validation in `internal/config/instance_validators.go`
3. Factory in `internal/factory/<name>.go`
4. Register in `internal/factory/registry.go` `buildSCMHandlers()`
5. Client in `internal/notifier/scm/<name>/client.go`
6. Handler files in `internal/notifier/scm/<name>/` — one per action type
7. Tests

**OAuth2 support**: If the provider supports OAuth2, use `resolveOAuth2Refresher()` from `factory/gitlab.go` (shared by all factories) to create a `TokenRefresher`. Pass it to the client via a transport-based or AuthSource-based constructor. NEVER call `client.Token()` at build time and pass the static string to handlers.

## Adding a new action type to existing provider

1. Add to `notifier.ActionType` constants in `internal/notifier/notifier.go`
2. Handler file in `internal/notifier/scm/<provider>/<action>.go`
3. Build function in factory's `Build()` method
4. Config `Action.Type` validation in `internal/config/`

## Key directories

| Path | Purpose |
|------|---------|
| `cmd/receiver/` | Binary entrypoint, app lifecycle, config reload |
| `internal/domain/` | Core types: Event, State, Resource (leaf, no deps) |
| `internal/event/` | Decoder interface, Envelope, Registry, annotation constants |
| `internal/event/tekton/` | 4 decoders: TaskRun, PipelineRun, CustomRun, EventListener |
| `internal/notifier/` | ActionHandler interface, Registry, Base (Template Method), FilteredHandler, ConditionalHandler |
| `internal/notifier/middleware/` | CEL wrap, filter wrap, context_per_task wrap |
| `internal/notifier/scm/` | Shared SCM: BaseClient, TokenRefresher, TokenTransport, templates, upsert markers, state maps, labels, limits, refs |
| `internal/notifier/scm/github/` | 8 handlers + go-github client + App auth |
| `internal/notifier/scm/gitlab/` | 6 handlers + GitLab SDK (SaaS + self-managed) |
| `internal/notifier/scm/gitea/` | 4 handlers + Gitea SDK |
| `internal/notifier/scm/azuredevops/` | 4 handlers + Azure DevOps SDK |
| `internal/notifier/scm/bitbucket/` | Dual-variant (cloud/server) with separate clients and handlers |
| `internal/notifier/scm/sourcehut/` | 1 handler (commit status only) + BaseClient |
| `internal/notifier/slack/` | Webhook + bot token, Block Kit |
| `internal/notifier/teams/` | Adaptive Cards via Incoming Webhooks |
| `internal/notifier/discord/` | Webhook + bot token, embeds |
| `internal/notifier/pagerduty/` | Events API v2 (trigger/resolve) |
| `internal/notifier/datadog/` | Events API v2 |
| `internal/notifier/webhook/` | Generic HTTP + gojq transform + auth |
| `internal/notifier/grafana/` | Grafana Annotations API |
| `internal/notifier/sentry/` | Releases + deploy markers |
| `internal/notifier/email/` | SMTP with STARTTLS/TLS/none |
| `internal/pipeline/` | Chain: Validator, Filter, Deduper, Enricher, Dispatcher, MetricsHandler, StatusTracker |
| `internal/factory/` | Generic `HandlerFactory[C]`, `BuildAll()`, per-provider factory files |
| `internal/config/` | Config structs, Load(), validation, env expansion |
| `internal/store/` | Store interface + memory, valkey, olric backends |
| `internal/http/` | HTTP server, CloudEvents handler, DLQ API, health endpoints |
| `internal/http/middleware/` | Auth, rate limit, logging, recovery, body limit |
| `internal/httpx/` | HTTP client with retry (exponential backoff + jitter, Retry-After aware) |
| `internal/cel/` | CEL compilation, evaluation, custom macros (isPR, stateIn, etc.) |
| `internal/cehttp/` | CloudEvent HTTP parsing (binary + structured mode) |
| `internal/metrics/` | 23 Prometheus collectors + HTTP middleware |
| `internal/tracing/` | OpenTelemetry OTLP setup + HTTP middleware |
| `internal/logging/` | Zap JSON logger setup |
| `internal/dlq/` | File-based dead letter queue (JSONL) |
| `internal/accumulator/` | F3: aggregates TaskRuns into pipeline summary PR comments |
| `internal/errors/` | RetryableError classification |
| `internal/secrets/` | File-based secret resolution with path traversal protection |

## HTTP middleware chain

Applied outermost-to-innermost in `internal/http/server.go`:

1. **Observability** (tracing + metrics) — outermost, runs first
2. **Auth** (optional, HMAC-SHA256 or Bearer, timestamp replay protection)
3. **Rate limit** (optional, per-source with TTL eviction, max 10000 entries)
4. **Panic recovery**
5. **Body limit** (default 1MB)
6. **Request logging** — innermost, runs last before handler

## SCM handler patterns

- Each SCM provider has a `Client` wrapping an SDK (`go-github`, `gitlab-api`, `gitea-sdk`) or `scm.BaseClient` (for providers without official SDKs: Bitbucket, SourceHut)
- Each action type has a separate handler implementing `ActionHandler`
- `notifier.Base` provides Template Method: `BuildPayload`, `BuildURL`, `Auth`, `Method` hooks + `Send()` with retry
- Upsert mode uses invisible HTML markers (`scm.Marker()`, `scm.HasMarker()`) for idempotent comments
- `scm.StateMap` maps `domain.State` to provider-specific status strings
- `scm.Limits` defines per-provider API field constraints with `Truncate()`
- Template rendering via `scm.CompileTemplate()` with sprig functions (dangerous ones like `env`, `expandenv`, `base64` stripped)
- GitHub App auth via RSA private key + installation token (`internal/notifier/scm/github/app_client.go`)
- Bitbucket has dual-variant support: `CloudClient` (Basic auth) and `ServerClient` (Bearer token) with separate handler implementations
- **Token refresh**: OAuth2 and GitHub App tokens auto-refresh via `scm.TokenRefresher` interface. GitHub handlers accept `HTTPDoer` (shared client with auto-refresh). GitLab uses `AuthSource` interface. Gitea uses `TokenTransport` (custom `http.RoundTripper`). NEVER pass static token strings to handler constructors.

## Notifier patterns

- All notifiers implement `ActionHandler` with `Type() = ActionNotify`
- Notifier handlers always match in dispatcher (no provider gate — unlike SCM handlers)
- Chat notifiers (Slack, Teams, Discord) support custom Go templates via `template` config field
- Webhook notifier supports gojq transform expressions for payload reshaping
- Webhook auth types: bearer, basic, apikey, hmac
- PagerDuty uses RunID as dedup_key for idempotency
- Sentry only acts on successful runs with a commit SHA
- Email uses raw SMTP (not `notifier.Base`); supports STARTTLS, implicit TLS, or unencrypted

## Testing

- **No testify** — standard `testing` package only. All assertions use `t.Fatal`, `t.Fatalf`, `t.Error`, `t.Errorf`
- Table-driven tests with `t.Run()` (config, CEL, webhook auth, validators)
- `httptest.NewServer` for SCM SDK mocking — GitHub, GitLab, Gitea SDKs make HTTP calls during `NewClient()`
- `alicebob/miniredis` for Valkey/Redis store tests
- Integration tests in `*_integration_test.go` files (`cmd/receiver/`, `internal/pipeline/`, `internal/accumulator/`, `internal/notifier/scm/`)
- `-short` flag skips expensive tests in CI/pre-commit
- Race detector always on (`-race`)
- Coverage: `-coverprofile=coverage.txt -covermode=atomic`

## Helm chart

### Installation from OCI registry

```bash
helm install tekton-events-relay \
  oci://ghcr.io/fabioluciano/charts/tekton-events-relay \
  --version 0.7.6 \
  --namespace tekton-events-relay --create-namespace \
  -f values.yaml
```

### Upgrade existing installation

```bash
helm upgrade tekton-events-relay \
  oci://ghcr.io/fabioluciano/charts/tekton-events-relay \
  --version 0.7.6 \
  --namespace tekton-events-relay \
  -f values.yaml
```

### Developer commands

```bash
mise run helm-lint
mise run helm-template       # render templates for debug
mise run helm-kubeconform    # K8s schema validation
mise run helm-security       # all security checks (kubelinter, kubesec, trivy, kubeconform)
```

- Chart at `charts/tekton-events-relay/`, OCI image: `ghcr.io/fabioluciano/tekton-events-relay`
- OCI registry: `oci://ghcr.io/fabioluciano/charts/tekton-events-relay`
- Template files (`charts/*/templates/*.yaml`) **excluded** from yamllint and check-yaml hooks
- 15 default Go templates in `configmap-templates.yaml` (accumulator, per-provider comment formats)
- Secrets mounted at `/etc/secrets/{provider}/{instance}/` — config references via `secretRef.name`/`secretRef.key`
- Safety guard: refuses multi-replica + memory store unless `unsafe.allowMemoryStoreWithMultipleReplicas=true`
- Config checksum annotation forces pod restart on ConfigMap change
- Values structure mirrors Go config: `config.server`, `config.filter`, `config.store`, `config.scm.github[]`, `config.notifiers.slack[]`, etc.

## Environment quirks

- `GOFLAGS="-mod=readonly"` is set in `.mise.toml` — run `go mod tidy` explicitly when deps change.
- **Wiki is a git submodule** at `wiki/` — `git submodule update --init` after clone.
- Docker image is **distroless nonroot** — no shell, no package manager.
- Config supports file-based secret resolution; never use environment variables for secrets.
- `go.mod` has a `replace` directive: `github.com/armon/go-metrics => github.com/hashicorp/go-metrics`

## Pipeline chain handlers — detailed behavior

### Validator (`internal/pipeline/validator.go`)
- Checks `Envelope != nil`, `CloudEventID != ""`, `RunName != ""`
- EventListener events skip provider check (no SCM context)
- Other events require `Provider != ""`
- `CommitSHA` is optional — handlers that need it (commit_status, check_run) validate individually

### Filter (`internal/pipeline/filter.go`)
- Drops events by resource type based on `AllowTaskRun`, `AllowPipelineRun`, `AllowCustomRun`, `AllowEventListener` config
- `IgnoreUnknown` drops `dev.tekton.event.*.unknown.v1` events (emitted on every Condition change — noise)
- Default: `AllowPipelineRun=true`, `IgnoreUnknown=true` (if no filter configured)

### Deduper (`internal/pipeline/deduper.go`)
- Uses `store.DedupeStore.FirstSeen(ctx, cloudEventID)` to check if event was seen before
- **Fails open**: if store is unavailable, event is processed (possible duplicate, never dropped)
- Increments `deduper_hits` metric on duplicate detection
- Dedupe state lives in the shared store, survives config reloads

### Enricher (`internal/pipeline/enricher.go`)
- Generates `TargetURL` (Tekton Dashboard link) if `DashboardURL` is configured and `TargetURL` is empty
- Format: `{dashboard_url}/#/namespaces/{namespace}/{kind}/{runName}`
- Kind mapping: taskrun→taskruns, pipelinerun→pipelineruns, customrun→customruns
- EventListener has no dashboard page (returns empty)

### Dispatcher (`internal/pipeline/dispatcher.go`)
- Filters handlers: SCM handlers match only if `env.Report.Provider == handler.Name()`; `ActionNotify` handlers always match
- Fans out via `errgroup.Group` with `SetLimit(maxConcurrency)`
- Per-handler timeout via `context.WithTimeout(handlerTimeout)`
- Records per-handler metrics: `handler_duration`, `notifier_latency`, `events_processed`
- Records per-handler status in `StatusTracker` (for `/readyz` endpoint)
- Returns `errors.Join(errs...)` — all errors collected, none stops other handlers

### MetricsHandler (`internal/pipeline/metrics.go`)
- Wraps any `Handler` with Prometheus timing and counter instrumentation
- Used to instrument each chain link: validator, filter, deduper, enricher, dispatcher
- `pipeline.WithMetrics(handler, durationObserver, counter, stepName)`

## Factory pattern — detailed walkthrough

### Generic factory (`internal/factory/factory.go`)
```go
type HandlerFactory[C any] interface {
    Build(cfg C, log *zap.Logger) ([]notifier.ActionHandler, error)
}
```
Each provider implements `HandlerFactory` with its config type (e.g., `GitHubFactory` implements `HandlerFactory[GitHubInstance]`).

### BuildAndRegister (`internal/factory/builder.go`)
```go
func BuildAndRegister[C any](instances []C, f HandlerFactory[C], log *zap.Logger, reg *notifier.Registry) error
```
Iterates instances, calls `f.Build()` for each, registers all returned handlers.

### buildActionsWithMiddleware (`internal/factory/builder.go`)
For each enabled action:
1. Call provider-specific `buildFn(action)` → concrete handler
2. If `ErrUnsupportedActionType` → skip silently (partial support is OK)
3. Wrap with `middleware.WrapWithCEL(handler, action.When, log)` → `ConditionalHandler`
4. Wrap with `middleware.WrapWithFilter(handler, action.Filter)` → `FilteredHandler`
5. If `action.Type == ActionCommitStatus` → wrap with `middleware.WrapWithContextPerTask(handler, action.ContextPerTask)`

### Provider-specific factories

Each factory file (`internal/factory/github.go`, etc.) implements:
- `Build(cfg InstanceConfig, log *zap.Logger) ([]notifier.ActionHandler, error)`
- Resolves secrets via `secrets.Resolve()` or `secrets.ResolveOrInfer()`
- Creates the SDK client (e.g., `github.NewClient(token, baseURL)`)
- Maps `config.Action.Type` to concrete handler constructors
- Returns `ErrUnsupportedActionType` for unsupported action types

### BuildAll order (`internal/factory/registry.go`)
1. `buildSCMHandlers()` — GitHub, GitLab, Bitbucket, Azure DevOps, Gitea, SourceHut
2. `buildNotifierHandlers()` — Slack, Teams, Discord, PagerDuty, Datadog, Webhook, Grafana, Sentry, Email
3. `BuildAccumulator()` — last, needs to look up already-registered SCM providers by name

## Config validation rules (`internal/config/instance_validators.go`)

### SCM provider validation

**GitHub**: enabled requires either `auth.secret_file` (token) OR `auth.app_id` + `auth.installation_id` (App). Cannot use both. App auth requires private key PEM at `/etc/github-app/private-key.pem`.

**GitLab**: `variant` must be `saas` or `self-managed`. Self-managed requires `base_url`. Auth requires either `auth.secret_file` OR `auth.oauth2` (client_credentials). Cannot use both.

**Gitea**: Auth requires either `auth.secret_file` OR `auth.oauth2`. Cannot use both. Requires `base_url`.

**Bitbucket**: `variant` must be `cloud` or `server`. Cloud requires `(username_file + app_password_file)` OR `oauth2`. Server requires `token_file` + `base_url`.

**Azure DevOps**: Requires `secret_file` when enabled. Requires `base_url`.

**SourceHut**: Requires `auth.secret_file` when enabled. Requires `base_url`.

### Notifier validation

**Slack/Discord**: Auth requires either `auth.webhook_url_file` OR `auth.bot_token`. Bot token requires `token_file` + `channel_id`.

**Teams**: Auth requires `auth.webhook_url_file`.

**PagerDuty**: Auth requires `auth.integration_key_file`.

**Datadog**: Auth requires `auth.api_key_file`.

**Webhook**: Requires `url_file` when enabled. Auth types validated: bearer→token_file, basic→username_file+password_file, apikey→token_file+header, hmac→secret_file.

**Grafana**: Requires `url` + `auth.token_file` when enabled.

**Sentry**: Requires `auth.token_file` + `org` when enabled.

**Email**: Requires `host`, `from`, `to` (at least one) when enabled. Port validated 1-65535.

### Common validation
- CEL `when` expressions compiled at config load time via `CELCompileFunc`
- Go `template` fields parsed at config load time
- `gojq` transform expressions parsed at config load time
- Duplicate instance names within the same provider type are rejected
- Label actions require at least one add or remove entry

## Notifier.Base — Template Method pattern (`internal/notifier/base.go`)

`Base` provides the common HTTP send flow for webhook-based notifiers:

```go
type Base struct {
    HTTP         *http.Client
    BuildPayload PayloadBuilder   // func(Event) (any, error)
    BuildURL     URLBuilder       // func(Event) (string, error)
    Auth         AuthApplier      // func(*http.Request) error
    Method       MethodSelector   // func(Event) string (default: POST)
    UserAgent    string
    Log          *zap.Logger
}
```

`Base.Send(ctx, event)` flow:
1. BuildURL → BuildPayload → JSON marshal
2. Create HTTP request with Content-Type/Accept/User-Agent headers
3. Apply auth
4. `httpx.DoWithRetryPolicy()` — retries with exponential backoff
5. Log delivery status (started/success/failed/error)

`DefaultHTTPClient` is a cached singleton with 10s timeout.

## ConditionalHandler (`internal/notifier/conditional.go`)

Wraps an `ActionHandler` with CEL guard:
- If `program == nil` → always delegates (no guard)
- If CEL eval returns `true` → delegates to inner handler
- If CEL eval returns `false` → logs debug, returns nil (skip)
- If CEL eval returns error → logs error, returns error (**fail-closed**)

## FilteredHandler (`internal/notifier/filter.go`)

Wraps an `ActionHandler` with allow/deny lists:
- Pre-built maps for O(1) lookup (case-insensitive, lowercase keys)
- Deny list checked first (deny wins)
- Empty name → pass through (cannot filter)
- Unknown resource type → pass through
- Per-resource-type filtering: tasks, pipelines, custom_runs, event_listeners

## SCM shared utilities

### Upsert markers (`internal/notifier/scm/upsert.go`)
- `Marker(runID, action)` → `<!-- tekton-events-relay:{runID}:{action} -->`
- `WithMarker(marker, body)` → prepends marker on its own line
- `HasMarker(body, marker)` → `strings.Contains`
- Mode: `create` (default) or `upsert` (edit existing marked comment)

### StateMap (`internal/notifier/scm/statemap.go`)
Maps `domain.State` to provider-specific strings. Example for GitHub:
- pending/running → "pending"
- success → "success"
- failure/error/canceled → "failure"

### Limits (`internal/notifier/scm/limits.go`)
Per-provider API field constraints:

| Provider | StatusDescription | StatusContext | CommentBody | LabelName |
|----------|------------------|--------------|-------------|-----------|
| github | 140 | 255 | 65000 | 50 |
| gitlab | 255 | N/A | 1000000 | 255 |
| bitbucket-cloud | 255 | 255 | 65000 | N/A |
| bitbucket-server | 255 | 255 | 65000 | N/A |
| azure-devops | 4000 | N/A | 65000 | 255 |
| gitea | 255 | 255 | 65000 | 50 |

`Truncate(s, limit)` shortens with "..." suffix. `Validate(provider, field, value)` checks limits.

### Labels (`internal/notifier/scm/labels.go`)
- `Label{Name, Color}` — color is hex without `#` (e.g., "d73a4a")
- `LabelSet{Add, Remove}` — Go templates evaluated against event
- Color validation via regex `^[0-9a-fA-F]{6}$`
- Template cache (`sync.Map`) for compiled label templates
- Removal runs before addition so overlapping names converge

### Cross-references (`internal/notifier/scm/references.go`)
Provider-specific markdown syntax:
- GitHub: `#123` (same repo), `owner/repo#123` (cross-repo), `!123` not used
- GitLab: `#123` (issue), `!123` (merge request — GitLab uses `!` for MRs)
- Azure DevOps: `#123` (no cross-repo support)
- SourceHut: no inline refs (email-based workflow)

## SCM providers — supported actions matrix

| Provider | commit_status | check_run | pr_comment | commit_comment | issue_comment | discussion_comment | label | deployment_status |
|----------|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|
| GitHub | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| GitLab | ✓ | — | ✓ | ✓ | — | — | ✓ | ✓ |
| Gitea | ✓ | — | ✓ | — | ✓ | — | ✓ | — |
| Bitbucket | ✓ | — | ✓ | — | — | — | — | — |
| Azure DevOps | ✓ | — | ✓ | — | — | — | ✓ | — |
| SourceHut | ✓ | — | — | — | — | — | — | — |

## Notifiers — implementation details

### Slack (`internal/notifier/slack/`)
- Webhook mode: POST to Slack Incoming Webhook URL
- Bot token mode: `chat.postMessage` API with `Authorization: Bearer {token}`
- Block Kit attachments with color-coded states (green=success, red=failure, yellow=running, gray=pending)
- Custom Go templates via `template` config field

### Teams (`internal/notifier/teams/`)
- Adaptive Card format via Incoming Webhooks
- POST to webhook URL with `Content-Type: application/json`
- Custom Go templates via `template` config field

### Discord (`internal/notifier/discord/`)
- Webhook mode: POST to Discord webhook URL
- Bot token mode: `channels/{id}/messages` API
- Discord embeds with color-coded states
- Custom Go templates via `template` config field

### PagerDuty (`internal/notifier/pagerduty/`)
- Events API v2: `trigger` on failure/error, `resolve` on success
- `dedup_key` = RunID (idempotency)
- Severity mapping from config

### Datadog (`internal/notifier/datadog/`)
- Events API v2 with auto-generated tags: state, context, namespace, run_id, resource

### Webhook (`internal/notifier/webhook/`)
- Generic HTTP POST with JSON payload
- Optional `gojq` transform expression for payload reshaping
- Auth types: bearer, basic, apikey, hmac
- Custom headers via `headers` config

### Grafana (`internal/notifier/grafana/`)
- Grafana Annotations API for deployment markers
- Uses `scm.CompileTemplate()` for text rendering

### Sentry (`internal/notifier/sentry/`)
- Creates Sentry releases (version=CommitSHA) and deploy markers
- Only acts on successful runs with a commit SHA

### Email (`internal/notifier/email/`)
- Raw SMTP (not `notifier.Base`)
- Encryption: `starttls` (default, port 587), `tls` (implicit, port 465), `none` (in-cluster)
- Subject and body are Go templates
- HTML mode supported
- Header sanitization (CR/LF stripping)

## DLQ — Dead Letter Queue (`internal/dlq/`)

- `Queue` interface: `Enqueue(ctx, envelope, cause)`, `List(ctx, limit)`, `Remove(ctx, id)`, `Size(ctx)`, `Close()`
- `FileQueue` implementation: JSONL file with atomic writes (tmp+rename)
- `DeadEvent`: ID (CloudEvent ID), FailedAt, Cause, RetryCount, Envelope
- Re-enqueueing existing ID replaces entry and bumps RetryCount
- Oldest entries dropped when file exceeds `MaxSizeBytes` (default 10MB)
- HTTP API: `GET /api/v1/dlq` (list), `POST /api/v1/dlq/replay` (replay through pipeline)

## Store backends — implementation details

### Memory (`internal/store/memory.go`)
- `hashicorp/golang-lru/v2` for dedupe (fixed capacity, LRU eviction)
- `expirable.LRU` for run buffer (TTL-based eviction)
- Per-pod, state lost on restart, single-replica only
- No network calls, zero latency

### Valkey (`internal/store/valkey.go`)
- Any RESP-compatible server (Redis/Valkey)
- Uses `go-redis/v9` client
- Lua script for atomic flush operations
- Shared across replicas, correct dedup at scale
- Password from mounted secret file

### Olric (`internal/store/olric.go`)
- Embedded distributed cache via memberlist gossip
- Zero extra deployments — relay pods form the cluster themselves
- Distributed locks for Add/Flush operations
- Peers discovered via headless K8s service
- Environment profiles: local, lan, wan

## CI/CD workflows (`.github/workflows/`)

### `ci.yaml` — PR orchestrator
Triggers on PRs to `main`, `rc/**`, `rc-*`. Calls 4 reusable workflows: `ci-go`, `ci-docker`, `ci-helm`, `security-codeql`. Concurrency: cancel superseded runs per PR.

### `ci-go.yaml` — Go quality + security
Go 1.26.2, golangci-lint v2.12.2. Checks: gofmt, goimports, go vet, golangci-lint, build, test (`-race -count=1 -timeout=5m -short`), go mod tidy (no diff). Security: Gitleaks + Trivy.

### `ci-helm.yaml` — Helm validation
6 parallel jobs: helm-lint, kubeconform (K8s 1.29-1.31), kubesec (score ≥ 0), kube-linter, trivy, yamllint.

### `ci-docker.yaml` — Docker validation
Hadolint + Docker Buildx build + Trivy image scan.

### `release.yaml` — Release pipeline
Push to `main`/`rc/**`: CI → semantic-release → multi-platform Docker build (amd64+arm64, no QEMU) → Cosign sign → Helm package → push OCI → ArtifactHub metadata.

### `security-codeql.yaml` — Code analysis
CodeQL for Go + GitHub Actions. Weekly schedule + PR triggers.

## Helm values structure

```yaml
replicaCount: 1                    # >1 requires valkey/olric store
image:
  repository: ghcr.io/fabioluciano/tekton-events-relay
  tag: ""                          # defaults to Chart.appVersion
config:
  server:
    addr: ":8080"
    metrics_addr: ":9090"
  filter:
    allow_taskrun: false
    allow_pipelinerun: true
  store:
    backend: memory                # memory|valkey|olric
    valkey:
      embedded:
        enabled: false             # uses subchart
  scm:
    github:
      - name: github
        enabled: false
        auth:
          secretRef:
            name: github-token
            key: token
        actions:
          - name: commit-status
            type: commit_status
            enabled: true
  notifiers:
    slack:
      - name: slack
        enabled: false
        webhook_url:
          secretRef:
            name: slack-webhook
            key: webhook_url
        channel: "#ci"
securityContext:
  runAsNonRoot: true
  runAsUser: 65532
  readOnlyRootFilesystem: true
  allowPrivilegeEscalation: false
  capabilities:
    drop: [ALL]
```

Secret mount pattern: `secretRef.name` + `secretRef.key` → mounted at `/etc/secrets/{provider}/{instance}/{key}` → config references via `{provider}.{instance}.auth.{field}_file`.

## Cross-package dependency graph

```
domain (leaf — no deps)
  ↑
event (depends on domain)
  ↑
event/tekton (depends on domain, event)
  ↑
notifier (depends on domain, httpx, cel)
  ↑
notifier/middleware (depends on notifier, cel, config)
notifier/scm (depends on domain, httpx, notifier)
notifier/scm/github (depends on notifier, notifier/scm, httpx, domain)
notifier/scm/gitlab (depends on notifier, notifier/scm, httpx, domain)
... (same pattern for all SCM providers)
notifier/slack (depends on notifier, domain)
... (same pattern for all notifiers)
  ↑
pipeline (depends on event, domain, notifier, store, metrics)
  ↑
factory (depends on config, notifier, notifier/middleware, notifier/scm, secrets, accumulator)
  ↑
http (depends on cehttp, event, pipeline, metrics, dlq, errors, http/middleware, tracing, secrets)
  ↑
cmd/receiver (depends on everything)
```

Leaf packages (no internal deps): `domain`, `errors`, `secrets`, `logging`, `metrics`, `cehttp`.

## Testing patterns — detailed

### Table-driven tests
```go
tests := []struct {
    name     string
    input    string
    wantErr  bool
    wantVal  string
}{
    {name: "valid", input: "github", wantErr: false},
    {name: "invalid", input: "unknown", wantErr: true},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        // ...
    })
}
```

### httptest mock servers
SCM SDK clients make HTTP calls during `NewClient()`. Tests use `httptest.NewServer` to mock API responses:
```go
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    // mock response
}))
defer srv.Close()
client, err := github.NewClient("token", srv.URL)
```

### miniredis for store tests
```go
mr := miniredis.RunT(t)
defer mr.Close()
st, err := store.New(store.StoreConfig{
    Backend: "valkey",
    Valkey: store.ValkeyConfig{Address: mr.Addr()},
}, store.Options{...})
```

### Integration tests
Files named `*_integration_test.go` test end-to-end flows:
- `cmd/receiver/cel_integration_test.go` — CEL expression evaluation through full pipeline
- `cmd/receiver/integration_backpressure_test.go` — 503 back-pressure on retryable errors
- `internal/pipeline/integration_test.go` — full chain: decode→filter→dedupe→enrich→dispatch
- `internal/accumulator/integration_test.go` — TaskRun accumulation and flush
- `internal/notifier/scm/integration_test.go` — SCM handler end-to-end

### Spy/mock handlers
```go
type mockHandler struct {
    callCount int
    lastEvent domain.Event
    err       error
}
func (m *mockHandler) Handle(ctx context.Context, e domain.Event) error {
    m.callCount++
    m.lastEvent = e
    return m.err
}
```

## Debugging tips

- **Config validation**: `./tekton-events-relay --config path --validate` catches errors before starting
- **Readyz endpoint**: `GET /readyz` shows per-handler status (last event, last error, success/failure counts)
- **Metrics**: `GET /metrics` — check `handler_duration`, `events_processed`, `errors_permanent`, `deduper_hits`
- **DLQ inspection**: `GET /api/v1/dlq` lists failed events; `POST /api/v1/dlq/replay` re-processes them
- **Debug logging**: Set `logging.level: debug` + `logging.verbose.payloads: true` to see CloudEvent payloads
- **Tracing**: Set `tracing.endpoint` to see OTLP traces per handler execution
- **Config reload**: Send `SIGHUP` or modify the config file (fsnotify watches the directory)
- **Immutable sections**: `server`, `store`, `dlq`, `logging`, `tracing` changes require restart (logged as warnings)
- **CEL debugging**: Invalid CEL expressions are caught at config load time; runtime eval errors are logged with handler name

## Event decoders — detailed behavior

### TaskRunDecoder (`internal/event/tekton/taskrun_decoder.go`)
- Handles: `dev.tekton.event.taskrun.v1.*`
- Extracts from annotations: provider, repo owner/name, commit SHA, PR/issue/discussion numbers
- Extracts from metadata: namespace, name (RunName), uid (RunID), TaskName (from `tekton.dev/task` label), PipelineName (from `tekton.dev/pipeline` label), PipelineTaskName (from `tekton.dev/pipelineTask` label)
- Extracts from status: state (mapped from event suffix), startTime, completionTime, taskResults
- `IsFinallyTask` from `tekton.dev/memberOf` label = "finally"

### PipelineRunDecoder (`internal/event/tekton/pipelinerun_decoder.go`)
- Handles: `dev.tekton.event.pipelinerun.v1.*`
- Same annotation extraction as TaskRun
- Extracts `TaskCount` from `status.childReferences` length
- PipelineName from `tekton.dev/pipeline` label

### CustomRunDecoder (`internal/event/tekton/customrun_decoder.go`)
- Handles: `dev.tekton.event.customrun.v1.*`
- Minimal extraction — custom runs have user-defined specs

### EventListenerDecoder (`internal/event/tekton/eventlistener_decoder.go`)
- Handles: `dev.tekton.event.eventlistener.v1.*`
- Two sub-flows:
  - `started.v1`: extracts SCM context from webhook headers (X-Gitea-Event, X-Github-Event, X-Gitlab-Event, X-Event-Key)
  - Lifecycle events (successful/failed/done): maps to done/success/failure states
- Gitea checked before GitHub (Gitea sends X-GitHub-Event for compatibility)
- `normalizeGitLabEvent()`, `normalizeBitbucketEvent()`, `normalizeGiteaEvent()` canonicalize webhook event types

## OAuth2 support

`internal/notifier/scm/oauth2/client.go` — OAuth2 client_credentials grant:
- Configured via `auth.oauth2` in GitLab and Gitea instances
- Fields: `client_id_file`, `client_secret_file`, `token_url`
- Token cached and auto-refreshed before expiry
- Used by `scm.BaseClient` as the auth function

### Token refresh mechanism (`internal/notifier/scm/token.go`)

All SCM providers use a unified token refresh pattern to avoid stale tokens:

**`TokenRefresher` interface** — provides a valid token, refreshing automatically:
```go
type TokenRefresher interface {
    Token(ctx context.Context) (string, error)
}
```

**Implementations:**
- `StaticToken` — wraps a fixed token (PATs, static credentials)
- `oauth2.Client` — auto-refreshes via `x/oauth2` TokenSource
- `github.AppClient` — auto-refreshes JWT→installation token

**Provider-specific strategies:**

| Provider | Strategy | Files changed |
|----------|----------|---------------|
| **GitHub** | All handlers accept `HTTPDoer` interface (shared client with auto-refresh) | handler configs, `comment_common.go` |
| **GitLab** | `NewClientWithRefresher()` — uses SDK's `AuthSource` interface for per-request token injection | `gitlab/client.go`, factory |
| **Gitea** | `NewClientWithRefresher()` — uses `TokenTransport` (custom `http.RoundTripper`) | `gitea/client.go`, factory |
| **Bitbucket Cloud** | `resolveOAuth2Refresher()` — factory fetches token via refresher | factory only |

**Key rule: NEVER call `client.Token()` at factory build time and pass the static string to handlers.** The token will expire. Always pass the `TokenRefresher` or `HTTPDoer` so tokens refresh at request time.

**`TokenTransport`** (`internal/notifier/scm/token.go`) — `http.RoundTripper` that injects fresh tokens:
- `AuthStyleBearer` — `Authorization: Bearer {token}` (OAuth2 standard)
- `AuthStyleToken` — `Authorization: token {token}` (Gitea convention)
- `AuthStyleHeader` — custom header (e.g., `PRIVATE-TOKEN` for GitLab)

## Webhook transform (`internal/notifier/webhook/transform.go`)

The webhook notifier supports `gojq` transform expressions:
- Input: the full JSON payload (event data)
- Output: transformed payload sent to the webhook URL
- Compiled at factory build time via `gojq.Parse()`
- Example: `del(.results) | .status = .state` removes results field and renames state

## Webhook auth (`internal/notifier/webhook/auth.go`)

Four auth types with specific header patterns:
- `bearer`: `Authorization: Bearer {token}`
- `basic`: `Authorization: Basic {base64(username:password)}`
- `apikey`: Custom header (e.g., `X-API-Key: {token}`)
- `hmac`: `X-Hub-Signature-256: sha256={hmac_hex}` (HMAC-SHA256 of payload)

## Notifier Registry (`internal/notifier/notifier.go`)

Thread-safe registry indexed by name and type:
- `Register(h)` — appends to handlers list, byName map, byType map
- `FindByName(name)` — returns all handlers for a provider (e.g., all "github" handlers)
- `FindByType(type)` — returns all handlers of an action type (e.g., all commit_status handlers)
- `All()` — returns copy of all handlers (used by Dispatcher fan-out)
- `Lookup(name)` — returns first handler with given name
- `Names()` — sorted, deduplicated provider names (cached)

## Config loading — detailed flow (`internal/config/config.go`)

1. `os.ReadFile(path)` — read YAML file
2. `yaml.Unmarshal` — parse into `Config` struct
3. `applyDefaults(&cfg)` — fill zero-value fields with defaults:
   - `Server.Addr` → `:8080`, `ReadTimeoutSec` → 10, `WriteTimeoutSec` → 10, `ShutdownTimeoutSec` → 30
   - `DedupeSize` → 10000, `MaxConcurrency` → 100, `HandlerTimeout` → 10s
   - `Retry.MaxAttempts` → 4, `InitialBackoff` → 250ms, `MaxBackoff` → 30s
   - `Store.Backend` → "memory", `Store.TTL` → 1h
   - `DLQ.Path` → "/var/lib/tekton-events-relay/dlq.jsonl", `MaxSizeBytes` → 10MB
   - `Filter.AllowPipelineRun` → true, `IgnoreUnknown` → true (if no filter configured)
   - `Logging.Level` → "info"
5. `cfg.Validate()` — struct tag validation + custom validators

## Metrics collectors — full list (`internal/metrics/registry.go`)

```go
type Collectors struct {
    // Events
    EventsReceived       *prometheus.CounterVec   // {type}
    EventsProcessed      *prometheus.CounterVec   // {handler, status}
    EventsFiltered       prometheus.Counter
    EventsBackpressure   prometheus.Counter
    EventsUnsupportedType *prometheus.CounterVec  // {type}

    // Handlers
    HandlerDuration      *prometheus.HistogramVec // {handler}
    HandlerTimeouts      *prometheus.CounterVec   // {handler}
    NotifierLatency      *prometheus.HistogramVec // {handler, type}
    HandlersRegistered   prometheus.Gauge

    // Deduper
    DeduperHits          prometheus.Counter
    DedupeCacheSize      prometheus.Gauge
    DeduperEvictions     prometheus.Counter

    // Pipeline
    ChainDuration        prometheus.Histogram
    PipelineErrors       *prometheus.CounterVec   // {step, status}
    ErrorsPermanent      *prometheus.CounterVec   // {reason}

    // HTTP
    HTTPRequestDuration  *prometheus.HistogramVec // {method, status}
    HTTPRequestsTotal    *prometheus.CounterVec   // {method, status}
    HTTPRequestsInFlight prometheus.Gauge

    // Store
    StoreErrors          *prometheus.CounterVec   // {backend, operation}

    // DLQ
    DLQSize              prometheus.Gauge
    DLQEnqueued          prometheus.Counter

    // Retry
    NotifierRetries      *prometheus.CounterVec   // {host, reason}
    NotifierRateLimitHits *prometheus.CounterVec  // {host}

    // Config
    ConfigReloads        *prometheus.CounterVec   // {result}
}
```

## HTTP middleware — detailed

### Auth (`internal/http/middleware/auth.go`)
- HMAC-SHA256: validates `X-Hub-Signature-256` header against request body
- Bearer: validates `Authorization: Bearer <token>` header
- Timestamp replay protection: `X-Webhook-Timestamp` header (unix seconds) within `TimestampTolerance` (default 5m)
- Secret resolved from file via `secrets.Resolve()`

### Rate limit (`internal/http/middleware/ratelimit.go`)
- Per-source rate limiting keyed on `Ce-Source` header, falls back to remote IP
- Token bucket algorithm via `golang.org/x/time/rate`
- Max 10000 entries with 5-minute TTL eviction
- Background cleanup goroutine every 60 seconds
- `RateLimiter.Stop()` registered on server shutdown

### Request logging (`internal/http/middleware/logging.go`)
- Generates `X-Request-Id` header (UUID) if not present
- Logs method, path, status, duration, request ID
- Logs at INFO level

### Panic recovery (`internal/http/middleware/recovery.go`)
- Catches panics, returns 500
- Logs stack trace at ERROR level

### Body limit (`internal/http/middleware/bodylimit.go`)
- Wraps `http.Request.Body` with `io.LimitReader`
- Returns 413 if body exceeds `MaxBodySize` (default 1MB)

## Tracing middleware (`internal/tracing/middleware.go`)

- Creates root span per HTTP request
- Span name: `HTTP {method}`
- Attributes: `http.method`, `http.target`
- Errors recorded on span
- Span ended on response

## Logging (`internal/logging/logger.go`)

- JSON encoding, ISO8601 timestamps
- Levels: debug, info, warn, error
- Verbose options (debug only): Caller (file:line), HTTPCalls, Payloads
- `zap.Logger` used throughout

## Secrets resolution (`internal/secrets/`)

### `secrets.Resolve(filePath, log)`
- Reads file, trims whitespace
- Returns error if file doesn't exist

### `secrets.ResolveOrInfer(explicitPath, provider, instance, key, log)`
- If `explicitPath` is set, reads from it
- Otherwise infers `/etc/secrets/{provider}/{instance}/{key}`
- Used by all factory files for secret resolution

### Path traversal protection
- `sanitizePath()` rejects paths with `..` components
- Prevents directory traversal attacks

## Configuration — full annotated example

```yaml
server:
  addr: ":8080"
  metrics_addr: ":9090"
  read_timeout_sec: 10
  write_timeout_sec: 10
  shutdown_timeout_sec: 30
  max_body_size: 1048576  # 1MB
  rate_limit:
    enabled: true
    requests_per_second: 100
    burst: 200
  auth:
    enabled: true
    type: hmac-sha256
    secret_file: /etc/secrets/webhook/hmac-secret
    validate_timestamp: true
    timestamp_tolerance: 5m
  tls:
    cert_file: /etc/tls/tls.crt
    key_file: /etc/tls/tls.key

dashboard_url: "https://tekton-dashboard.example.com"

filter:
  allow_taskrun: true
  allow_pipelinerun: true
  allow_customrun: false
  allow_eventlistener: false
  ignore_unknown: true

dedupe_size: 10000
max_concurrency: 100
handler_timeout: 10s

retry:
  max_attempts: 4
  initial_backoff: 250ms
  max_backoff: 30s

store:
  backend: memory  # memory | valkey | olric
  ttl: 1h
  valkey:
    address: "valkey:6379"
    password_file: /etc/secrets/valkey/password
    db: 0
    key_prefix: "ter:"
  olric:
    bind_addr: "0.0.0.0"
    bind_port: 12320
    memberlist_port: 12321
    peers: ["tekton-events-relay-0.tekton-events-relay:12321"]
    env: lan

dlq:
  enabled: true
  path: /var/lib/tekton-events-relay/dlq.jsonl
  max_size_bytes: 10485760  # 10MB

accumulator:
  enabled: true
  ttl: 10m
  max_size: 1000
  template: ""
  provider:
    name: github

logging:
  level: info
  verbose:
    caller: false
    http_calls: false
    payloads: false

tracing:
  endpoint: "http://otel-collector:4318"
  service_name: tekton-events-relay
  insecure: true

scm:
  github:
    - name: github
      enabled: true
      auth:
        secret_file: /etc/secrets/github/token
      actions:
        - name: task-checks
          type: commit_status
          enabled: true
          context_per_task: true
        - name: pr-summary
          type: pr_comment
          enabled: true
          mode: upsert
          when: 'isPipelineRun() && stateIn("running", "success", "failure")'
        - name: ci-labels
          type: label
          enabled: true
          labels:
            add: ["ci::{{.State}}"]
            remove: ["ci::running", "ci::success", "ci::failure"]

notifiers:
  slack:
    - name: prod-alerts
      enabled: true
      auth:
        webhook_url_file: /etc/secrets/slack/webhook-url
      channel: "#prod-alerts"
      when: 'event.Namespace == "production" && stateIn("failure", "error")'
```

## Go code conventions

- Package comments on every package (required by `golangci-lint`)
- Exported types and functions have doc comments
- `sync.OnceValue` for lazy singletons (e.g., `DefaultHTTPClient`)
- `atomic.Pointer` for hot-swappable state (registry, chain)
- `errgroup.Group` with `SetLimit()` for bounded concurrency
- `context.WithTimeout` for per-handler deadlines
- `errors.Join` for collecting multiple errors
- `fmt.Errorf` with `%w` for error wrapping
- No global mutable state except process-wide defaults (retry policy, metrics)
- `zap.Logger` passed explicitly, never global
