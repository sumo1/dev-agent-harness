# 技术方案：Agent Harness 会话架构重分层

> 上游：[`requirement.md`](./requirement.md)
> 下游：[`breakdown.md`](./breakdown.md)、[`plan/`](./plan/)
> 本文定：领域模型、上下文注入、命令模型、Issue 解耦、UI 分层、迁移顺序。

## §0 核心判断

当前坏味道不是某个按钮的位置不对，而是**产品语义被 Task 吞掉了**。

`Task` 在系统里同时承担了三层含义：

1. 产品入口：用户看到的“复杂任务”。
2. 执行引擎：后台派给 daemon/runtime 的 task queue job。
3. 通用会话：Issue、助理、目标模式都借它表达运行过程。

这三个概念必须拆开。否则 Issue 这种“已知问题直接处理”的入口，会被复杂任务的 DAG/规划语义污染；聊天助理也会被迫继承不需要的任务生命周期。

新的核心结构：

```text
WorkItem(kind: goal | issue | assistant)
  -> AgentSession
    -> RuntimeRun
      -> TaskQueueJob(内部实现细节)
```

产品层看 `WorkItem` 和 `AgentSession`，执行层看 `RuntimeRun` 和内部 task queue。两层不要混成一个名词。

## §1 领域模型

### WorkItem：用户正在处理的东西

```ts
type WorkItemKind = "goal" | "issue" | "assistant";

type WorkItem = {
  id: string;
  workspaceId: string;
  kind: WorkItemKind;
  title: string;
  description?: string;
  status: string;
  projectId?: string;
  createdAt: string;
  updatedAt: string;
};
```

三类入口的语义：

| kind | 产品名 | 本质 | 默认运行方式 | 是否天然 DAG |
|---|---|---|---|---|
| `goal` | 任务 / 复杂目标 | 不确定路径的大目标 | 讨论、确认、规划、执行 | 是 |
| `issue` | Issue | 已知问题定义 | 直接修复、验证、回报 | 否 |
| `assistant` | 助理 | 开放式协作对话 | 聊天、工具调用、沉淀 | 否 |

### AgentSession：一段可对话、可运行、可追踪的会话

```ts
type AgentSession = {
  id: string;
  workspaceId: string;
  workItemId: string;
  workItemKind: WorkItemKind;
  title: string;
  runtimeId?: string;
  agentId?: string;
  status: "idle" | "running" | "waiting" | "failed" | "completed" | "cancelled";
  contextSnapshotId: string;
  createdAt: string;
  updatedAt: string;
};
```

会话层承载：

- 消息时间线。
- 操作按钮。
- 当前运行状态。
- runtime run 列表。
- 可回放的 context snapshot。

### RuntimeRun：一次具体运行

```ts
type RuntimeRun = {
  id: string;
  sessionId: string;
  runtimeId: string;
  provider: "codex" | "claude_code" | "openclaw" | "cursor" | "gemini" | string;
  status: "queued" | "running" | "succeeded" | "failed" | "cancelled";
  taskQueueJobId?: string;
  externalRef?: RuntimeExternalRef;
  startedAt?: string;
  endedAt?: string;
};
```

`interrupt`、`cancel`、`retry`这些能力应该挂在 session/run 层，而不是挂在 goal/issue/assistant 某一个页面里。

`externalRef` 用于承载外部运行时的原生引用，例如 OpenClaw 的 conversation id、message id、automation id。内部 `taskQueueJobId` 可以继续存在，但不能假设所有 runtime 都只有一种内部队列执行形态。

### RuntimeContext：模型运行时看到的环境

```ts
type RuntimeContext = {
  workItem: {
    kind: WorkItemKind;
    id: string;
    title: string;
    description?: string;
  };
  workspace: {
    id: string;
    name: string;
  };
  project?: {
    id: string;
    name: string;
    localDirectory?: string;
    gitRemote?: string;
  };
  runtime?: {
    id: string;
    provider: string;
    name: string;
  };
  agent?: {
    id: string;
    name: string;
  };
  issue?: IssueRuntimeContext;
  goal?: GoalRuntimeContext;
  assistant?: AssistantRuntimeContext;
  customBlocks: RuntimeContextBlock[];
};
```

