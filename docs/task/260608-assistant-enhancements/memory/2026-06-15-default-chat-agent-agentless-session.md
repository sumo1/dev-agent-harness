---
name: default-chat-agent-agentless-session
description: 助理新建会话支持"只选运行时不选智能体"——不是真无 agent(chat_session.agent_id NOT NULL 改不动)，而是 workspace 预置一个默认对话 agent(空 instructions 纯转发)，resolve-or-create 缓存在 workspace.default_chat_agent_id
metadata:
  type: project
---

## 需求与约束
用户要"助理新建会话不用选智能体，只选运行时"。但 `chat_session.agent_id` + `agent_task_queue.agent_id`
都是 **NOT NULL**，prompt 构建/任务派发/daemon claim/权限过滤全依赖 agent——真做"无 agent 会话"
是 2-3 sprint 底层重构。用户拍板走"**默认 agent**"：预置一个空 instructions 的对话 agent，不选就用它。

## 落地（零风险，底层 schema 不动）
- **迁移 117** `workspace.default_chat_agent_id`（仿 `default_planner_agent_id` migration 115，nullable，
  `ON DELETE SET NULL`）。chat_session / agent_task_queue **完全不动**。
- **`resolveOrCreateDefaultChatAgent`**（handler/chat.go）：读 workspace 缓存的 agent，存在且未归档就复用；
  否则建一个 `name="Chat"`、**`instructions=""`（纯转发）**、绑给所选 runtime 的 agent，缓存 id 到
  `workspace.default_chat_agent_id`（`SetWorkspaceDefaultChatAgent`）。一个默认 agent 服务所有 runtime——
  因为 `chat_session.runtime_id` 能 per-session 覆盖路由（migration 060）。
- **CreateChatSession**：`agent_id` 空时 → 要求 `runtime_id` → resolve-or-create 默认 agent 填进去。
  原有"选了 agent"路径不变。
- **前端**：new-session-dialog 把**运行时提为主必选**，agent 收进可折叠 `<details>`、顶部一个"直接对话
  （不绑定智能体）"选项，默认 `selectedAgentId=""`；`canCreate` 只要 runtime online。
  `api.createChatSession` / `useCreateChatSession` 的 `agent_id` 改可选。

## 坑
- **agent 表 `custom_env` NOT NULL `{}`、`custom_args` NOT NULL `[]`、`mcp_config` nullable**。
  CreateAgent 传 nil 会 23502。默认 agent 要传 `[]byte("{}")` / `[]byte("[]")` / nil。
- **给 workspace.sql 加列后，`ListWorkspaces` 的显式投影必须同步加 `default_chat_agent_id`**，
  否则 sqlc 把它从 `db.Workspace` 降级成独立的 `ListWorkspacesRow`，`workspaceToResponse(db.Workspace)`
  编译失败。（`GetWorkspace` 用 `SELECT *` 不受影响。）
- i18n 新 key（aria-label / 文案）HMR 不重载语言包（boot 时 module-import），桌面端要完整重启才显示
  正确文案；功能不受影响。

## 实机验证（活库 + 桌面端 computer-use）
- 活 API：POST /api/chat/sessions 只带 runtime_id（无 agent_id）→ 会话创建、绑默认 Chat agent；
  连发两次**复用同一个**默认 agent（idempotent，不建重复）；agent 名 Chat、instructions 空、缓存到 workspace ✅。
- 桌面端：新建会话对话框运行时为主、agent 可折叠可选；不选 agent 点「开始对话」→ 新会话绑 `Chat`
  默认 agent（`is_default_chat=t`）✅。
- 清理：删测试会话 + 默认 agent + 清 workspace 指针（下次真用会重新 resolve-or-create）。

关联：[[which-model-ran-it-attribution]]、[[task-mode]]、[[desktop-is-the-target-end]]。
