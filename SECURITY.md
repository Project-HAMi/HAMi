# Security Policy

## Supported Versions

The following table outlines which versions of HAMi receive security updates:

| Version | Supported          |
|---------|--------------------|
| 2.9.x   | ✅ Security fixes |
| 2.8.x   | ✅ Security fixes |
| before 2.8.0   | ❌ No longer supported |

## Reporting a Vulnerability

If you discover a security vulnerability in HAMi, we strongly encourage you to report it responsibly. Please **do not** disclose security vulnerabilities publicly without following our responsible disclosure process.

### How to Report
- **GitHub Security Advisories**: [submit a private vulnerability report via GitHub](https://github.com/Project-HAMi/HAMi/security/advisories/new).
- **Bug Bounty**: Currently, HAMi does not offer a public bug bounty program.

### Information to Include
When reporting a security issue, please include:
- A clear and concise description of the vulnerability.
- Steps to reproduce the issue.
- Any potential attack scenarios or security impact.
- Suggested mitigations or fixes, if available.

## Is It In Scope?

HAMi's in-container enforcement (HAMi-core/libvgpu and vendor libraries) limits GPU memory and compute for cooperative multi-tenant sharing on a trusted cluster. It is not a hard security boundary against a workload with enough privilege to bypass its own hook, for example by unsetting `LD_PRELOAD`, using a static binary, or `ptrace`.

- A report that a workload can exceed its own quota, without affecting another tenant, is not a new vulnerability by itself.
- A report that lets a workload reach another tenant's data, device, or namespace it was not granted is in scope, please report it through the process above.

## Response Process

We follow a structured process to handle security reports:

Response times could be affected by weekends, holidays, breaks or time zone differences. That said, the maintainers will endeavour to reply as soon as possible, ideally within 5 working days.


## Third-Party Dependencies

HAMi relies on third-party libraries and containers. We monitor dependencies and promptly apply security patches.


Thank you for helping us make HAMi more secure! 🔒