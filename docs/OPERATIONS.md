# Operating XRouter

This runbook covers the local and production operating path for the checked-in Go gateway.

## Runtime Shape

XRouter runs as a single Go HTTP service. It exposes:

```text
POST /v1/chat/completions
POST /v1/responses
GET  /v1/models
GET  /healthz
GET  /metrics
GET  /debug/prefix-cache   # only when server.debug=true
```

The gateway is OpenAI-compatible on the northbound side and provider-configured on the southbound side. It does not persist request state. In-memory stores include metrics, sticky sessions, and prefix-cache bookkeeping.

## Local Development

Create an ignored local config:

```bash
cp config.example.json config.local.json
```

Set provider credentials only in the environment:

```bash
export OPENAI_API_KEY=sk-...
export OPENROUTER_API_KEY=sk-or-...
```

Run the service:

```bash
make run CONFIG=config.local.json
```

Equivalent direct command:

```bash
go run . -config config.local.json
```

## Production Baseline

For shared or public deployments, set an XRouter entry API key allowlist and provider credentials:

```bash
export XROUTER_API_KEYS=team-key-1,team-key-2
export OPENAI_API_KEY=sk-...
export OPENROUTER_API_KEY=sk-or-...
```

Client requests then use:

```text
Authorization: Bearer team-key-1
```

If provider credentials are loaded but no XRouter API key is configured, startup logs a warning. That warning is not a hard startup failure because anonymous local development remains supported.

Recommended production config posture:

| Area | Recommendation |
|---|---|
| `auth.api_keys_env` | Keep `XROUTER_API_KEYS` or another secret-backed environment variable. |
| Provider keys | Prefer `api_key_env`; avoid inline `api_key` outside controlled local demos. |
| `request_overrides.allow_provider_key_override` | Keep `false` for shared deployments. |
| `server.debug` | Keep `false` unless the debug endpoint is intentionally protected and needed. |
| `routing.unknown_model_policy` | Keep `reject` unless passthrough is an explicit product decision. |
| `prefix_cache.hash_salt` | Use a high-entropy deployment-specific value when stable prefix hash keys are required. |

## Docker

Build locally:

```bash
docker build -t xrouter:go .
```

Run with the example config baked into the image:

```bash
docker run --rm -p 8080:8080 \
  -e XROUTER_API_KEYS=team-key-1 \
  -e OPENAI_API_KEY=$OPENAI_API_KEY \
  -e OPENROUTER_API_KEY=$OPENROUTER_API_KEY \
  xrouter:go
```

The release workflow publishes `ghcr.io/blueskyxn/xrouter:<tag>` only for tag-driven releases. PR and branch CI build Docker images for validation but do not publish them.

## Smoke Checks

Health check:

```bash
curl -i http://127.0.0.1:8080/healthz
```

List configured public route and target IDs:

```bash
curl -i http://127.0.0.1:8080/v1/models \
  -H 'Authorization: Bearer team-key-1'
```

Dry-run smart routing without provider calls:

```bash
curl -i http://127.0.0.1:8080/v1/chat/completions \
  -H 'content-type: application/json' \
  -H 'Authorization: Bearer team-key-1' \
  -d @examples/chat.smart-router-dry-run.json
```

For real upstream calls, use non-dry-run examples only after provider keys are present and the target provider is expected to be reachable.

## Logs And Debugging

Important startup signals:

| Log signal | Meaning |
|---|---|
| `xrouter listening on ...` | Service loaded config and started HTTP listener. |
| `WARNING: provider credentials are loaded...` | Provider keys are available but XRouter entry auth is not configured. Safe for local demos, unsafe for shared exposure. |

Optional debug endpoint:

```text
GET /debug/prefix-cache
```

It is only registered when `server.debug=true` and still requires XRouter API auth when API keys are configured.

## Troubleshooting

| Symptom | Check |
|---|---|
| `401 unauthorized` | Confirm `Authorization: Bearer <key>` or `x-api-key` matches `XROUTER_API_KEYS`. |
| `missing_model` | Request JSON must contain a non-empty `model`. |
| `route_error` for unknown models | Check `routing.unknown_model_policy`; `reject` is the default. |
| Provider 401/403 | Confirm provider `api_key_env` is set in the service environment, not just in the client shell. |
| Responses request returns shim marker | `x-xrouter-responses-shim: true` means the selected target used Chat Completions shim rather than native `/responses`. |
| Stateful Responses request fails | `previous_response_id` and `conversation` require a native Responses target; the chat shim does not implement hosted state. |
| Long streaming response stops early | Verify upstream or proxy timeouts. XRouter uses a streaming client without a global response-body timeout. |

## Pre-Deploy Validation

Run the normal local gate before publishing a deployment artifact:

```bash
gofmt -l ./*.go
go vet ./...
go test ./...
make build
./dist/xrouter -version
```

For release packaging changes, also run:

```bash
make release-snapshot VERSION=v0.0.0-local
```

Do not create tags, GitHub Releases, GHCR pushes, or production deployments from a docs or config cleanup PR unless that release action is explicitly requested.
