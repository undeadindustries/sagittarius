"""Model routing and settings.json for the Harbor adapter.

Kept free of a Harbor import so the routing rules can be checked without
the framework installed.
"""

from __future__ import annotations

from typing import Any

OPENROUTER = "openrouter"
GEMINI = "gemini-apikey"
VLLM = "vllm"

OPENROUTER_PREFIX = "openrouter/"
GEMINI_PREFIX = "gemini/"
VLLM_PREFIX = "vllm/"

HOME = "/logs/agent/sagittarius-home"
# ResolveSettingsPath is $SAGITTARIUS_HOME/.sagittarius/settings.json.
SETTINGS = HOME + "/.sagittarius/settings.json"
TRAJECTORY = "/logs/agent/sagittarius-trajectory.json"
LOG = "/logs/agent/sagittarius.txt"
BINARY = "/usr/local/bin/sagittarius"

REPO = "undeadindustries/sagittarius"


class RouteError(ValueError):
    """The Harbor -m string is not one this adapter can run."""


def resolve_route(model_name: str, base_url: str | None) -> tuple[str, str]:
    """Return (provider id, model id) for a Harbor model string.

    base_url wins: any model name is sent to a generated openai-chat provider.
    Otherwise the string must be openrouter/<vendor>/<model> or gemini/<model>.
    """
    name = (model_name or "").strip()
    url = (base_url or "").strip()
    if url:
        model = name[len(VLLM_PREFIX) :] if name.startswith(VLLM_PREFIX) else name
        if not model:
            raise RouteError("base_url is set but the model name is empty")
        return VLLM, model
    if name.startswith(OPENROUTER_PREFIX):
        model = name[len(OPENROUTER_PREFIX) :]
        if "/" not in model:
            raise RouteError(
                "OpenRouter needs a vendor and a model: "
                "-m openrouter/<vendor>/<model>"
            )
        return OPENROUTER, model
    if name.startswith(GEMINI_PREFIX):
        model = name[len(GEMINI_PREFIX) :]
        if not model:
            raise RouteError("gemini model is empty: -m gemini/<model>")
        return GEMINI, model
    raise RouteError(
        "unsupported model. Use -m openrouter/<vendor>/<model>, "
        "-m gemini/<model>, or any model with "
        "--agent-kwarg base_url=<openai-compatible URL>."
    )


def release_asset(version: str, uname_m: str) -> tuple[str, str]:
    """Return (download URL, archive file name) for a GitHub release.

    version may be v0.21.0 or 0.21.0. uname -m x86_64 maps to amd64;
    aarch64 and arm64 map to arm64.
    """
    raw = version.strip()
    if raw.startswith("v"):
        raw = raw[1:]
    if not raw:
        raise RouteError("version is empty")
    arch = {"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}.get(
        uname_m.strip()
    )
    if arch is None:
        raise RouteError(f"unsupported container architecture {uname_m!r}")
    filename = f"sagittarius_{raw}_linux_{arch}.tar.gz"
    url = f"https://github.com/{REPO}/releases/download/v{raw}/{filename}"
    return url, filename


def build_settings(
    provider_id: str,
    model: str,
    *,
    base_url: str | None = None,
    context_limit: int | None = None,
    temperature: float | None = None,
    reasoning_effort: str | None = None,
    vllm_api_key_env: bool = False,
) -> dict[str, Any]:
    """Fresh settings for one trial. Keys stay in the environment."""
    model_cfg: dict[str, Any] = {}
    if temperature is not None:
        model_cfg["temperature"] = temperature
    if reasoning_effort:
        model_cfg["reasoningEffort"] = reasoning_effort
    if context_limit is not None:
        model_cfg["contextLimit"] = context_limit

    instance: dict[str, Any] = {"model": model}
    if model_cfg:
        instance["models"] = {model: model_cfg}

    providers: dict[str, Any] = {"active": provider_id, provider_id: instance}
    if provider_id == OPENROUTER:
        providers["custom"] = {
            OPENROUTER: {
                "displayName": "OpenRouter",
                "baseUrl": "https://openrouter.ai/api/v1/chat/completions",
                "apiKeyEnvVar": "OPENROUTER_API_KEY",
                "wireFormat": "openai-chat",
                "defaultModel": model,
            }
        }
    elif provider_id == VLLM:
        definition: dict[str, Any] = {
            "displayName": "vLLM",
            "baseUrl": (base_url or "").strip(),
            "defaultModel": model,
            "wireFormat": "openai-chat",
        }
        if vllm_api_key_env:
            definition["apiKeyEnvVar"] = "VLLM_API_KEY"
        if context_limit is not None:
            definition["defaultContextLimit"] = context_limit
        providers["custom"] = {VLLM: definition}

    return {
        "sagittarius": {
            "maxToolRounds": 0,
            "sessions": {"autoTitle": "off"},
            "update": {"autoCheck": False},
        },
        "providers": providers,
    }
