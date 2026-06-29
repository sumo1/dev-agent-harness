# step-7: OpenClaw runtime/channel/automation

> 所属任务: 260616-agent-harness-session-architecture ｜ 依赖: step-1, step-2, step-3, step-5, step-6 ｜ 并行组: 独立串行

## 施工契约（给执行 Agent）

### 范围

- 可改文件:
  - `server/internal/**/runtime*`
  - `server/internal/**/channel*`
  - `server/internal/**/automation*`
  - `server/internal/handler/**`
  - `packages/core/**/runtimes*`
  - `packages/core/**/channels*`
  - `packages/core/**/automations*`
  - `packages/views/workspace/**`
  - `packages/views/automations/**`
  - `packages/views/common/**`
  - 对应 locale 文件
- 不可改文件 / 冻结边界:
  - 不新增第四种 `WorkItemKind`。
  - 不复制 OpenClaw 的完整数据模型。
  - 不让本地缓存成为 OpenClaw 对话和定时任务的主事实。
  - 不绕过统一 `AgentSession / RuntimeRun / SessionCommand`。

### 产出清单

- `RuntimeProvider(openclaw)`：OpenClaw 出现在运行时列表和运行状态里。
- `OpenClawConnector`：封装 OpenClaw 原生通讯、对话、定时任务接口。
- `ChannelSurface(provider=openclaw)`：工作区“龙虾”入口的数据模型。
- `ChannelProjectionService`：把 OpenClaw 对话历史投影为可展示 timeline。
- `AutomationSourceService(provider=openclaw)`：同步 OpenClaw 已有定时任务。
- `LobsterPage`：工作区左侧“龙虾”页面。
- 分发命令：
  - `dispatch_as_goal`
  - `dispatch_as_issue`
  - `continue_in_assistant`
- 自动化命令：
  - `sync_openclaw_automations`
  - `pause_openclaw_automation`
  - `resume_openclaw_automation`
  - `edit_openclaw_automation`
  - `delete_openclaw_automation`

### 约束

- OpenClaw 是 runtime provider、channel provider、automation source，不是工作项类型。
- “龙虾”页面只做统一入口和任务分发，不拥有新的任务生命周期。
- OpenClaw 对话历史默认只读投影；继续对话或分发任务时，才创建本系统的 `AgentSession`。
- OpenClaw 定时任务管理必须写回 OpenClaw connector，不能只改本地状态。
- 所有由 OpenClaw 触发的运行必须带 `channel_context`：

```text
<channel_context>
provider: openclaw
channel: lobster
external_conversation_id: ...
external_message_id: ...
</channel_context>
```

## 建议交互

### 工作区左侧入口

增加一级菜单：

```text
龙虾
```

页面结构：

```text
LobsterPage
  ContextBar(OpenClaw runtime/channel/workspace)
  CommandBar(sync / bind / dispatch)
  Conversations
  ConversationDetail + AgentSessionPanel
  Automations
```

### 对话历史

- 左侧列表展示 OpenClaw conversations。
- 右侧展示 conversation timeline。
- 当前 conversation 可以直接继续聊天。
- 当前 conversation 可以显式分发为：
  - 复杂任务 Goal
  - Issue
  - Assistant 会话

### 自动化频道

自动化页面增加来源筛选：

```text
All / Native / Lobster(OpenClaw)
```

“龙虾频道”展示 OpenClaw 原生定时任务：

- 名称
- cron / schedule
- 状态
- 最近执行时间
- 下次执行时间
- 绑定 runtime/channel
- 操作：同步、暂停、恢复、编辑、删除

## API 建议

短期可以先做 provider-specific API，不急着抽象成通用外部通道协议：

```text
GET    /api/channels/openclaw/status
POST   /api/channels/openclaw/connect
GET    /api/channels/openclaw/conversations
GET    /api/channels/openclaw/conversations/{id}
POST   /api/channels/openclaw/conversations/{id}/messages
POST   /api/channels/openclaw/conversations/{id}/dispatch

GET    /api/channels/openclaw/automations
POST   /api/channels/openclaw/automations/sync
POST   /api/channels/openclaw/automations/{id}/commands/{command_id}
```

长期如果接入更多外部通道，再收敛为：

```text
/api/channels/{provider}/...
/api/automation-sources/{provider}/...
```

不要过早抽象。现在真实需求只有 OpenClaw。

## 验收契约（给验收）

### 数据 / 行为验收

- [ ] OpenClaw 出现在 runtime provider / runtime list 中。
- [ ] 工作区左侧出现“龙虾”入口。
- [ ] 龙虾页面能读取 OpenClaw 对话历史。
- [ ] 当前 OpenClaw 对话能分发为 Goal。
- [ ] 当前 OpenClaw 对话能分发为 Issue。
- [ ] 当前 OpenClaw 对话能继续为 Assistant 会话。
- [ ] 自动化区出现“龙虾频道”。
- [ ] 龙虾频道能同步 OpenClaw 已有定时任务。
- [ ] 暂停 / 恢复 / 编辑 / 删除操作写回 OpenClaw，而不是只改本地投影。
- [ ] OpenClaw 触发的运行 prompt 中包含 `channel_context`。

### 命令验收

| 命令 | 通过标准 |
|------|---------|
| `pnpm --filter @multica/core typecheck` | 0 error |
| `pnpm --filter @multica/views typecheck` | 0 error |
| `cd server && go test ./internal/handler ./internal/daemon` | 0 fail |
| 桌面 E2E | 用 computer-use-harness 验证“龙虾”入口、对话分发、自动化管理 |
