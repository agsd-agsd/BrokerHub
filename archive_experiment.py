#!/usr/bin/env python3
"""Archive one experiment run into a git-friendly directory."""

from __future__ import annotations

import argparse
import json
import re
import shutil
import subprocess
import sys
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Iterable


REPO_ROOT = Path(__file__).resolve().parent
DEFAULT_SOURCE_CANDIDATES = (
    REPO_ROOT / "hubres",
    REPO_ROOT / "supervisor" / "committee" / "hubres",
    REPO_ROOT / "hubres_best",
)
DEFAULT_OUTPUT_ROOT = REPO_ROOT / "experiments" / "runs"
ARTIFACT_PATTERNS = ("hub*.csv", "*.pdf", "*.json", "*.txt")


@dataclass(frozen=True)
class GitSnapshot:
    branch: str
    head: str
    dirty: bool


def run_git(args: list[str]) -> str:
    result = subprocess.run(
        ["git", *args],
        cwd=REPO_ROOT,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        check=True,
    )
    return result.stdout.strip()


def safe_git_snapshot() -> GitSnapshot:
    try:
        branch = run_git(["rev-parse", "--abbrev-ref", "HEAD"])
        head = run_git(["rev-parse", "HEAD"])
        dirty = bool(run_git(["status", "--short"]))
        return GitSnapshot(branch=branch, head=head, dirty=dirty)
    except Exception:
        return GitSnapshot(branch="unknown", head="unknown", dirty=True)


def slugify(value: str) -> str:
    value = value.strip().lower()
    value = re.sub(r"[^a-z0-9]+", "-", value)
    value = re.sub(r"-{2,}", "-", value).strip("-")
    return value or "run"


def resolve_source_dir(explicit: str | None) -> Path:
    if explicit:
        source_dir = Path(explicit).expanduser()
        if not source_dir.is_absolute():
            source_dir = (REPO_ROOT / source_dir).resolve()
        if not source_dir.exists():
            raise FileNotFoundError(f"source directory does not exist: {source_dir}")
        return source_dir

    for candidate in DEFAULT_SOURCE_CANDIDATES:
        if candidate.exists() and any(candidate.iterdir()):
            return candidate

    joined = ", ".join(str(path) for path in DEFAULT_SOURCE_CANDIDATES)
    raise FileNotFoundError(
        "could not find an experiment output directory. "
        f"Tried: {joined}"
    )


def collect_artifacts(source_dir: Path) -> list[Path]:
    found: list[Path] = []
    for pattern in ARTIFACT_PATTERNS:
        found.extend(sorted(source_dir.glob(pattern)))
    deduped = sorted({path.resolve() for path in found if path.is_file()})
    if not deduped:
        raise FileNotFoundError(f"no archiveable artifacts found in {source_dir}")
    return deduped


def relative_paths(paths: Iterable[Path], base_dir: Path) -> list[str]:
    rel_paths: list[str] = []
    for path in paths:
        try:
            rel_paths.append(path.relative_to(base_dir).as_posix())
        except ValueError:
            rel_paths.append(path.name)
    return rel_paths


def write_metadata(
    destination_dir: Path,
    *,
    run_id: str,
    source_dir: Path,
    artifacts: list[Path],
    git_snapshot: GitSnapshot,
    command: str,
    notes: str,
) -> None:
    metadata = {
        "run_id": run_id,
        "created_at": datetime.now().astimezone().isoformat(timespec="seconds"),
        "source_dir": str(source_dir),
        "artifacts": relative_paths(artifacts, source_dir),
        "git": {
            "branch": git_snapshot.branch,
            "head": git_snapshot.head,
            "dirty": git_snapshot.dirty,
        },
        "command": command,
        "notes": notes,
    }
    (destination_dir / "metadata.json").write_text(
        json.dumps(metadata, indent=2, ensure_ascii=False) + "\n",
        encoding="utf-8",
    )


def write_readme(
    destination_dir: Path,
    *,
    run_id: str,
    source_dir: Path,
    artifacts: list[Path],
    git_snapshot: GitSnapshot,
    command: str,
    notes: str,
) -> None:
    artifact_lines = "\n".join(
        f"- `artifacts/{path.name}`" for path in artifacts
    )
    notes_block = notes if notes else "(none)"
    command_block = command if command else "(not provided)"
    readme = f"""# Experiment {run_id}

## Source

- Source directory: `{source_dir}`
- Git branch: `{git_snapshot.branch}`
- Git HEAD: `{git_snapshot.head}`
- Working tree dirty when archived: `{str(git_snapshot.dirty).lower()}`

## Reproduction

```text
{command_block}
```

## Notes

{notes_block}

## Artifacts

{artifact_lines}
"""
    (destination_dir / "README.md").write_text(readme, encoding="utf-8")


def archive_run(args: argparse.Namespace) -> Path:
    source_dir = resolve_source_dir(args.source_dir)
    artifacts = collect_artifacts(source_dir)
    git_snapshot = safe_git_snapshot()

    output_root = Path(args.output_root).expanduser()
    if not output_root.is_absolute():
        output_root = (REPO_ROOT / output_root).resolve()
    output_root.mkdir(parents=True, exist_ok=True)

    timestamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    slug = slugify(args.name) if args.name else "run"
    run_id = f"{timestamp}-{slug}"

    destination_dir = output_root / run_id
    artifacts_dir = destination_dir / "artifacts"
    artifacts_dir.mkdir(parents=True, exist_ok=False)

    for artifact in artifacts:
        shutil.copy2(artifact, artifacts_dir / artifact.name)

    write_metadata(
        destination_dir,
        run_id=run_id,
        source_dir=source_dir,
        artifacts=artifacts,
        git_snapshot=git_snapshot,
        command=args.command,
        notes=args.notes,
    )
    write_readme(
        destination_dir,
        run_id=run_id,
        source_dir=source_dir,
        artifacts=artifacts,
        git_snapshot=git_snapshot,
        command=args.command,
        notes=args.notes,
    )

    return destination_dir


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description=(
            "Archive one experiment run into experiments/runs/ so it can be "
            "tracked with git and pushed to GitHub."
        )
    )
    parser.add_argument(
        "--name",
        default="run",
        help="Short label for this run, used in the destination directory name.",
    )
    parser.add_argument(
        "--source-dir",
        help=(
            "Directory containing experiment outputs. If omitted, the script "
            "auto-detects from ./hubres, ./supervisor/committee/hubres, "
            "or ./hubres_best."
        ),
    )
    parser.add_argument(
        "--output-root",
        default=str(DEFAULT_OUTPUT_ROOT),
        help="Directory that will receive archived runs.",
    )
    parser.add_argument(
        "--command",
        default="",
        help="The command you used to produce this experiment.",
    )
    parser.add_argument(
        "--notes",
        default="",
        help="Optional short notes about this run.",
    )
    return parser


def main() -> int:
    parser = build_parser()
    args = parser.parse_args()

    try:
        destination_dir = archive_run(args)
    except Exception as exc:
        print(f"archive failed: {exc}", file=sys.stderr)
        return 1

    print(f"Archived experiment to: {destination_dir}")
    print("Next steps:")
    print(f"  git add \"{destination_dir}\"")
    print(f"  git commit -m \"exp: {destination_dir.name}\"")
    print("  git push")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
