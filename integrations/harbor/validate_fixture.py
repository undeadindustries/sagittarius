"""Validate an ATIF JSON file with Harbor's own Trajectory model.

Usage: uv run --with harbor python integrations/harbor/validate_fixture.py <file>
"""

import json
import sys
from pathlib import Path


def main() -> None:
    if len(sys.argv) != 2:
        print("usage: validate_fixture.py <trajectory.json>", file=sys.stderr)
        sys.exit(2)
    path = Path(sys.argv[1])
    data = json.loads(path.read_text(encoding="utf-8"))
    from harbor.models.trajectories.trajectory import Trajectory

    Trajectory.model_validate(data)
    print(f"Harbor accepted {path}")


if __name__ == "__main__":
    main()
