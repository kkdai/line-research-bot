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

## 5. Bucket + queue + agent

```bash
PROJECT_ID="${PROJECT_ID}" ./create-bucket.sh
PROJECT_ID="${PROJECT_ID}" LOCATION="${REGION}" ./create-tasks-queue.sh
PROJECT_ID="${PROJECT_ID}" ./create-agent.sh
```

## 6. Deploy

```bash
PROJECT_ID="${PROJECT_ID}" REGION="${REGION}" SA_EMAIL="${SA_EMAIL}" \
    ./deploy.sh
```

## 7. LINE console

In LINE Developers Console for your Messaging API channel:
- Webhook URL: `<Cloud Run URL>/webhook`
- Use webhook: ON
- Auto-reply messages: OFF (so our bot owns replies)
- Verify webhook → expect `200`.
