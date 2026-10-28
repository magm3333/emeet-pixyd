#!/usr/bin/env python3
"""Copy daemon state into a Docker state directory with an optional auto mode."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source", type=Path, help="existing emeet-pixyd state.json")
    parser.add_argument("destination", type=Path, help="Docker state.json destination")
    parser.add_argument(
        "--auto",
        choices=("off", "full", "tracking-only", "privacy-only"),
        help="override only the copied state's autoMode (recommended: off in a private container PID namespace)",
    )
    args = parser.parse_args()

    state = json.loads(args.source.read_text(encoding="utf-8"))
    if not isinstance(state, dict) or "v" not in state:
        parser.error("source is not an emeet-pixyd state.json")
    if args.auto is not None:
        state["autoMode"] = args.auto

    args.destination.parent.mkdir(parents=True, exist_ok=True)
    temporary = args.destination.with_suffix(args.destination.suffix + ".tmp")
    temporary.write_text(json.dumps(state, separators=(",", ":")) + "\n", encoding="utf-8")
    os.chmod(temporary, 0o600)
    os.replace(temporary, args.destination)
    print(f"Imported {args.source} -> {args.destination}")


if __name__ == "__main__":
    main()
