# LINE Research Planner Bot

A LINE chatbot demo built on Google Cloud that uses the Pre-GA
[Gemini Enterprise Agent Platform](https://docs.cloud.google.com/gemini-enterprise-agent-platform/build/managed-agents)
(Managed Agents API) to research a user-given topic by cross-comparing multiple
Google sources and publishing a Markdown report as a public HTML page.
Subsequent messages can deepen the same report — the Agent reuses its
sandboxed filesystem across turns.

> **Status:** Demo project. The underlying Agents API is Pre-GA — not for
> production data, no SLA, may change without notice.

---

## What it does

```
You (LINE):  研究 SOTA 開源向量資料庫的選型
Bot:         📋 已收到題目，開始規劃…
Bot:         🔍 計畫完成：將比對 7 類來源
Bot:         📊 已比對 7 份來源，發現 3 處重要分歧
Bot:         [Flex card]  Title · 500-word summary · 3 citations
             [ 閱讀完整報告 →  https://storage.googleapis.com/... ]

You (LINE):  第 2 章再深一點，加日文來源
Bot:         [Flex card v2]  same URL, content updated, old version snapshotted
```

Under the hood, each user has their own persistent Agent sandbox
(`environment_id`). The same `/workspace/sources.json` and `/workspace/report.md`
survive across messages — that's how progressive deepening works without
having to re-search.

---

## Architecture

```
┌────────────┐ Webhook/Push  ┌─────────────────────────────────────┐
│  LINE App  │ ◀────────────▶│  Cloud Run: line-research-bot       │
└────────────┘                │  (Python 3.11 + FastAPI)            │
                              │  ├ /webhook     (LINE entry)        │
                              │  ├ /tasks/run-research  (Cloud Tasks)│
                              │  └ /readyz                          │
                              └──┬───────────────┬──────────────────┘
                                 │ Firestore     │ Agents API + SDK
                                 ↓               ↓
                       ┌───────────────┐ ┌──────────────────────┐
                       │  Firestore    │ │  Managed Agent:      │
                       │  line_bot_*   │ │  research-planner    │
                       │  collections  │ │  (per-user sandbox)  │
                       └───────────────┘ └──────────┬───────────┘
                                                    │
                                                    │ output_text (markdown)
                                                    ↓
                       ┌──────────────────────────────────────────┐
                       │  Cloud Run worker                        │
                       │  ├ markdown → HTML (Cloud Run side)      │
                       │  └ upload to GCS via SA + storage SDK    │
                       └──────────┬───────────────────────────────┘
                                  ↓
                       ┌──────────────────────────┐
                       │  Cloud Storage           │
                       │  gs://<bucket>/<id>/     │
                       │    index.html (public)   │
                       │    snapshots/v*.html     │
                       └──────────┬───────────────┘
                                  │ HTTPS
                       ┌──────────┴────────┐
                       │  Viewer's browser │
                       └───────────────────┘
```

### Why render + upload in Cloud Run, not in the Agent sandbox?

The Pre-GA Managed Agents sandbox does **not** have GCP credentials, and the
on-host `gsutil` is **mocked** (returns "simulated copy" without writing
anywhere). So the Agent generates Markdown, returns it via `report_md` in
its JSON output, and Cloud Run (running as a real service account with
`storage.objectAdmin`) does the HTML rendering and the actual upload.

This is documented as **踩坑二** in the accompanying blog post.

---

## Repo layout

```
app/
├── main.py                  FastAPI app + DI wiring
├── webhook.py               POST /webhook handler
├── worker.py                Three-stage orchestrator (PLAN → SEARCH → WRITE)
├── intent.py                Keyword intent classifier (pure)
├── flex.py                  Progress text + Flex Message builders
├── state.py                 Firestore DAO (line_bot_users, line_bot_reports)
├── line_client.py           line-bot-sdk v3 wrapper + HMAC verify
├── agents_client.py         google-genai SDK wrapper (enterprise + background polling)
├── tasks_client.py          Cloud Tasks dispatcher
├── publisher.py             Markdown → HTML + GCS upload (Cloud Run side)
├── config.py                pydantic-settings (env vars)
└── system_instructions/
    ├── plan.md              Stage 1: design search queries
    ├── search_compare.md    Stage 2: search + cross-compare sources
    └── write_report.md      Stage 3: write Markdown report

tests/
├── unit/                    51 tests, run against Firestore Emulator
└── integration/             Real Agents API smoke tests (manual, RUN_FULL_WRITE_TEST=1)

deploy/
├── create-agent.sh          One-off: create the Managed Agent
├── create-bucket.sh         One-off: GCS bucket with public read
├── create-tasks-queue.sh    One-off: Cloud Tasks queue
├── deploy.sh                Idempotent: gcloud run deploy --source=.
└── README.md                Step-by-step deployment walkthrough

docs/
├── e2e-checklist.md         Manual end-to-end test list
└── superpowers/             Original spec + plan (where this all came from)
```

---

## Quick start (local)

### Prerequisites

- Python 3.11+
- `gcloud` CLI (`gcloud auth login` + `gcloud auth application-default login`)
- A GCP project with the Pre-GA Antigravity / Managed Agents preview enabled
- A LINE Messaging API channel (channel secret + access token)
- Java (for the Firestore Emulator when running unit tests)
- `firebase-tools` (`npm install -g firebase-tools`) or `gcloud emulators`

### Install

```bash
git clone https://github.com/kkdai/line-research-bot.git
cd line-research-bot
pip install -e ".[dev]"
cp .env.example .env  # fill in values
```

### Run tests

```bash
firebase emulators:start --only firestore &
make test
```

Expected: `51 passed`.

### Smoke test the Agents API (requires real GCP)

```bash
GCP_PROJECT_ID=<your-project> \
  pytest tests/integration/test_filesystem_persistence.py -v -s
```

This is the load-bearing assumption test: it verifies that
`environment_id` actually persists files across two interactions.

---

## Deploy to Cloud Run

See [`deploy/README.md`](deploy/README.md) for the full walkthrough.

TL;DR (assumes `gcloud auth login` is done and Pre-GA allowlist applied):

```bash
export PROJECT_ID=your-gcp-project
export REGION=asia-east1
export SA_EMAIL=line-bot-sa@${PROJECT_ID}.iam.gserviceaccount.com

# 1. Enable APIs
gcloud services enable aiplatform.googleapis.com run.googleapis.com \
    cloudtasks.googleapis.com firestore.googleapis.com \
    storage.googleapis.com secretmanager.googleapis.com

# 2. Service account + IAM
gcloud iam service-accounts create line-bot-sa
for role in aiplatform.user datastore.user cloudtasks.enqueuer \
            storage.objectAdmin secretmanager.secretAccessor \
            iam.serviceAccountTokenCreator run.invoker logging.logWriter; do
  gcloud projects add-iam-policy-binding "${PROJECT_ID}" \
      --member="serviceAccount:${SA_EMAIL}" \
      --role="roles/${role}" --condition=None
done
# SA must also be able to actAs itself (for Cloud Tasks OIDC)
gcloud iam service-accounts add-iam-policy-binding "${SA_EMAIL}" \
    --member="serviceAccount:${SA_EMAIL}" \
    --role="roles/iam.serviceAccountUser"

# 3. Firestore (native mode, asia-east1)
gcloud firestore databases create --location=asia-east1

# 4. Secrets (use stdin so values don't end up in shell history)
printf '%s' "<LINE channel secret>" | \
    gcloud secrets create LINE_CHANNEL_SECRET --data-file=-
printf '%s' "<LINE channel access token>" | \
    gcloud secrets create LINE_CHANNEL_ACCESS_TOKEN --data-file=-

# 5. GCS bucket, Cloud Tasks queue, Managed Agent
PROJECT_ID="${PROJECT_ID}" BUCKET=<your-bucket-name> ./deploy/create-bucket.sh
PROJECT_ID="${PROJECT_ID}" LOCATION="${REGION}" ./deploy/create-tasks-queue.sh
PROJECT_ID="${PROJECT_ID}" ./deploy/create-agent.sh

# 6. Deploy
PROJECT_ID="${PROJECT_ID}" REGION="${REGION}" SA_EMAIL="${SA_EMAIL}" \
    ./deploy/deploy.sh
```

Then in the LINE Developers Console:

- **Webhook URL**: `https://<your-cloud-run-url>/webhook`
- **Use webhook**: ON
- **Auto-reply messages**: OFF (so the bot owns replies)

---

## Environment variables

| Var | Purpose |
|---|---|
| `GCP_PROJECT_ID` | The GCP project |
| `GCP_LOCATION` | Agents API location (default: `global`) |
| `AGENT_ID` | Managed Agent id (default: `research-planner`) |
| `GCS_BUCKET` | Where to upload report HTML |
| `CLOUD_TASKS_QUEUE` | Queue name for background work |
| `CLOUD_TASKS_LOCATION` | Queue region |
| `CLOUD_RUN_SERVICE_URL` | Public URL used as Cloud Tasks dispatch target |
| `LINE_CHANNEL_SECRET` | LINE HMAC signing secret (Secret Manager) |
| `LINE_CHANNEL_ACCESS_TOKEN` | LINE Messaging API token (Secret Manager) |
| `USE_FIRESTORE_EMULATOR` | `true` to point Firestore at a local emulator |
| `FIRESTORE_EMULATOR_HOST` | Emulator host:port (default `localhost:8081`) |

---

## Operations cheat-sheet

```bash
# Tail logs
gcloud run services logs read line-research-bot \
    --project=$PROJECT_ID --region=$REGION --limit=100

# Inspect Firestore state for a user
python3 -c "
from google.cloud import firestore
fs = firestore.Client(project='$PROJECT_ID')
for d in fs.collection('line_bot_users').stream():
    print(d.id, d.to_dict())
"

# List queued tasks (if a request seems stuck)
gcloud tasks list --queue=research-jobs \
    --location=$REGION --project=$PROJECT_ID

# Clear stale locks (for development only)
python3 -c "
from google.cloud import firestore
fs = firestore.Client(project='$PROJECT_ID')
for d in fs.collection('line_bot_users').stream():
    fs.collection('line_bot_users').document(d.id).update({'lock': None})
"

# Save cost when idle (drops cold-start protection)
gcloud run services update line-research-bot \
    --region=$REGION --min-instances=0
```

---

## Known limitations

- **Pre-GA**: the Agents API is preview software, no SLA, behaviour may change.
  Don't put production data into the sandbox.
- **Sandbox `gsutil` is mocked**: any "the agent uploads to my GCS bucket"
  pattern will silently fail. Have the agent return the content and let your
  service handle the upload. (See blog post for diagnostic interaction.)
- **Cold-start on first message**: `agents.create_environment` happens lazily
  on the first interaction; expect a slow first response.
- **Single rolling research per user**: by design — the demo doesn't support
  multiple concurrent research projects per LINE user.
- **No LINE Login / LIFF**: report URLs are public (with a hard-to-guess
  UUID). If you need per-user access control, that's a follow-up project.
- **Webhook batch handling**: events in a webhook payload are processed in a
  best-effort loop; an exception on one event is logged but doesn't stop the
  others.

---

## Acknowledgements

- Google Cloud team for the Pre-GA Managed Agents preview
- Google's official notebook
  [`intro_managed_agents_python.ipynb`](https://github.com/GoogleCloudPlatform/generative-ai/blob/main/agents/managed-agents/intro_managed_agents_python.ipynb)
  — this was the key reference for getting the SDK call shape right
- [Claude Code](https://docs.anthropic.com/en/docs/claude-code) for pair
  programming the entire spec → plan → 22-commit implementation → deploy →
  debug cycle

---

## Further reading

- [Managed Agents on Agent Platform](https://docs.cloud.google.com/gemini-enterprise-agent-platform/build/managed-agents)
- [Interact with managed agents](https://docs.cloud.google.com/gemini-enterprise-agent-platform/build/managed-agents/interact-with-agents)
- [Create and manage agents](https://docs.cloud.google.com/gemini-enterprise-agent-platform/build/managed-agents/create-manage)
- [Sandbox environment](https://docs.cloud.google.com/gemini-enterprise-agent-platform/build/managed-agents/sandbox-environment)
