# XRouter 背景

XRouter 的背景不是做一个 OpenRouter 替代品，而是做一个自托管的 LLM 路由控制层。它的核心前提是：`model` 字段不再只是模型名，也可以是策略入口。

传统链路是：

```text
client -> model ID -> provider -> answer
```

XRouter 面向的生产链路是：

```text
client
  -> OpenAI-compatible gateway
  -> model ID strategy resolver
  -> direct / smart router / prefix-cache / judge / MoV / race
  -> upstream provider
  -> response
```

## 定位

XRouter 是 Go 实现的 OpenAI-compatible 模型路由网关。它把模型选择、provider 选择、缓存亲和、多模型编排、降质防护放在一个可配置、可部署、可测试的自托管控制层里。

对外优先兼容常见 OpenAI-style API：

```text
POST /v1/chat/completions
POST /v1/responses
GET  /v1/models
```

这样 OpenAI SDK、Coding Agent、IDE 插件和内部服务可以通过替换 `baseURL` 接入，而不需要为每个上游 provider 写一套客户端适配。

## Model ID 是策略入口

请求体里的 `model` 先进入路由解析：

```text
gpt-4o-mini                 -> direct_alias
xrouter/auto                -> smart_router
xrouter/code/*              -> code policy smart_router
xrouter/mov/parallel-synth  -> multi-model orchestration
xrouter/race/boundary-guard -> degradation guard race
```

配置文件承担控制面职责：

```json
{
  "routes": {
    "gpt-4o-mini": {
      "kind": "direct_alias",
      "target": "openai-fast"
    },
    "xrouter/auto": {
      "kind": "smart_router",
      "objective": "balanced",
      "candidates": ["openai-fast", "or-auto", "or-sonnet"]
    },
    "xrouter/race/boundary-guard": {
      "kind": "mov",
      "flow": "boundary_guard_race_v1"
    }
  }
}
```

请求里的 `xrouter` 字段只作为本次 request override；它不替代配置文件这个稳定控制面。

## Direct Alias 是信任基线

`direct_alias` 是公开 model ID 到固定 upstream target 的映射：

```text
不改 prompt
不加隐藏 system
不调 judge model
不做 MoA / MoV / race
只做固定映射 + fallback
```

没有这个模式，XRouter 会变成不可预测的黑盒。企业或开发者应该能先验证“传入某个 model ID，就稳定转发到配置里的目标模型”，再逐步启用 auto、MoV 或 race。

## Smart Router 默认仍是单模型

`smart_router` 的重点不是多模型同时回答，而是根据任务、成本、延迟、能力、缓存、历史状态选择一个最终 target。

基础评分维度：

```text
score(target) =
    w_quality     * quality
  + w_cost        * inverse_cost
  + w_latency     * latency
  + w_reliability * reliability
  + w_cache       * prefix_cache_affinity
  + w_capability  * capability_fit
  + w_sticky      * session_stickiness
  + w_judge       * judge_score
  - penalties
```

这让 XRouter 和简单 proxy 区分开：它不是只转发，而是把“模型选择”变成可配置算法。

## Prefix Cache 是 Bookkeeping

XRouter 不假装自己能控制上游 provider 的 prompt cache。它维护的是 prefix-cache bookkeeping：

```text
prefix hash
session ID
target
last seen
hit count
reported cached tokens
provider health
```

这些信号用于 smart router 评分，让后续请求更倾向已有缓存亲和证据的 target。

## Judge Router 只做路由

Judge model 的职责必须很窄：只评估候选 target，不回答最终问题。

```text
request -> candidate target list -> judge model -> route score -> final target answers
```

这解决规则路由不知道“任务是简单补全还是复杂架构推理”的问题，同时避免把 judge router 混成 MoA。

## MoV / MoA 是显式升级路径

MoV 是 multi-model orchestration variants，MoA 是其中一种典型形式。XRouter 可以支持：

```text
parallel_synthesize_v1
parallel_judge_select_v1
best_of_n_self_consistency_v1
propose_critique_revise_v1
serial_chain_relay_v1
map_reduce_specialists_v1
verify_then_escalate_v1
cascade_budget_v1
dual_path_tool_acting_v1
shadow_evaluation_v1
```

这些路径成本更高、延迟更高、错误传播路径更多，所以不应默认开启。它们应由明确 model ID、smart router 的复杂度判断或 request override 触发。

## Race / Degradation Guard 只依赖可观察信号

XRouter 的 race 策略不依赖隐藏链路，也不试图获取 provider 内部推理。它只基于外部可观察结果：

```text
visible output length
finish_reason
usage.output_tokens
usage.reasoning_tokens
latency
boundary hit
incomplete
JSON validity
tool-call validity
```

当可观察信号显示某次输出疑似落入低预算边界、过短、incomplete 或质量异常时，XRouter 可以通过赛马、重试、升级、裁判选择等方式降低单次异常对用户的影响。

## 为什么收敛到单 Go 实现

XRouter 的复杂度主要在策略层、provider 适配、prefix-cache bookkeeping、MoV flow、race scoring、配置契约和观测数据，而不是语言本身。

单 Go 实现带来的工程收益：

```text
一个 binary
一个配置格式
一套测试
一条 GitHub Actions 发布链路
一个 Dockerfile
更少跨语言偏移
```

所以当前正式仓库采用 Go-only 结构，把复杂度集中到路由策略和发布质量上。
