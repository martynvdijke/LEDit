#!/usr/bin/env python3
"""Set the ledit_device package version.

Invoked by semantic-release during the prepare step so the Python package
version tracks the LEDit release version (``device/pyproject.toml`` reads it
dynamically from ``ledit_device.__version__``).
"""

import re
import sys
from pathlib import Path

INIT_FILE = Path(__file__).resolve().parent.parent / "device" / "ledit_device" / "__init__.py"
VERSION_RE = re.compile(r'^__version__ = ".*"$', re.MULTILINE)


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: set_device_version.py <version>", file=sys.stderr)
        return 2

    version = sys.argv[1]
    text = INIT_FILE.read_text(encoding="utf-8")
    updated, count = VERSION_RE.subn(f'__version__ = "{version}"', text, count=1)
    if count != 1:
        print(f"error: no __version__ assignment found in {INIT_FILE}", file=sys.stderr)
        return 1

    INIT_FILE.write_text(updated, encoding="utf-8")
    print(f"set ledit_device.__version__ = {version}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
