# LINE 研究規劃師 Bot — 設計規格

- **日期**：2026-06-01
- **作者**：evanslin
- **定位**：Demo（不是生產系統）
- **目標**：用 LINE Bot 展示 Google Gemini Enterprise Agent Platform（Managed Agents API）獨有的能力：**沙箱中的 code_execution、持久化 filesystem、google_search + url_context、多輪 session**
- **狀態**：Pre-GA、不接觸敏感資料

---

## 1. 一句話描述

使用者在 LINE 丟出研究主題，Bot 在 GCP 沙箱中規劃搜尋、多方比對 Google 來源、寫成 Markdown 報告並以公開 HTML 連結回傳；後續使用者可在同一份報告上「漸進深化」（補來源、改章節），Bot 在同一個沙箱內更新同一份檔案。

---

## 2. 範圍

### 2.1 在範圍內

- 一位 LINE 用戶 = 一個 Agents API 沙箱（environment_id），鎖定為「同時只能進行一份研究」。
- 三段式 Orchestration：規劃 → 搜尋＋比對 → 寫作。
- 報告以 Markdown 撰寫、沙箱內轉成 HTML、上傳到 GCS Bucket、公開 URL 回傳。
- 漸進深化：在同沙箱內保留 `sources.json` + `report.md`，可重寫指定章節、補充來源。
- 多版本：每次深化在 GCS 保留前一版 snapshot。
- 全 GCP 部署：Cloud Run + Firestore + Cloud Storage + Cloud Tasks + Secret Manager + Agents API。

### 2.2 不在範圍內

- LINE Login / LIFF 鑑權。
- 多份並行研究、研究專案命名與切換。
- 非文字訊息輸入（圖、檔案、語音）。
- 報告內容自動正確性評估。
- 多用戶高並發負載。
- 真實生產資料與敏感資訊。

---

## 3. 使用情境

### 3.1 Flow A — 新研究

1. 使用者在 LINE 傳「研究 SOTA 開源向量資料庫的選型」。
2. Bot 立即回覆「📋 已收到題目，開始規劃…」（用 reply token，避免 60 秒過期）。
3. Cloud Tasks 派背景 worker 跑三段式：
   - **第 1 段 PLAN**：Agent 產出搜尋計畫（4-8 條 query、來源類別、語言），寫入 `/workspace/plan.json`。Bot Push「🔍 計畫完成：將比對 7 類來源…」。
   - **第 2 段 SEARCH_COMPARE**：Agent 用 `google_search` + `url_context` 跑計畫、把每個來源摘要 + 一致/分歧點寫入 `/workspace/sources.json`。Bot Push「📊 已比對 7 份來源，發現 3 處重要分歧…」。
   - **第 3 段 WRITE_REPORT**：Agent 根據 `sources.json` 寫 `/workspace/report.md`，用 `code_execution` 跑 `pip install markdown` + 自訂 CSS 轉成 `/workspace/report.html`，再 `gsutil cp` 到 `gs://line-reports/{report_id}/index.html`。Agent 回傳 `{report_id, summary_500, top_citations}`。
4. Bot 寫入 Firestore，Push 給使用者一張 Flex Message（標題、500 字摘要、3 條引用、「閱讀完整報告」按鈕連到 GCS URL）。

### 3.2 Flow B — 漸進深化

1. 使用者傳「第 3 章再深一點，加日文來源」。
2. Bot 用簡單關鍵字 intent 分類為 `deepen`，從 Firestore 取出 `current_report_id` 與 `last_interaction_id`。
3. Cloud Tasks 派 worker 跑：
   - 串接 `previous_interaction_id`、共用原 `environment_id`。
   - 指示 Agent：補搜日文來源、更新 `sources.json` 中 `chapter3` 段、重寫 `report.md` 第 3 章、重渲染 HTML、覆寫同一個 GCS key、把舊版 snapshot 搬到 `snapshots/v{N-1}.html`。
4. Bot 把同一個短連結再 Push 一次（內容已更新）、Firestore `version += 1`、`history[]` 累加上一版的 snapshot path。

### 3.3 Flow C — 看報告

