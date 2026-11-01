# Deploy

One-off GCP setup, then `deploy.sh` for every code change.

## Pre-reqs

```bash
export PROJECT_ID="your-gcp-project"
export REGION="asia-east1"
export SA_EMAIL="line-bot-sa@${PROJECT_ID}.iam.gserviceaccount.com"

gcloud config set project "${PROJECT_ID}"
gcloud auth login
gcloud auth application-default login
```

## 1. Enable APIs

```bash
gcloud services enable \
    aiplatform.googleapis.com \
    run.googleapis.com \
    cloudtasks.googleapis.com \
    firestore.googleapis.com \
    storage.googleapis.com \
    secretmanager.googleapis.com
```

## 2. Service account

```bash
gcloud iam service-accounts create line-bot-sa \
    --display-name="LINE Research Bot"

for role in aiplatform.user datastore.user cloudtasks.enqueuer \
            storage.objectAdmin secretmanager.secretAccessor \
            iam.serviceAccountTokenCreator run.invoker; do
  gcloud projects add-iam-policy-binding "${PROJECT_ID}" \
      --member="serviceAccount:${SA_EMAIL}" \
      --role="roles/${role}"
done
```

## 3. Firestore (native mode)

```bash
gcloud firestore databases create --location="${REGION}"
```

## 4. Secrets

```bash
echo -n "<paste LINE channel secret>" | \
    gcloud secrets create LINE_CHANNEL_SECRET --data-file=-
echo -n "<paste LINE channel access token>" | \
    gcloud secrets create LINE_CHANNEL_ACCESS_TOKEN --data-file=-
```

## 5. Bucket + queue

No Managed Agent needs to be created: the agents are defined in code with
ADK for Go and call Gemini on Vertex AI directly (`roles/aiplatform.user`).

```bash
PROJECT_ID="${PROJECT_ID}" BUCKET=<bucket> ./create-bucket.sh
PROJECT_ID="${PROJECT_ID}" LOCATION="${REGION}" ./create-tasks-queue.sh
```

## 6. Deploy

```bash
PROJECT_ID="${PROJECT_ID}" REGION="${REGION}" SA_EMAIL="${SA_EMAIL}" \
    GCS_BUCKET=<bucket> ./deploy.sh
```

`--source=.` builds the Go `Dockerfile` with Cloud Build. The script then
sets `CLOUD_RUN_SERVICE_URL` to the service's own URL; it is used as the
Cloud Tasks target and as the expected OIDC audience on
`/tasks/run-research`. The SA also needs `roles/iam.serviceAccountUser` on
itself so Cloud Tasks can mint OIDC tokens:

```bash
gcloud iam service-accounts add-iam-policy-binding "${SA_EMAIL}" \
    --member="serviceAccount:${SA_EMAIL}" --role="roles/iam.serviceAccountUser"
```

## 7. LINE console

In LINE Developers Console for your Messaging API channel:
- Webhook URL: `<Cloud Run URL>/webhook`
- Use webhook: ON
- Auto-reply messages: OFF (so our bot owns replies)
- Verify webhook → expect `200`.
