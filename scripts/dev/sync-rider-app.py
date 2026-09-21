#!/usr/bin/env python3
"""Copies the Rider app from the Windows folder into the repo (apps/rider-mobile), so it can be
committed. Build output and machine-specific files are left out.

Run from anywhere:
    python3 scripts/dev/sync-rider-app.py [path to the app]
The default path is C:\\Users\\7akoom\\OneDrive\\Desktop\\rider_app.

It replaces the contents of apps/rider-mobile with the app as it is now, so run it again
whenever the app changes and commit the result. It does not commit anything itself.
"""
import shutil
import subprocess
import sys
from pathlib import Path

DEFAULT_SOURCE = Path("/mnt/c/Users/7akoom/OneDrive/Desktop/rider_app")

# Folders that are rebuilt on the next `flutter run` or belong to one machine.
SKIP_DIRS = {
    "build",
    ".dart_tool",
    ".idea",
    ".gradle",
    ".vscode",
    ".symlinks",
    "Pods",
    "ephemeral",
}

SKIP_FILES = {
    "local.properties",
    "rider_app.iml",
    ".flutter-plugins",
    ".flutter-plugins-dependencies",
    "Generated.xcconfig",
    "flutter_export_environment.sh",
    "Thumbs.db",
    "desktop.ini",
}

BIG_FILE_BYTES = 5 * 1024 * 1024


def main() -> int:
    source = Path(sys.argv[1]) if len(sys.argv) > 1 else DEFAULT_SOURCE
    repo = Path(__file__).resolve().parents[2]
    destination = repo / "apps" / "rider-mobile"

    if not (source / "pubspec.yaml").is_file():
        print("ABORT: %s is not a Flutter app (no pubspec.yaml)." % source, file=sys.stderr)
        return 1

    if not (repo / ".git").exists():
        print("ABORT: %s is not the repo root." % repo, file=sys.stderr)
        return 1

    destination.mkdir(parents=True, exist_ok=True)

    # Start from an empty folder, so a file deleted in the app is deleted in the repo too.
    for child in destination.iterdir():
        if child.is_dir():
            shutil.rmtree(child)
        else:
            child.unlink()

    copied = 0
    total = 0
    big = []

    for path in sorted(source.rglob("*")):
        relative = path.relative_to(source)

        if any(part in SKIP_DIRS for part in relative.parts):
            continue

        if not path.is_file() or path.name in SKIP_FILES:
            continue

        target = destination / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(path, target)

        size = path.stat().st_size
        copied += 1
        total += size

        if size > BIG_FILE_BYTES:
            big.append((relative, size))

    print("copied %d files (%.1f MB) to %s" % (copied, total / 1024 / 1024, destination))

    for relative, size in big:
        print("  note: %s is %.1f MB" % (relative, size / 1024 / 1024))

    print()
    print("git status of the repo:")
    subprocess.run(["git", "-C", str(repo), "status", "-sb"], check=False)

    return 0


if __name__ == "__main__":
    sys.exit(main())
