# E2E 验证 Case：Agent Harness 会话架构重分层

> 上游：[`design.md`](./design.md)
> 执行契约：[`plan/step-8-e2e-verification.md`](./plan/step-8-e2e-verification.md)

## 总原则

这组 case 验证的是 agent harness 的架构边界，不是单个页面是否能渲染。

验证前必须先读：

- `.agents/skills/dev-agent-harness-self-dogfooding/SKILL.md`
- `.agents/skills/computer-use-desktop-e2e/SKILL.md`
- `docs/step-self-dogfooding/README.md`
- `docs/step-e2e-testing/使用-computer-use-验证桌面端.md`

环境边界：

- 控制平面使用当前稳定 checkout。
- 候选实现和验证使用独立 worktree。
- 候选 server、daemon、desktop 使用 `.env.worktree` 分配的端口、DB 和 Electron userData。
- 桌面端 UI 验证必须用 `computer-use-harness` 操作候选 Electron app。
- 不允许用 Playwright、Chrome 或普通浏览器结果冒充 desktop E2E。

证据最少包含：

- `computer-use trace --last --pretty` 的 trace path。
- 目标 app 名称。
- 关键 UI 状态或 AX 文本。
- 相关 API 返回。
- 相关 DB 查询。
- 失败时的 server/daemon log 摘要。

## Case 1：Goal 复杂任务 DAG/session

### 目标

证明复杂任务仍然走 Goal 语义：可以讨论、规划、生成 DAG、执行子任务、看到运行流和 summary。

### 前置条件

- 候选 desktop 已启动。
- 候选 daemon 已连接候选 server。
- 测试 workspace 中有一个 project，`local_directory` 指向候选 worktree 或另一个安全测试目录。

### 操作步骤

1. 用 `computer-use observe` 确认目标 app 是候选 desktop。
2. 进入复杂任务 / Goal 入口。
3. 创建一个小目标，例如“解释当前工程 README 的核心定位并给出一个改进建议”。
4. 确认规划并启动执行。
5. 观察 DAG / 子任务 / summary 区域。

### 预期 UI

- 页面显示 Goal/复杂任务语义，而不是 Issue 或 Assistant。
- 能看到子任务或执行节点。
- 子任务执行流使用统一 transcript/timeline 展示。
- 中断、继续、重试等按钮来自同一命令区。

### API 验证

```text
GET /api/tasks/{taskId}/messages
```

预期：

- 返回按 `seq` 排序的 task messages。
- message 能和 UI transcript 对上。

### DB 验证

```sql
SELECT id, status, goal_run_id, goal_subtask_id, created_at, completed_at
FROM agent_task_queue
WHERE goal_run_id = '<goal-run-id>'
ORDER BY created_at ASC;
```

```sql
SELECT task_id, seq, type, tool, content, created_at
FROM task_message
WHERE task_id = '<task-id>'
ORDER BY seq ASC;
```

### 失败判定

- Goal 执行没有可见 task messages。
- 子任务输出只存在 DB，不出现在 UI。
- Goal prompt 或消息里缺失 work item / runtime 环境。

## Case 2：Issue direct session

### 目标

证明 Issue 默认是“已知问题直接处理”，不隐式启动复杂任务 DAG。

### 前置条件

- 测试 workspace 有可写测试 project。
- 可以创建一个低风险 Issue，例如“修正文档里的一个拼写错误”。

### 操作步骤

1. 进入 Issue 入口。
2. 创建一个明确 Issue。
3. 点击开始修复 / 运行处理。
4. 观察 Issue detail 的执行日志和会话。
5. 不点击“升级为复杂任务”。

### 预期 UI

- Issue 页面停留在 Issue 语义中。
- 执行日志展示 direct fix session。
- 不跳转到复杂任务 DAG 页面。
- 中断入口在运行中可见或 disabled 状态合理。

### API 验证

```text
GET /api/tasks/{taskId}/messages
```

预期：

- 有 issue 修复相关消息。
- prompt 或消息上下文包含 Issue 标题/描述。

### DB 验证

```sql
SELECT id, status, issue_id, chat_session_id, goal_subtask_id, created_at, completed_at
FROM agent_task_queue
WHERE issue_id = '<issue-id>'
ORDER BY created_at DESC;
```

```sql
SELECT id, goal, status, issue_id, created_at
FROM goal_run
WHERE issue_id = '<issue-id>';
```

预期：

- `agent_task_queue` 有 Issue direct run。
- `goal_run` 默认 0 行。

### 失败判定

- 创建或修复 Issue 时自动生成 `goal_run`。
- Issue prompt 出现 goal planning / DAG 语义。
- Issue 页面必须进入任务页才能看执行日志。

## Case 3：Assistant chat session + retry/interrupt

### 目标

证明 Assistant 是开放式聊天会话，retry 是追加可见消息，interrupt 是 runtime control，不是页面私有逻辑。

### 前置条件

- 候选 daemon 可执行一个短任务。
- Assistant 入口可创建聊天会话。

### 操作步骤

1. 进入 Assistant。
2. 发送一个短请求。
3. 运行中观察 interrupt。
4. 对失败或完成的会话点击 retry。
5. 观察消息列表。

### 预期 UI

