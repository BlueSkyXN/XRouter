# XRouter

XRouter is a self-hosted, OpenAI-compatible LLM routing control layer implemented as a single Go gateway.

The `model` field is treated as a strategy entrypoint, not only an upstream model name. See `docs/BACKGROUND.md` for the product background. This repository is the Go-only migration of the earlier dual-language package: the deployable unit is one Go module, one Docker image, and one `xrouter` binary.

## What it does

XRouter lets clients keep using OpenAI-style APIs while routing requests through configurable strategies:

- `direct_alias`: fixed public model ID to upstream target mapping, with no prompt rewrite.
- `smart_router`: single-target auto routing using hard filters, weighted scoring, prefix-cache bookkeeping, sticky sessions, keyword rules, and optional judge-router signal.
- `moa` / `mov`: multi-model orchestration flows such as parallel synthesize, judge select, verify-then-escalate, cascade budget, shadow evaluation, and race strategies.
- `race`: degradation-guard flows for selecting among multiple attempts, including boundary-aware reasoning-token detection and output-volume racing.
- `passthrough`: optional forwarding for unknown model IDs through an explicit provider policy.

## API surface

```text
POST /v1/chat/completions
POST /v1/responses
GET  /v1/models
GET  /healthz
GET  /metrics
GET  /debug/prefix-cache
```

The northbound interface is OpenAI-compatible for the common chat and responses paths. Provider-specific behavior is isolated behind configured targets.

When `XROUTER_API_KEYS` or inline API keys are configured, all API/debug/metrics endpoints except `/healthz` require `Authorization: Bearer ...`.

## Repository layout

```text
.
├── *.go                         # Go-only implementation
├── config.example.json           # Runnable strategy/provider config
├── Dockerfile
├── Makefile
├── .github/workflows/ci.yml      # Go lint/test/build workflow
├── .github/workflows/release.yml # Tag-driven packaging and release workflow
├── docs/
│   ├── BACKGROUND.md
│   ├── ARCHITECTURE.md
│   ├── CONFIGURATION.md
│   ├── RELEASING.md
│   ├── STRATEGIES.md
│   ├── DEGRADATION_GUARD_AND_RACE.md
│   ├── GO_ONLY_MIGRATION.md
│   └── TESTING_AND_CI.md
└── examples/
    ├── chat.direct-alias.json
    ├── chat.smart-router-dry-run.json
    ├── chat.race.boundary-guard.json
    └── ...
```

## Run locally

```bash
cp config.example.json config.local.json

export OPENAI_API_KEY=sk-...
export OPENROUTER_API_KEY=sk-or-...

make run CONFIG=config.local.json
```

Equivalent direct command:

```bash
go run . -config config.local.json
```

## Test and build

```bash
make fmt
make vet
make test
make build
./dist/xrouter -version
```

The built binary is written to:

```text
dist/xrouter
```

## Docker

```bash
docker build -t xrouter:go .
docker run --rm -p 8080:8080 \
  -e OPENAI_API_KEY=$OPENAI_API_KEY \
  -e OPENROUTER_API_KEY=$OPENROUTER_API_KEY \
  xrouter:go
```

## Production safety

`config.example.json` is runnable for local development. For shared or public deployments, set `XROUTER_API_KEYS` and keep provider credentials in environment variables instead of inline config.

```bash
export XROUTER_API_KEYS=team-key-1,team-key-2
export OPENAI_API_KEY=sk-...
export OPENROUTER_API_KEY=sk-or-...
```

Requests then need:

```text
Authorization: Bearer team-key-1
```

## GitHub Actions

The workflow in `.github/workflows/ci.yml` runs:

```text
gofmt check
go vet ./...
go test ./...
make build
./dist/xrouter -version
upload dist/xrouter as xrouter-linux-amd64
```

The workflow in `.github/workflows/release.yml` runs on `v*` tags and manual dispatch. It publishes:

```text
linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64 archives
SHA256SUMS
GitHub Release assets
ghcr.io/blueskyxn/xrouter:<tag>
```

There is no Rust job, no Cargo dependency, and no second implementation to keep in sync.

## Basic examples

Direct alias:

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'content-type: application/json' \
  -d @examples/chat.direct-alias.json
```

Smart router dry-run:

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'content-type: application/json' \
  -d @examples/chat.smart-router-dry-run.json
```

Boundary-aware race:

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'content-type: application/json' \
  -d @examples/chat.race.boundary-guard.json
```

Responses shim / passthrough:

```bash
curl http://127.0.0.1:8080/v1/responses \
  -H 'content-type: application/json' \
  -d @examples/responses.smart-router.json
```

## Configuration model

The request `model` field is treated as a strategy key:

```text
request.model
  -> exact route match
  -> prefix route match
  -> default route
  -> configured target ID
  -> unknown model policy
  -> direct / smart_router / mov / moa / passthrough executor
```

Unknown model passthrough is explicit: `reject` rejects unknown IDs, `passthrough_openai` sends them to the configured `openai` provider, and `passthrough_openrouter` sends them to the configured `openrouter` provider. Model-name shape does not override that policy.

The canonical runnable config is `config.example.json`. Request-level overrides are accepted under `xrouter` when enabled:

```json
{
  "model": "xrouter/auto",
  "messages": [{"role": "user", "content": "Debug this Go concurrency issue."}],
  "xrouter": {
    "objective": "quality",
    "candidates": ["openai-smart", "or-sonnet"],
    "dry_run": true,
    "explain": true,
    "cache_prefix_hint": "repo:acme/backend:main",
    "judge_enabled": true
  }
}
```

## Validation status of this package

The Go-only package has been generated from the previous Go implementation, then migrated to root-module form. Local validation performed during packaging:

```text
go test ./...
make build
./dist/xrouter -version
```

See `docs/GO_ONLY_MIGRATION.md` for the migration notes.