任何人拿到 `https://storage.googleapis.com/line-reports/{report_id}/index.html` 開瀏覽器即可看到渲染好的 HTML。無需登入。頁面頂部顯示「最後更新時間」與「v2」徽章。

---

## 4. 架構

### 4.1 元件圖

```
┌────────────┐ LINE Webhook/Push ┌─────────────────────────────────────┐
│  LINE App  │ ◀────────────────▶│  Cloud Run: line-research-bot        │
└────────────┘                    │  (Python 3.11 + FastAPI)            │
                                  │  ├ /webhook                          │
                                  │  ├ /tasks/run-research (Cloud Tasks) │
                                  │  └ /healthz                          │
                                  └──┬───────────────────────┬───────────┘
                                     │ Firestore             │ Agents API
                                     ↓                       ↓
                              ┌───────────────┐     ┌──────────────────────┐
                              │  Firestore    │     │  Managed Agent:       │
                              │  users/       │     │  research-planner     │
                              │  reports/     │     │  base: antigravity-   │
                              └───────────────┘     │        preview-05-2026│
                                                    │  tools:               │
                                                    │   - google_search     │
                                                    │   - url_context       │
                                                    │   - code_execution    │
                                                    │   - filesystem        │
                                                    └────────┬──────────────┘
                                                             │ gsutil cp
                                                             ↓
                                                    ┌──────────────────────┐
                                                    │  Cloud Storage       │
                                                    │  gs://line-reports/  │
                                                    │   {report_id}/       │
                                                    │     index.html       │
                                                    │     snapshots/v*.html│
                                                    └──────────┬───────────┘
                                                               │ HTTPS
                                                    ┌──────────┴───────────┐
                                                    │  觀看者瀏覽器          │
                                                    └──────────────────────┘
```

### 4.2 GCP 服務責任

| 服務 | 用途 |
|---|---|
| Cloud Run | LINE webhook 入口、Cloud Tasks 背景 worker、Flex Message 建構 |
| Firestore (Native) | LINE userId ↔ environment_id 對應、目前報告指標、報告 metadata、單一任務鎖 |
| Agents API | 一個共用 Agent（`research-planner`）；每位用戶各自一個 environment_id 沙箱 |
| Cloud Storage | 公開 HTML 託管、版本 snapshot 保留 |
| Cloud Tasks | webhook → 背景 worker 的非同步派送，避免 LINE reply token 60 秒超時 |
| Secret Manager | LINE channel secret / access token |
| Cloud Run Service Account | 直接掛 `roles/aiplatform.user`，免 SA key 呼叫 Agents API |

### 4.3 為什麼三段式而不是 single-shot

- 漸進深化只需重跑第 3 段，回應更快。
- 觀眾在 demo 時能看到「Agent 在做什麼階段」（每段一則進度 Push）。
- 單段失敗可獨立重試、不污染狀態。

---

## 5. 元件規格

### 5.1 Cloud Run 服務目錄結構

```
app/
  main.py                  # FastAPI app 入口
  webhook.py               # POST /webhook：驗簽、解析、立即 reply、丟 Cloud Tasks
  worker.py                # POST /tasks/run-research：三段式 orchestration、Push 進度
  intent.py                # 純函式：分類 new / deepen / reset / recall / chitchat
  agents_client.py         # Agents API 薄包裝：plan / search_compare / write_report
  line_client.py           # LINE Messaging API 薄包裝 + Flex Message builder
  state.py                 # Firestore DAO：users / reports / lock 操作
  config.py                # 環境變數、Secret Manager 讀取
  system_instructions/
    plan.md
    search_compare.md
    write_report.md
tests/
  unit/
  integration/
Makefile                   # make test / make test-integration / make deploy
pyproject.toml
Dockerfile
```

### 5.2 Agents API 設定

