#!/usr/bin/env bash
set -euo pipefail
echo "Phase A checklist"
echo "1) Confirm endpoint cache on us1"
echo "2) Capture one agent_chat_generation request (headers+body+sse)"
echo "3) Verify whether QODER_PERSONAL_ACCESS_TOKEN works alone"
echo "4) Drop redacted samples into testdata/"
echo "5) Only then implement executor.ChatNonStream for real"
