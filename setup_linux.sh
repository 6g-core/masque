#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/client"
# Install Go >= 1.25 separately; this compiles the native executable, not a Docker image.
"${GO_BIN:-${HOME}/.local/lib/beizhan-toolchains/go/bin/go}" build -o ../client_rayneo/client ./cmd/client
