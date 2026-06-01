#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${PROJECT_ID:?set PROJECT_ID}"
REGION="${REGION:-asia-east1}"
SERVICE="${SERVICE:-line-research-bot}"
SA_EMAIL="${SA_EMAIL:?set SA_EMAIL}"  # e.g. line-bot-sa@PROJECT.iam.gserviceaccount.com

# Required Secret Manager secrets must exist already:
#   - LINE_CHANNEL_SECRET
#   - LINE_CHANNEL_ACCESS_TOKEN

gcloud run deploy "${SERVICE}" \
    --project="${PROJECT_ID}" \
    --region="${REGION}" \
    --source=. \
    --service-account="${SA_EMAIL}" \
    --min-instances=1 \
    --max-instances=4 \
    --memory=1Gi \
    --cpu=1 \
    --concurrency=10 \
    --timeout=600 \
    --allow-unauthenticated \
    --set-env-vars="GCP_PROJECT_ID=${PROJECT_ID},GCP_LOCATION=global,AGENT_ID=research-planner,GCS_BUCKET=line-reports,CLOUD_TASKS_QUEUE=research-jobs,CLOUD_TASKS_LOCATION=${REGION},SERVICE_ACCOUNT_EMAIL=${SA_EMAIL}" \
    --update-secrets="LINE_CHANNEL_SECRET=LINE_CHANNEL_SECRET:latest,LINE_CHANNEL_ACCESS_TOKEN=LINE_CHANNEL_ACCESS_TOKEN:latest"

URL="$(gcloud run services describe "${SERVICE}" --region="${REGION}" --format='value(status.url)')"
echo "✅ Deployed: ${URL}"
echo "👉 Re-deploy with CLOUD_RUN_SERVICE_URL=${URL} so Cloud Tasks knows the target."
gcloud run services update "${SERVICE}" --region="${REGION}" \
    --update-env-vars="CLOUD_RUN_SERVICE_URL=${URL}"

echo "👉 LINE webhook URL: ${URL}/webhook"
