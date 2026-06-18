# step-5: UI 分层落地

> 所属任务: 260616-agent-harness-session-architecture ｜ 依赖: step-3, step-4 ｜ 并行组: 独立串行

## 施工契约（给执行 Agent）

### 范围

- 可改文件:
  - `packages/views/common/**`
  - `packages/views/tasks/**`
  - `packages/views/issues/**`
  - `packages/views/assistant/**`
  - locale 文件
- 不可改文件 / 冻结边界:
  - 不改底层调度。
  - 不做大范围视觉重绘。

### 产出清单

- `ContextBar`：展示 work item kind、workspace、project、local directory、runtime、agent、自定义上下文摘要。
- `CommandBar`：渲染 SessionCommandRegistry。
- `AgentSessionPanel`：复用消息时间线、composer、run status。
- Goal 页面只保留 DAG/成员/子任务视图。
- Issue 页面只保留 Issue 详情/复现/验证/修复会话。
- Assistant 页面只保留聊天/沉淀/转换。
- 为 Lobster/OpenClaw 这类外部通道页面预留复用公共 session 组件的组合方式。

### 约束

- 不要把 card 套 card。
- 复杂页面保留密度，不做营销式 hero。
- 文案要区分“复杂任务”“Issue”“助理会话”。

## 验收契约（给验收）

### UI 验收

- [ ] 三类页面都有 ContextBar。
- [ ] 三类页面都有 CommandBar，命令集合按类型变化。
- [ ] Issue 页面不 import Goal 复杂任务启动组件。
- [ ] Assistant 页面不出现 DAG、子任务、验证节点等 Goal 专属概念。
- [ ] 公共组件能被 Lobster 页面复用。
- [ ] 不需要为了 Lobster 页面自建第二套聊天和命令系统。

### 命令验收

| 命令 | 通过标准 |
|------|---------|
| `pnpm --filter @multica/views typecheck` | 0 error |
| 桌面 E2E | 用 computer-use-harness 验证三类页面的上下文和命令入口 |
