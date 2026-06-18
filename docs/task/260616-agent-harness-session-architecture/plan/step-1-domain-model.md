# step-1: 领域模型和命名边界

> 所属任务: 260616-agent-harness-session-architecture ｜ 依赖: 无 ｜ 并行组: 独立串行

## 施工契约（给执行 Agent）

### 范围

- 可改文件:
  - `packages/core/types/**`
  - `packages/core/**/session*`
  - `server/internal/**` 中纯类型、service adapter
- 不可改文件 / 冻结边界:
  - 不改 UI 交互。
  - 不改数据库 schema，除非执行前另行确认。
  - 不改 goal DAG 行为。

### 产出清单

- 定义 `WorkItemKind = "goal" | "issue" | "assistant"`。
- 定义 `AgentSession`、`RuntimeRun`、`RuntimeContext`、`SessionCommand` 类型。
- 定义 `RuntimeExternalRef`、`ChannelSurface`、`ChannelProvider`，为 OpenClaw 这类外部通道留出位置。
- 增加 adapter，把现有 `goal_run / issue / chat_session` 映射到新模型。
- 明确内部 `task_queue` 是 `TaskQueueJob`，不要把它暴露成产品 Task。

### 约束

- 保持现有 API response 兼容。
- 不删除旧字段。
- 新类型先服务后续重构，不强迫所有页面立刻迁移。
- OpenClaw 不允许新增为第四种 `WorkItemKind`；它只能是 runtime/channel/automation source。

## 验收契约（给验收）

### 代码结构验证

- [ ] core 层能 import 到统一类型。
- [ ] server 层有清晰 adapter，不在 handler 里散落 kind 判断。
- [ ] 类型层能表达 `provider=openclaw`，但 `WorkItemKind` 仍只有 `goal / issue / assistant`。
- [ ] grep 产品 UI 文案，没有把新模型误写成内部 task queue。

### 命令验收

| 命令 | 通过标准 |
|------|---------|
| `pnpm --filter @multica/core typecheck` | 0 error |
| `cd server && go test ./internal/handler ./internal/daemon` | 0 fail |
