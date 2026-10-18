#!/usr/bin/env bash
# Upload a plugin bundle to chat.unstaticlabs.com and load it.
#
#   scripts/usl-install.sh [bundle.tar.gz]
#
# Without an argument, it takes the newest dist/*.tar.gz. Build it first
# with `make usl-dist`.
#
# The server refuses plugin uploads by default. This script turns uploads on
# for the install, and a trap turns them off again, also when a step fails.
#
# Override with HOST=..., CONTAINER=... in the environment.
set -euo pipefail

HOST="${HOST:-odoo}"
CONTAINER="${CONTAINER:-mattermost-mattermost-1}"
LIMIT_BYTES=94371840 # MM_FILESETTINGS_MAXFILESIZE in the gitops compose

cd "$(dirname "$0")/.."

# The plugin id, from the manifest of this repository.
PLUGIN_ID=$(python3 -c 'import json; print(json.load(open("plugin.json"))["id"])')

BUNDLE="${1:-$(ls -t dist/*.tar.gz 2>/dev/null | head -1)}"
[ -n "$BUNDLE" ] && [ -f "$BUNDLE" ] || {
  echo "No bundle found. Run 'make usl-dist' first, or give a path." >&2
  exit 1
}

SIZE=$(wc -c <"$BUNDLE" | tr -d ' ')
if [ "$SIZE" -gt "$LIMIT_BYTES" ]; then
  echo "$BUNDLE is $SIZE bytes, above the server limit of $LIMIT_BYTES." >&2
  echo "Build it with 'make usl-dist', not 'make dist'." >&2
  exit 1
fi

VERSION=$(tar xzf "$BUNDLE" -O "$PLUGIN_ID/plugin.json" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("version","?"))')
echo "Installing $BUNDLE (version $VERSION, $SIZE bytes) on $HOST/$CONTAINER"

REMOTE="/tmp/$PLUGIN_ID-upload.tar.gz"

mm() { ssh "$HOST" docker exec "$CONTAINER" mmctl --local "$@"; }

cleanup() {
  mm config set PluginSettings.EnableUploads false >/dev/null || \
    echo "WARNING: could not turn plugin uploads off. Do it by hand." >&2
  # The Mattermost 12 image has no rm, so the copy in the container stays
  # in /tmp. The next install overwrites it.
  ssh "$HOST" "rm -f $REMOTE" || true
}

scp -q "$BUNDLE" "$HOST:$REMOTE"
ssh "$HOST" docker cp "$REMOTE" "$CONTAINER:$REMOTE"

trap cleanup EXIT
mm config set PluginSettings.EnableUploads true >/dev/null
mm plugin add --force "$REMOTE"
mm plugin enable "$PLUGIN_ID"

echo
mm plugin list | grep -i -A0 "$PLUGIN_ID" || true
