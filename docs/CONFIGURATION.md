# Configuration

The canonical runnable configuration is:

```text
config.example.json
```

Use this file as the deployment baseline, then create an environment-specific copy such as `config.local.json`.

## Top-level shape

```json
{
  "server": {},
  "auth": {},
  "request_overrides": {},
  "routing": {},
  "prefix_cache": {},
  "providers": {},
  "targets": {},
  "routes": {}
}
```

## Server

```json
"server": {
  "listen": ":8080",
  "request_timeout_ms": 120000,
  "read_header_timeout_ms": 10000,
  "max_request_body_bytes": 33554432,
  "max_upstream_body_bytes": 67108864,
  "debug": false
}
```

| Field | Meaning |
|---|---|
| `listen` | Go `net/http` listen address. |
| `request_timeout_ms` | Non-streaming upstream client timeout and streaming response-header timeout. Streaming response bodies are not cut off by this global timeout. |
| `read_header_timeout_ms` | HTTP server read-header timeout. |
| `max_request_body_bytes` | Maximum JSON request body size for API handlers. |
| `max_upstream_body_bytes` | Maximum non-streaming upstream response body size. Exceeding this fails the attempt instead of buffering unbounded data. |
| `debug` | Enables debug endpoints such as `/debug/prefix-cache`. Keep this `false` for shared deployments unless the endpoint is protected by API keys and intentionally exposed. |

## Auth

```json
"auth": {
  "api_keys_env": "XROUTER_API_KEYS",
  "api_keys": []
}
```

If no API keys are configured, XRouter accepts unauthenticated requests. This is convenient for local development but unsafe for shared or public deployments because upstream provider credentials may be consumed by anyone who can reach the gateway. When provider credentials are loaded but no XRouter API keys are configured, startup logs a warning. For shared environments, set `XROUTER_API_KEYS` to a comma-separated allowlist.

When API keys are configured, `/v1/chat/completions`, `/v1/responses`, `/v1/models`, enabled debug endpoints such as `/debug/prefix-cache`, and `/metrics` require the same bearer token. `/healthz` remains unauthenticated for liveness checks.

```bash
export XROUTER_API_KEYS=dev-key-1,dev-key-2
```

Requests then use:

```text
Authorization: Bearer dev-key-1
```

## Providers

A provider is an OpenAI-compatible upstream API base.

```json
"providers": {
  "openai": {
    "base_url": "https://api.openai.com/v1",
    "api_key_env": "OPENAI_API_KEY",
    "headers": {},
    "supports": ["chat", "responses"]
  },
  "openrouter": {
    "base_url": "https://openrouter.ai/api/v1",
    "api_key_env": "OPENROUTER_API_KEY",
    "headers": {
      "HTTP-Referer": "https://xrouter.local",
      "X-OpenRouter-Title": "XRouter"
    },
    "supports": ["chat"]
  }
}
```

| Field | Meaning |
|---|---|
| `base_url` | Provider base URL without trailing endpoint path assumptions. |
| `api_key_env` | Environment variable used for provider API key. |
| `api_key` | Inline key, normally avoided outside local demos. |
| `headers` | Static headers forwarded to the provider. |
| `supports` | Capability list: `chat`, `responses`. |

## Targets

A target binds an internal target ID to a provider and upstream model.

```json
"targets": {
  "openai-smart": {
    "provider": "openai",
    "model": "gpt-5.5",
    "quality": 0.96,
    "cost_in": 5.0,
    "cost_out": 15.0,
    "latency_ms": 1800,
    "reliability": 0.97,
    "cache_support_score": 0.8,
    "capabilities": {
      "tools": true,
      "vision": true,
      "json": true,
      "responses": true
    },
    "tags": ["smart", "reasoning", "code"],
    "extra_body": {
      "provider": {
        "order": ["openai"]
      }
    }
  }
}
```

Target scores are local policy hints, not provider claims. The smart router uses them as weighted inputs.

`extra_body` is for provider-specific request extensions. It can add fields that are absent from the client request, but it does not override client-provided prompt, tool, response format, or generation fields. XRouter still removes provider-specific OpenRouter fields before forwarding to non-OpenRouter providers.

## Routing defaults

```json
"routing": {
  "unknown_model_policy": "reject",
  "default_route": ""
}
```

Supported unknown-model policies:

