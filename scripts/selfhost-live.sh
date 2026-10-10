#!/usr/bin/env bash
# Live self-host acceptance (SH1-3): real local Ollama, real model, real executor,
# disposable fixture project. Run on the owner's machine. Never pulls a model.
#
#   DEVCADENCE_OLLAMA_MODEL=<installed-model> scripts/selfhost-live.sh
#
# Optional: DEVCADENCE_OLLAMA_URL (loopback only). The fixture runs in yolo mode
# (commands unconfined as your user, NOT a sandbox) inside a temp DEVCADENCE_HOME.
set -euo pipefail
cd "$(dirname "$0")/.."

if [ -z "${DEVCADENCE_OLLAMA_MODEL:-}" ]; then
  echo "error: set DEVCADENCE_OLLAMA_MODEL to an installed Ollama model (see 'ollama list'); this script never pulls or picks one" >&2
  exit 2
fi
command -v ollama >/dev/null || { echo "error: ollama is not installed or not on PATH" >&2; exit 2; }

# Preflight: Ollama reachable and the model installed (fails with an actionable error).
home="$(mktemp -d)"
trap 'rm -rf "$home"' EXIT
mkdir -p "$home/config"
printf '{"model":"%s"}\n' "$DEVCADENCE_OLLAMA_MODEL" >"$home/config/selfhost.json"
DEVCADENCE_HOME="$home" go run ./cmd/devcadence selfhost check

DEVCADENCE_LIVE_OLLAMA=1 go test ./internal/selfhost -run 'TestLiveOllamaRepair' -count=1 -v -timeout 40m
