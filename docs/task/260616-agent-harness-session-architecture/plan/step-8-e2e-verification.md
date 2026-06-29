# step-8: E2E 验证和日志证据闭环

> 所属任务: 260616-agent-harness-session-architecture ｜ 依赖: step-1..step-7 ｜ 并行组: 收尾串行

## 施工契约（给执行 Agent）

### 范围

- 可读文件:
  - `docs/task/260616-agent-harness-session-architecture/e2e-verification-cases.md`
  - `docs/step-self-dogfooding/README.md`
  - `docs/step-e2e-testing/使用-computer-use-验证桌面端.md`
  - `.agents/skills/dev-agent-harness-self-dogfooding/SKILL.md`
  - `.agents/skills/computer-use-desktop-e2e/SKILL.md`
- 可操作对象:
  - 候选 worktree 的 server / daemon / desktop。
  - 候选 worktree 的 DB。
  - 候选 desktop app。
- 不可操作对象:
  - 正在派发任务的控制平面 checkout。
  - 正在派发任务的控制平面 server / daemon / desktop。
  - 主控 DB 和主控 Electron userData。

### 产出清单

- 跑通至少一条 Goal case。
- 跑通至少一条 Issue direct session case。
- 跑通至少一条 Assistant command case。
- 跑通 OpenClaw/Lobster channel case，或在 OpenClaw 原生环境不可用时跑 mock/fixture 并明确缺口。
- 对每条运行记录：
  - UI 观察结果。
  - API 查询结果。
  - DB 查询结果。
  - server/daemon log 摘要。
  - computer-use trace path。
- 把验证结论写回任务 memory，文件名形如：

```text
docs/task/260616-agent-harness-session-architecture/memory/YYYY-MM-DD-e2e-verification.md
```

### 约束

- 桌面端 E2E 必须使用 `computer-use-harness`。
- 不能用 Playwright / Chrome / web 页面替代候选 Electron app。
- 验证失败要记录失败点和下一步，不要写成“基本可用”。
- 如果只验证文档或 typecheck，不能声称 E2E 通过。

## 验收契约（给验收）

### 证据验收

- [ ] 每个 case 都有明确 `taskId` / `sessionId` / `issueId` / `goalRunId` 中至少一个可追踪 id。
- [ ] UI transcript 和 `GET /api/tasks/{taskId}/messages` 能对上。
- [ ] API messages 和 `task_message` DB rows 能对上。
- [ ] server/daemon log 能定位同一运行。
- [ ] desktop 操作有 `computer-use` trace path。
- [ ] Issue direct session 默认没有 `goal_run`。
- [ ] OpenClaw 分发运行有 `channel_context`，且没有第四种 WorkItem。
- [ ] retry 被证明是追加可见用户消息。
- [ ] interrupt 被证明是 runtime control。

### 命令验收

| 命令 | 通过标准 |
|------|---------|
| `git diff --check` | 0 error |
| `pnpm --filter @multica/core typecheck` | 0 error |
| `pnpm --filter @multica/views typecheck` | 0 error |
| `pnpm --filter @multica/desktop typecheck` | 0 error |
| `cd server && go test ./internal/handler ./internal/daemon` | 0 fail |
| 桌面 E2E | 用 `computer-use-harness` 产出真实 trace |

## 最小汇报格式

```text
验证对象：
- worktree:
- app:
- server:
- daemon:

Case:
- 名称:
- task/session id:
- UI:
- API:
- DB:
- log:
- trace:
- 结论:

残留风险：
- ...
```
