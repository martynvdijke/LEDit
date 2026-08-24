#!/bin/sh
# Reads PluginRequest JSON on stdin, returns rows variant.
# If env MOCK_PLUGIN_FAIL=1, exit 1 with stderr.

if [ "$MOCK_PLUGIN_FAIL" = "1" ]; then
  echo "mock failure" >&2
  exit 1
fi

# Read stdin (ignore content, always succeed)
cat >/dev/null

# Return v:1 rows
printf '{"v":1,"rows":[{"label":"Temp","value":"22","text":"mock plugin ok"}]}'
