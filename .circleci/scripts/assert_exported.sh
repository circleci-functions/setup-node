#!/bin/bash
# Asserts an env var was exported to $BASH_ENV by a previous step (i.e. is
# set once $BASH_ENV is sourced at the start of a new step). Usage:
#   .circleci/scripts/assert_exported.sh VAR_NAME
set -euo pipefail

var="$1"
if [ -z "${!var:-}" ]; then
  echo "FAIL: $var not set" >&2
  exit 1
fi
echo "OK: $var=${!var}"