| Policy | Behavior |
|---|---|
| `reject` | Return an error for unknown model IDs. |
| `passthrough_openai` | Forward unknown model IDs unchanged to the configured `openai` provider. |
| `passthrough_openrouter` | Forward unknown model IDs unchanged to the configured `openrouter` provider. |

Passthrough is explicit. With `reject`, slash-style model IDs such as `anthropic/claude-...` are not auto-forwarded. With `passthrough_openai`, even slash-style IDs use the `openai` provider; with `passthrough_openrouter`, plain IDs also use the `openrouter` provider.

Route target references still validate configured target names. Under a passthrough policy, only explicit provider/model-style references such as `anthropic/claude-...` are treated as passthrough; plain typos like `opnai-smart` fail startup validation.

## Prefix cache bookkeeping

```json
"prefix_cache": {
  "enabled": true,
  "max_entries": 4096,
  "ttl_seconds": 86400,
  "prefix_chars": 4096,
  "min_prefix_chars": 128,
  "hash_salt": "replace-me-per-deployment",
  "recency_half_life_seconds": 1800,
  "update_from_usage": true
}
```

Prefix cache is routing bookkeeping, not response caching. XRouter hashes the configured prompt prefix window and stores target/provider metadata plus cached-token evidence; it does not store raw prompt prefixes. If `hash_salt` is empty or left as `replace-me-per-deployment`, XRouter replaces it with a process-local random salt at startup; set an explicit high-entropy deployment salt only when stable hash keys are intentionally required. `update_from_usage` defaults to `true`; set it to `false` to keep prefix-cache scoring enabled for existing in-memory entries while preventing new updates from upstream usage telemetry.

## Route kinds

### `direct_alias`

Fixed public model ID to target mapping. No prompt rewrite.

```json
"gpt-4o-mini": {
  "kind": "direct_alias",
  "target": "openai-fast",
  "fallbacks": ["or-auto"]
}
```

### `smart_router`

Selects exactly one target using filters and scoring.

```json
"xrouter/auto": {
  "kind": "smart_router",
  "objective": "balanced",
  "candidates": ["openai-fast", "openai-smart", "or-auto", "or-sonnet"],
  "weights": {
    "quality": 0.35,
    "cost": 0.15,
    "latency": 0.15,
    "reliability": 0.15,
    "capability": 0.1,
    "cache": 0.1,
    "sticky": 0.05,
    "judge": 0.1
  },
  "prefix_cache": {
    "enabled": true,
    "weight": 1.5
  },
  "keyword_rules": [
    {
      "name": "code-required",
      "any": ["debug", "golang", "typescript"],
      "tags": ["code"],
      "require": true,
      "boost": 0.12
    }
  ],
  "judge": {
    "enabled": false,
    "target": "judge-small",
    "candidates": ["openai-smart", "or-sonnet"],
    "timeout_ms": 1200,
    "weight": 0.25
  }
}
```

Capability filters for tools, JSON output, and vision are hard filters. If every configured candidate is incompatible with the request, XRouter returns a route error instead of falling back to an incompatible target.

Keyword rules are score adjustments by default. When a matching rule sets `require: true`, it becomes a hard target/tag gate: only candidates listed in `targets` or carrying one of the rule `tags` remain eligible. Startup validation rejects `require: true` rules that do not declare `targets` or `tags`.

The smart router also folds recent in-memory target health into scoring. Repeated retryable failures temporarily demote a target behind other compatible candidates, while keeping it available as a later fallback.

When `judge.candidates` is non-empty, the judge-router prompt and score map are limited to the intersection of the route candidates and that list. Other candidates remain eligible through normal weighted scoring, but they do not receive judge-score boosts.

### Prefix route

Prefix model IDs are matched after exact route IDs.

```json
"xrouter/code/*": {
  "kind": "smart_router",
  "match_prefixes": ["xrouter/code/"],
  "objective": "quality",
  "candidates": ["openai-smart", "or-sonnet"]
}
```

### `mov` / `moa`

Multi-model orchestration route.

```json
"xrouter/mov/parallel-synth": {
  "kind": "mov",
  "flow": "parallel_synthesize_v1",
  "references": ["openai-fast", "or-sonnet"],
  "aggregator": "openai-smart",
  "allow_partial": true
}
```

Use the named `flow` implementations documented in `docs/STRATEGIES.md`. The generic `stages` array is reserved for a future stage DSL and is rejected by config validation in this release rather than being silently ignored.

## Race / degradation guard

Race strategies are configured as MoV routes with a `race` block.

