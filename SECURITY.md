# Security Policy

## Supported Versions

Aegis Gateway is pre-1.0 software. Security fixes target the latest released version and the `main` branch.

## Reporting a Vulnerability

Please report suspected vulnerabilities privately by opening a GitHub security advisory for this repository. Do not include API keys, prompt content, request bodies, database files, or other sensitive local data in public issues.

Useful reports include:

- Affected version or commit
- Operating system and GPU environment
- Minimal reproduction steps
- Expected and observed behavior
- Whether the issue exposes local-only data, API keys, prompt content, or request metadata

## Security Principles

Aegis Gateway is designed to run locally with no cloud dependency, no telemetry, and no prompt or response persistence. Request logs must remain metadata-only. API keys must never be stored or printed in raw form after creation.
