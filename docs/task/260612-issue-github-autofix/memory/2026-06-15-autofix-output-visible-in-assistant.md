---
name: autofix-execution-output-visible-in-assistant
description: 修"issue 的 autofix 执行输出哪都看不到"——issue→打开助理会话只显示聊天，不渲染 goal_run 的规划/子任务执行流。给助理页接通：goal_run 会话渲染状态树+TaskStream，复用任务页组件，零后端改动
metadata:
  type: project
---

## 现象
issue autofix 跑起来后，「打开助理会话」跳到助理页能看到那个会话，但**看不到任何执行输出**
（PMO 规划流、子任务的 thinking/工具/结果）。

## 根因
助理页拿到 `?goal_run_id=` 后**只把它解析成 chat_session_id 就丢掉 goal_run 上下文**，
右栏只渲染 `ChatMessageList`(chat_messages)，从不渲染 task_messages 执行流。
任务页做对了（GoalStatusTree + TaskStream + timelineInsert），助理页没接。
后端数据全现成：`enrichGoalResponse` 早就暴露 `planning_task_id` / `summary_task_id` /
每个 subtask 的 `task_id`，task_messages 也在。**纯前端缺渲染。**

## 修法（接通助理页，零后端改动，复用现成组件）
`assistant-page.tsx`：
- 保留 `locatorGoalRun`（原来 fetch 了却丢弃）。当 **active session === locatorGoalRun.chat_session_id**
  时进入"goal 执行视图"（即 issue→助理跳转那条流）。
- `GoalExecutionHeader`：顶部进度 chip → Popover 打开复用的 `GoalStatusTree`（**只读，不传 intervene**，
  助理页是查看器；要干预去任务页）。点子任务 → 切到该子任务的流。
- 内容区：选了子任务 → `SubtaskStreamView`(标题+spec+`TaskStream`)；否则 `ChatMessageList` 用
  `timelineInsert` 在 confirmed_at 锚点把 planning + summary 的 `TaskStream` 插进聊天（和任务页一模一样）。
- 复用导出组件：`GoalStatusTree`(assistant/)、`TaskStream`(tasks/)。`SubtaskOutput` 是 tasks-page 私有，
  没导出 → 自己写了个精简 `SubtaskStreamView`（标题+spec+failure+TaskStream），不为复用硬抽。
- i18n 复用 chat 命名空间已有的 `task_page.{status_tree,planning_hint,summarizing}`。

## 约束/注意
- `ChatSession` 不带 `goal_run_id`（type + server 响应都没有）→ 没法对任意选中会话解析 goal_run。
  所以本轮只覆盖"从 issue 跳进来(带 goal_run_id 参数)"这条流；用户在助理页自己点别的会话不触发执行视图。
  够用（这正是用户的场景）；要全覆盖得给 ChatSession 加 goal_run_id（下轮）。

## 实机验证
EET-3「今天几号」autofix run(failed)，planning task 有 9 条真实 task_messages（PMO 读项目契约 + Bash
工具调用 + 结果 + 末尾 API Error）。点「打开助理会话」→ 助理页显示「任务拆解」状态树 chip +
planning 流内容（"I have the project's contract dialect…API Error: socket closed"）——以前完全不可见。
views typecheck + assistant-page 测试通过。无 seed 数据，无需清理。

关联：[[execution-output-visibility]]（任务页 ④ 流的原始实现）、[[quick-actions-and-failed-state-landed]]、
[[which-model-ran-it-attribution]]、[[desktop-is-the-target-end]]。