`customBlocks`用于承载用户在 UI 上配置的扩展上下文，包括但不限于：

- 工作目录。
- 环境变量。
- 运行时选择。
- 是否持久化到工程。
- 绑定的 repo 文档。
- 本地 skills 提示。
- 用户自定义说明。

### ChannelSurface：运行时的沟通入口

OpenClaw 这类系统同时具备 runtime 和 channel 属性：它不只是“能执行任务”，还天然有对话历史、频道、定时任务。

不要把 OpenClaw 做成第四种 `WorkItemKind`。更干净的模型是：

```ts
type ChannelProvider = "openclaw" | string;

type ChannelSurface = {
  id: string;
  workspaceId: string;
  provider: ChannelProvider;
  runtimeId?: string;
  displayName: string;
  status: "connected" | "disconnected" | "error";
  externalRef?: RuntimeExternalRef;
};
```

关系应该是：

```text
OpenClaw ChannelSurface
  -> external conversations / automations
  -> AgentSession projection
  -> dispatch to WorkItem(kind: goal | issue | assistant)
```

也就是说，“龙虾”是一个工作区通道入口；用户可以从这里把对话分发成复杂目标、Issue 或助理会话，但它自己不是新的任务类型。

## §2 三类入口边界

### Goal / 任务

适用场景：

- 目标大，路径不确定。
- 需要拆解、分配、多 agent 并行或串行执行。
- 需要用户确认计划。
- 需要主任务和子任务视图。

默认流程：

```text
discussion -> confirm -> planning -> executing -> summary -> completed/partial/failed
```

Goal 可以使用 DAG、子任务、验证节点和执行总结。

### Issue

适用场景：

- 问题已经定义清楚。
- 用户期望“修这个问题”，不是“帮我规划一个大目标”。
- 结果通常是：复现、定位、修改、验证、说明。

默认流程：

```text
created -> direct fix session -> verify -> report
```

Issue 默认不创建 goal_run，不进入复杂任务 DAG。只有用户显式点击“升级为复杂任务”时，才创建 goal。

Issue session 的 prompt 应该直接包含：

```text
<issue_context>
title: ...
description: ...
expected_behavior: ...
actual_behavior: ...
attachments: ...
repo_context: ...
</issue_context>
```

### Assistant

适用场景：

- 开放式对话。
- 代码咨询、设计讨论、临时操作。
- 可沉淀为 Issue 或 Goal，但默认不是。

默认流程：

```text
chat -> optional run -> optional persist/convert
```

Assistant 不应该自动继承 goal 的拆解动作，也不应该默认创建 Issue。

### OpenClaw / 龙虾通道

适用场景：

- 用户希望把 OpenClaw 作为本系统可见、可控的运行时。
- 用户希望在工作区内查看 OpenClaw 的聊天历史和运行反馈。
- 用户希望从 OpenClaw 对话窗口里分发任务到当前 agent harness。
- 用户希望统一管理 OpenClaw 已有的定时任务。

默认流程：

```text
connect OpenClaw -> sync conversations / automations -> open Lobster channel
  -> chat / inspect history
  -> dispatch as Goal / Issue / Assistant
```

OpenClaw 的关键边界：

- OpenClaw 是 `RuntimeProvider + ChannelSurface + AutomationSource`。
- OpenClaw 不是 `WorkItemKind`。
- OpenClaw 原生对话和定时任务的主事实仍在 OpenClaw。
- 本系统可以保存轻量索引、同步时间、映射关系和 context snapshot，但不要复制一整套 OpenClaw 数据模型。

## §3 Prompt 上下文注入协议

所有运行都必须生成一个可见、可审计的上下文块。上下文不能只藏在后端字段里。

### 结构

```text
<runtime_context>
work_item_kind: issue
work_item_id: ...
workspace: E2E Test
project: AI-GAME
local_directory: /Users/sumo/workplace/ai/AI-GAME
runtime: codex
agent: ...
mode: direct_issue_fix
</runtime_context>

<work_item_context>
...
</work_item_context>

<operator_context>
...
</operator_context>

<channel_context>
provider: openclaw
channel: lobster
external_conversation_id: ...
external_message_id: ...
</channel_context>
```

