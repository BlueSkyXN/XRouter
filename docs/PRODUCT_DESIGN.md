# XRouter Product Design

## Positioning

XRouter is a model-infrastructure router, not an application chatbot. Its northbound contract is OpenAI-compatible; its southbound contract is provider-adapter based.

The design constraint is that the caller should be able to switch from:

```text
https://api.openai.com/v1
```

to:

```text
http://xrouter.example/v1
```

and use `model` as either a real model ID or a strategy model ID.

## Product primitives

| Primitive | Meaning |
|---|---|
| Provider | Upstream API base URL and credentials. |
| Target | Internal target ID mapped to provider + upstream model. |
| Route | Public model ID strategy. |
| Direct alias | Fixed public model ID to target mapping. |
| Smart router | Single-target router with scoring. |
| Prefix-cache BK | Local evidence for prefix-to-target affinity. |
| Judge router | Optional route selector model. |
| MoV | Multi-model orchestration variant. |
| MoA | Reference models plus aggregator; one MoV subtype. |

## Why model ID driven strategy

Clients already send `model`. XRouter uses that as the main dispatch key:

```text
model = gpt-4o-mini                  -> direct_alias
model = xrouter/auto                 -> smart_router
model = xrouter/code/go            -> prefix smart_router
model = xrouter/mov/parallel-synth   -> MoV flow
```

This makes strategy selection declarative and SDK-compatible.

## Non-goals

- Not a full OpenAI API clone.
- Not a provider billing reconciler.
- Not a persistent cache of raw prompts.
- Not a replacement for upstream model safety systems.
- Not a guarantee that MoV always improves answers; MoV is a configurable expensive path.