- Assistant 页面不出现 DAG、子任务、Issue 修复专属概念。
- retry 后新增一条明确用户消息。
- interrupt 控制当前 active run，不伪装成用户聊天。

### API 验证

```text
GET /api/chat/sessions/{sessionId}/messages
GET /api/tasks/{taskId}/messages
```

预期：

- chat messages 中能看到 retry 对应的用户消息。
- task messages 中能看到运行输出。

### DB 验证

```sql
SELECT id, status, chat_session_id, created_at, completed_at
FROM agent_task_queue
WHERE chat_session_id = '<session-id>'
ORDER BY created_at DESC;
```

### 失败判定

- retry 静默重新创建运行，但用户消息不可见。
- interrupt 只能在某个页面有，Assistant 不可用。
- Assistant 被迫创建 Goal 或 Issue。

## Case 4：OpenClaw / Lobster channel

### 目标

证明 OpenClaw 是 `RuntimeProvider + ChannelSurface + AutomationSource`，不是第四种 WorkItem。

### 前置条件

- OpenClaw provider 在候选环境中可被列出，或使用可控 mock/fixture。
- 候选 desktop 能打开 Lobster 页面。

### 操作步骤

1. 进入工作区左侧“龙虾”入口。
2. 观察 OpenClaw conversations。
3. 打开一个 conversation。
4. 分别触发：
   - dispatch as Goal
   - dispatch as Issue
   - continue in Assistant
5. 进入自动化页，切到 Lobster/OpenClaw 来源。
6. 同步或操作一个 OpenClaw automation。

### 预期 UI

- 左侧有“龙虾”入口。
- Lobster 页面显示 channel/context，而不是新 WorkItem 类型。
- 分发按钮语义明确：Goal / Issue / Assistant。
- 自动化页有 All / Native / Lobster(OpenClaw) 或等价来源筛选。

### API 验证

```text
GET /api/channels/openclaw/status
GET /api/channels/openclaw/conversations
GET /api/channels/openclaw/conversations/{id}
POST /api/channels/openclaw/conversations/{id}/dispatch
GET /api/channels/openclaw/automations
POST /api/channels/openclaw/automations/sync
```

### DB / prompt 验证

OpenClaw 分发出的运行必须能看到：

```text
<channel_context>
provider: openclaw
channel: lobster
external_conversation_id: ...
external_message_id: ...
</channel_context>
```

同时检查类型边界：

```sql
SELECT id, status, chat_session_id, issue_id, goal_subtask_id, created_at
FROM agent_task_queue
WHERE id = '<task-id>';
```

预期：

- 分发到 Goal / Issue / Assistant 后，落到对应 work item/session。
- 不出现 `work_item_kind = openclaw` 这类第四类型。

### 失败判定

- OpenClaw 被建成第四种 WorkItem。
- Lobster 页面自己维护一套独立聊天生命周期。
- 自动化操作只改 UI 状态，不调 OpenClaw 原生接口。
- OpenClaw 来源运行缺失 `channel_context`。

## Case 5：日志 / 执行记录闭环

### 目标

证明任一运行都可以从 UI、API、DB、server/daemon log、computer-use trace 五层追踪。

### 前置条件

- 任取 Case 1-4 中产生的一个 `taskId`。
- 可访问候选 DB。
- 可读取候选 server / daemon 日志。

### 操作步骤

1. 在 UI transcript 中定位一条运行消息。
2. 用 API 查询同一 `taskId`。
3. 用 SQL 查询同一 `taskId` 的 `task_message`。
4. 在 server/daemon log 中定位 claim/report/finish 相关记录。
5. 读取 `computer-use trace --last --pretty`。

### API 验证

```text
GET /api/tasks/{taskId}/messages
```

### DB 验证

```sql
SELECT id, task_id, seq, type, tool, content, input, output, created_at
FROM task_message
WHERE task_id = '<task-id>'
ORDER BY seq ASC;
```

### 日志验证

至少记录：

- task id。
- session id / issue id / goal run id。
- runtime provider。
- started / completed / failed 状态。
- 错误摘要。

### Trace 验证

```bash
computer-use trace --last --pretty
```

记录：

- trace path。
- action id。
- observe/click/type/key 等关键动作。
- 最终可见 UI 状态。

### 失败判定

- UI 有消息，但 API/DB 查不到。
- DB 有消息，但 UI 不展示。
- server log 无法定位这次运行。
- 没有 desktop trace，却声称做过桌面 E2E。

## Case 6：上下文注入一致性

### 目标

证明 UI 顶部显示的上下文和模型实际 prompt 使用的上下文一致。

### 操作步骤

1. 在 Goal / Issue / Assistant / Lobster 任一入口选择 project、runtime、agent、自定义上下文。
2. 启动一次运行。
3. 在 UI 查看 context summary。
4. 在 task messages 或 daemon log 中查看 prompt/context。

### 预期

- UI 显示的工作目录进入 prompt。
- UI 显示的 runtime/provider 进入 prompt。
- Work item 类型进入 prompt。
- OpenClaw 来源运行额外带 `channel_context`。

### 失败判定

- UI context 和 prompt context 不一致。
- 某个按钮自行追加不可见 prompt。
- custom context 顺序不稳定，导致每次运行 diff 抖动。