### 注入规则

1. UI 顶部 Context Bar 里能看到的环境，必须进入 `runtime_context`。
2. 用户自定义选项不能散落在各自组件里，统一进入 `customBlocks`。
3. prompt builder 只消费 `RuntimeContext`，不要从多个业务表临时拼字段。
4. 每次运行保存 `contextSnapshot`，方便重试、审计和复现。
5. 不同 WorkItemKind 只改变 `work_item_context` 的内容，不改变底层会话协议。
6. 如果运行来自 OpenClaw/龙虾通道，必须额外带上 `channel_context`，让模型知道任务来自哪个外部对话和频道。

### 负面规则

- 不允许按钮自己偷偷补一段不可见 prompt。
- 不允许 Issue 运行时伪装成 Goal 规划任务。
- 不允许一个页面用一套 prompt builder，另一个页面复制一套相似逻辑。

## §4 命令模型

按钮先分为四类，不要混。前三类服务 `AgentSession / RuntimeRun`，第四类服务外部自动化源。

```ts
type SessionCommandKind =
  | "prompt_shortcut"
  | "runtime_control"
  | "workflow_transition"
  | "automation_control";

type SessionCommandScope =
  | "all"
  | WorkItemKind
  | `channel:${ChannelProvider}`
  | `automation:${ChannelProvider}`;

type SessionCommand = {
  id: string;
  kind: SessionCommandKind;
  scopes: SessionCommandScope[];
  label: string;
  messageTemplate?: string;
  requiresRunningRun?: boolean;
};
```

### prompt_shortcut

本质是追加一条用户消息。

| 命令 | 适用 | 行为 |
|---|---|---|
| retry | all | 基于当前会话历史，重试失败步骤 |
| continue | all | 继续当前未完成工作 |
| explain | all | 解释当前结果和下一步 |
| verify | issue/goal | 要求基于当前改动执行验证 |

`retry`示例：

```text
请基于当前会话历史，重试刚才失败的步骤。
请先说明上次失败原因，再给出新的处理方式。
不要丢弃当前上下文，不要重新解释整个任务。
```

### runtime_control

本质是控制正在运行的 runtime。

| 命令 | 适用 | 行为 |
|---|---|---|
| interrupt | all | 中断当前 running RuntimeRun |
| cancel | all | 取消当前 queued/running RuntimeRun |

这类命令不能伪装成聊天消息。它们是对 run 的控制。

### workflow_transition

本质是改变 WorkItem 状态或创建新 WorkItem。

| 命令 | 适用 | 行为 |
|---|---|---|
| split | goal | 进入 DAG 拆解 |
| confirm_plan | goal | 确认规划并开始执行 |
| close_issue | issue | 关闭 Issue |
| convert_to_issue | assistant | 从聊天沉淀 Issue |
| upgrade_to_goal | issue/assistant | 显式创建复杂任务 |
| dispatch_as_goal | channel:openclaw | 基于当前 OpenClaw 对话创建复杂目标 |
| dispatch_as_issue | channel:openclaw | 基于当前 OpenClaw 对话创建 Issue |
| continue_in_assistant | channel:openclaw | 把当前 OpenClaw 对话投影为助理会话 |
| bind_openclaw_runtime | channel:openclaw | 绑定 OpenClaw 运行时 |

### automation_control

OpenClaw 自动化不是普通聊天命令，也不是内部 workflow transition。它是对外部自动化源的管理动作。

| 命令 | 适用 | 行为 |
|---|---|---|
| sync_openclaw_automations | automation:openclaw | 同步 OpenClaw 定时任务 |
| pause_openclaw_automation | automation:openclaw | 暂停 OpenClaw 原生定时任务 |
| resume_openclaw_automation | automation:openclaw | 恢复 OpenClaw 原生定时任务 |
| edit_openclaw_automation | automation:openclaw | 编辑 OpenClaw 原生定时任务 |
| delete_openclaw_automation | automation:openclaw | 删除 OpenClaw 原生定时任务 |

这类命令必须通过 OpenClaw connector 调原生接口，不要只改本地投影状态。

## §5 UI 分层

