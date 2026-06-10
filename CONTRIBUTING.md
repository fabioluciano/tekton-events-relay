# Contributing

## Setup

```bash
git clone https://github.com/fabioluciano/tekton-events-relay
cd tekton-events-relay
make test
```

## Conventional Commits

**Required.** semantic-release uses the title of each commit (and this PR)
to calculate the version and generate the CHANGELOG automatically.

| Prefix | When to use | Version |
|---|---|---|
| `feat: description` | new feature | minor `1.x.0` |
| `fix: description` | bug fix | patch `1.0.x` |
| `perf: description` | performance improvement | patch |
| `refactor: description` | refactoring without behavior change | patch |
| `docs: description` | documentation | no release |
| `test: description` | tests | no release |
| `chore: description` | maintenance, dependencies | no release |
| `ci: description` | CI/CD | no release |
| `feat!: description` | breaking change | major `x.0.0` |

Breaking change can also be declared in the body:

```
feat: new decoder for Jenkins

BREAKING CHANGE: the event.Decoder interface gained a Version() string method
```

### Valid examples

```
feat: add Jenkins decoder
fix: github reporter returns 500 on missing owner annotation  
perf: replace sync.Map with RWMutex in registry
refactor: extract truncate helper to internal/stringx
docs: add tekton example for Gitea
test: add coverage for azure devops builder
chore: upgrade distroless base image to nonroot-debian12
ci: cache go modules between PR runs
feat!: rename scm.provider label to pipeline.scm.provider
```

## Adding a new decoder (pipeline engine)

1. Create `internal/event/<name>/decoder.go` implementing `event.Decoder`.
2. Add tests in `internal/event/<name>/decoder_test.go`.
3. Register in `cmd/receiver/main.go` inside `buildDecoders()`.
4. Add examples in `docs/examples/`.

## Adding a new SCM provider

1. Create `internal/scm/<name>/reporter.go` implementing `scm.Reporter`.
2. Add tests in `internal/scm/adapters_test.go`.
3. Register in `cmd/receiver/main.go` inside `buildRegistry()`.
4. Add config in `internal/config/config.go`.
5. Add PipelineRun/Workflow example in `docs/examples/`.

## Checklist before PR

```bash
make fmt              # format
make vet              # static analysis
make test             # tests with race detector
golangci-lint run     # linting (config in .golangci.yml)
```

## Pre-commit hooks

The repository uses [pre-commit](https://pre-commit.com) to ensure quality
before each commit. The hooks cover: `gofmt`, `go vet`, `go build`,
`go test -race`, Conventional Commits validation, secret detection
(gitleaks), and YAML linting.

```bash
# Install pre-commit (once per machine)
pip install pre-commit

# Install the hooks in the repository
pre-commit install                            # pre-commit hook
pre-commit install --hook-type commit-msg     # commit-msg hook (Conventional Commits)

# Run manually on all files
pre-commit run --all-files
```

## Commit message template

Configure Git to use the commit message template:

```bash
git config commit.template .gitmessage
```

The template documents the types, scopes, and examples directly in the commit
editor, without needing to consult external documentation.
