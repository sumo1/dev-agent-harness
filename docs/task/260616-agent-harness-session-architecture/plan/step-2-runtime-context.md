# step-2: RuntimeContext 注入

> 所属任务: 260616-agent-harness-session-architecture ｜ 依赖: step-1 ｜ 并行组: 独立串行

## 施工契约（给执行 Agent）

### 范围

- 可改文件:
  - `server/internal/daemon/prompt.go`
  - `server/internal/daemon/**`
  - `packages/core/**` 中 context 类型和 API client
  - 必要的 prompt 单测
- 不可改文件 / 冻结边界:
  - 不重写 goal DAG 调度。
  - 不改变 runtime claim 协议的既有必填字段，新增字段必须兼容。

### 产出清单

- `RuntimeContextBuilder`：从 WorkItem、workspace、project、runtime、agent、自定义选项生成上下文。
- `RuntimeContextSnapshot`：每次运行固定一份可审计上下文。
- prompt builder 改为消费 snapshot，而不是到处临时查字段。
- prompt 明确输出：
  - `work_item_kind`
  - workspace
  - local_directory
  - runtime/provider
  - mode，如 `goal_planning`、`direct_issue_fix`、`assistant_chat`
- OpenClaw/龙虾通道触发的运行额外输出：
  - `channel_provider: openclaw`
  - `channel: lobster`
  - `external_conversation_id`
  - `external_message_id`

### 约束

- UI 顶部能看到的环境必须进入 prompt。
- 按钮不能自己藏 prompt 片段。
- custom context blocks 要有稳定顺序，避免每次运行 prompt diff 抖动。
- channel context 只能来自统一 builder，不能由龙虾页面自己拼 prompt。

## 验收契约（给验收）

### 数据 / 字段验收

- [ ] Goal prompt 中出现 `work_item_kind: goal`。
- [ ] Issue prompt 中出现 `work_item_kind: issue`，且不出现 goal planning 语义。
- [ ] Assistant prompt 中出现 `work_item_kind: assistant`。
- [ ] 工作目录进入 prompt。
- [ ] OpenClaw 触发的 prompt 中出现 `channel_provider: openclaw`。

### 命令验收

| 命令 | 通过标准 |
|------|---------|
| `cd server && go test ./internal/daemon -run Prompt` | 0 fail |
| `pnpm --filter @multica/core typecheck` | 0 error |
