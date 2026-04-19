#!/bin/sh
# Loadmode smoke check: verifies that the APERTURE_* env vars are
# present and that the manifest path exists, then exits 0. Used by
# testdata/eval/loadmode-smoke/*.eval.yaml.
set -e
[ -f "$APERTURE_MANIFEST_PATH" ] || { echo "missing APERTURE_MANIFEST_PATH" >&2; exit 1; }
[ -n "$APERTURE_MANIFEST_HASH" ] || { echo "missing APERTURE_MANIFEST_HASH" >&2; exit 1; }
[ -d "$APERTURE_REPO_ROOT" ] || { echo "missing APERTURE_REPO_ROOT" >&2; exit 1; }
exit 0
