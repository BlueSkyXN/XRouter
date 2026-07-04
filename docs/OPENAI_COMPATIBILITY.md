# OpenAI Compatibility

XRouter is not a full OpenAI API clone. It implements the compatibility surface needed for routing OpenAI-style model calls.

## Supported endpoints

```text
POST /v1/chat/completions
POST /v1/responses
GET  /v1/models
```

## Chat Completions

XRouter accepts standard chat completion fields and removes the private `xrouter` field before forwarding upstream.

Commonly preserved fields include:

- `messages`
- `temperature`
- `top_p`
- `max_tokens`
- `stream`
- `tools`
- `tool_choice`
- `response_format`
- provider-specific fields used by OpenRouter, where applicable

The selected target controls the final upstream model ID.

## Streaming

Direct and smart-routed streaming use upstream passthrough.

Retryable upstream failures can fall back before any response is written. Once an upstream streaming response is written to the client, XRouter returns that response as-is and does not append a second XRouter error object.

MoV/MoA flows generally require collecting intermediate outputs before finalization. XRouter therefore rejects or downgrades streaming for multi-model flows rather than returning a misleading partial stream.

## Responses

`/v1/responses` has two paths:

1. Native passthrough when the selected provider supports `/responses`.
2. Chat shim when the selected provider only supports `/chat/completions`.

The shim maps:

| Responses field | Chat field |
|---|---|
| `instructions` | `system` message |
| `input` string | `user` message |
| `input` message array | `messages` |
| `max_output_tokens` | `max_tokens` |
| `temperature`, `top_p`, `tools`, `tool_choice` | forwarded when representable |

The response is wrapped into a Responses-shaped object with `output_text` and `output` items. Text answers become an output message. Chat Completions `tool_calls` are preserved as Responses-style `function_call` output items so tool-only assistant messages are not dropped.

Shim responses include `x-xrouter-responses-shim: true`.

Advanced hosted state semantics are not implemented by the shim. Requests containing `previous_response_id` or `conversation` require a native Responses target. If native Responses targets return retryable upstream failures for those stateful requests, XRouter preserves the native failure status/body instead of rewriting the request as a shim `unsupported_state` client error.

## Unknown model policy

When `unknown_model_policy` is `reject`, XRouter returns an error for unknown models. This is the default and it does not dynamically forward slash-style provider slugs.

When set to `passthrough_openai`, unknown `model` IDs are sent unchanged to the configured `openai` provider. When set to `passthrough_openrouter`, they are sent unchanged to the configured `openrouter` provider. The policy selects the provider explicitly; model-name shape does not override it.
