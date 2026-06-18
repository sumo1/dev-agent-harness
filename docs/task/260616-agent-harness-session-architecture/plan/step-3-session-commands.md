# step-3: 统一 Session Command

> 所属任务: 260616-agent-harness-session-architecture ｜ 依赖: step-2 ｜ 并行组: A

## 施工契约（给执行 Agent）

### 范围

- 可改文件:
  - `packages/core/**/commands*`
  - `packages/views/common/**`
  - `packages/views/tasks/**`
  - `packages/views/issues/**`
  - `packages/views/assistant/**`
  - 对应 locale 文件
- 不可改文件 / 冻结边界:
  - 不改变 Issue 默认运行链路，交给 step-4。
  - 不把 prompt shortcut 做成隐藏 workflow。

### 产出清单

- `SessionCommandRegistry`。
- `CommandBar` 通用组件。
- `retry`、`continue` 作为 prompt shortcut。
- `interrupt`、`cancel` 作为 runtime control。
- `dispatch_as_goal / dispatch_as_issue / continue_in_assistant` 作为 OpenClaw channel workflow transition。
- `sync/pause/resume/edit/delete_openclaw_automation` 作为 automation control。
- 命令按 `WorkItemKind`、channel provider、automation source 和 session/run 状态过滤。

### 约束

- `retry`必须追加可见消息。
- `interrupt`必须控制当前 active run。
- 所有命令都必须能解释“它是消息、运行控制还是工作流迁移”。
- OpenClaw 自动化命令必须解释为外部 automation source control，不能伪装成聊天消息。

## 验收契约（给验收）

### UI 验收

- [ ] Goal / Issue / Assistant 都能看到适用的中断入口。
- [ ] 没有 running run 时不显示不可用中断，或显示 disabled 状态。
- [ ] 点击 retry 后消息时间线出现一条明确用户指令。
- [ ] 龙虾页面的分发按钮来自同一个 command registry。
- [ ] 龙虾频道的自动化操作不出现在普通 Goal / Issue / Assistant 页面。

### 命令验收

| 命令 | 通过标准 |
|------|---------|
| `pnpm --filter @multica/views typecheck` | 0 error |
| `pnpm --filter @multica/core typecheck` | 0 error |
