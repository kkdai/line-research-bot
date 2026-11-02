# LINE Research Planner Bot

A LINE chatbot demo built on Google Cloud, written in **Go** with
**[ADK for Go](https://github.com/google/adk-go)** (`google.golang.org/adk`).
It researches a user-given topic by cross-comparing multiple Google sources
with Gemini and publishes a Markdown report as a public HTML page. Subsequent
messages can deepen the same report — the agent's working memory (plan,
sources, report) is kept per user and fed back into ADK session state.

> **History:** the first version was Python + the Pre-GA Managed Agents API
> (sandbox filesystem). It was rewritten in Go on top of ADK — see issue #1.

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

## Architecture

```
┌────────────┐ Webhook/Push  ┌──────────────────────────────────────────┐
│  LINE App  │ ◀────────────▶│  Cloud Run: line-research-bot (Go)       │
└────────────┘                │  ├ POST /webhook            (HMAC)       │
                              │  ├ POST /tasks/run-research (OIDC)       │
                              │  └ GET  /readyz                          │
                              └──┬──────────────┬───────────────┬────────┘
                                 │ Cloud Tasks  │ ADK runner    │ storage
                                 ↓              ↓               ↓
                       ┌───────────────┐ ┌───────────────────┐ ┌──────────────────┐
                       │  Firestore    │ │ ADK llmagents      │ │ Cloud Storage    │
                       │  line_bot_*   │ │  plan              │ │ <id>/index.html  │
                       │  users        │ │  search_compare    │ │ <id>/snapshots/  │
                       │  reports      │ │  write_report      │ └──────────────────┘
                       │  workspaces   │ │ Gemini on Vertex AI│
                       └───────────────┘ │ + google_search    │
                                         │ + url_context      │
                                         └───────────────────┘
```

### How ADK is used

| Concern | ADK for Go feature |
|---|---|
| Three research stages | `llmagent.New` × 3 (`internal/research/agents.go`) |
| Web research | `geminitool.GoogleSearch{}` + `geminitool.New("url_context", …)` |
| Structured JSON output | `OutputSchema` (ADK injects `set_model_response`, so it works alongside built-in tools) |
| Hand-off between stages | `OutputKey` → session state; instructions use `{plan}`, `{sources}`, `{report_md?}` placeholders |
| Execution | `runner.Runner` + `session.InMemoryService()` |
| Model | `gemini.NewModel` on Vertex AI (`GEMINI_MODEL`, default `gemini-3.8-flash`) |

**Persistence:** each stage runs in a fresh ADK session seeded from the
user's Firestore workspace (`line_bot_workspaces/{userId}`), and results are
written back afterwards. Instances stay stateless, and a deepen request hours
later still sees the same sources and report.

**Model choice:** the agents need Google Search + url_context + OutputSchema in
one request. `gemini-3.8-flash` (default) and `gemini-3-flash-preview`
support that; `gemini-2.5-flash` rejects controlled generation together with
`url_context`.

---

## Repo layout

```
cmd/server/              main: wiring, HTTP server, graceful shutdown
internal/
├── config/              env vars
├── research/            ADK agents, schemas, prompts (go:embed)
│   └── prompts/         plan.md, search_compare.md, write_report.md
├── worker/              PLAN → SEARCH_COMPARE → WRITE_REPORT → publish; deepen; retries
├── server/              /webhook, /tasks/run-research, /readyz
├── state/               Firestore DAO (users, reports, workspaces, lock)
├── publisher/           Markdown → HTML (goldmark) + GCS upload/snapshot
├── linebot/             line-bot-sdk-go v8 wrapper + signature verify
├── tasks/               Cloud Tasks dispatcher (OIDC, 30-min deadline)
├── intent/              keyword intent classifier
└── flex/                progress text + Flex card
deploy/                  bucket / queue setup scripts, deploy.sh
```

---

## Quick start (local)

Prerequisites: Go 1.26+, `gcloud auth application-default login`, a GCP
project with Vertex AI enabled, and a LINE Messaging API channel.

```bash
cp .env.example .env   # fill in values
make test              # unit tests (no GCP needed)
GCP_PROJECT_ID=<project> make test-integration   # real Gemini via ADK, ~1 min
make run
```

---

## Deploy to Cloud Run

See [`deploy/README.md`](deploy/README.md). TL;DR:

```bash
PROJECT_ID=<project> REGION=asia-east1 \
SA_EMAIL=line-bot-sa@<project>.iam.gserviceaccount.com \
GCS_BUCKET=<bucket> ./deploy/deploy.sh
```

Then set the LINE webhook to `<service URL>/webhook` (Use webhook: ON,
auto-reply: OFF).

---

## Environment variables

| Var | Purpose |
|---|---|
| `GCP_PROJECT_ID` | The GCP project (also used for Vertex AI) |
| `GCP_LOCATION` | Vertex AI location for Gemini (default: `global`) |
| `AGENT_ID` | ADK app name / agent name prefix (default: `research-planner`) |
| `GEMINI_MODEL` | Optional, default `gemini-3.8-flash` |
| `GCS_BUCKET` | Where to upload report HTML |
| `CLOUD_TASKS_QUEUE` | Queue name for background work |
| `CLOUD_TASKS_LOCATION` | Queue region |
| `CLOUD_RUN_SERVICE_URL` | Service's own URL: Cloud Tasks target and expected OIDC audience |
| `SERVICE_ACCOUNT_EMAIL` | SA used for the Cloud Tasks OIDC token (and checked on receipt) |
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

# Inspect Firestore state: Console → Firestore → line_bot_users / line_bot_reports / line_bot_workspaces
# Clear a stale lock: set line_bot_users/<userId>.lock to null

# List queued tasks (if a request seems stuck)
gcloud tasks list --queue=research-jobs \
    --location=$REGION --project=$PROJECT_ID

# Keep one warm instance (default is 0; Go cold starts are fast)
gcloud run services update line-research-bot \
    --region=$REGION --min-instances=1
```

---

## Known limitations

- **Single rolling research per user**: by design — one active report per
  LINE user; a per-user lock prevents concurrent jobs.
- **No LINE Login / LIFF**: report URLs are public (with a hard-to-guess
  ID). Per-user access control is a follow-up project.
- **Webhook batch handling**: events in a webhook payload are processed in a
  best-effort loop; an error on one event is logged but doesn't stop the
  others.

---

## Acknowledgements

- [ADK for Go](https://github.com/google/adk-go)
- The original Python version was based on Google's
  [`intro_managed_agents_python.ipynb`](https://github.com/GoogleCloudPlatform/generative-ai/blob/main/agents/managed-agents/intro_managed_agents_python.ipynb)
- [Claude Code](https://docs.anthropic.com/en/docs/claude-code) for pair
  programming the entire spec → plan → 22-commit implementation → deploy →
  debug cycle

---

## Further reading

- [ADK documentation](https://google.github.io/adk-docs/)
- [ADK for Go on GitHub](https://github.com/google/adk-go)
- [Grounding with Google Search on Vertex AI](https://cloud.google.com/vertex-ai/generative-ai/docs/grounding/grounding-with-google-search)
