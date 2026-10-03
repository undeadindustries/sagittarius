# Benchmarking Sagittarius with Harbor

A Harbor trial runs the Sagittarius CLI inside the task container and scores the result. The number is a Sagittarius-plus-model score: the full tool set, the system prompt, and the tool loop. It is not a model-only score.

`terminus-2` and Harbor's `dsh-minimal` agent are smaller scaffolds (a shell and a string replace). Their published numbers are not comparable to a Sagittarius run. Say which harness you used.

## Install

Harbor is not a Go dependency. The opt-in Make targets install it with `uv` for that command:

```bash
uv run --with harbor harbor --version
```

Run the commands below from the repository root so `PYTHONPATH=.` can import the adapter.

## One trial

`--agent` takes `module:Class`. `--agent-import-path` still sets the same field, and current Harbor marks it deprecated.

Until a release includes `--atif-out`, upload a Linux binary. `make harbor-binary` writes `dist/sagittarius-linux-amd64` and `dist/sagittarius-linux-arm64`.

```bash
make harbor-binary
PYTHONPATH=. uv run --with harbor harbor run \
  -d terminal-bench@2.0 \
  --agent integrations.harbor.sagittarius_agent:Sagittarius \
  -m openrouter/deepseek/deepseek-v4-flash \
  --agent-kwarg binary_path=$PWD/dist/sagittarius-linux-amd64 \
  --ae OPENROUTER_API_KEY=$OPENROUTER_API_KEY
```

Use the arm64 binary on an arm64 host. `--agent-kwarg version=0.21.0` downloads that GitHub release instead, once the release exists. `harbor run` takes `--agent-kwarg`, not `--ak` (`--ak` is only on `harbor analyze`).

Gemini is the same shape, with `-m gemini/<model>` and `--ae GEMINI_API_KEY=$GEMINI_API_KEY` (or `GOOGLE_API_KEY`).

The adapter writes a fresh `$SAGITTARIUS_HOME/.sagittarius/settings.json` for the trial: unlimited tool rounds, auto-title off, update check off. Optional pins:

```bash
--agent-kwarg temperature=0 --agent-kwarg reasoning_effort=low
```

## A local OpenAI-compatible server

`--agent-kwarg base_url=...` builds a custom `openai-chat` provider and ignores the `openrouter/` and `gemini/` prefixes. `VLLM_API_KEY` is optional. `context_limit` is optional.

```bash
PYTHONPATH=. uv run --with harbor harbor run \
  -t hello-world/hello-world \
  --agent integrations.harbor.sagittarius_agent:Sagittarius \
  -m Qwen/Qwen3-8B \
  --agent-kwarg binary_path=$PWD/dist/sagittarius-linux-amd64 \
  --agent-kwarg base_url=http://host.docker.internal:8000/v1 \
  --extra-docker-compose integrations/harbor/host-gateway-compose.yaml
```

Harbor's Docker environment does not add `host.docker.internal`. Docker Desktop does. Linux Docker does not, unless the compose service `main` sets `extra_hosts: ["host.docker.internal:host-gateway"]`. The file above is that overlay, passed with `--extra-docker-compose`. The server must already be listening on the host. This is not specific to one machine.

A task whose network policy is an allowlist also has to allow the host. A public network policy does not.

## What the trial writes

The CLI runs with `--yolo`, `--atif-out /logs/agent/sagittarius-trajectory.json`, and the instruction in `HARBOR_INSTRUCTION`. If the process is killed before that file exists, the adapter runs `--export-atif latest` with the same home directory. `populate_context_post_run` copies the file to `agent/trajectory.json` and fills Harbor's token and cost fields. `final_metrics.extra.outcome` is copied to `context.metadata["outcome"]`.

`make atif-harbor-validate` checks the checked-in fixture with Harbor's own Trajectory model. `make bench-smoke` runs `hello-world/hello-world` on OpenRouter and checks that `agent/trajectory.json` validates. Both need `uv`. The smoke target also needs Docker and `OPENROUTER_API_KEY`.

## What to publish

Publish the Harbor version, the dataset name and version, the Sagittarius version, the model id, the temperature and reasoning pins, the task count, and the pass rate. A trial whose outcome is `max_rounds` did not finish. Count it as incomplete, not as a pass.
