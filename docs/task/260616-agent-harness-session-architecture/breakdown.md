# 拆解：Agent Harness 会话架构重分层

> 上游：[`requirement.md`](./requirement.md)、[`design.md`](./design.md)
> 目标：把架构改造拆成可以逐步执行、逐步验收的步骤。

## 依赖图

```text
S1 领域模型和命名边界
  │
  ▼
S2 RuntimeContext 注入
  │
  ├───────────────┐
  ▼               ▼
S3 统一命令模型   S4 Issue direct session
  │               │
  └───────┬───────┘
          ▼
S5 UI 分层落地
          │
          ▼
S6 清理耦合和命名债
          │
          ▼
S7 OpenClaw 通道
```

## 串并行判断

| 步骤 | 是否可并行 | 原因 |
|---|---|---|
| S1 | 串行地基 | 后续类型和服务都依赖它 |
| S2 | 串行 | prompt/context 是命令和 issue direct session 的共同输入 |
| S3/S4 | 可部分并行 | S3 主要做命令抽象，S4 主要做 Issue 路径；都依赖 S2，但文件边界需要执行前再确认 |
| S5 | 串行 | 需要 S3/S4 的稳定接口，并为外部通道页面预留公共 session 组件 |
| S6 | 串行收尾 | 需要核心新路径验证后再删旧耦合 |
| S7 | 串行落地 | OpenClaw 要复用 S5 的 ContextBar / CommandBar / AgentSessionPanel，避免自建第二套会话 UI；执行前确认 S6 没有遗留 Issue -> Task 隐式链路 |

## Step 摘要

### S1：领域模型和命名边界

建立 `WorkItemKind / AgentSession / RuntimeRun / RuntimeContext / SessionCommand` 的类型和服务边界。短期不要求重建数据库模型，可以先用 adapter 映射现有 `goal_run / issue / chat_session / task_queue`。

产出文件：[`plan/step-1-domain-model.md`](./plan/step-1-domain-model.md)

### S2：RuntimeContext 注入

统一收敛 prompt 输入来源，让模型每次运行都知道 work item 类型、工作目录、runtime、自定义上下文。Context snapshot 必须可显示、可审计。

产出文件：[`plan/step-2-runtime-context.md`](./plan/step-2-runtime-context.md)

### S3：统一命令模型

把 `retry / continue / interrupt / cancel / verify` 从页面私有逻辑里抽出来。按钮分为 prompt shortcut、runtime control、workflow transition。

产出文件：[`plan/step-3-session-commands.md`](./plan/step-3-session-commands.md)

### S4：Issue direct session

Issue 默认进入直接修复会话，不再隐式创建复杂任务。只有用户点击“升级为复杂任务”时才创建 Goal。

产出文件：[`plan/step-4-issue-direct-session.md`](./plan/step-4-issue-direct-session.md)

### S5：UI 分层落地

抽出 `ContextBar / CommandBar / AgentSessionPanel`，Goal、Issue、Assistant 页面只保留各自类型特有内容。

产出文件：[`plan/step-5-ui-composition.md`](./plan/step-5-ui-composition.md)

### S6：清理耦合和命名债

删除旧的 Issue -> Task 隐式链路，清理产品层 Task 泛化文案，补充 docs/memory。

产出文件：[`plan/step-6-cleanup.md`](./plan/step-6-cleanup.md)

### S7：OpenClaw runtime/channel/automation

把 OpenClaw 接成运行时通道：工作区增加“龙虾”入口，展示 OpenClaw 对话历史，支持从对话分发为 Goal / Issue / Assistant，并在自动化区同步和管理 OpenClaw 原生定时任务。

产出文件：[`plan/step-7-openclaw-runtime-channel.md`](./plan/step-7-openclaw-runtime-channel.md)

## 验收总标准

- `Issue`默认处理路径不创建 `goal_run`。
- `Goal`仍保留 DAG、子任务、验证节点和 summary 能力。
- `Assistant`仍是开放式会话，不被任务模式污染。
- 三类入口都能看到同一套中断、重试、继续能力。
- `retry`表现为追加显式消息，不隐藏重建流程。
- 模型 prompt 中明确包含 `work_item_kind` 和运行环境。
- UI 顶部显示的上下文和 prompt 注入的上下文一致。
- OpenClaw 作为 runtime provider/channel provider 接入，不变成第四种 WorkItem。
- “龙虾”页面能展示 OpenClaw 对话历史，并能把对话显式分发为 Goal / Issue / Assistant。
- 自动化区能看到“龙虾频道”，并通过 OpenClaw 原生接口管理定时任务。
