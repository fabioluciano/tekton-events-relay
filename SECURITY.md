# Security Policy

## Supported Versions

We release patches for security vulnerabilities in the following versions:

| Version | Supported          |
| ------- | ------------------ |
| latest  | :white_check_mark: |
| < 1.0   | :x:                |

## Reporting a Vulnerability

If you discover a security vulnerability, please report it privately via one of these methods:

### GitHub Security Advisories (Recommended)

Report via GitHub's private vulnerability reporting:
1. Go to https://github.com/fabioluciano/tekton-events-relay/security/advisories/new
2. Click "Report a vulnerability"
3. Fill in the details
4. Submit

This is the preferred method as it allows coordinated disclosure.

### Email

Alternatively, send an email to the maintainer:
- **Email**: Create an issue at https://github.com/fabioluciano/tekton-events-relay/issues with `[SECURITY]` prefix

**Please do not report security vulnerabilities through public GitHub issues.**

## What to Include

When reporting a vulnerability, please include:

- **Description**: Clear description of the vulnerability
- **Impact**: What an attacker could achieve
- **Reproduction**: Step-by-step instructions to reproduce
- **Affected versions**: Which versions are vulnerable
- **Suggested fix**: If you have one (optional)

## Response Timeline

- **Initial response**: Within 48 hours
- **Confirmation**: Within 7 days
- **Fix timeline**: Depends on severity
  - Critical: Within 7 days
  - High: Within 30 days
  - Medium/Low: Within 90 days

## Security Best Practices

When deploying tekton-events-relay:

### Secrets Management

- **Never commit tokens** in `config.yaml`
- Use environment variables: `${GITHUB_TOKEN}`, `${GITLAB_TOKEN}`, etc.
- Use Kubernetes Secrets or external secret managers (Vault, SOPS, Sealed Secrets)

### Network Security

- **Enable TLS**: Run behind an ingress controller with TLS termination
- **Network policies**: Restrict ingress to pipeline engines (Tekton)
- **Egress filtering**: Allow outbound only to SCM provider APIs

### RBAC

- **Minimal ServiceAccount**: Don't grant unnecessary Kubernetes permissions
- **Read-only tokens**: Use read-write SCM tokens only when needed
- **Scope tokens**: GitHub fine-grained tokens, GitLab project-scoped tokens

### Runtime Security

- **Read-only root filesystem**: Set `securityContext.readOnlyRootFilesystem: true`
- **Non-root user**: Default Dockerfile runs as UID 10001
- **Drop capabilities**: `securityContext.capabilities.drop: ["ALL"]`
- **Security scanning**: Enable Trivy, hadolint, and kubesec in CI (already configured)

### Monitoring

- **Audit logs**: Monitor CloudEvents ingestion for anomalies
- **Rate limiting**: Set up rate limits at ingress level
- **Alerting**: Configure PagerDuty/Slack for failed notifications

## Security Scanning

This project uses:

- **Trivy**: Docker image vulnerability scanning
- **hadolint**: Dockerfile linting
- **kubesec**: Kubernetes manifest security analysis
- **gosec**: Go code security scanning (via golangci-lint)
- **Dependabot**: Automated dependency updates

All scans run on every PR and upload results to GitHub Security tab.

## Known Issues

Check the [Security Advisories](https://github.com/fabioluciano/tekton-events-relay/security/advisories) page for disclosed vulnerabilities.

## Credits

We appreciate security researchers who responsibly disclose vulnerabilities. If you'd like to be credited in the CHANGELOG, let us know when you report.
