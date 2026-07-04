# Go-only migration

## Conclusion

XRouter is now maintained as a single Go implementation. The previous dual-language layout has been removed to reduce duplicated protocol work, duplicated strategy logic, and CI variance.

## Removed

```text
xrouter-rust/
Cargo.toml
Rust Dockerfile
Rust Makefile targets
Rust GitHub Actions job
Rust build artifacts
```

## Current deployable unit

```text
Go module: github.com/BlueSkyXN/XRouter
Binary:    xrouter
Config:    config.example.json / config.local.json
CI:        .github/workflows/ci.yml
Release:   .github/workflows/release.yml
```

## Why single Go

XRouter is routing infrastructure. Most of its complexity is not raw CPU work; it is policy correctness, provider compatibility, timeout handling, orchestration, observability, and configuration stability. A single implementation avoids the main failure mode of dual stacks: one version silently lagging the other.

## Migration mapping

| Previous path | New path |
|---|---|
| `xrouter-go/*.go` | `*.go` at repository root |
| `xrouter-go/config.example.json` | `config.example.json` |
| `xrouter-go/Dockerfile` | `Dockerfile` |
| `xrouter-go/Makefile` | root `Makefile` |
| `xrouter-rust/*` | removed |
| Rust CI job | removed |
| Go CI job with `working-directory: xrouter-go` | root Go CI job |

## CI contract

The repository must pass:

```bash
gofmt -l *.go
go vet ./...
go test ./...
make build
./dist/xrouter -version
```

## Strategy coverage retained

The Go implementation keeps the v5 strategy set:

- exact and prefix model ID dispatch
- direct aliases
- smart router scoring
- prefix-cache bookkeeping
- judge-router signal
- request-level overrides
- dry-run route decisions
- MoA / MoV flows
- degradation guard / race flows
- Responses API native passthrough and chat shim

## Operational note

A single implementation does not mean a single provider or single strategy. XRouter still supports OpenAI, OpenRouter, and generic OpenAI-compatible providers through configuration.
