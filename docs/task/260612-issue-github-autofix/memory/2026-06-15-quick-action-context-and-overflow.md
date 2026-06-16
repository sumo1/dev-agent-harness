---
name: quick-action-loses-context-and-stream-overflow
description: 修两处——① issue 快捷操作("重试修复"等)派会话给 goal_run 会话后 agent 丢失 issue 上下文(只在 discussion 态注入 goal，跑起来后就没了)；② 执行流的工具结果行 truncate 缺 min-w-0 横向溢出被切
metadata:
  type: project
---

## 问题①：丢失历史上下文（关键 bug）
issue 快捷操作把预置文本（"重新尝试修复这个问题{reason}"）经 `sendChatMessage` 派给 goal_run 的
discussion 会话。但 agent 收到后说"没有具体 issue 或上下文"，去 list issues 瞎找。

**根因**：daemon chat-claim 的 goal 上下文注入有 `run.Status == "discussion"` 硬条件
（`daemon.go` ~1410）。autofix 修复跑起来后 goal_run 早是 planning/executing/**failed**，条件不满足 →
`GoalDiscussionActive=false` → prompt 里**零 goal/issue 上下文**，只有空泛的用户消息。
预置文本本身也不含 issue 标题/描述（只有通用模板 + reason）。

**修法（后端，上下文从 server 流向 agent，不在前端预烘焙）**：
- daemon chat-claim：只要 `cs.GoalRunID.Valid` 就**无条件**填 `GoalContextTitle`/`GoalContextGoal`
  （= goal.title / goal.goal，autofix 的 goal = issue 标题+描述）；`discussion` 态再额外叠加
  facilitation 框架。新字段加在 daemon types.go + handler agent.go + daemon.go claim。
- `buildChatPrompt`：takeover 之后、discussion 之前插一个 `## Task context` 块（标题+goal/problem +
  "act on THIS goal, don't go looking for which issue"）；discussion 态由 facilitation 块独占 goal 文本，
  不重复渲染。单测 `TestBuildChatPromptGoalContext`（注入 + discussion 不重复 + 普通聊天无此块）。
- 所有快捷操作(retry/needs_info/complete/freeform)自动受益，预置文本保持精简。

## 问题②：内容显示不全（横向溢出被切）
助理页执行流(TimelineView)里 Bash 工具调用/结果的折叠摘要行用 `truncate` 的 span，但
**flex 父容器没 min-w-0** → flex item 不允许缩到内容宽度以下 → 整行撑爆容器、右侧被切。
修：`timeline-view.tsx` 两处摘要 span 加 `min-w-0`（ToolUseRow summary + ToolResultRow label/preview）。
经典 flexbox truncate 必备。

## 验证
- go build+vet、daemon prompt 测试（含新 GoalContext 用例）、views typecheck + timeline/assistant 测试全过。
- 实机：往 EET-3(failed goal_run, goal="今天几号…") 的会话发"重新尝试修复" → live daemon 真领真跑；
  agent 的首条消息已**按名引用 EET-3 + autofix 记录**（来自注入的 Task context，用户消息里没有这些）。
  注意：EET-3 的 goal 是个问句不是真 bug，所以 agent 合理地判断"没东西可修"——是测试数据差，不是上下文没传到。
  上下文注入本身由构造 + 单测确证。
- 清理了实机发的测试消息/任务/token。

## 问题③：助理页详情滚动失效（紧接着②同轮报的）
我给助理页加执行流时，把 ChatMessageList 包进了 `<div className="flex-1 min-h-0 overflow-hidden">`
——**一个普通 block，不是 flex**。但 ChatMessageList 的滚动根是 `flex-1 overflow-y-auto`，只有当父级是
**flex column** 时它的 `flex-1` 才拿到有界高度 → 才能滚。普通 block 父级 → flex-1 不解析 → 无界高度 →
overflow-y-auto 没东西可滚 → **滚动死**。改前它原本是 flex 列的直接子项才好使，我插了一层 block 破坏了链。
修：wrapper 改 `flex min-h-0 flex-1 flex-col overflow-hidden`。

## 教训
- **goal 上下文注入别绑死在 discussion 态**：autofix / 后续跟进发生在 goal 离开 discussion 之后，
  那时才最需要上下文。任何"会话绑了 goal"的消息都应携带 goal 上下文。
- **truncate 必须配 min-w-0**：flex 子项默认 min-width:auto，truncate 不生效反而撑爆父容器。
- **在滚动容器外面插中间层，那层必须保持 flex 链**：ChatMessageList(flex-1 overflow-y-auto) 依赖父级是
  flex column + min-h-0。中间插一个普通 block 就把高度链断了、滚动失效。插层时照搬 `flex min-h-0 flex-col`。

关联：[[autofix-execution-output-visible-in-assistant]]、[[quick-actions-and-failed-state-landed]]、
[[default-chat-agent-agentless-session]]、[[desktop-is-the-target-end]]。
