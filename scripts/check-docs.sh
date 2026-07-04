#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

required_files=(
  "README.md"
  "AGENTS.md"
  "Makefile"
  "Dockerfile"
  "config.example.json"
  ".github/AGENTS.md"
  ".github/workflows/ci.yml"
  ".github/workflows/release.yml"
  "docs/README.md"
  "docs/ARCHITECTURE.md"
  "docs/CONFIGURATION.md"
  "docs/OPERATIONS.md"
  "docs/OPENAI_COMPATIBILITY.md"
  "docs/RELEASING.md"
  "docs/TESTING_AND_CI.md"
  "examples/chat.smart-router-dry-run.json"
  "examples/responses.smart-router.json"
  "scripts/check-docs.sh"
  "scripts/smoke-local.sh"
)

missing=()
for path in "${required_files[@]}"; do
  if [ ! -f "$path" ]; then
    missing+=("$path")
  fi
done

if [ "${#missing[@]}" -ne 0 ]; then
  printf 'Missing required repository files:\n' >&2
  printf '  %s\n' "${missing[@]}" >&2
  exit 1
fi

for script in scripts/check-docs.sh scripts/smoke-local.sh; do
  if [ ! -x "$script" ]; then
    printf 'Script is not executable: %s\n' "$script" >&2
    exit 1
  fi
done

python3 - <<'PY'
import json
import pathlib
import re
import sys

root = pathlib.Path.cwd().resolve()

json_paths = [pathlib.Path("config.example.json")]
json_paths.extend(sorted(pathlib.Path("examples").glob("*.json")))

errors = []
for path in json_paths:
    try:
        with path.open("r", encoding="utf-8") as handle:
            json.load(handle)
    except Exception as exc:  # noqa: BLE001 - script reports the failing file.
        errors.append(f"{path}: invalid JSON: {exc}")

markdown_paths = [pathlib.Path("README.md")]
markdown_paths.extend(sorted(pathlib.Path("docs").glob("*.md")))
link_pattern = re.compile(r"(?<!!)\[[^\]]+\]\(([^)]+)\)")
external_prefixes = ("http://", "https://", "mailto:")

for path in markdown_paths:
    text = path.read_text(encoding="utf-8")
    for match in link_pattern.finditer(text):
        target = match.group(1).strip()
        if not target or target.startswith("#") or target.startswith(external_prefixes):
            continue
        if target.startswith("<") and target.endswith(">"):
            target = target[1:-1].strip()
        if " " in target and not target.startswith(("./", "../")):
            target = target.split(" ", 1)[0]
        target = target.split("#", 1)[0]
        if not target:
            continue
        if target.startswith("/"):
            errors.append(f"{path}: absolute local Markdown link is not portable: {target}")
            continue
        resolved = (path.parent / target).resolve()
        if root != resolved and root not in resolved.parents:
            errors.append(f"{path}: Markdown link escapes repository: {target}")
            continue
        if not resolved.exists():
            errors.append(f"{path}: broken Markdown link: {target}")

readme = pathlib.Path("README.md").read_text(encoding="utf-8")
for needle in (
    "docs/README.md",
    "docs/OPERATIONS.md",
    "docs/RELEASING.md",
    "docs/TESTING_AND_CI.md",
):
    if needle not in readme:
        errors.append(f"README.md does not mention {needle}")

ci = pathlib.Path(".github/workflows/ci.yml").read_text(encoding="utf-8")
for needle in (
    "scripts/check-docs.sh",
    "scripts/smoke-local.sh",
    "actionlint",
    "go test -race -count=1 ./...",
    "make release-snapshot VERSION=v0.0.0-ci",
):
    if needle not in ci:
        errors.append(f".github/workflows/ci.yml does not include {needle}")

release = pathlib.Path(".github/workflows/release.yml").read_text(encoding="utf-8")
for needle in (
    "scripts/check-docs.sh",
    "scripts/smoke-local.sh",
    "go test -race -count=1 ./...",
):
    if needle not in release:
        errors.append(f".github/workflows/release.yml does not include {needle}")

for path in ("Makefile", ".github/workflows/release.yml", "Dockerfile"):
    text = pathlib.Path(path).read_text(encoding="utf-8")
    forbidden = ("cp -R local", "cp -r local", "COPY local", "local/")
    if any(item in text for item in forbidden):
        errors.append(f"{path}: release/build path must not package ignored local/ material")

if errors:
    print("Documentation and repository contract checks failed:", file=sys.stderr)
    for error in errors:
        print(f"  - {error}", file=sys.stderr)
    sys.exit(1)
PY

echo "docs and repository contracts passed"
