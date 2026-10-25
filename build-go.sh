#!/bin/zsh
# Build a signed, isolated preview only. Does not install, register or launch it.
set -eu
ATLAS_SOURCE="${0:A:h}"
ATLAS_PREVIEW_OUTPUT="$ATLAS_SOURCE/service/.work/build"
ATLAS_GO_BINARY=""
while (( $# )); do
  case "$1" in
    --go-service) [[ $# -ge 2 ]] || exit 2; ATLAS_GO_BINARY="$2"; shift 2 ;;
    --output-directory) [[ $# -ge 2 ]] || exit 2; ATLAS_PREVIEW_OUTPUT="$2"; shift 2 ;;
    *) print -u2 "Usage: zsh build-go.sh --go-service /absolute/atlas-service [--output-directory /absolute/directory]"; exit 2 ;;
  esac
done
[[ "$ATLAS_GO_BINARY" = /* && -f "$ATLAS_GO_BINARY" ]] || { print -u2 "Supply an absolute built Go executable"; exit 1; }
ATLAS_BUILD_DIR="$ATLAS_PREVIEW_OUTPUT" ATLAS_BACKEND=go ATLAS_REGISTER_APP=0 ATLAS_GO_SERVICE="$ATLAS_GO_BINARY" /bin/zsh "$ATLAS_SOURCE/build.sh"
