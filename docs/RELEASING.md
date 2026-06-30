# Releasing XRouter

XRouter releases are tag-driven. A release publishes multi-platform binary archives, checksums, and a GHCR container image.

## Local preflight

```bash
make fmt
make vet
make test
make build
./dist/xrouter -version
```

Build local release archives for all supported platforms:

```bash
make release-snapshot VERSION=v0.0.0-local
ls dist/packages
```

## Tag release

```bash
git tag -a v0.1.0 -m "XRouter v0.1.0"
git push origin v0.1.0
```

Pushing a `v*` tag runs `.github/workflows/release.yml`.

## Manual release

The Release workflow can also be run from GitHub Actions with `workflow_dispatch`. Provide an existing tag name. Manual releases default to draft mode so the generated notes and assets can be reviewed before publication.

## Published assets

The release workflow builds:

```text
xrouter_<tag>_linux_amd64.tar.gz
xrouter_<tag>_linux_arm64.tar.gz
xrouter_<tag>_darwin_amd64.tar.gz
xrouter_<tag>_darwin_arm64.tar.gz
xrouter_<tag>_windows_amd64.zip
SHA256SUMS
```

Each archive contains:

```text
xrouter / xrouter.exe
README.md
LICENSE
config.example.json
docs/
examples/
```

The workflow also publishes:

```text
ghcr.io/blueskyxn/xrouter:<tag>
```

## Version metadata

Release binaries embed:

```text
version = git tag
commit  = GitHub SHA
date    = UTC build timestamp
```

Check a binary with:

```bash
xrouter -version
```
