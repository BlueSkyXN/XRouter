# XRouter agent instructions

## Purpose

XRouter 是一个自托管的 OpenAI-compatible LLM 路由控制层。它不是 OpenRouter 替代品；核心职责是把 `model` / model ID 解析为可配置策略入口，并在 Go gateway 内执行 direct alias、smart router、prefix-cache bookkeeping、judge router、MoV / MoA、race / degradation guard 等路径。

## Codex startup behavior

- Codex 通常从仓库根目录 `/Users/sky/Github/XRouter` 启动；本文件是启动期主规则。
- 子目录 `AGENTS.md` 是按需导航卡片；修改带本地卡片的目录前，必须先读取该文件。
- 如果从子目录启动，Codex 可能自动加载路径链上的本地 `AGENTS.md`；仍以本文件的目录地图作为根启动 workflow 的 router。
- 本仓库当前只需要一个 `.github/AGENTS.md` 子卡片，用于 CI、release、GHCR 发布等高风险路径。

## Directory map

| Path | Responsibility | Local AGENTS.md | Read when |
|---|---|---:|---|
| `*.go` / `*_test.go` | Go-only gateway implementation and unit tests | No | 修改 routing、provider、auth、metrics、Responses shim、MoV、race、config schema 或测试时直接遵循根规则 |
| `go.mod` | Go module definition (`github.com/BlueSkyXN/XRouter`) | No | 修改 Go version 或新增长期依赖前；优先不用新依赖 |
| `config.example.json` | Canonical runnable routing/provider config | No | 修改 config schema、默认 route、provider target、request override、race/MoV 示例时 |
| `Makefile` | Local build, test, packaging, checksum commands | No | 修改构建、打包、版本注入或 dist 产物布局时 |
| `Dockerfile` / `.dockerignore` | Container build and runtime image | No | 修改 container entrypoint、base image、copied files、ports 或 build args 时 |
| `.github/` | CI, release, binary packaging, GitHub Release and GHCR publishing | Yes | 修改任何 workflow、release permission、artifact、tag/manual dispatch、GHCR 相关文件前 |
| `docs/` | Human-facing architecture, configuration, strategy, release, testing docs | No | 修改行为、配置、API、发布流程或背景定位后，同步相关文档 |
| `docs/strategy/` | Long-form Chinese strategy design material | No | 修改 strategy terminology 或 model-ID-as-strategy design 时，可参考但不要把长文复制进 README |
| `examples/` | Request and integration examples for chat, responses, OpenCode, strategy sketches | No | 修改 route IDs、config schema、request override 字段、Responses compatibility 时 |
| `local/` | Ignored source/archive material from earlier local/cloud packages | No | 默认不要读取或发布；只有用户明确要求追溯来源包时再进入 |
| `dist/` | Ignored build/package output | No | 不手工编辑；由 `make build`、`make package-current`、`make release-snapshot` 生成 |

## On-demand cat protocol

Before editing files under a directory that has a local `AGENTS.md`, read that file first using:

```bash
cat <path>/AGENTS.md
```

If multiple nested `AGENTS.md` files exist on the path to the target file, read them from shallow to deep before editing. Do not assume a subdirectory card was loaded just because the root card was loaded.

## Commands

Use commands that exist in this repository. Do not invent npm, pnpm, cargo, rust, python, or monorepo commands.

| Command | Purpose | Scope | Sandbox notes |
|---|---|---|---|
| `gofmt -l ./*.go` | Check root Go formatting without modifying files | repo root Go files | OK |
| `make fmt` | Format root Go files with `gofmt -w *.go` | repo root Go files | Modifies files |
| `go vet ./...` | Go vet checks | repo | OK |
| `make vet` | Makefile wrapper for `go vet ./...` | repo | OK |
| `go test ./...` | Unit tests | repo | OK; current tests do not require live providers |
| `make test` | Makefile wrapper for `go test ./...` | repo | OK |
| `make build` | Build `dist/xrouter` with version ldflags | repo | Writes `dist/` |
| `./dist/xrouter -version` | Smoke-check built binary version metadata | repo | Requires `make build` first |
| `go run . -config config.example.json` | Run local server from example config | repo | Starts a server; upstream calls need provider credentials/network |
| `make run CONFIG=config.local.json` | Run with local config | repo | Requires local ignored config and provider env vars for real upstream calls |
| `make release-snapshot VERSION=v0.0.0-local` | Build local cross-platform archives and checksums | repo | Writes `dist/`; no GitHub publication |
| `docker build -t xrouter:go .` | Build container image | repo | Requires Docker daemon; may need network for base image |

## Global rules

