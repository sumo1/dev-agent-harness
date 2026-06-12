# 技术方案：260612-issue-github-autofix

> 上游：[`requirement.md`](./requirement.md) · [`breakdown.md`](./breakdown.md)
> 下游：`plan/step-*.md`（双契约）
> 本文定：数据结构、边界、破坏性风险、串/并行依赖图。基线事实见 §0。

## §0 基线校准（2026-06-12，三路 Explore 实读）

### goal_run / goal_subtask（migration 112）

```sql
goal_run.status CHECK IN
  ('discussion','confirmed','planning','executing',
   'completed','partial','failed','cancelled')
goal_subtask.status CHECK IN
  ('pending','ready','running','completed','failed','blocked','skipped')
```

- `goal_run` 已有 `project_id`（migration 116，nullable FK → project）。
- 终态由 `recomputeGoalStatus`（`server/internal/service/goal.go:1915–1942`）汇总 subtask 决定：全 completed → `completed`；部分 → `partial`；全失败 → `failed`。
- 状态是裸字符串，**无 Go 常量**。

### issue（migration 001，最新列见 models.go）

- 已有 nullable FK：`project_id`、`parent_issue_id`、`origin_type`/`origin_id`、`metadata JSONB`。
- 状态枚举 `('backlog','todo','in_progress','in_review','done','blocked','cancelled')`。
- **无 `goal_run_id`**。

### 任务派发（FK-less 模式，已验证踩坑两次）

- `EnqueueTaskForIssue`（`task.go:392`）在 issue 创建后被调用（`handler/issue.go:2264`），要求 issue 有 assignee+live agent。
- FK-less goal 任务（planning/summary/decision/persist）只在 `context` JSONB 带 workspace/goal；
  **任何解析 workspace 的 handler 必须走 `ResolveTaskWorkspaceID`（`task.go:1855`）**，否则 `task:message` WS 广播丢失，④ 执行流空白。新任务类型必须在此注册（`task.go:1919`）。
- prompt 在 `daemon/prompt.go:BuildPrompt`（17–51）按字段路由；新增 kind 加一个 builder + 一条路由分支。
- 派发的 claim 由 daemon 富化出 `ProjectResources`（github_repo / local_directory）+ repos，agent 在仓库目录用自身 `gh`/`git` 凭证干活。worktree 命名 `agent/{name}/{short-task-id}`（`repocache/cache.go:435`）。

### 前端（共享 @multica/views）

- sidebar `NavKey` 已含 `"issues"` 但未在 `workspaceNav` 数组里（`app-sidebar.tsx:105`）——放出即可。
- 旧建 issue 入口共 4 处：sidebar 按钮（605）、全局 `c` 快捷键（465）、search 命令面板、issue 详情"加子 issue"。
- `useFileDropZone`（`editor/use-file-drop-zone.ts`）已支持拖拽；paste 要在 ContentEditor 上加 `onPaste`。
- 三栏版式参考 `packages/views/tasks/components/tasks-page.tsx`（本地 state 选中，非 store）。
- `paths.workspace(slug).issues()` → `/{slug}/issues` 已存在；web 页 `apps/web/app/[workspaceSlug]/(dashboard)/issues/`，desktop 路由 `apps/desktop/.../routes.tsx`。
- 助理会话选中走 `useChatStore.activeSessionId`（store 驱动，非 URL）。
- issue 类型/zod 在 `packages/core`，列表 query key `["issues", wsId, ...]`。

---

## §1 数据结构决策

### 决策 A：issue↔goal_run 用 metadata，不加专列 ⭐

需求 §4.4「issue→goal_run 一对多」（首次没复现、补充信息后再来）。
**不加 `issue.goal_run_id` 单列**——单列表达不了"最新+历史"，且改 issue 表是高频核心表的破坏面。

改用既有 `issue.metadata JSONB`（migration 105 已有，`SetIssueMetadataKey` 查询现成）：

```jsonc
issue.metadata.autofix = {
  "goal_run_ids": ["<uuid>", ...],   // 历史全部，append
  "latest_goal_run_id": "<uuid>",     // 最新一次，跳转用
  "github": {                          // S2 回报
    "issue_number": 1234,
    "issue_url": "https://github.com/owner/repo/issues/1234"
  }
}
```

- 零迁移、零破坏：metadata 本就 fail-soft（前端 zod parse + 默认 `{}`）。
- 一对多天然支持：`goal_run_ids` append，`latest_goal_run_id` 覆盖。
- 跳转：`latest_goal_run_id` → `GetGoalRunByChatSession` 反查 discussion 会话。

> 这消掉了 breakdown S1 里"加 goal_run_id 列 + 反查查询"那一坨——
> **good taste：用现成的数据结构让特殊情况消失。**

### 决策 B：第三态用 `partial` + metadata 标记，不加新枚举值 ⭐

需求三态：未启动 / 完成 / 需要补充信息。

