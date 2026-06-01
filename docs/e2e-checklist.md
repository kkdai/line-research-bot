# E2E Checklist (run on LINE)

Before each Demo run:

1. Add the LINE bot as friend on the demo account.
2. Verify health: `curl <SERVICE_URL>/healthz` → `{"status":"ok"}`.

## Flow A — Happy path

- [ ] Send: `研究 SOTA 開源向量資料庫的選型`
- [ ] Within ~3 sec: see `📋 已收到題目，開始規劃…`
- [ ] Within ~30 sec: see `🔍 計畫完成：將比對 N 類來源`
- [ ] Within ~90 sec: see `📊 已比對 N 份來源，發現 M 處重要分歧`
- [ ] Within ~3 min: see Flex card with title, summary, "閱讀完整報告" button
- [ ] Tap button → opens GCS HTML → readable, styled, has v1 banner

## Flow B — Progressive deepening

- [ ] Send: `第 2 章再深一點，加日文來源`
- [ ] See new Flex card with `v2` badge
- [ ] Tap button → same URL → updated content; sources include Japanese
- [ ] Manually browse `gs://line-reports/<id>/snapshots/v1.html` → old content

## Flow C — Public link

- [ ] Open report URL in incognito browser → page loads without login

## Edge cases

- [ ] Send a sticker → reply `目前只支援文字題目`
- [ ] Send a new topic while previous still running → reply `上一份研究還在跑`
- [ ] Manually clear `environment_id` in Firestore → next message recreates sandbox and pushes `已過期，重新從頭研究 🔄`
- [ ] Send `重新開始` → reply `✅ 已清空目前研究`
- [ ] Send `連結` → reply with last report URL (or `目前沒有進行中的研究`)
