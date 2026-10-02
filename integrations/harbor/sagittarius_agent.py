"""
Harbor agent adapter for Sagittarius CLI.

Implements BaseInstalledAgent to benchmark Sagittarius against Terminal-Bench 2.0,
SWE-bench, and other Harbor evaluation environments.
"""

import json
import os
import shutil
import subprocess
import tarfile
import urllib.request
from pathlib import Path
from typing import Any, Dict, Optional

# Harbor BaseInstalledAgent import
try:
    from harbor.agents.installed import BaseInstalledAgent  # type: ignore
except ImportError:
    # Fallback interface stub when harbor package is not directly installed in environment
    class BaseInstalledAgent:  # type: ignore
        def __init__(self, **kwargs: Any) -> None:
            self.kwargs = kwargs


class Sagittarius(BaseInstalledAgent):
    """Sagittarius CLI adapter for Harbor benchmarks."""

    VERSION = "0.20.1"
    REPO = "undeadindustries/sagittarius"

    def __init__(
        self,
        provider: str = "openrouter",
        model: Optional[str] = None,
        base_url: Optional[str] = None,
        persona: str = "programmer",
        extra_flags: Optional[str] = None,
        **kwargs: Any,
    ) -> None:
        super().__init__(**kwargs)
        self.provider = provider
        self.model = model or "anthropic/claude-3.5-sonnet"
        self.base_url = base_url
        self.persona = persona
        self.extra_flags = extra_flags or ""

    def install(self, target_dir: Path) -> Path:
        """Download and install the sagittarius binary into the container environment."""
        target_dir = Path(target_dir)
        target_dir.mkdir(parents=True, exist_ok=True)
        bin_path = target_dir / "sagittarius"

        if bin_path.exists():
            return bin_path

        # Check if local binary is available or download from GitHub releases
        arch = "amd64" if os.uname().machine in ("x86_64", "amd64") else "arm64"
        os_name = "linux"
        tar_name = f"sagittarius_{self.VERSION}_{os_name}_{arch}.tar.gz"
        url = f"https://github.com/{self.REPO}/releases/download/v{self.VERSION}/{tar_name}"

        tar_dest = target_dir / tar_name
        try:
            urllib.request.urlretrieve(url, tar_dest)
            with tarfile.open(tar_dest, "r:gz") as tar:
                tar.extractall(path=target_dir)
            bin_path.chmod(0o755)
        except Exception as e:
            # Check PATH fallback
            fallback = shutil.which("sagittarius")
            if fallback:
                return Path(fallback)
            raise RuntimeError(f"Failed to install sagittarius from {url}: {e}")
        finally:
            if tar_dest.exists():
                tar_dest.unlink()

        return bin_path

    def setup_config(self, config_dir: Path) -> None:
        """Write throwaway settings.json in SAGITTARIUS_HOME."""
        config_dir.mkdir(parents=True, exist_ok=True)
        settings_file = config_dir / "settings.json"

        settings: Dict[str, Any] = {
            "sagittarius": {
                "defaultMode": "agent",
                "systemPrompt": self.persona,
            },
            "providers": {
                "active": self.provider,
            },
        }

        if self.base_url:
            settings["providers"]["custom"] = {
                self.provider: {
                    "displayName": self.provider,
                    "baseUrl": self.base_url,
                    "wireFormat": "openai-chat",
                    "defaultModel": self.model,
                }
            }

        with open(settings_file, "w", encoding="utf-8") as f:
            json.dump(settings, f, indent=2)

    def run(self, instruction: str, workspace_dir: Path, logs_dir: Path) -> None:
        """Execute Sagittarius headlessly for one benchmark problem turn."""
        workspace_dir = Path(workspace_dir)
        logs_dir = Path(logs_dir)
        logs_dir.mkdir(parents=True, exist_ok=True)

        home_dir = logs_dir / ".sagittarius_home"
        self.setup_config(home_dir)

        bin_path = self.install(logs_dir / "bin")
        trajectory_out = logs_dir / "sagittarius-trajectory.json"

        env = os.environ.copy()
        env["SAGITTARIUS_HOME"] = str(home_dir)

        cmd = [
            str(bin_path),
            "--yolo",
            "-p",
            instruction,
            "--output-format",
            "stream-json",
            "--atif-out",
            str(trajectory_out),
        ]

        if self.model:
            cmd.extend(["--model", self.model])

        if self.extra_flags:
            cmd.extend(self.extra_flags.split())

        stdout_log = logs_dir / "sagittarius.stdout.log"
        stderr_log = logs_dir / "sagittarius.stderr.log"

        with open(stdout_log, "w", encoding="utf-8") as out_f, open(
            stderr_log, "w", encoding="utf-8"
        ) as err_f:
            subprocess.run(
                cmd,
                cwd=workspace_dir,
                env=env,
                stdout=out_f,
                stderr=err_f,
                check=False,
            )

    def populate_context_post_run(self, context: Any, logs_dir: Path) -> None:
        """Populate evaluation context with ATIF trajectory and token/cost telemetry."""
        logs_dir = Path(logs_dir)
        traj_file = logs_dir / "sagittarius-trajectory.json"
        dest_file = logs_dir / "trajectory.json"

        if traj_file.exists():
            shutil.copyfile(traj_file, dest_file)
            try:
                with open(dest_file, "r", encoding="utf-8") as f:
                    data = json.load(f)
                fm = data.get("final_metrics", {})
                if hasattr(context, "n_input_tokens") and "total_prompt_tokens" in fm:
                    context.n_input_tokens = fm["total_prompt_tokens"]
                if hasattr(context, "n_output_tokens") and "total_completion_tokens" in fm:
                    context.n_output_tokens = fm["total_completion_tokens"]
                if hasattr(context, "cost_usd") and "total_cost_usd" in fm:
                    context.cost_usd = fm["total_cost_usd"]
            except Exception:
                pass
