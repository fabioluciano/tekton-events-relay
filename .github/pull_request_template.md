## Description

<!-- What does this PR do? Why? -->

## Type of change

<!-- Check what applies. The type defines the version bump in semantic-release. -->

- [ ] `feat:` — new feature (minor bump)
- [ ] `fix:` — bug fix (patch bump)
- [ ] `perf:` — performance improvement (patch bump)
- [ ] `refactor:` — refactoring without behavior change (patch bump)
- [ ] `docs:` — documentation only (no release)
- [ ] `test:` — tests only (no release)
- [ ] `chore:` — maintenance, dependencies (no release)
- [ ] `ci:` — CI/CD changes (no release)
- [ ] `feat!:` / `BREAKING CHANGE:` — breaking change (major bump)

## How to test

<!-- Commands or steps to validate the change locally. -->

```bash
go test ./...
```

## Checklist

- [ ] `golangci-lint run` passes without errors
- [ ] `go test ./...` passes without errors
- [ ] This PR title follows Conventional Commits (`feat: ...`, `fix: ...`)
- [ ] New code has tests
- [ ] Documentation updated if necessary
