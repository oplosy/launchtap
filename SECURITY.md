# Security Policy

This repository's security review target is immutable commit `6184bc5febd43de99ab6cc2b6f71f7f90c878bb6`
unless a later audit explicitly names another revision. Plan 5 Tasks 1–7 are read-only audits;
Task 8 is the only remediation task and may change source only for an independently validated
finding or measured quality/performance case.

## Audit and reportability policy

An alert, advisory, hypothesis, test failure, complexity metric, or timing result is not a
finding by itself. A reportable item needs a reachable source-to-sink or failure path, exact
`path:line` evidence, affected asset, attacker or failure prerequisites, impact, confidence,
counterevidence, and a reproducible proof or reproduction method. Quality findings use
`IMPORTANT` or `MINOR` only when they show a concrete defect, maintenance cost, drift risk, or
measured resource waste; style-only preferences are excluded.

The canonical schema, severity calibration, state transitions, and regression-test requirements
are kept with the maintainer's internal audit records; the engine 2 scope for external review is
the [external audit package](docs/audits/external-audit-package.md).

## Handling sensitive information

Do not commit or paste real private keys, seed phrases, Privy verification keys, database
credentials, private RPC URLs, production tokens, or other secrets into source, reports, tests,
logs, prompts, or issues. Use synthetic canaries for leak-path testing and redact command output.
Production deployment, governance, hosting, monitoring, and external-audit evidence remains an
external prerequisite tracked by the maintainer.

## Scope boundary

Vendored dependencies, generated artifacts, test fixtures, local-development controls, and
production-conditional configuration receive the explicit provenance, reachability, or
conditional disposition defined in the Task 1 ledger. They are never silently treated as clean.
A passing local or CI gate does not establish production readiness without the external controls
listed in the audit pack.
