#!/usr/bin/env sh
set -eu

cd "$(dirname "$0")"
mkdir -p data
PORT=${PORT:-8080} go run .
