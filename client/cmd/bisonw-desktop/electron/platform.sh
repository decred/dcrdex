#!/usr/bin/env bash
set -e

case "$(uname -s)" in
    Linux) platform="linux" ;;
    Darwin) platform="mac" ;;
    CYGWIN*|MSYS*|MINGW*) platform="win" ;;
    *) platform="linux" ;;
esac
echo "${platform}"
