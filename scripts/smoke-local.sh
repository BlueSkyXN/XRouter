#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

binary="${XROUTER_SMOKE_BINARY:-$repo_root/dist/xrouter}"
if [ ! -x "$binary" ]; then
  make build
fi

port="${XROUTER_SMOKE_PORT:-}"
if [ -z "$port" ]; then
  port="$(python3 - <<'PY'
import socket

with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
    sock.bind(("127.0.0.1", 0))
    print(sock.getsockname()[1])
PY
)"
fi

tmpdir="$(mktemp -d)"
server_pid=""

cleanup() {
  if [ -n "${server_pid:-}" ] && kill -0 "$server_pid" 2>/dev/null; then
    kill -TERM "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  rm -rf "$tmpdir"
}
trap cleanup EXIT INT TERM

python3 - "$repo_root/config.example.json" "$tmpdir/config.json" "$port" <<'PY'
import json
import sys

source, dest, port = sys.argv[1:4]
with open(source, "r", encoding="utf-8") as handle:
    cfg = json.load(handle)

cfg.setdefault("server", {})["listen"] = f"127.0.0.1:{port}"
cfg["server"]["debug"] = True
cfg.setdefault("auth", {})["api_keys"] = ["smoke-key"]
cfg["auth"]["api_keys_env"] = "XROUTER_SMOKE_API_KEYS"

with open(dest, "w", encoding="utf-8") as handle:
    json.dump(cfg, handle, indent=2, sort_keys=True)
    handle.write("\n")
PY

"$binary" -config "$tmpdir/config.json" >"$tmpdir/server.log" 2>&1 &
server_pid="$!"
base_url="http://127.0.0.1:$port"

ready=0
for _ in $(seq 1 100); do
  if curl -fs "$base_url/healthz" >"$tmpdir/health.json" 2>/dev/null; then
    ready=1
    break
  fi
  if ! kill -0 "$server_pid" 2>/dev/null; then
    printf 'xrouter server exited during startup\n' >&2
    cat "$tmpdir/server.log" >&2
    exit 1
  fi
  sleep 0.1
done

if [ "$ready" -ne 1 ]; then
  printf 'xrouter server did not become ready at %s\n' "$base_url" >&2
  cat "$tmpdir/server.log" >&2
  exit 1
fi

python3 - "$tmpdir/health.json" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    payload = json.load(handle)
if payload.get("ok") is not True or payload.get("impl") != "go":
    raise SystemExit(f"unexpected health payload: {payload}")
PY

status="$(curl -sS -o "$tmpdir/models.noauth.json" -w "%{http_code}" "$base_url/v1/models" || true)"
if [ "$status" != "401" ]; then
  printf 'expected unauthenticated /v1/models to return 401, got %s\n' "$status" >&2
  cat "$tmpdir/models.noauth.json" >&2
  exit 1
fi

curl -fsS \
  -H "Authorization: Bearer smoke-key" \
  "$base_url/v1/models" >"$tmpdir/models.json"

python3 - "$tmpdir/models.json" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    payload = json.load(handle)
ids = {item.get("id") for item in payload.get("data", [])}
required = {"xrouter/auto", "openai-fast"}
missing = sorted(required - ids)
if missing:
    raise SystemExit(f"/v1/models missing expected IDs: {missing}")
PY

curl -fsS \
  -H "content-type: application/json" \
  -H "Authorization: Bearer smoke-key" \
  -d @examples/chat.smart-router-dry-run.json \
  "$base_url/v1/chat/completions" >"$tmpdir/dry-run.json"

python3 - "$tmpdir/dry-run.json" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    payload = json.load(handle)
if payload.get("object") != "xrouter.route_decision":
    raise SystemExit(f"dry-run response has unexpected object: {payload}")
if payload.get("model") != "xrouter/auto":
    raise SystemExit(f"dry-run response has unexpected model: {payload}")
if not payload.get("targets"):
    raise SystemExit(f"dry-run response is missing targets: {payload}")
PY

status="$(curl -sS -o "$tmpdir/unknown-model.json" -w "%{http_code}" \
  -H "content-type: application/json" \
  -H "Authorization: Bearer smoke-key" \
  -d '{"model":"unknown/not-configured","messages":[{"role":"user","content":"hello"}]}' \
  "$base_url/v1/chat/completions" || true)"
if [ "$status" != "400" ]; then
  printf 'expected unknown model to return 400, got %s\n' "$status" >&2
  cat "$tmpdir/unknown-model.json" >&2
  exit 1
fi

curl -fsS \
  -H "Authorization: Bearer smoke-key" \
  "$base_url/debug/prefix-cache" >"$tmpdir/prefix-cache.json"

python3 - "$tmpdir/prefix-cache.json" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    payload = json.load(handle)
if payload.get("enabled") is not True:
    raise SystemExit(f"unexpected prefix-cache payload: {payload}")
PY

echo "xrouter local non-live smoke passed at $base_url"
