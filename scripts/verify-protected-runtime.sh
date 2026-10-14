#!/usr/bin/env bash
set -euo pipefail

# Reuse the reviewed baselines; never regenerate or accept digests here.
# Both release preparation and backend CI use this read-only entry.
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
export PYTHONDONTWRITEBYTECODE=1
bash scripts/verify-phase2c-reference-runtime.sh
bash scripts/verify-phase2c-litellm-runtime.sh