页面应该拆成公共层和类型层。

### 公共组件

```text
ContextBar
CommandBar
SessionTimeline
RuntimeRunStatus
RuntimeRunControls
MessageComposer
```

公共层负责：

- 展示当前 `RuntimeContext`。
- 选择工作目录 / runtime / agent。
- 展示和触发通用命令。
- 展示消息和工具输出。
- 中断 / 继续 / 重试。

### 类型页面

| 页面 | 只负责 |
|---|---|
| GoalPage | 讨论、成员、DAG、子任务、summary |
| IssuePage | Issue 描述、复现信息、修复会话、验证结果 |
| AssistantPage | 纯聊天、临时运行、沉淀/转换 |
| LobsterPage | OpenClaw 对话历史、任务分发、自动化频道 |

不要让 IssuePage import GoalPage 的执行入口。可以共享 `AgentSessionPanel`，不能共享“复杂任务启动流程”。

### 推荐布局

```text
┌───────────────┬────────────────────────────────────────────┐
│ WorkItem List │ ContextBar + CommandBar                    │
│               ├────────────────────────────────────────────┤
│               │ Type-specific body                         │
│               │ - Goal: discussion + DAG + sub sessions    │
│               │ - Issue: issue detail + fix session        │
│               │ - Assistant: chat session                  │
│               │ - Lobster: OpenClaw channel + automations  │
│               ├────────────────────────────────────────────┤
│               │ MessageComposer                            │
└───────────────┴────────────────────────────────────────────┘
```

### 龙虾入口

工作区左侧增加“龙虾”菜单，语义是 OpenClaw channel，不是普通导航分组。

页面建议分三块：

- `Conversations`：OpenClaw 对话历史列表，支持查看详情和继续对话。
- `Dispatch`：基于当前对话分发为 Goal / Issue / Assistant。
- `Automations`：OpenClaw 定时任务列表，支持同步、启停、编辑、删除。

这里的关键不是做一个漂亮列表，而是保持数据边界清晰：展示来自 OpenClaw 的事实，操作通过 connector 写回 OpenClaw。

## §6 Issue 从 Task 解耦

### 当前问题

Issue 自动修复容易走进 goal/task 链路。这样做的后果：

- Issue 被误认为复杂目标。
- prompt 里出现不必要的规划语言。
- UI 上跳到任务执行，用户不知道自己是在修 Issue 还是跑 Goal。
- 中断、重试等能力分散在任务页面里，Issue 页面缺失。

### 新边界

Issue 的默认执行路径：

```text
Issue
  -> AgentSession(kind=issue)
    -> RuntimeRun(mode=direct_issue_fix)
      -> internal task_queue job
```

只有显式升级时：

```text
Issue
  -> upgrade_to_goal
    -> Goal WorkItem
      -> goal DAG
```

### 命名建议

产品层：

- `Goal`：复杂任务。
- `Issue`：已知问题。
- `Assistant`：对话。
- `Session`：一次 agent 协作会话。
- `Run`：一次 runtime 执行。

实现层：

- `task_queue` 可以保留为内部执行队列。
- 但 API/UI 文案里不要把 issue direct run 叫 task。

## §7 后端/API 改造方向

优先做“兼容新增”，不要一次性推倒。

### 新增或收敛的 API 能力

```text
GET    /api/work-items?kind=...
GET    /api/sessions?work_item_id=...
POST   /api/sessions
POST   /api/sessions/{id}/messages
POST   /api/sessions/{id}/commands/{command_id}
POST   /api/runs/{id}/interrupt
GET    /api/channels/openclaw/conversations
POST   /api/channels/openclaw/conversations/{id}/messages
POST   /api/channels/openclaw/conversations/{id}/dispatch
GET    /api/channels/openclaw/automations
POST   /api/channels/openclaw/automations/sync
POST   /api/channels/openclaw/automations/{id}/commands/{command_id}
```

短期可以不新建完整 REST 资源，而是在现有 `chat_session`、`goal_run`、`issue` handler 上先抽服务层：

```text
SessionService
RuntimeContextBuilder
SessionCommandService
RuntimeRunControlService
OpenClawConnector
ChannelProjectionService
AutomationSourceService
```

