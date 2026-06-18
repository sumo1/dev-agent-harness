# step-4: Issue direct session

> 所属任务: 260616-agent-harness-session-architecture ｜ 依赖: step-2 ｜ 并行组: A

## 施工契约（给执行 Agent）

### 范围

- 可改文件:
  - `server/internal/handler/issue*.go`
  - `server/internal/handler/chat.go`
  - `server/internal/daemon/prompt.go`
  - `packages/core/issues/**`
  - `packages/views/issues/**`
- 不可改文件 / 冻结边界:
  - 不删除 goal_run 引擎。
  - 不破坏已有 Issue CRUD。
  - 不默认创建 goal_run。

### 产出清单

- Issue “开始修复”创建 `AgentSession(kind=issue)`。
- Issue direct fix prompt。
- Issue 验证/回报命令。
- `upgrade_to_goal` 显式入口。
- Issue detail 能显示 direct session timeline。

### 约束

- Issue 直接修复是默认路径。
- 只有用户显式升级，才创建 Goal。
- Issue direct session 可以使用内部 task_queue job，但 UI/API 不把它叫复杂任务。

## 验收契约（给验收）

### 数据 / 行为验收

- [ ] 新建 Issue 后不自动创建 `goal_run`。
- [ ] 点击“开始修复”后创建 issue session。
- [ ] session prompt 含 `<issue_context>`。
- [ ] 点击“升级为复杂任务”后才创建 Goal。

### 命令验收

| 命令 | 通过标准 |
|------|---------|
| `pnpm --filter @multica/views typecheck` | 0 error |
| `cd server && go test ./internal/handler -run Issue` | 0 fail |

