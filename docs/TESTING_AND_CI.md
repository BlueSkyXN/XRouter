# Testing and CI

## Local checks

From the repository root:

```bash
make fmt
make vet
make test
make build
./dist/xrouter -version
```

Equivalent explicit commands:

```bash
gofmt -w *.go
go vet ./...
go test ./...
make build
./dist/xrouter -version
```

## Test coverage

The Go test suite covers:

- Responses API to Chat Completions shim conversion
- Chat Completions to Responses-shaped wrapping
- request controls and request-level overrides
- request override routing-target list bounds
- direct alias behavior with no prompt rewrite
- exact vs prefix model ID dispatch
- unknown model reject and explicit OpenAI/OpenRouter passthrough policy
- smart-router hard-filter failure when no candidate supports required capabilities
- streaming non-retryable upstream errors without appended XRouter error bodies
- request body size limit
- metrics endpoint authentication when API keys are configured
- startup config validation for target references and passthrough routes
- Responses shim preservation of Chat Completions tool calls
- prefix-cache bookkeeping influence on smart-router ordering
- `prefix_cache.update_from_usage=false` disabling telemetry-driven updates
- MoV route materialization
- auto route conditional MoA escalation
- request bypass target override
- race boundary formula
- boundary-aware race selection
- race plan expansion with reasoning effort ladders

## GitHub Actions

The workflow is:

```text
.github/workflows/ci.yml
```

It runs a Go job:

```text
checkout
setup-go from go.mod
gofmt check
go vet ./...
go test ./...
make build
./dist/xrouter -version
upload dist/xrouter as xrouter-linux-amd64
```

It also runs a Docker build job after the Go job:

```text
checkout
setup-qemu
setup-buildx
docker buildx build for linux/amd64 and linux/arm64 without pushing
```

There is no Rust job and no Cargo toolchain dependency.

## Release workflow

The release workflow is:

```text
.github/workflows/release.yml
```

It runs for `v*` tags and manual dispatch against an existing tag. It:

- verifies format, vet, and tests
- builds archives for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, and windows/amd64
- publishes SHA256SUMS
- creates or updates the GitHub Release
- publishes a multi-platform GHCR image tagged with the release tag

See `docs/RELEASING.md` for the operator flow.

## Recommended release checks

Before tagging a release:

```bash
make clean
make fmt
make vet
make test
make build
./dist/xrouter -version
make release-snapshot VERSION=v0.0.0-local
```

Then run a dry-run route check:

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'content-type: application/json' \
  -d @examples/chat.smart-router-dry-run.json
```

After a real request, inspect prefix-cache state only when `server.debug=true`:

```bash
curl http://127.0.0.1:8080/debug/prefix-cache
```

## Docker check

```bash
docker build -t xrouter:go .
docker run --rm -p 8080:8080 xrouter:go
```