### prompt builder 收敛

当前 prompt builder 不应该继续按“issue/goal/chat 各自拼上下文”的方式增长。

目标：

```text
业务入口 -> RuntimeContextBuilder -> RuntimeContextSnapshot -> PromptBuilder
```

PromptBuilder 只做格式化，不做业务查询。

## §8 前端改造方向

### 状态来源

- server state 继续归 React Query。
- client selection 继续归 Zustand 或页面本地 state。
- `RuntimeContext` 由 server 返回快照，前端可以显示，但不要自己重新推导一份不同的事实。

### Command Registry

前端不应该到处写：

```ts
if (kind === "issue") showRetry...
```

应该是数据驱动：

```ts
const commands = getSessionCommands({
  workItemKind,
  sessionStatus,
  runStatus,
  channelProvider,
});
```

按钮只是渲染 registry 返回的命令。

### OpenClaw / 龙虾页面

`LobsterPage` 不应该自己实现一套聊天系统。它应该复用：

- `AgentSessionPanel` 显示投影后的对话。
- `CommandBar` 显示分发和自动化管理命令。
- `ContextBar` 显示 OpenClaw runtime、channel、workspace、工作目录。

OpenClaw 原生聊天历史可以通过 `ChannelProjectionService` 投影成只读 timeline；当用户继续对话或分发任务时，再创建本系统的 `AgentSession` 和 `RuntimeRun`。

## §9 分阶段执行

### Step 1：领域模型和命名边界

目标：先把类型和服务边界立住，不改用户可见行为。

产出：

- `WorkItemKind`
- `AgentSession`
- `RuntimeRun`
- `RuntimeContext`
- `SessionCommand`
- `RuntimeExternalRef / ChannelSurface / ChannelProvider`
- 当前 API 到新模型的 adapter

验收：

- 现有 Task/Issue/Assistant 页面不破。
- 类型层能表达三类入口。
- 类型层能表达 OpenClaw runtime/channel，但不新增第四种 WorkItem。

### Step 2：RuntimeContext 注入

目标：把顶部上下文、工作目录、自定义选项统一成 context snapshot，并进入 prompt。

产出：

- `RuntimeContextBuilder`
- prompt builder 改为消费 `RuntimeContext`
- prompt snapshot 可在 UI 展示
- OpenClaw 来源运行带 `channel_context`

验收：

- Goal/Issue/Assistant 的运行 prompt 都能看到明确 `work_item_kind`。
- 工作目录和 runtime 信息稳定注入。
- OpenClaw 触发的运行能看到 `provider: openclaw` 和外部 conversation 引用。

### Step 3：统一命令模型

目标：把重试、继续、中断等从页面私有逻辑里抽出来。

产出：

- `SessionCommandRegistry`
- `CommandBar`
- `interrupt`统一 run control
- `retry/continue`统一 prompt shortcut
- OpenClaw 分发命令和自动化命令进入同一个 registry

验收：

- 三类入口都有中断。
- 重试只追加显式消息，不偷偷重建隐藏流程。
- 龙虾页面的分发/自动化按钮不是页面私有逻辑。

### Step 4：Issue direct session

目标：Issue 默认走直接修复会话，不再串联复杂任务。

产出：

- Issue 创建/详情页启动 `AgentSession(kind=issue)`
- issue fix prompt
- issue verify/report command
- 显式 `upgrade_to_goal`

验收：

- 修 Issue 不创建 goal_run。
- 用户显式升级后才进入 Goal DAG。

### Step 5：UI 分层落地

目标：把页面拆成公共 session 层和类型页面层。

产出：

- `ContextBar`
- `CommandBar`
- `AgentSessionPanel`
- Goal/Issue/Assistant 各自只保留类型特有内容
- `LobsterPage` 复用公共 session 组件

验收：

- 三类页面操作一致但语义不同。
- Issue 页面不 import 复杂任务启动组件。
- 龙虾页面不自建第二套聊天和命令系统。

### Step 6：清理耦合和命名债

目标：去掉产品层 Task 泛化。

产出：

- UI 文案收敛：复杂任务叫 Goal/任务，内部队列不暴露为任务。
- 删除旧的 issue -> task 隐式链路。
- 补充架构文档和 memory。

