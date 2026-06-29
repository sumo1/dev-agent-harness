# OpenClaw 集成说明

## 概述

OpenClaw 作为"龙虾"运行时通道接入 dev-agent-harness，提供外部自动化来源和运行时通道能力。

## 架构位置

OpenClaw 不是第四种 WorkItem（不与 Goal/Issue/Assistant 并列），而是作为：
- **Runtime provider** - 提供运行时执行能力
- **Channel provider** - 提供通道接入能力

## 核心组件

### 1. 通道层 (packages/core/channels/)

```typescript
// packages/core/channels/openclaw.ts
export interface OpenClawChannel {
  id: string;
  name: string;
  type: string;
  // ... 通道定义
}

// packages/core/channels/openclaw-queries.ts
// 查询接口：列出通道、获取对话历史等

// packages/core/channels/openclaw-mutations.ts
// 变更接口：创建通道、分发任务等
```

### 2. 会话命令系统 (packages/core/session-commands/)

统一的会话命令注册和调度机制：
```typescript
// packages/core/session-commands/registry.ts
export interface SessionCommand {
  id: string;
  label: string;
  action: () => Promise<void>;
  // ...
}
```

### 3. Agent 会话面板 (packages/views/common/agent-session/)

可复用的会话组件，Goal、Issue、Assistant、Lobster 都使用：
- `agent-session-panel.tsx` - 主面板
- `command-bar.tsx` - 统一命令栏
- `context-bar.tsx` - 上下文显示栏

### 4. Lobster 页面 (packages/views/lobster/)

工作区"龙虾"入口：
- 展示 OpenClaw 对话历史
- 支持从对话分发为 Goal / Issue / Assistant
- 在自动化区同步和管理 OpenClaw 原生定时任务

### 5. 后端处理器 (server/internal/handler/)

```go
// server/internal/handler/openclaw_channel.go
// OpenClaw 通道 CRUD 和 WebSocket 连接
```

## 数据流

```text
OpenClaw 外部系统
      ↓
OpenClaw Channel (通道层)
      ↓
Agent Session (会话抽象)
      ↓
WorkItem 分发 (Goal/Issue/Assistant)
      ↓
Runtime 执行 (Claude Code/Codex/...)
```

## UI 入口

### 侧边栏

- 图标：Shell (🐚)
- 路由：`/{workspaceSlug}/lobster`
- 位置：在 Assistant 和 Autopilots 之间

### 功能区域

1. **对话历史**
   - 展示 OpenClaw 的对话记录
   - 支持搜索和筛选

2. **任务分发**
   - 从对话创建 Goal（复杂任务 + DAG）
   - 从对话创建 Issue（单次修复）
   - 从对话创建 Assistant 会话（开放式对话）

3. **自动化管理**
   - 查看 OpenClaw 原生定时任务
   - 同步和管理定时任务

## 国际化

支持语言：
- 英语 (en)
- 日语 (ja)
- 韩语 (ko)
- 简体中文 (zh-Hans)

翻译文件：`packages/views/locales/{locale}/lobster.json`

## 类型定义

```typescript
// packages/core/types/openclaw-channel.ts
export interface OpenClawChannel {
  // 通道类型定义
}

// packages/core/types/agent-session.ts
export interface AgentSession {
  // 会话类型定义
}
```

## 测试

```go
// server/internal/handler/openclaw_channel_test.go
// 通道 CRUD 和 WebSocket 测试
```

## 设计原则

### 1. OpenClaw 是通道，不是 WorkItem

OpenClaw 提供运行时能力和自动化来源，但不创建新的工作项类型。所有工作最终仍是 Goal/Issue/Assistant。

### 2. 复用会话组件

`packages/views/common/agent-session/` 是共享层，避免每种页面重复实现会话 UI。

### 3. 统一命令模型

所有会话类型（Goal/Issue/Assistant/Lobster）共享同一套命令系统：
- retry
- continue
- interrupt
- cancel
- verify

### 4. 上下文可见

RuntimeContext 注入对用户可见，在 ContextBar 中展示：
- work_item_kind
- 工作目录
- runtime
- 自定义上下文

## 相关任务

详细设计和实现计划见：
- [260616-agent-harness-session-architecture](../docs/task/260616-agent-harness-session-architecture/)
  - [requirement.md](../docs/task/260616-agent-harness-session-architecture/requirement.md) - 需求原文
  - [design.md](../docs/task/260616-agent-harness-session-architecture/design.md) - 技术方案
  - [breakdown.md](../docs/task/260616-agent-harness-session-architecture/breakdown.md) - 拆解（S1-S8）
  - [plan/step-7-openclaw-runtime-channel.md](../docs/task/260616-agent-harness-session-architecture/plan/step-7-openclaw-runtime-channel.md) - OpenClaw 通道实现计划

## 后续扩展

### 短期
- OpenClaw 对话历史同步
- 从对话创建 WorkItem 的 UI
- 定时任务管理界面

### 中期
- OpenClaw 原生指令集成
- 多 OpenClaw 实例管理
- OpenClaw 到 Goal 的自动转换规则

### 长期
- OpenClaw 作为 Runtime Provider 的完整实现
- 跨运行时任务调度优化
- OpenClaw 与其他通道（如 Slack、Email）的统一抽象
