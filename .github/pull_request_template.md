## What

<!-- One paragraph: what changed and why. Link issues if applicable. -->

## Type

<!-- Defines the semantic-release version bump. Pick one. -->

- [ ] `feat:` — new SCM provider, notifier, CEL macro, or config field (minor)
- [ ] `fix:` — bug fix in event handling, config validation, or notifier delivery (patch)
- [ ] `perf:` — throughput / latency improvement (patch)
- [ ] `refactor:` — internal restructuring, no behavior change (patch)
- [ ] `feat!:` / `BREAKING CHANGE:` — config schema or API change requiring migration (major)
- [ ] `ci:` — workflow or release pipeline change (no release)
- [ ] `chore:` / `docs:` / `test:` — maintenance (no release)

## Scope

<!-- Check all that apply. -->

- [ ] Config model / validation (`internal/config/`)
- [ ] SCM provider (`internal/notifier/scm/`)
- [ ] Notifier (`internal/notifier/discord|slack|teams|…`)
- [ ] CEL expressions (`internal/cel/`)
- [ ] Event decoding (`internal/event/`)
- [ ] Pipeline / dispatcher (`internal/pipeline/`)
- [ ] Helm chart (`charts/`)
- [ ] CI / release workflows (`.github/workflows/`)

## Config changes

<!-- If you changed the Go config struct, confirm these were updated: -->

- [ ] `internal/config/config.go` — struct fields / YAML tags
- [ ] `internal/config/validator_helpers.go` — validation rules
- [ ] `charts/tekton-events-relay/values.yaml` — Helm defaults
- [ ] `charts/tekton-events-relay/values.schema.json` — JSON schema
- [ ] `docs/` — documentation / examples
- [ ] N/A — no config changes

## Testing

```bash
# minimum
go test ./...

# if touching config/validation
go test ./internal/config/... -v -run TestValidate

# if touching a notifier
go test ./internal/notifier/... -v

# full lint
golangci-lint run ./...
```

<!-- Describe any manual testing done against a real cluster or webhook payload. -->

## Checklist

- [ ] PR title follows Conventional Commits (`feat: …`, `fix: …`, `feat!: …`)
- [ ] `go test ./...` passes
- [ ] `golangci-lint run ./...` passes
- [ ] `pre-commit run --all-files` passes
- [ ] Helm chart updated if runtime config changed
- [ ] No secrets or credentials in diff