验收：

- grep 不再出现 Issue 默认启动 Goal/Task 的路径。
- 旧功能可通过显式升级保留。

### Step 7：OpenClaw runtime/channel/automation

目标：把 OpenClaw 接成可见、可控、可分发任务的运行时通道。

产出：

- `OpenClawConnector`
- `ChannelSurface(provider=openclaw)`
- 工作区“龙虾”入口
- OpenClaw conversation projection
- OpenClaw automation source sync
- 对话分发为 Goal / Issue / Assistant

验收：

- OpenClaw 可以作为 runtime provider 出现在运行时列表。
- 龙虾页面可以看到 OpenClaw 对话历史。
- 当前 OpenClaw 对话可以显式分发成 Goal、Issue 或 Assistant。
- 自动化区可以看到“龙虾频道”，并能管理 OpenClaw 原生定时任务。

## §10 风险和防线

### 风险 1：一次性改太大

防线：先加 adapter 和公共服务，不立刻删旧入口。每一步都能独立验收。

### 风险 2：内部 task_queue 和产品 Task 混淆

防线：文档和类型明确：

- `TaskQueueJob`是内部执行单位。
- `Goal`才是产品里的复杂任务。

### 风险 3：Prompt 隐式行为继续扩散

防线：所有 prompt 输入统一来自 `RuntimeContextSnapshot`，按钮不能私自拼隐形 prompt。

### 风险 4：Issue 用户路径变长

防线：Issue 默认提供“开始修复”一键 direct session；升级 Goal 是高级入口，不打扰主路径。

### 风险 5：Desktop API 兼容

防线：新增字段走可选解析，旧 desktop 不因缺字段白屏。API response 保持兼容。

### 风险 6：OpenClaw 被错误建模成第四种任务类型

防线：文档和类型明确：

- `goal / issue / assistant` 才是 WorkItem 类型。
- `openclaw` 是 runtime provider、channel provider 和 automation source。
- 龙虾入口只负责通道聚合和任务分发，不拥有新的任务生命周期。

### 风险 7：同步 OpenClaw 数据导致双写漂移

防线：OpenClaw 原生数据为主事实。本系统只保存索引、映射、快照和必要缓存；管理动作必须写回 OpenClaw connector。

## §11 当前实现状态矩阵

这份任务不是“OpenClaw 页面做完就结束”。OpenClaw 是纵切验证样例，整体架构还要按 `WorkItem -> AgentSession -> RuntimeRun -> TaskQueueJob` 的边界继续迁移。

| 主题 | 当前状态 | 已落地证据 | 剩余缺口 |
|---|---|---|---|
| OpenClaw runtime | 部分落地 | 已有 `server/pkg/agent/openclaw.go`，运行时列表已能识别 openclaw provider | 还需要纳入统一 `RuntimeRun` 和 `RuntimeContextSnapshot` |
| Lobster channel | 部分落地 | 已有 `packages/views/lobster/`、`apps/web/.../lobster/`、`server/internal/handler/openclaw_channel.go` | 还需要复用统一 `AgentSessionPanel / CommandBar / ContextBar`，不要自建第二套会话层 |
| OpenClaw automations | 部分落地 | 自动化页已有来源筛选和 openclaw API 调用 | 暂停、恢复、编辑、删除必须证明写回 OpenClaw 原生接口，而不是只改本地投影 |
| WorkItemKind | 未完整落地 | 文档和部分类型已描述 `goal / issue / assistant` | 需要统一 core/server adapter，明确 OpenClaw 不是第四种 WorkItem |
| AgentSession | 未完整落地 | 现有 `chat_session`、`goal_run`、`task_message` 可作为迁移底座 | 需要显式 session 层承载消息、命令、运行状态和 context snapshot |
| RuntimeContext 注入 | 部分落地 | daemon prompt 已有 goal/issue/chat 上下文字段 | 仍需统一 builder；按钮和页面不能各自拼 prompt |
| SessionCommand | 未完整落地 | 现有页面已有 retry/interrupt/continue 等散落实现 | 需要 registry，把 prompt shortcut、runtime control、workflow transition、automation control 分开 |
| Issue direct session | 未完整落地 | Issue 页面已有执行日志和 live card | 需要确保默认修复不创建 `goal_run`，只在显式升级时进入 Goal |
| 日志和证据 | 部分落地 | `task_message`、`GET /api/tasks/{taskId}/messages`、WS task message cache 已存在 | 需要把 UI transcript、API、DB、server log、computer-use trace 串成统一验收证据链 |

