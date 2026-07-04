# 发布清单

本文件描述 XRouter 当前正式仓库包的可发布内容。`local/` 是早期材料归档，`dist/` 是本地构建产物，二者默认不进入 Git 或 release source。

## 仓库内容

```text
xrouter/
  README.md
  MANIFEST.md
  Makefile
  Dockerfile
  .dockerignore
  go.mod
  .gitignore
  AGENTS.md
  .github/AGENTS.md
  .github/workflows/ci.yml
  .github/workflows/release.yml

  *.go
  *_test.go
  config.example.json

  docs/
    BACKGROUND.md
    ARCHITECTURE.md
    CONFIGURATION.md
    RELEASING.md
    STRATEGIES.md
    DEGRADATION_GUARD_AND_RACE.md
    GO_ONLY_MIGRATION.md
    OPENAI_COMPATIBILITY.md
    PRODUCT_DESIGN.md
    TESTING_AND_CI.md
    strategy/XROUTER_STRATEGY_DESIGN_FULL.md

  examples/
    chat.direct-alias.json
    chat.smart-router-dry-run.json
    chat.smart-router-prefix-cache.json
    chat.judge-router-dry-run.json
    chat.mov.parallel-synth.json
    chat.mov.verify-escalate.json
    chat.mov.cascade-budget.json
    chat.race.max-output.json
    chat.race.boundary-guard.json
    chat.race.effort-ladder.json
    chat.race.serial-escalate.json
    responses.smart-router.json
    responses.race.boundary-guard.json
    opencode.xrouter.json
    xrouter.strategy.example.yaml
```

## 已从旧 dual-language 包中移除

```text
xrouter-go/ wrapper directory
xrouter-rust/
Cargo.toml
Rust Dockerfile
Rust Makefile
Rust GitHub Actions job
Rust build artifacts
```

当前正式交付面是 Go-only root module。不要恢复 Rust / Cargo / dual-language build path。

## 本地验证命令

```bash
gofmt -l ./*.go
make vet
go test ./...
actionlint
make build
./dist/xrouter -version
make release-snapshot VERSION=v0.0.0-local
git diff --check
```

## 发布边界

- 普通 PR 只运行 CI，不创建 GitHub Release，不发布 GHCR image。
- `release.yml` 只在 `v*` tag 或手动 dispatch 时构建 release assets。
- Release artifacts 包含多平台 archive、`SHA256SUMS`、GitHub Release assets 和 `ghcr.io/blueskyxn/xrouter:<tag>`。
- 创建 tag、触发 release 或发布 GHCR image 前，需要明确确认。
