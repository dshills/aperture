#!/usr/bin/env bash
# Test fixture: stand-in for the claude CLI. Echoes the APERTURE_*
# environment variables and the stdin body so tests can assert the
# adapter contract without pulling in the real claude binary.
#
# Invoked by the internal/agent and internal/cli test suites via a
# controlled PATH that points at testdata/bin/. Honors APERTURE_EXIT
# so a single stub can exercise both success and non-zero propagation
# paths.

set -euo pipefail

echo "APERTURE_MANIFEST_PATH=${APERTURE_MANIFEST_PATH:-}"
echo "APERTURE_MANIFEST_MARKDOWN_PATH=${APERTURE_MANIFEST_MARKDOWN_PATH:-}"
echo "APERTURE_TASK_PATH=${APERTURE_TASK_PATH:-}"
echo "APERTURE_PROMPT_PATH=${APERTURE_PROMPT_PATH:-}"
echo "APERTURE_REPO_ROOT=${APERTURE_REPO_ROOT:-}"
echo "APERTURE_MANIFEST_HASH=${APERTURE_MANIFEST_HASH:-}"
echo "APERTURE_VERSION=${APERTURE_VERSION:-}"
echo "args:" "$@"
echo "stdin_start"
cat
echo "stdin_end"

exit "${APERTURE_EXIT:-0}"