判断标准很简单：如果一个入口只能在自己的页面里跑通，但不能用统一 session/run/context/command 解释，它就是临时实现，不算架构落地。

## §12 E2E 验证策略

端到端验证不是“点一下页面看起来能打开”。本任务的验证目标是证明四件事：

1. 三类入口的语义没有串：Goal 仍是复杂任务，Issue 默认 direct session，Assistant 仍是对话。
2. 操作按钮是统一命令：retry 是可见消息，interrupt 是 run control，OpenClaw 自动化是 external source control。
3. prompt 里能看到运行环境：`work_item_kind`、工作目录、runtime、agent、自定义上下文和 `channel_context`。
4. 证据链能闭环：UI timeline、API task messages、DB rows、server log、computer-use trace 能互相对上。

执行验证时必须遵守自举隔离：

```text
stable control plane -> candidate worktree -> candidate desktop -> computer-use trace
```

- 控制平面只负责派发、观察和记录。
- 代码修改、server/daemon/desktop 启停只发生在候选 worktree。
- 桌面端验证只用 `computer-use-harness`，不要用浏览器或 Playwright 替代 Electron 实机。
- 候选 desktop 使用独立 `DESKTOP_APP_SUFFIX`、`DESKTOP_RENDERER_PORT` 和 Electron userData，不能抢当前正在使用的主控界面。

具体 case 写在 [`e2e-verification-cases.md`](./e2e-verification-cases.md)。执行 Agent 不应该临时发挥；按 case 跑，按证据回填。

## §13 日志和证据模型

本任务的日志验证按五层取证，不要只看一层：

| 层级 | 证据 | 用途 |
|---|---|---|
| UI | `TaskTranscript`、Issue execution log、Goal stream、Lobster timeline | 证明用户可见结果正确 |
| API | `GET /api/tasks/{taskId}/messages`、OpenClaw channel API、automation API | 证明前端看到的不是假状态 |
| DB | `task_message`、`agent_task_queue`、`goal_run`、`chat_session`、Issue 相关表 | 证明执行链路和状态机正确 |
| Server/daemon log | server log、daemon log、runtime 子进程 stderr/stdout | 定位 prompt、claim、runtime、connector 问题 |
| Desktop trace | `.computer-use/traces/trace_action_*.jsonl` | 证明真实 Electron UI 被操作和观察过 |

最小日志查询契约：

```sql
SELECT id, task_id, seq, type, tool, content, input, output, created_at
FROM task_message
WHERE task_id = '<task-id>'
ORDER BY seq ASC;
```

```sql
SELECT id, status, issue_id, chat_session_id, goal_subtask_id, created_at, completed_at
FROM agent_task_queue
WHERE id = '<task-id>' OR issue_id = '<issue-id>' OR chat_session_id = '<session-id>'
ORDER BY created_at DESC;
```

如果验收点涉及 Issue direct session，还必须证明默认路径没有创建 `goal_run`：

```sql
SELECT id, goal, status, issue_id, created_at
FROM goal_run
WHERE issue_id = '<issue-id>';
```

期望结果：默认 direct fix 为 0 行；只有显式 `upgrade_to_goal` 后才出现对应记录。

如果验收点涉及 OpenClaw/Lobster，还必须证明通道上下文进入 prompt 或运行消息：

```text
<channel_context>
provider: openclaw
channel: lobster
external_conversation_id: ...
external_message_id: ...
</channel_context>
```

坏验收的典型样子：

- 只说“页面能打开”，没有 trace。
- 只看 DB 有 task，没有 UI transcript。
- 只看 UI 有消息，没有 API/DB 对应行。
- 只验证 OpenClaw 页面，不验证它没有变成第四种 WorkItem。
- 只点了 retry，没有证明它追加了可见用户消息。
