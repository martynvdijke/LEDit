#!/usr/bin/env python3
"""Set the Flutter Android app version in ``mobile/pubspec.yaml``.

Invoked by semantic-release during the prepare step so the APK attached to the
GitHub release carries the server release version. The build number is
incremented on every release so consecutive APKs stay installable over each
other.
"""

import re
import sys
from pathlib import Path

PUBSPEC = Path(__file__).resolve().parent.parent / "mobile" / "pubspec.yaml"
VERSION_RE = re.compile(r"^version:\s*(\S+)\s*$", re.MULTILINE)


def bump_build_number(current: str) -> str:
    """Increment the ``+build`` suffix, defaulting to ``+1`` when absent."""
    if "+" not in current:
        return "1"
    _, _, build = current.partition("+")
    try:
        return str(int(build) + 1)
    except ValueError:
        return "1"


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: set_mobile_version.py <version>", file=sys.stderr)
        return 2

    version = sys.argv[1]
    text = PUBSPEC.read_text(encoding="utf-8")
    match = VERSION_RE.search(text)
    if match is None:
        print(f"error: no version field found in {PUBSPEC}", file=sys.stderr)
        return 1

    build = bump_build_number(match.group(1))
    updated = VERSION_RE.sub(f"version: {version}+{build}", text, count=1)
    if updated == text:
        print(f"error: version {version}+{build} is already set in {PUBSPEC}", file=sys.stderr)
        return 1

    PUBSPEC.write_text(updated, encoding="utf-8")
    print(f"set mobile/pubspec.yaml version = {version}+{build}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
