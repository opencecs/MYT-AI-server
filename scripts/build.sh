#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
MYT_DIR="$PROJECT_ROOT/MYT"

exec bash "$MYT_DIR/scripts/build.sh" "$@"
