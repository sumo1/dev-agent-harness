# S1' — 地基：autofix metadata 形状 + 三态派生（无迁移）

> 依赖：无 ｜ 下游：S3'、S4、S5 全依赖此处定义的形状与纯函数

## 施工契约（怎么做）

### 后端（Go）

1. `server/internal/service/` 新增 issue autofix metadata 读写 helper（或落在 goal.go/issue 相关 service）：
   - `appendAutofixGoalRun(metadata, goalRunID)` → 把 goal_run_id append 进 `metadata.autofix.goal_run_ids`，并设 `latest_goal_run_id`。
   - `setAutofixGithub(metadata, number, url)` → 写 `metadata.autofix.github`。
   - `setAutofixNeedsInfo(metadata, reason)` → 写 `metadata.autofix.needs_info_reason`。
   - 落库走现成 `Queries.SetIssueMetadataKey`（key=`autofix`）。
2. metadata 结构（JSONB，key `autofix`）：
   ```jsonc
   { "goal_run_ids": [], "latest_goal_run_id": "", "github": {...}, "needs_info_reason": "" }
   ```

### 前端（packages/core，共享）

3. `packages/core/issues/` 新增 zod schema `autofixMetadataSchema`，parse-don't-cast + 默认空对象。
4. 三态派生**纯函数** `deriveAutofixStatus(issue, goalRun?)`：
   - 无 `autofix` / 无 goal_run → `"not_started"`
   - goalRun.status `completed` + github/PR 信息 → `"completed"`
   - goalRun.status `partial` + `needs_info_reason` → `"needs_info"`
   - 其余（planning/executing）→ `"running"`
   返回 discriminated union，含可选 reason / PR url。

## 验收契约（怎么算做完）

- `packages/core` 单测：喂畸形 metadata（缺字段 / null 数组 / 错类型）→ zod 返回默认、不抛。
- `deriveAutofixStatus` 单测：四态 + running 全覆盖，含 goalRun 缺失分支。
- Go 单测：三个 helper 对空 metadata / 已有 metadata 幂等追加正确；`SetIssueMetadataKey` 调用参数正确。
- `pnpm typecheck` + `go build ./...` 0 error。
- **无新迁移文件**（本步硬约束）。

## 边界

- 只定义形状 + 纯函数 + 写库 helper，**不接触**触发逻辑（S3'）和 UI（S4）。
- 不动 issue 表 schema、不动 goal_run CHECK 约束。
