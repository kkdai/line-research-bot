#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${PROJECT_ID:?set PROJECT_ID}"
REGION="${REGION:-asia-east1}"
SERVICE="${SERVICE:-line-research-bot}"
SA_EMAIL="${SA_EMAIL:?set SA_EMAIL}"  # e.g. line-bot-sa@PROJECT.iam.gserviceaccount.com
GCS_BUCKET="${GCS_BUCKET:-line-reports}"
AGENT_ID="${AGENT_ID:-research-planner}"
GEMINI_MODEL="${GEMINI_MODEL:-gemini-3.8-flash}"

# Required Secret Manager secrets must exist already:
#   - LINE_CHANNEL_SECRET
#   - LINE_CHANNEL_ACCESS_TOKEN

gcloud run deploy "${SERVICE}" \
    --project="${PROJECT_ID}" \
    --region="${REGION}" \
    --source=. \
    --service-account="${SA_EMAIL}" \
    --min-instances=0 \
    --max-instances=4 \
    --memory=2Gi \
    --cpu=1 \
    --concurrency=10 \
    --timeout=3600 \
    --allow-unauthenticated \
    --set-env-vars="GCP_PROJECT_ID=${PROJECT_ID},GCP_LOCATION=global,AGENT_ID=${AGENT_ID},GEMINI_MODEL=${GEMINI_MODEL},GCS_BUCKET=${GCS_BUCKET},CLOUD_TASKS_QUEUE=research-jobs,CLOUD_TASKS_LOCATION=${REGION},SERVICE_ACCOUNT_EMAIL=${SA_EMAIL},USE_FIRESTORE_EMULATOR=false" \
    --update-secrets="LINE_CHANNEL_SECRET=LINE_CHANNEL_SECRET:latest,LINE_CHANNEL_ACCESS_TOKEN=LINE_CHANNEL_ACCESS_TOKEN:latest"

# Cloud Tasks needs the service's own URL as the dispatch target (and the
# service uses it as the expected OIDC audience).
URL="$(gcloud run services describe "${SERVICE}" --project="${PROJECT_ID}" --region="${REGION}" --format='value(status.url)')"
gcloud run services update "${SERVICE}" --project="${PROJECT_ID}" --region="${REGION}" \
    --update-env-vars="CLOUD_RUN_SERVICE_URL=${URL}"

echo "✅ Deployed ${SERVICE}. LINE webhook path: <service URL>/webhook"
echo "   (run: gcloud run services describe ${SERVICE} --region=${REGION} --format='value(status.url)')"
