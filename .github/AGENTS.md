# .github navigation card

This directory controls CI, release packaging, GitHub Release assets, and GHCR image publication.
Read this card before modifying any workflow file.
Key files: `.github/workflows/ci.yml`, `.github/workflows/release.yml`.

## Why this is high-risk

- `release.yml` has `contents: write` and `packages: write`; changes can publish GitHub Release assets and container images.
- Tag pushes matching `v*` and manual dispatch can create externally visible release artifacts.
- Workflow changes define the public validation signal for this repository and can silently weaken packaging or security guarantees.

## Required before changes

- Inspect `go.mod`, `Makefile`, `Dockerfile`, `README.md`, and `docs/RELEASING.md` before changing build/release behavior.
- Keep CI aligned with local commands: format check, `make vet`, `make test`, `make build`, and `./dist/xrouter -version`.
- Keep release artifacts aligned with docs: Linux amd64/arm64, Darwin amd64/arm64, Windows amd64, `SHA256SUMS`, GitHub Release assets, and GHCR image.
- Use least permissions. `ci.yml` should not need write permissions. `release.yml` needs write permissions only for release assets and packages.

## Do not

- Do not create tags, trigger releases, publish GHCR images, or run manual release dispatch without explicit user confirmation.
- Do not add secrets, tokens, private URLs, or inline credentials to workflow YAML.
- Do not use `pull_request_target` unless the user explicitly asks and the security implications are reviewed.
- Do not weaken format/test/build checks to make a release pass.
- Do not publish files from `local/` or `dist/` source state; release workflows should build fresh artifacts.

## Validation

- Local default before workflow release changes: `go test ./...`, `make build`, `./dist/xrouter -version`.
- For packaging layout changes, also run `make release-snapshot VERSION=v0.0.0-local`.
- Docker/GHCR changes need `docker build -t xrouter:go .` when Docker is available.
- GitHub-side workflow execution requires a pushed branch or manual GitHub run; if not run, state that limitation in the final report.