```yaml
agent_id: research-planner
base_agent: antigravity-preview-05-2026
tools:
  - code_execution
  - filesystem
  - google_search
  - url_context
network_allowlist:
  - storage.googleapis.com
  - "*"   # 研究內容需開放搜尋目的網域
system_instruction: |
  你是研究規劃師。你會收到三種任務之一（由訊息開頭標籤指定）：
  - PLAN：根據主題產出搜尋計畫，寫入 /workspace/plan.json，回傳該 JSON。
  - SEARCH_COMPARE：讀 plan.json，用 google_search + url_context 跑每條 query，
    把每個來源的摘要 + 跨來源一致/分歧點寫入 /workspace/sources.json。
  - WRITE_REPORT：讀 sources.json 寫 /workspace/report.md，
    code_execution 安裝 markdown 套件、轉成 report.html（含內嵌 CSS），
    gsutil cp 到 gs://line-reports/{report_id}/index.html，
    若是深化模式，先把舊 index.html 搬到 snapshots/v{N-1}.html。
    最後回傳 JSON：{report_id, summary_500, top_citations[]}。
  輸出嚴格 JSON，不要多餘解說。
```

每位 LINE 使用者擁有自己的 `environment_id`；三段呼叫透過 `previous_interaction_id` 串接，共享沙箱檔案系統。

### 5.3 Firestore Schema

```
users/{lineUserId}
  environment_id: string
  current_report_id: string | null
  last_interaction_id: string | null
  last_active_at: timestamp
  lock: { task_id: string, lock_until: timestamp } | null

reports/{reportId}
  user_id: string
  topic: string
  summary: string
  gcs_url: string
  version: int
  history: [
    { version: int, gcs_snapshot_url: string, created_at: timestamp }
  ]
  created_at: timestamp
  updated_at: timestamp
```

### 5.4 Cloud Storage Bucket

```
gs://line-reports/
  {report_id}/
    index.html             # 最新版（公開讀取）
    style.css              # 共用樣式（建置時一次性上傳）
    snapshots/
      v1.html
      v2.html
      ...
```

- `uniform-bucket-level-access` 啟用
- `allUsers:objectViewer` 公開讀取
- 預設 `Cache-Control: no-cache`（深化後立即生效）

### 5.5 Intent 分類

第一版採關鍵字法（不再多花一次 Agent 呼叫）：

| Intent | 觸發條件（任一） |
|---|---|
| `new` | 訊息不符其它類別、且無 `current_report_id`；或開頭含「研究 / 幫我查 / 找一下」 |
| `deepen` | 開頭含「深化 / 補 / 改 / 重寫 / 第 N 章」 |
| `reset` | 訊息為「重新開始 / 換題目 / 清除」 |
| `recall` | 訊息為「上次的 / 我之前的研究 / 連結」 |
| `chitchat` | 其餘短句（< 5 字、含問候） |

`reset` 行為：歸檔 `current_report_id` 到使用者報告列表、清空指標、**保留** `environment_id`（沙箱繼續沿用）。

---

## 6. 錯誤處理

### 6.1 沙箱過期（環境 7 天 TTL）

- **偵測**：Interactions API 回 `404 environment not found` 或 `failed_precondition`。
- **處置**：清掉 Firestore 的 `environment_id`，呼叫 `CreateEnvironment` 開新沙箱寫回；若原任務是深化，**自動退回新研究模式**並 Push「上次研究檔案已過期，重新從頭研究」。

### 6.2 LINE Reply Token 60 秒超時

- Webhook 收到後立即用 reply token 回覆 ACK 訊息。
- 所有後續進度通知改用 Push API。

### 6.3 三段中任一段失敗

| 段 | 行為 |
|---|---|
| PLAN | Push「規劃失敗，請換個說法重試」；不寫入 Firestore 報告。 |
| SEARCH_COMPARE | 重試 1 次；仍失敗 → Push 告知並中止；不污染 `current_report_id`。 |
| WRITE_REPORT | 重試 1 次；仍失敗 → Push「資料已找齊，組稿失敗，回『重寫』可再試」；保留 `sources.json`。 |

### 6.4 漸進深化指到不存在章節

`WRITE_REPORT` 的 system instruction 要求 Agent 拒絕並回 `{error: "chapter_not_found", available_chapters: [...]}`；Worker 把章節清單 Push 給使用者。

### 6.5 LINE 訊息長度上限 5000 字

摘要超過 4500 字 → 裁切並追加「（完整內容看連結）」。

### 6.6 GCS 發佈失敗

