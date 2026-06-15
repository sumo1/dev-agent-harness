---
name: quick-actions-and-failed-state-landed
description: Issue 详情快捷操作（预置会话→编辑→派给 agent）+ 补 failed/执行错误态已落地+桌面端实机验证。零新后端端点（复用 sendChatMessage 到 goal_run 的 discussion 会话）
metadata:
  type: project
---

## 做了什么（Q1–Q3，全绿，实机验证）

### Q1 补 failed/执行错误态（历史方案漏的一态，是真 bug）
`requirement §3` 只规划了三态，`deriveAutofixStatus` 把 goal_run 的 **`failed`/`cancelled` 落进 default
→ UI 显示成"进行中"**。失败的修复看起来跟还在跑一样。
- `AutofixStatus` 加 `{state:"failed";reason}`；`failed`/`cancelled` → failed 态。
- **reason 取第一个 failed 子任务的 `failure_reason`**——GoalRun 响应没有 run 级 failure_reason，但带 subtasks。
- 五态颜色重排：**needs_info 从红改琥珀**（"等人给料"不是"坏了"），**红色让给真错误 failed**。
- 签名放宽：`goalRun?: Pick<...,"status"> & Partial<Pick<...,"subtasks">>`，老调用只传 status 仍 typecheck。

### Q2 快捷操作（预置会话→编辑→派给 agent）⭐ 零新后端端点
详情区 banner 下 `QuickActions`：5 按钮（重试修复/补充信息/新建工作树/**完成 issue**/自由跟进）
→ 点按钮内联展开**预填 + 可编辑 textarea** → 派发。
- **机制**：`StartAutofixGoalRun` 建 run 时已开 discussion 会话并把 `chat_session_id` 写回 goal_run。
  派发 = `api.sendChatMessage(goalRun.chat_session_id, 编辑后的文本)` → 总控 PMO 收到 → 决定下一步。
  **复用现有 sendChatMessage，零新端点、零新机制。**
- **「完成 issue」**（用户明确要的）= 确认完成信号，预置"我确认完成，请收尾，**你判断走 PR 还是其它方式**"，
  **由模型决定怎么合**。multica 不直接写 main、不写死必须开 PR（贴合 requirement 决策 2/3）。
- 预置文本带上下文：failed/needs_info 把 reason 织进"重试修复"的预置里。
- 无 chat_session（理论上都有）→ 灰掉 + 提示，fail-soft。

### 实机验证（桌面端 computer-use + 活库）
- seed 一个 failed goal_run（带失败子任务 reason）+ discussion 会话 + 关联 issue。
- 详情区：**执行错误 banner（红）+ 失败原因「编译失败：handler 缺少 import」+ 快捷操作 5 按钮** 全部 ✅。
- 点「完成 issue」→ 内联展开预填 textarea，内容正是「我确认这个 issue 已修复完成…你来判断走 PR…」+ 派发/取消 ✅。
- 派发链路（活 server）：`POST /api/chat/sessions/{chat}/messages` → 200 + message_id + task_id，
  chat_message 0→1，**enqueue 了一个 running 的 agent 任务** ✅（agent 会接走、自己决定怎么做）。

## 坑

- **桌面端 issue 列表按 status 分桶查询**（`fetchFirstPages` 对 BOARD_STATUSES 每个 status 一个
  `GET /api/issues?status=X`）。新 seed 的 issue 列表里不出现，多半是渲染进程**缓存了那个 status 桶**，
  Cmd+R 不一定刷新——**clean restart desktop 才稳**（API `?status=todo` 直接验证返回正确，确认是缓存非逻辑）。
- computer-use 在 Electron **AXPress 点击好使、键盘输入打不进**：表单"填字→提交"那一下走真实 API
  （派发按钮调的同一个 sendChatMessage）验证，UI 的点击/预填渲染/按钮用 computer-use 实机看。

## 设计文档
`docs/task/260612-issue-github-autofix/design-quick-actions.md`（§1 五态 + §2 快捷操作）。

关联：[[repo-ssot-persist-and-judgment-landed]]、[[which-model-ran-it-attribution]]、[[desktop-is-the-target-end]]、
[[desktop-e2e-found-metadata-schema-strips-autofix]]（同样是只在桌面端暴露/验证的）。