```json
"xrouter/race/boundary-guard": {
  "kind": "mov",
  "flow": "boundary_guard_race_v1",
  "candidates": ["openai-smart", "or-sonnet"],
  "parallelism": 2,
  "race": {
    "selection": "boundary_aware",
    "boundary_start": 516,
    "boundary_step": 518,
    "boundary_tolerance": 0,
    "boundary_penalty": 2500,
    "visible_weight": 1.0,
    "output_weight": 0.2,
    "reasoning_weight": 0.05,
    "include_debug": true
  }
}
```

When `parallelism` is omitted or set to `0`, XRouter runs all reference or race attempts for that route concurrently, up to the number of work items. Set `parallelism` explicitly to cap concurrency.

For `race.selection: "fastest_acceptable"`, the first successful, non-degraded attempt returns immediately and cancels remaining in-flight attempts. Boundary-aware and max-output selections still wait for all attempts because their scoring needs comparison data.

`cascade_budget_v1` evaluates each step with observable quality gates before escalating: successful HTTP status, non-incomplete finish reason, optional `race.min_visible_tokens`, and JSON parseability when the request asks for JSON output.

## Request-level overrides

When `request_overrides.enabled` is true, callers can pass XRouter-private controls in the JSON body:

```json
{
  "model": "xrouter/auto",
  "messages": [{"role": "user", "content": "Debug this Go concurrency issue."}],
  "xrouter": {
    "objective": "quality",
    "candidates": ["openai-smart", "or-sonnet"],
    "multi_model": "never",
    "dry_run": true,
    "explain": true,
    "cache_prefix_hint": "repo:acme/backend:main",
    "disable_prefix_cache": false,
    "judge_enabled": true,
    "session_id": "tenant-a/thread-123"
  }
}
```

Common headers are also supported:

```text
x-xrouter-route: xrouter/auto
x-xrouter-mode: direct | auto | moa | mov | bypass | passthrough
x-xrouter-target: openai-fast
x-xrouter-candidates: openai-fast,or-sonnet
x-xrouter-references: openai-fast,or-sonnet
x-xrouter-aggregator: openai-smart
x-xrouter-objective: balanced | quality | cost | latency
x-xrouter-multi-model: never | auto | always
x-xrouter-dry-run: true
x-xrouter-explain: true
x-xrouter-cache-prefix-hint: repo:acme/backend:main
x-xrouter-disable-prefix-cache: true
x-xrouter-judge-enabled: true
x-xrouter-session-id: tenant-a/thread-123
```

`request_overrides.max_routing_targets` bounds request-provided `targets`, `candidates`, and `references` lists before routing or multi-model orchestration starts. The default is `32`. Oversized lists are rejected instead of being truncated silently. `max_shadow_targets` and `max_listener_targets` similarly bound request-provided side-channel work.

Request-provided target references are resolved before dry-run output or upstream calls. Unknown or disallowed `target`, `targets`, `candidates`, `references`, `aggregator`, listener, judge, race, and fallback targets fail route resolution instead of producing a dry-run decision that would later fail at execution time. `mode: "passthrough"` is still governed by `routing.unknown_model_policy`; with the default `reject` policy it cannot forward an unknown model ID.

Provider API keys can be supplied per request only if explicitly allowed:

```json
"xrouter": {
  "provider_api_keys": {
    "openai": "sk-...",
    "openrouter": "sk-or-..."
  }
}
```

Equivalent headers:

```text
x-xrouter-provider-key-openai: sk-...
x-xrouter-provider-key-openrouter: sk-or-...
```

`config.example.json` keeps `request_overrides.allow_provider_key_override=false`. Only enable provider key override when the caller trust boundary is clear. In hosted shared deployments, prefer server-side provider credentials plus XRouter API keys.

## Dry-run

`dry_run` returns route decisions without calling an upstream model. Use this before changing production policies.

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'content-type: application/json' \
  -d @examples/chat.smart-router-dry-run.json
```

## Startup validation

`LoadConfig` validates configuration before the server starts. It checks provider URLs and declared endpoint support, target provider/model references, route target/fallback/listener/judge/race references, default route references, MoA requirements, supported MoV flows, and passthrough route provider policy.

This is intentionally stricter than request-time routing: common deployment mistakes should fail fast during startup rather than on the first live request.

`serial_listeners[].mode` currently supports only `serial`; unknown modes fail validation instead of being silently skipped.
