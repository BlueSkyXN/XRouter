# XRouter Documentation

This directory is the maintained documentation surface for XRouter. It should describe the checked-in Go gateway, config files, workflows, and release behavior as they exist in this repository.

## First Reads

Read these files in order when onboarding or reviewing a change:

| File | Use it for |
|---|---|
| [../README.md](../README.md) | Product summary, quick start, API surface, repository layout, and examples. |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Runtime architecture, request lifecycle, route dispatch order, and invariants. |
| [CONFIGURATION.md](CONFIGURATION.md) | Canonical config schema, route kinds, provider targets, request overrides, and security-sensitive fields. |
| [OPERATIONS.md](OPERATIONS.md) | Local runbook, production deployment baseline, smoke checks, and troubleshooting. |
| [OPENAI_COMPATIBILITY.md](OPENAI_COMPATIBILITY.md) | OpenAI-compatible endpoint support and explicit compatibility limits. |
| [TESTING_AND_CI.md](TESTING_AND_CI.md) | Local validation commands and GitHub Actions coverage. |
| [RELEASING.md](RELEASING.md) | Tag-driven release packaging and GHCR publication behavior. |

## Strategy And Routing

| File | Use it for |
|---|---|
| [STRATEGIES.md](STRATEGIES.md) | Route types, smart routing, MoV/MoA, request overrides, and operational patterns. |
| [DEGRADATION_GUARD_AND_RACE.md](DEGRADATION_GUARD_AND_RACE.md) | Race flows, degradation guard semantics, boundary checks, and cascade budget behavior. |
| [BACKGROUND.md](BACKGROUND.md) | Product background and why XRouter treats `model` as a strategy entrypoint. |
| [PRODUCT_DESIGN.md](PRODUCT_DESIGN.md) | Product positioning and user-facing design principles. |
| [strategy/XROUTER_STRATEGY_DESIGN_FULL.md](strategy/XROUTER_STRATEGY_DESIGN_FULL.md) | Long-form source material. Do not copy this wholesale into public entry docs. |

## Migration And History

| File | Use it for |
|---|---|
| [GO_ONLY_MIGRATION.md](GO_ONLY_MIGRATION.md) | What changed when the project was formalized as a Go-only root module. |

Local or generated history belongs in ignored `local/` material, not in public docs, unless it has been summarized into stable product or operations guidance.

## Documentation Rules

- Keep `config.example.json` as the canonical runnable config. Documentation examples must match it or explain why they intentionally differ.
- Keep route IDs, target IDs, config keys, HTTP headers, environment variables, and CLI commands in English.
- Do not describe XRouter as an OpenRouter clone or full OpenAI API clone.
- Do not claim live provider validation unless a real upstream call was run and the command is recorded.
- When behavior changes, update the closest user-facing doc and, when relevant, `OPENAI_COMPATIBILITY.md`.
- When config schema changes, update `CONFIGURATION.md`, `config.example.json`, defaults, validation, and focused tests together.
- When release packaging changes, update `RELEASING.md` and read `.github/AGENTS.md` before editing workflows.

## Validation For Docs PRs

For docs-only PRs, run at least:

```bash
scripts/check-docs.sh
git diff --check
```

If examples, config snippets, Docker behavior, or release commands changed, also run the relevant repository checks from [TESTING_AND_CI.md](TESTING_AND_CI.md).