Agent 在 WRITE_REPORT 段 `gsutil cp` 後立即 `curl -sI` 驗證 200；若失敗回傳 `{error: "publish_failed"}`。Worker 重試 1 次；仍失敗 → Push「報告寫好但發佈失敗，回『重新發佈』可再試」。

### 6.7 同用戶並行任務

Worker 開工前在 Firestore 設 `lock = { task_id, lock_until = now + 5min }`。第二個任務進來時 webhook 看到 lock → reply「上一份研究還在跑（剩約 X 秒），完成後再傳新題目」。Worker 結束時釋鎖；`lock_until` 自然過期保險。

### 6.8 簽章驗證與非預期 payload

- LINE 簽章不對 → 401。
- 收到 sticker / 圖 / 語音 / 檔案 → reply「目前只支援文字題目」。

---

## 7. 測試策略

### 7.1 單元測試（pytest，CI 跑）

| 模組 | 重點 |
|---|---|
| `intent.py` | 給定字串、斷言類別 |
| `line_client.py` Flex builder | snapshot test（pytest-snapshot） |
| `state.py` Firestore DAO | 用 Firestore Emulator |
| `worker.py` 三段式編排 | mock `agents_client` 與 `line_client`，斷言 push 訊息與 Firestore 狀態變化；涵蓋 happy / 三段失敗 / 沙箱過期 / 鎖 |

覆蓋率目標：`worker.py + intent.py + state.py` ≥ 85%。

### 7.2 整合測試（本機，半自動，不在 CI）

| 案例 | 目的 |
|---|---|
| 真實 PLAN 一次 | 驗 `system_instructions/plan.md` 仍能讓 Agent 產合法 JSON |
| 真實 WRITE_REPORT 一次 | 驗沙箱內 gsutil + IAM 真的能上傳 |
| 同 environment_id 第二次呼叫看得到第一次寫的檔 | 驗 Agents API filesystem 持久化 |

`make test-integration` 觸發；改 `system_instructions/` 或 `agents_client.py` 時手動跑。

### 7.3 端到端（ngrok 本機 + 部署後手動）

| 場景 | 通過條件 |
|---|---|
| Flow A happy path | 60 秒內收到「規劃完成」、3 分鐘內收到摘要 + 連結、HTML 渲染正常 |
| Flow B 深化 | 同連結內容更新、`version=2`、`snapshots/v1.html` 存在 |
| Flow C 公開連結 | 第三人裝置可開、無需登入 |
| 沙箱過期 | 手動清 `environment_id` → Push 提醒、自動重建 |
| 並行鎖 | 第二題收到「請等待」 |
| 非文字訊息 | reply「只支援文字題目」 |

### 7.4 本機開發迴圈

```bash
# 終端 1
firebase emulators:start --only firestore
# 終端 2
uvicorn app.main:app --reload --port 8080
# 終端 3
ngrok http 8080
# 終端 4
make test
```

使用 `gcloud auth application-default login` 取得 ADC，本機無需 SA key 即可呼叫 Agents API。

---

## 8. 開放問題（留給實作階段確認）

- Agents API 對單次 interaction 的 timeout 上限（會影響 SEARCH_COMPARE 段超過 2 分鐘的處理策略）。
- `code_execution` 內 `gsutil` 是否預裝；若未裝，改用 `gcloud storage cp` 或直接 Python `google-cloud-storage` SDK。
- LINE Push API 在短時間連送 3-4 則進度訊息是否會被限流；若會，合併為 2 則。
- Cloud Run cold start 對 webhook 首 reply 是否常常逼近 60 秒；若會，保持 min-instances=1。
- Firestore Emulator 對 transaction 行為與真 Firestore 的細微差異（鎖機制要在真環境也驗一次）。

---

## 9. 不做的事（明確排除）

- 不做 LINE Login / LIFF。
- 不做多份並行研究、研究專案命名管理。
- 不做圖片 / 語音 / 檔案輸入。
- 不做報告內容正確性自動評估。
- 不為 demo 設計多用戶負載與限流防禦。
- 不在 Cloud Run 自己呼叫 Google Custom Search API；搜尋全由 Agent 沙箱內的 `google_search` 工具完成。
