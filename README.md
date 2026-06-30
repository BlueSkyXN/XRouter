# XRouter

XRouter 是一个自托管、OpenAI-compatible 的 LLM 路由控制层，当前正式实现是单一 Go gateway。

它的目标不是做一个 OpenRouter 替代品，而是在你自己的基础设施里提供可审计、可配置、可发布的模型路由控制面。XRouter 把请求里的 `model` 字段视为策略入口，不只是上游模型名：同一个 OpenAI-style client 可以继续调用 `/v1/chat/completions` 或 `/v1/responses`，由 XRouter 在后端执行 direct alias、smart router、MoV / MoA、race / degradation guard、prefix-cache bookkeeping、explicit passthrough 等策略。

本仓库是早期 dual-language / local package 的 Go-only 正式化版本。可部署单元明确收敛为：

- 一个 Go module：`github.com/BlueSkyXN/XRouter`
- 一个 Docker image
- 一个 `xrouter` binary
- 一套 GitHub Actions CI / release packaging workflow

## XRouter 做什么

XRouter 让客户端继续使用 OpenAI-compatible API，同时把 model ID 映射到可配置策略：

- `direct_alias`：固定 public model ID 到 upstream target 的映射，不改 prompt、messages、tools、tool choice、response format 或 generation parameters。
- `smart_router`：单目标自动路由，支持 hard filters、weighted scoring、prefix-cache bookkeeping、sticky sessions、keyword rules、可选 judge-router signal。
- `moa` / `mov`：显式多模型编排，包括 parallel synthesize、judge select、verify-then-escalate、cascade budget、shadow evaluation、race strategies。
- `race`：面向退化防护的多尝试选择，包括 boundary-aware reasoning-token detection 和 output-volume racing。
- `passthrough`：只在显式 unknown model policy 允许时，把未知 model ID 转发到指定 provider。

## 不是什么

- 不是 OpenRouter clone，也不根据 model ID 的字符串形状自动选择上游。
- 不是 prompt rewriting 黑盒；任何 hidden system、judge prompt、aggregator prompt 都必须来自明确配置或明确 flow。
- 不是完整 OpenAI API clone；它保持常用 northbound path 兼容，同时在文档里明确支持边界。
- 不是 response cache；prefix cache 只做 bookkeeping，不保存原始 prompt prefix 或用户内容。

## API surface

```text
POST /v1/chat/completions
POST /v1/responses
GET  /v1/models
GET  /healthz
GET  /metrics
GET  /debug/prefix-cache
```

当配置了 `XROUTER_API_KEYS` 或 inline API keys 后，除 `/healthz` 外，API、debug、metrics endpoint 都要求：

```text
Authorization: Bearer <xrouter-api-key>
```

Provider credentials 应优先通过环境变量注入，例如 `OPENAI_API_KEY`、`OPENROUTER_API_KEY`，不要提交到 Git。

## Repository layout

```text
.
├── *.go                         # Go-only gateway implementation
├── *_test.go                    # focused unit tests
├── config.example.json           # canonical runnable strategy/provider config
├── Dockerfile
├── Makefile
├── AGENTS.md                     # 中文 Codex repo-local capability/router
├── .github/
│   ├── AGENTS.md                 # CI/release/GHCR guardrail
│   └── workflows/
│       ├── ci.yml                # Go lint/test/build workflow
│       └── release.yml           # tag/manual release packaging workflow
├── docs/
│   ├── BACKGROUND.md
│   ├── ARCHITECTURE.md
│   ├── CONFIGURATION.md
│   ├── RELEASING.md
│   ├── STRATEGIES.md
│   ├── DEGRADATION_GUARD_AND_RACE.md
│   ├── GO_ONLY_MIGRATION.md
│   ├── OPENAI_COMPATIBILITY.md
│   ├── PRODUCT_DESIGN.md
│   └── TESTING_AND_CI.md
└── examples/
    ├── chat.direct-alias.json
    ├── chat.smart-router-dry-run.json
    ├── chat.race.boundary-guard.json
    └── ...
```

`local/` 是早期云端/本地包和过程材料的归档目录，默认不发布；`dist/` 是构建产物目录，默认由 Makefile 生成。

## 快速启动

```bash
cp config.example.json config.local.json

export OPENAI_API_KEY=sk-...
export OPENROUTER_API_KEY=sk-or-...

make run CONFIG=config.local.json
```

等价的直接命令：

```bash
go run . -config config.local.json
```

默认监听地址来自配置文件。真实 upstream 调用需要相应 provider key；dry-run 示例不需要真实调用 provider。

## 测试和构建

```bash
make fmt
make vet
make test
make build
./dist/xrouter -version
```

构建后的本机 binary 位于：

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

## 生产部署安全基线

`config.example.json` 用于本地可运行示例。共享或公开部署时，应设置 XRouter 自己的 API key，并把 provider key 放在环境变量中：

```bash
export XROUTER_API_KEYS=team-key-1,team-key-2
export OPENAI_API_KEY=sk-...
export OPENROUTER_API_KEY=sk-or-...
```

请求侧使用：

```text
Authorization: Bearer team-key-1
```

不要提交 `.env*`、`config.local.json`、`*.local.json`、真实 token、内部 URL、客户数据或个人信息。

## GitHub Actions

`.github/workflows/ci.yml` 在 push / PR 上执行：

```text
gofmt check
go vet ./...
go test ./...
make build
./dist/xrouter -version
upload dist/xrouter as xrouter-linux-amd64
```

`.github/workflows/release.yml` 在 `v*` tag 或手动 dispatch 时执行 release packaging：

```text
linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64 archives
SHA256SUMS
GitHub Release assets
ghcr.io/blueskyxn/xrouter:<tag>
```

注意：普通分支 push / PR 不会创建 tag，不会触发 GitHub Release，也不会发布 GHCR image。

## 基础示例

Direct alias：

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'content-type: application/json' \
  -d @examples/chat.direct-alias.json
```

Smart router dry-run：

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'content-type: application/json' \
  -d @examples/chat.smart-router-dry-run.json
```

Boundary-aware race：

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'content-type: application/json' \
  -d @examples/chat.race.boundary-guard.json
```

Responses shim / passthrough：

```bash
curl http://127.0.0.1:8080/v1/responses \
  -H 'content-type: application/json' \
  -d @examples/responses.smart-router.json
```

## 配置模型

请求里的 `model` 字段按策略 key 处理：

```text
request.model
  -> exact route match
  -> prefix route match
  -> default route
  -> configured target ID
  -> unknown model policy
  -> direct / smart_router / mov / moa / passthrough executor
```

Unknown model passthrough 必须显式配置：

- `reject`：拒绝未知 model ID。
- `passthrough_openai`：转发到配置里的 `openai` provider。
- `passthrough_openrouter`：转发到配置里的 `openrouter` provider。

model name 是否包含 `/` 不会覆盖这个 policy。

Canonical runnable config 是 `config.example.json`。启用 request-level overrides 后，客户端可以在 `xrouter` 字段中传入路由目标、dry-run、explain、prefix hint、judge 等控制信息：

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

## 当前验证状态

本包已经从早期 Go implementation 迁移为 root Go module，并补齐 CI、release packaging、Docker、examples 和文档入口。本地已验证：

```text
gofmt -l ./*.go
make vet
go test ./...
actionlint
make release-snapshot VERSION=v0.0.0-local
make build
./dist/xrouter -version
git diff --check
```

详见 `docs/GO_ONLY_MIGRATION.md`、`docs/TESTING_AND_CI.md` 和 `docs/RELEASING.md`。
