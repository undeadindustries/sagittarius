# Benchmarking Sagittarius with Harbor

Sagittarius integrates with the [Harbor benchmarking framework](https://github.com/harbor-framework/harbor) to evaluate task performance on standard suites like **Terminal-Bench 2.0 (TB2)** and **SWE-bench**.

## Running Terminal-Bench 2.0

### Prerequisites
Install Harbor via `uv` or `pip`:
```bash
pip install harbor-framework
# or
uv tool install harbor-framework
```

Ensure API keys for your target model provider (e.g. `OPENROUTER_API_KEY`, `GEMINI_API_KEY`, `OPENAI_API_KEY`) are exported in your environment.

### Execution

To run Terminal-Bench 2.0 with the Sagittarius installed-agent adapter:
```bash
harbor run -d terminal-bench@2.0 \
  --agent integrations.harbor.sagittarius_agent:Sagittarius \
  --agent-kwargs '{"provider":"openrouter","model":"anthropic/claude-3.5-sonnet"}'
```

### Local vLLM on DGX Spark (ASUS Ascent GX10)

When benchmarking against a local vLLM instance running on host port 8000:
- Use `host.docker.internal` or the host IP to allow containers in Harbor's Docker network to reach the local endpoint.
- Pass `base_url`:
```bash
harbor run -d terminal-bench@2.0 \
  --agent integrations.harbor.sagittarius_agent:Sagittarius \
  --agent-kwargs '{"provider":"local-gx10","model":"Qwen/Qwen2.5-Coder-32B-Instruct","base_url":"http://host.docker.internal:8000/v1"}'
```

---

## Evaluating SWE-bench

Confirm available datasets with:
```bash
harbor datasets list
```

Run SWE-bench Lite:
```bash
harbor run -d swe-bench-lite@1.0 \
  --agent integrations.harbor.sagittarius_agent:Sagittarius \
  --agent-kwargs '{"provider":"openrouter","model":"anthropic/claude-3.5-sonnet"}'
```

---

## A/B Trajectory Analysis Workflow

When evaluating prompt, model, or tool modifications:
1. Run a fixed subset of benchmark tasks using the baseline version and save the exported trajectory.
2. Run the same tasks using the candidate version.
3. Compare trajectories using the built-in analyzer:

```bash
sagittarius --analyze-trajectory run-baseline/trajectory.json \
  --compare run-candidate/trajectory.json
```
The comparison table details differences in LLM calls, total token consumption, cache hit rates, tool failure rates, and detected loop hazards.
