"""Harbor installed agent for Sagittarius.

Load it from the repo root:

    PYTHONPATH=. harbor run \
      --agent integrations.harbor.sagittarius_agent:Sagittarius \
      -m openrouter/<vendor>/<model> \
      --ae OPENROUTER_API_KEY=$OPENROUTER_API_KEY
"""

from __future__ import annotations

import json
import shlex
from pathlib import Path

from harbor.agents.capabilities import AgentCapabilities
from harbor.agents.installed.base import BaseInstalledAgent, with_prompt_template
from harbor.agents.options import InstalledAgentOptions
from harbor.environments.base import BaseEnvironment
from harbor.models.agent.context import AgentContext
from pydantic import Field

from integrations.harbor.route import (
    BINARY,
    HOME,
    LOG,
    SETTINGS,
    TRAJECTORY,
    VLLM,
    build_settings,
    release_asset,
    resolve_route,
)


class SagittariusOptions(InstalledAgentOptions):
    """kwargs accepted by `harbor run --agent-kwarg key=value`."""

    binary_path: str | None = Field(
        default=None,
        description="Host path of a Linux sagittarius binary to upload.",
    )
    base_url: str | None = Field(
        default=None,
        description="OpenAI-compatible base URL (local vLLM or any compatible server).",
    )
    context_limit: int | None = Field(
        default=None,
        description="Context window pin for the selected model.",
    )
    temperature: float | None = Field(
        default=None,
        description="Temperature pin for the selected model.",
    )
    reasoning_effort: str | None = Field(
        default=None,
        description="Reasoning effort pin for the selected model.",
    )


class Sagittarius(BaseInstalledAgent):
    """Runs the Sagittarius CLI inside the trial and writes an ATIF trajectory."""

    options_model = SagittariusOptions
    capabilities = AgentCapabilities(atif=True)

    @staticmethod
    def name() -> str:
        return "sagittarius"

    def _options(self) -> SagittariusOptions:
        if not isinstance(self.options, SagittariusOptions):
            raise RuntimeError("Sagittarius options were not parsed")
        return self.options

    def _route(self) -> tuple[str, str]:
        if not self.model_name:
            raise ValueError("Sagittarius requires -m <model>")
        return resolve_route(self.model_name, self._options().base_url)

    @with_prompt_template
    async def run(
        self,
        instruction: str,
        environment: BaseEnvironment,
        context: AgentContext,
    ) -> None:
        provider_id, model = self._route()
        opts = self._options()
        settings = build_settings(
            provider_id,
            model,
            base_url=opts.base_url,
            context_limit=opts.context_limit,
            temperature=opts.temperature,
            reasoning_effort=opts.reasoning_effort,
            vllm_api_key_env=provider_id == VLLM and self._get_env("VLLM_API_KEY") is not None,
        )
        await self._write_settings(environment, settings)
        env = {
            "SAGITTARIUS_HOME": HOME,
            "HARBOR_INSTRUCTION": instruction,
        }
        command = (
            "set -o pipefail; "
            f"{shlex.quote(BINARY)} --yolo --output-format stream-json "
            f"-m {shlex.quote(model)} -p \"$HARBOR_INSTRUCTION\" "
            f"--atif-out {shlex.quote(TRAJECTORY)} | tee {shlex.quote(LOG)}"
        )
        try:
            await self.exec_as_agent(environment, command, env=env)
        finally:
            await self._export_if_missing(environment, env)

    async def install(self, environment: BaseEnvironment) -> None:
        self._route()
        # Go verifies TLS against the system pool. The trial image does not
        # always have CA certificates installed before the agent runs.
        await self.ensure_system_dependencies(environment, ("ca_certificates",))
        binary = self._options().binary_path
        if binary:
            await self._upload_binary(environment, Path(binary).expanduser())
            return
        version = self.version()
        if not version:
            raise RuntimeError(
                "no Sagittarius release is selected. Pass "
                "--agent-kwarg binary_path=<linux binary> until a release "
                "includes --atif-out, or --agent-kwarg version=<tag> after that."
            )
        await self._download_release(environment, version)

    def populate_context_post_run(self, context: AgentContext) -> None:
        source = self.logs_dir / "sagittarius-trajectory.json"
        dest = self.logs_dir / "trajectory.json"
        try:
            if not source.is_file():
                self.logger.warning("sagittarius trajectory missing at %s", source)
                return
            data = json.loads(source.read_text(encoding="utf-8"))
            dest.write_text(json.dumps(data), encoding="utf-8")
            metrics = data.get("final_metrics") or {}
            extra = metrics.get("extra") or {}
            prompt = metrics.get("total_prompt_tokens")
            cached = metrics.get("total_cached_tokens")
            completion = metrics.get("total_completion_tokens")
            cost = metrics.get("total_cost_usd")
            if prompt is not None:
                context.n_input_tokens = int(prompt)
            if cached is not None:
                context.n_cache_tokens = int(cached)
            if completion is not None:
                context.n_output_tokens = int(completion)
            if cost is not None:
                context.cost_usd = float(cost)
            outcome = extra.get("outcome")
            if outcome:
                if context.metadata is None:
                    context.metadata = {}
                context.metadata["outcome"] = outcome
        except Exception as exc:
            self.logger.warning("failed to record sagittarius trajectory: %s", exc)

    async def _write_settings(self, environment: BaseEnvironment, settings: dict) -> None:
        quoted_dir = shlex.quote(str(Path(SETTINGS).parent))
        await self.exec_as_agent(environment, f"mkdir -p {quoted_dir}")
        await self._upload_config_text(
            environment,
            content=json.dumps(settings, indent=2) + "\n",
            remote_path=SETTINGS,
            filename="settings.json",
        )

    async def _upload_binary(self, environment: BaseEnvironment, source: Path) -> None:
        if not source.is_file():
            raise FileNotFoundError(f"binary_path is not a file: {source}")
        await self._upload_agent_owned_file(environment, source, BINARY)
        await self.exec_as_root(environment, f"chmod 755 {shlex.quote(BINARY)}")

    async def _download_release(self, environment: BaseEnvironment, version: str) -> None:
        await self.ensure_system_dependencies(environment, ("curl", "tar", "ca_certificates"))
        uname = await self.exec_as_agent(environment, "uname -m")
        url, filename = release_asset(version, (uname.stdout or "").strip())
        command = (
            "set -euo pipefail; "
            "tmp=$(mktemp -d); "
            f"curl -fsSL {shlex.quote(url)} -o \"$tmp/{filename}\"; "
            f"tar -xzf \"$tmp/{filename}\" -C \"$tmp\"; "
            f"install -m 755 \"$tmp/sagittarius\" {shlex.quote(BINARY)}"
        )
        await self.exec_as_root(environment, command)

    async def _export_if_missing(self, environment: BaseEnvironment, env: dict[str, str]) -> None:
        check = await environment.exec(command=f"test -s {shlex.quote(TRAJECTORY)}")
        if check.return_code == 0:
            return
        self.logger.warning("trajectory missing after the run; exporting the session")
        try:
            await self.exec_as_agent(
                environment,
                f"{shlex.quote(BINARY)} --export-atif latest --atif-out {shlex.quote(TRAJECTORY)}",
                env=env,
            )
        except Exception as exc:
            self.logger.warning("fallback ATIF export failed: %s", exc)
