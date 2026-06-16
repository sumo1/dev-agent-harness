---
name: issue-detail-closed-loop-and-retry-reruns
description: issue 详情做成闭环面（内嵌 goal_run 回复+执行流+输入框，助理页退化成记录）；并修「重试修复」——planning-failed 时它原本只发条死会话消息，改成重新发起 autofix（起新 run）。附：执行错误根因=LLM socket 瞬时中断非代码 bug
metadata:
  type: project
---

## 背景：用户两条诉求
1. "issue 详情看不到回复，我希望能看到回复，这里闭环所有问题；助理里的聊天只是记录"。
   → 推翻早先 fork 的取舍（"执行输出只在助理页、issue 详情不嵌"）。
2. "上一个显示执行错误，查根因并修"。

## ② 执行错误根因（查清了，非代码 bug）
EET-3 `goal_run.failure_reason = "planning failed: agent_error"`，planning task 第 9 条消息
`API Error: The socket connection was closed unexpectedly`。**LLM API 调用时 socket 瞬时中断**，
不是 multica 逻辑错误。正确动作 = 重跑。

## ② 顺带发现并修的真 bug：「重试修复」对 planning-failed 无效
旧「重试修复」走 `sendChatMessage` 派给 goal_run 的 discussion 会话。但：
- planning 阶段就 failed → 根本没有子任务可 RetrySubtask；
- 给一个已 failed 的 goal_run 会话发条聊天，goal_run 状态不变、规划不会重跑 → **点了等于没用**。

`StartAutofixGoalRun` 每次都**新建**一个 goal_run（metadata.goal_run_ids append）。所以修法：
QuickActions 的 `openAction`，当 `key==="retry" && status.state==="failed"` → 调 `startAutofix.mutate()`
（POST /api/issues/{id}/autofix，起全新 run），而不是开聊天框。其它态的 retry 仍是发会话。

## ① issue 详情闭环（内嵌执行流+回复+输入）
`IssueDetailColumn` 重构成 flex 列：固定头部（identity+pickers+banner+QuickActions）+ 主体。
主体当有 goal_run.chat_session_id 时渲染 `IssueConversation`：
- goal_run 的 discussion 消息（agent 回复）+ planning/summary `TaskStream` 经 `timelineInsert` 插在
  confirm 锚点（和任务页/助理页同模型）；
- 顶部「执行 N/N」状态树 popover（复用 `GoalStatusTree`，只读），点子任务 → `SubtaskStream`；
- 底部 pinned `ChatInput`，直接在 issue 详情发消息。
无 run 时仍显示静态描述/图片。「打开助理会话」降级为 ghost 次要按钮（助理页 = 记录/镜像）。
新增 i18n `autofix_page.execution`。

## 实机验证
- ②：EET-3(failed) 点「重试修复」→ POST /autofix 202 + 日志 `autofix goal run started`(新 run) +
  goal_run_ids 1→2 ✅（旧行为是发死消息，零效果）。
- ①：issue 详情显示 执行错误 banner + 内嵌执行流(Bash/契约内容) + 输入框「告诉…该做什么」+ 快捷操作 ✅。
- views typecheck、page 测试 15 项（更新了 jump 按钮"仅有会话时显示"的两条断言 + 给 goalRun fixture 补 subtasks）、
  parity、assistant 测试全过。清理了重试 spawn 的 run。

## 教训
- **「重试」语义要看失败发生在哪**：planning-failed 没有子任务可重试，必须重起整个 run；只有
  subtask-level failure 才适合 RetrySubtask / 发会话。别把"重试"一刀切成发聊天。
- **闭环面优先**：用户要在出问题的地方直接解决（issue 详情），而不是被导去另一个页面。把执行流/回复/
  输入搬到 issue 详情，助理页留作记录。复用 ChatMessageList+TaskStream+GoalStatusTree+ChatInput，零后端改动。
- LLM socket/网络瞬时中断会冒成 `agent_error` → goal_run failed，这类靠重跑，不要去改代码。

关联：[[autofix-execution-output-visible-in-assistant]]（助理页那侧，本轮把主战场移到 issue 详情）、
[[start-fix-button-always-visible-with-guide]]、[[quick-action-context-and-overflow]]、[[desktop-is-the-target-end]]。
