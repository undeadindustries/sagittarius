#!/usr/bin/env bash
# smoke-e2e.sh — run the Sagittarius end-to-end suite.
#
# Default: live mode against real providers using cheap models. Requires at
# least one provider API key (GEMINI_API_KEY/GOOGLE_API_KEY, OPENAI_API_KEY) or a
# key in the OS keychain / encrypted credential file. Live runs make billable
# API calls.
#
# Mock mode: pass --mock (or set SAGITTARIUS_E2E_MOCK=1) to run the same scenario
# table against an in-process mock server with no keys and no network.
#
# Cost control (live): override the per-provider model via
#   SAGITTARIUS_E2E_MODEL_GEMINI, SAGITTARIUS_E2E_MODEL_OPENAI,
#   SAGITTARIUS_E2E_MODEL_OPENAI_RESPONSES
set -euo pipefail

cd "$(dirname "$0")/.."

if [[ "${1:-}" == "--mock" || "${SAGITTARIUS_E2E_MOCK:-}" == "1" ]]; then
	echo "Running E2E in mock mode (no API keys, no network)..."
	exec env SAGITTARIUS_E2E_MOCK=1 go test -count=1 ./tests/e2e/...
fi

# Live mode: require at least one usable provider key up front so the run fails
# loudly with guidance rather than silently skipping every scenario.
if [[ -z "${GEMINI_API_KEY:-}${GOOGLE_API_KEY:-}${OPENAI_API_KEY:-}" ]]; then
	cat >&2 <<'EOF'
No provider API key found in the environment for live E2E.

Set at least one of:
  GEMINI_API_KEY / GOOGLE_API_KEY   (Gemini)
  OPENAI_API_KEY                    (OpenAI, OpenAI Responses)

Or run the key-free mock suite:
  scripts/smoke-e2e.sh --mock
EOF
	exit 1
fi

echo "Running E2E in live mode (cheap models; billable API calls)..."
exec env SAGITTARIUS_E2E_LIVE=1 go test -count=1 -v ./tests/e2e/...