- 保持项目定位：XRouter 是自托管 LLM routing control layer，不是“OpenRouter clone / replacement”。
- `model` 字段是策略入口。实现或文档中要区分 public model ID / route ID、internal target ID、upstream provider model。
- `direct_alias` 是信任基线：只做固定 public model ID 到 target/provider/model 的映射，不改写 prompt、messages、tools、tool choice、response format 或 generation parameters。
- `smart_router` 默认是单模型选择器。除非 route config、complexity threshold 或 request override 明确允许，否则不要把 `smart_router` 改成默认多模型回答。
- MoV / MoA 是显式多模型编排路径。新增 flow 时要保证 `isSupportedMoVFlow`、config validation、route materialization、examples 和 docs 同步。
- Race / degradation guard 只能基于可观测信号做工程防护，例如 `finish_reason`、visible output length、usage token telemetry、latency、JSON/tool validity。不要在代码或文档里断言 provider 人为降质或 hidden chain-of-thought 被截断。
- Unknown model passthrough 必须由 `routing.unknown_model_policy` 显式控制。不要根据 model ID 是否包含 `/` 自动推断 OpenAI 或 OpenRouter provider。
- Prefix cache 是 bookkeeping，不是 response cache。只记录 hash、target、provider、last_seen、hit count、cached-token evidence 等；不要保存原始 prompt prefix 或用户内容。
- Request override 只能在 `request_overrides.enabled` 允许时生效；provider API key override 属于敏感路径，改动时必须覆盖 header 和 body 两种输入，避免越权扩散。
- API/debug/metrics auth 规则要保持清晰：配置 `XROUTER_API_KEYS` 或 inline API keys 后，除 `/healthz` 外的 API、debug、metrics endpoint 都要求 bearer token。
- OpenAI compatibility 是 northbound contract，但本仓库不是完整 OpenAI API clone。新增 endpoint 或字段时要在 `docs/OPENAI_COMPATIBILITY.md` 中说明支持边界。
- 新增或修改 config 字段时，同步 `types.go` defaults、`config.go` validation、`config.example.json`、相关 docs、至少一个 focused test 或 example。
- 新增 provider/target behavior 时，优先保持 provider-specific logic 在 `provider.go` 或 config 层，不要把 provider 特例散落到 routing algorithm。
- 新增 route/scoring behavior 时，优先加 focused unit tests；测试不应依赖真实 OpenAI、OpenRouter 或其他外部 provider。
- 面向用户的中文材料可以用中文说明产品定位、策略、背景和 tradeoff；代码标识、JSON key、route kind、flow name、header、env var、CLI command 保持 English。
- README 当前以 English 为主；公开入口文档若改成双语，要保持同一事实口径，避免中英文版本出现不同 config 或命令。

## Do not

- 不要提交 `.env*`、`config.local.json`、`*.local.json`、provider API keys、真实 bearer token、内部 URL、客户数据或个人信息。
- 不要把 `local/`、`dist/`、构建产物、临时日志、coverage、archive 包加入 Git。
- 不要恢复 Rust / Cargo / dual-language build path；当前正式形态是 Go-only root module。
- 不要把 XRouter 写成会偷偷改 prompt 的黑盒。任何 hidden system、judge prompt、reference prompt 或 aggregator prompt 行为都必须属于明确配置或明确 flow。
- 不要绕过 config validation 来容忍缺失 provider、target、route、MoA aggregator/reference、unsupported MoV flow 或非法 passthrough。
- 不要在没有用户明确确认时创建 tag、触发 GitHub Release、推送 GHCR image、发布生产包或修改 repository 权限。
- 不要手工编辑 `dist/` 下的文件；需要产物时重新运行 Makefile target。
- 不要为了修文档大段复制 `docs/strategy/XROUTER_STRATEGY_DESIGN_FULL.md`；抽取结论即可。

## Validation

完成代码、config、Makefile、Dockerfile、workflow、docs/examples 行为变更后，按风险选择验证。默认闭环：

1. `gofmt -l ./*.go` 应无输出；需要修复时运行 `make fmt`。
2. `go test ./...` 或 `make test` 必须通过。
3. `make build` 必须通过。
4. `./dist/xrouter -version` 必须能运行。

Scope-specific validation:

- Routing/config/schema changes: run `go test ./...` and add/update focused tests in `router_test.go`, `config_test.go`, `strategy_v3_test.go`, `responses_test.go`, or `race_test.go` as appropriate.
- Docs-only edits: if no behavior changed, at least inspect affected examples/commands for consistency; do not claim runtime validation unless commands were run.
- Release/package changes: read `.github/AGENTS.md`, then run local `go test ./...`, `make build`, and `./dist/xrouter -version`. Use `make release-snapshot VERSION=v0.0.0-local` only when archive layout or checksums changed.
- Docker changes: run `docker build -t xrouter:go .` when Docker is available; if Docker is unavailable, state that clearly.
- Live provider behavior: only validate with real upstream calls when user has provided or approved credentials/config for that test; otherwise stop at unit/build validation.

## Notes for future agents

- The background source material says clearly: model ID is now a strategy entrypoint. Preserve that framing in code review, docs, and release notes.
- `config.example.json` is the canonical runnable config. Examples should demonstrate specific route IDs and request override fields, not introduce a second schema.
- Keep northbound OpenAI-compatible paths stable: `/v1/chat/completions`, `/v1/responses`, `/v1/models`, `/healthz`, `/metrics`, `/debug/prefix-cache`.
- If a future task asks to publish, distinguish branch push / PR from tag-driven release. Pushing code to GitHub is not the same as creating a versioned GitHub Release.
