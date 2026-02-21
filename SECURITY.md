# Security Policy

## Supported versions

| Version | Supported |
|---------|-----------|
| `0.1.x` (latest) | Yes |
| older | No |

## Reporting a vulnerability

**Please do not open a public GitHub issue for security vulnerabilities.**

Report vulnerabilities privately via GitHub's built-in security advisory system:

1. Go to **[Security → Report a vulnerability](https://github.com/gyaan/fluxflow/security/advisories/new)**
2. Fill in the description, affected versions, and steps to reproduce.
3. You will receive an acknowledgement within **48 hours** and a status update within **7 days**.

If you prefer email, reach the maintainer at the address listed on the [GitHub profile](https://github.com/gyaan).

## Scope

Areas most relevant to security review:

| Area | Risk |
|------|------|
| `internal/condition/expression.go` | Regex injection via `matches` operator; untrusted expression strings |
| `internal/config/loader.go` | Path traversal if config path is user-controlled |
| `internal/api/handler.go` | JSON payload size limits, event type injection |
| `internal/action/points/reward.go` | Formula evaluation against untrusted payload values |

## Out of scope

- Denial-of-service via large but valid payloads (use a reverse proxy for request size limits)
- Issues only reproducible on unsupported Go versions
- Theoretical vulnerabilities without a proof-of-concept

## Disclosure policy

Once a fix is merged and released, a [GitHub Security Advisory](https://github.com/gyaan/fluxflow/security/advisories) will be published with full details, credit to the reporter, and CVE assignment if applicable.
