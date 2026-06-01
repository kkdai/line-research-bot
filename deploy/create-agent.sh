#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${PROJECT_ID:?set PROJECT_ID}"
AGENT_ID="${AGENT_ID:-research-planner}"
LOCATION="global"
SI_DIR="$(dirname "$0")/../app/system_instructions"

PLAN_SI="$(cat "${SI_DIR}/plan.md")"
SEARCH_SI="$(cat "${SI_DIR}/search_compare.md")"
WRITE_SI="$(cat "${SI_DIR}/write_report.md")"

# Combined system instruction — the agent receives the stage tag in each input
# and reads the matching section.
COMBINED=$(cat <<EOF
You are 研究規劃師. The first line of every user input is a stage tag (PLAN,
SEARCH_COMPARE, WRITE_REPORT). Read the section below that matches the tag and
follow it exactly.

---
${PLAN_SI}

---
${SEARCH_SI}

---
${WRITE_SI}
EOF
)

ACCESS_TOKEN="$(gcloud auth print-access-token)"

cat > /tmp/agent.json <<JSON
{
  "id": "${AGENT_ID}",
  "base_agent": "antigravity-preview-05-2026",
  "tools": [
    {"code_execution": {}},
    {"filesystem": {}},
    {"google_search": {}},
    {"url_context": {}}
  ],
  "network_allowlist": ["*"],
  "system_instruction": $(jq -Rs . <<<"${COMBINED}")
}
JSON

curl -sS -X POST \
    -H "Authorization: Bearer ${ACCESS_TOKEN}" \
    -H "Content-Type: application/json" \
    -d @/tmp/agent.json \
    "https://aiplatform.googleapis.com/v1beta1/projects/${PROJECT_ID}/locations/${LOCATION}/agents?agentId=${AGENT_ID}" \
  | jq .

rm -f /tmp/agent.json
echo "✅ Agent ${AGENT_ID} created (long-running op; check console)."
