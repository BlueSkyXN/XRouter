# Manifest

```text
xrouter/
  README.md
  MANIFEST.md
  Makefile
  Dockerfile
  .dockerignore
  go.mod
  .gitignore
  .github/workflows/ci.yml
  .github/workflows/release.yml

  *.go
  *_test.go
  config.example.json

  docs/
    BACKGROUND.md
    ARCHITECTURE.md
    CONFIGURATION.md
    RELEASING.md
    STRATEGIES.md
    DEGRADATION_GUARD_AND_RACE.md
    GO_ONLY_MIGRATION.md
    OPENAI_COMPATIBILITY.md
    PRODUCT_DESIGN.md
    TESTING_AND_CI.md
    strategy/XROUTER_STRATEGY_DESIGN_FULL.md

  examples/
    chat.direct-alias.json
    chat.smart-router-dry-run.json
    chat.smart-router-prefix-cache.json
    chat.judge-router-dry-run.json
    chat.mov.parallel-synth.json
    chat.mov.verify-escalate.json
    chat.mov.cascade-budget.json
    chat.race.max-output.json
    chat.race.boundary-guard.json
    chat.race.effort-ladder.json
    chat.race.serial-escalate.json
    responses.smart-router.json
    responses.race.boundary-guard.json
    opencode.xrouter.json
    xrouter.strategy.example.yaml
```

## Removed from previous dual-language packages

```text
xrouter-go/ wrapper directory
xrouter-rust/
Cargo.toml
Rust Dockerfile
Rust Makefile
Rust GitHub Actions job
Rust build artifacts
```

## Local validation commands

```bash
go test ./...
make build
./dist/xrouter -version
make release-snapshot VERSION=v0.0.0-local
```
