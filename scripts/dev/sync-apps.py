#!/usr/bin/env python3
"""Copies the Flutter apps from the Windows folders into the repo, so they can be committed.
Build output and machine-specific files are left out.

    python3 scripts/dev/sync-apps.py            both apps
    python3 scripts/dev/sync-apps.py rider      the Rider app only
    python3 scripts/dev/sync-apps.py driver     the Driver app only

    rider  C:\\Users\\7akoom\\OneDrive\\Desktop\\rider_app   ->  apps/rider-mobile
    driver C:\\Users\\7akoom\\OneDrive\\Desktop\\driver_app  ->  apps/driver-mobile

It replaces the contents of each destination with the app as it is now (a file deleted in
the app is deleted in the repo too), so run it again whenever an app changes and commit the
result. It does not commit anything itself.
"""
import shutil
import subprocess
import sys
from pathlib import Path

DESKTOP = Path("/mnt/c/Users/7akoom/OneDrive/Desktop")

APPS = {
    "rider": (DESKTOP / "rider_app", Path("apps/rider-mobile")),
    "driver": (DESKTOP / "driver_app", Path("apps/driver-mobile")),
}

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
    ".flutter-plugins",
    ".flutter-plugins-dependencies",
    "Generated.xcconfig",
    "flutter_export_environment.sh",
    "Thumbs.db",
    "desktop.ini",
}

BIG_FILE_BYTES = 5 * 1024 * 1024


def sync(name: str, repo: Path) -> bool:
    source, relative_destination = APPS[name]
    destination = repo / relative_destination

    if not (source / "pubspec.yaml").is_file():
        print("SKIPPED %s: %s is not a Flutter app (no pubspec.yaml)." % (name, source), file=sys.stderr)
        return False

    destination.mkdir(parents=True, exist_ok=True)

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

        if not path.is_file() or path.name in SKIP_FILES or path.suffix == ".iml":
            continue

        target = destination / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(path, target)

        size = path.stat().st_size
        copied += 1
        total += size

        if size > BIG_FILE_BYTES:
            big.append((relative, size))

    print("%s: copied %d files (%.1f MB) to %s" % (name, copied, total / 1024 / 1024, relative_destination))

    for relative, size in big:
        print("  note: %s is %.1f MB" % (relative, size / 1024 / 1024))

    return True


def main() -> int:
    repo = Path(__file__).resolve().parents[2]

    if not (repo / ".git").exists():
        print("ABORT: %s is not the repo root." % repo, file=sys.stderr)
        return 1

    wanted = sys.argv[1:] or list(APPS)

    for name in wanted:
        if name not in APPS:
            print("ABORT: unknown app %r (use rider or driver)." % name, file=sys.stderr)
            return 1

    done = [sync(name, repo) for name in wanted]

    print()
    print("git status of the repo:")
    subprocess.run(["git", "-C", str(repo), "status", "-sb"], check=False)

    return 0 if all(done) else 1


if __name__ == "__main__":
    sys.exit(main())