| 产品态 | 底层来源 | 不需要新枚举 |
|--------|---------|-------------|
| 未启动 | `metadata.autofix` 不存在 / 无 goal_run | 纯前端判断 |
| 完成 | goal_run `completed` + metadata 有 PR URL | 复用 `completed` |
| 需要补充信息 | goal_run `partial` + `metadata.autofix.needs_info_reason` | 复用 `partial` |

**不动 goal_run 的 CHECK 约束**（加枚举值要改迁移 + 所有 switch + 破坏 API 兼容）。
`partial` 语义已是"没全done"，刚好覆盖"验证 agent 报没复现/没发现问题"。
区分"真 partial 失败"vs"需补充信息"靠验证 agent 回报写进 `metadata.autofix.needs_info_reason`——
**回报驱动，server 不猜**（延续 goal_decision 铁律）。

> 结论：**S1 不需要任何迁移**。breakdown 里的 117 迁移取消。这是最大的简化。

### 决策 C：建 GitHub issue / 出 PR = goal_run 内的 subtask，不另造任务类型

breakdown S2 设想"新任务类型 + dispatch + claim 映射 + prompt 段 + ResolveTaskWorkspaceID 注册"。
但 goal_run 引擎已有完整的 subtask 派发 + workspace 解析 + prompt 拼装。
**把"建 GitHub issue""出 PR"做成 goal_run 规划出的 subtask 节点**，
prompt 思路注入到 PMO 规划引导里，复用 `goal_subtask` 全套——零新任务类型、零新 ResolveTaskWorkspaceID 分支。

---

## §2 端到端数据流（落地版）

```
① 中栏内联新建 issue（粘贴图片 + 一段话）
   └─ POST /api/issues（attachment_ids 走现成 upload-file）
   └─ assignee = workspace PMO/squad（触发 goal 链路的前提）

② issue 创建后触发自动修复 goal_run（handler/issue.go 创建路径末尾）
   └─ GoalService 起 goal_run（绑 issue 的 project_id），goal = issue 标题+描述+附件引用
   └─ metadata.autofix.goal_run_ids append + latest_goal_run_id 写回 issue
   └─ PMO 规划（leader 任务，server 不调 LLM）四节点：
        N1 建 GitHub issue（gh issue create，回报 number/url）
        N2 修复（worktree 改代码）
        N3 端到端验证（E2E；没复现→回报 needs_info）
        N4 出 PR（git push + gh pr create，回报 PR url）

③ 三态（前端从 goal_run.status + issue.metadata.autofix 派生）
   未启动 / 完成(completed+PR url) / 需补充信息(partial+needs_info_reason)

④ 完成态→PR：N4 已 push+开 PR；合并用户在 GitHub 点；webhook 同步回 issue（现成）

⑤ 助理详情：复用 TimelineView（现成）

⑥ issue→助理跳转：metadata.latest_goal_run_id → GetGoalRunByChatSession → 助理页 setActiveSession
```

> ⚠️ N1 建 GitHub issue 与 N2 修复无强序——但 requirement §1 要"issue 先推上去"。
> 设 N2/N3/N4 依赖 N1（DAG depends_on），保证 GitHub issue 先建。PR body 引用 issue number。

---

## §3 串/并行依赖图（修订）

```
            S1' (metadata 形状 + zod schema + 三态派生纯函数) ── 地基,无迁移
              │
        ┌─────┴───────────────┐
        ▼                      ▼
   S3' (自动修复 goal_run)   S4 (Issue 三栏页 + 粘贴 + 删旧入口)
   含 N1建issue/N2修复/         前端骨架可与 S3' 并行
   N3验证/N4出PR 的规划引导     (文件互斥:server vs views)
        │                      │
        └──────────┬───────────┘
                   ▼
                 S5 (issue→助理跳转,需 goal_run+跳转入口都在)
```

- **S1'**（地基）：定义 `metadata.autofix` 的 zod schema（`packages/core`）+ 三态派生纯函数 + Go 侧写 metadata 的 helper。**无迁移**。
- **S3' / S4 并行**：文件范围互斥（`server/internal/service|handler|daemon` vs `packages/views/issues`），约束各自显式，验收独立。
- **S5 串行收尾**：依赖 S3'（有 goal_run 才有会话）+ S4（跳转入口在 issue 详情里）。
- S2 已并入 S3'（N1 节点）。

## §4 破坏性风险

1. **删旧建 issue 入口的悬挂引用**（4 处）——删前全量 grep，避免死按钮。
2. **issue 必须有触发 goal 的 assignee**——若 workspace 没配 PMO/squad，自动修复起不来，要 fail-soft（issue 正常建，autofix 标"未启动"，不报错）。
3. **agent 环境凭证假设**（`gh auth`/git push）——缺则 N1/N4 失败，验证 agent 回报落 `needs_info`，不静默卡死。
4. **API 兼容**：`metadata.autofix` 所有读取走 zod parse + 默认值，老 desktop 读不到字段不白屏。

## §5 本轮不做

- goal_run 加新枚举值（用 partial 替代）。
- issue 加专列（用 metadata）。
- server 侧 GitHub API client（全走 agent）。
- 定时轮询（事件驱动）。
