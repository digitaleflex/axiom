# Deployment Expert Specifications

> Issue #39. Reference specifications for the bounded experts invoked by the Deployment Engine. Generated from and consistent with `schemas/experts/*.yaml`; the YAML contract is normative when they differ.

| Expert | Purpose |
|---|---|
| [Build Expert](build-expert.md) | Build an application image from an exact commit in an isolated workspace. |
| [Deployment Planner](deployment-planner.md) | Produce a deterministic, reviewable Deployment Plan from profile, server and configuration. |
| [Infrastructure Expert](infrastructure-expert.md) | Assess server eligibility and configure routing, domains and TLS through the Runtime Agent. |
| [Repository Analyzer](repository-analyzer.md) | Inspect a read-only repository snapshot and record findings with evidence and confidence. |
| [Runtime Expert](runtime-expert.md) | Translate plan runtime steps into bounded Runtime Agent operations and verify health. |
| [Security Expert](security-expert.md) | Evaluate plans and operations against security policy and produce a security report. |
| [Stack Detector](stack-detector.md) | Turn analysis findings and manifest into a canonical Application Profile resolved to a V0.1 preset. |

Framework: [`../../deployment/README.md`](../../deployment/README.md).
