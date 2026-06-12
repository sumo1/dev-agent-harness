# S5 — issue → 助理会话跳转

> 依赖：S1'（latest_goal_run_id）、S3'（goal_run 存在才有会话）、S4（跳转入口在 issue 详情里）
> 串行收尾。

## 施工契约（怎么做）

1. **助理页接受会话定位**：`packages/views/assistant/` 支持按 `goal_run_id` 或 `session_id` 直达：
   - 现选中走 `useChatStore.activeSessionId`（store 驱动）。
   - 新增：进页时若导航带定位参数（`goal_run_id`/`session_id`），useEffect 里 `setActiveSession(resolvedSessionId)`。
   - goal_run_id → session：用现成 `GetGoalRunByChatSession` 的反向（goal_run 自带 `chat_session_id`），前端经 query 拿到 goal_run 的 discussion 会话 id。
2. **路由参数透传**：经 `useNavigation().push()` 跳 `/{ws}/assistant`，定位参数走 query 或 navigation state（共享层用 NavigationAdapter，不碰 next/react-router）。
3. **issue 详情跳转入口**（落在 S4 的右栏详情）：按钮 onClick → `push(assistant path + 定位参数)`，参数取 `issue.metadata.autofix.latest_goal_run_id`。

## 验收契约（怎么算做完）

- `packages/views/` 测试：带定位参数进助理页 → 选中正确会话（mock chat store + goal_run query）。
- 端到端：issue 详情点"跳助理会话" → 落到对应 goal_run 的 discussion 会话，TimelineView 显示该 goal 的执行流。
- `pnpm typecheck` 0 error。
- 无 `next/*` / `react-router-dom` 导入。

## 边界

- 只碰 `packages/views/assistant`、`packages/core/paths`、导航参数透传、S4 详情里的跳转按钮接线。
- 不新增 server 端点（goal_run 已带 chat_session_id；如缺按 issue 查 latest goal_run 的只读端点，用 metadata 前端已有，优先前端解决）。
