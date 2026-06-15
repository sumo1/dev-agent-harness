# 设计增量：Issue 详情快捷操作 + 补「执行错误」态

> 所属任务: 260612-issue-github-autofix（对已落地的 Issue 三栏页的增强）
> 上游: [`requirement.md`](./requirement.md) §3 三态 + §4 合并 ｜ [`design.md`](./design.md) §1 决策 B
> 本文定: ① 补 failed/执行错误态（历史方案漏的一态，是真 bug）② 详情区快捷操作（预置会话→编辑→派给 agent）
> 本轮只设计，未执行。

## §0 检索结论（对照历史方案）

`requirement.md §3` 当初只列了三态：**完成 / 未启动 / 需要补充信息**。`design.md 决策 B` 把它们
映射到 `not_started / completed / needs_info`，外加一个隐含的 `running`。**漏了"执行错误"**——
而 goal_run 的 DB 枚举里 `failed`、`cancelled` 是存在的终态。

实测 `deriveAutofixStatus`：`goal_run.status === "failed"` 落进 default 分支 → **UI 显示成"进行中"**。
失败的修复看起来跟还在跑一样。这是历史方案的缺口，本轮补上（呼应你口述的"执行错误"态）。

## §1 状态：四态 → 五态

| 产品态 | 底层来源 | 列表点颜色 | 详情 banner |
|--------|---------|-----------|------------|
| 未启动 not_started | 无 goal_run | 灰 `muted` | 中性 |
| 进行中 running | planning/executing | 蓝 `primary` | 蓝 |
| 完成 completed | `completed` | 绿 `success` | 绿 + PR 链接 |
| 需要补充信息 needs_info | `partial` + needs_info_reason | **琥珀 `warning`** | 琥珀 + 原因 |
| **执行错误 failed**（新） | `failed` / `cancelled` | **红 `destructive`** | 红 + failure_reason |

> **颜色重排**：现在 needs_info 占用了红色。需补充信息是"等人给料"不是"坏了"，应是**琥珀/警告**色；
> **红色让给真正的执行错误 failed**。语义和颜色一一对应，5 态可辨。

### 改动点（小、纯前端，零迁移）

1. `packages/core/issues/autofix.ts`：
   - `AutofixStatus` union 加 `| { state: "failed"; reason?: string }`。
   - `deriveAutofixStatus`：`goalRun.status === "failed" || "cancelled"` → `{ state: "failed", reason: goalRun.failure_reason }`。
     （`failure_reason` 已在 goal_run 响应里——`CompleteGoalRun` 写的那个字段。）
2. `autofix-issues-page.tsx`：`AutofixDot` 颜色映射加 failed=红、needs_info 改琥珀；
   `AutofixStateBanner` 加 failed 分支（红 + reason）。
3. i18n `autofix_page.state` 4 locale 各加 `failed` key（"执行错误 / Failed"）。
4. 单测：`deriveAutofixStatus` 加 failed/cancelled 用例（之前会错判成 running，是回归守卫）。

### 列表可见性（你问的"怎么看到 AC 状态"）

列表行现在是**纯色点 + hover title**。增强：点旁边加一个**极简文字标签**（仅在非 running 时显示，
running/not_started 留点即可，避免噪声），或保持点但把 5 态颜色做准。建议：**点 + 选中行显示文字**，
未选中只点（列表紧凑）。详情区始终有完整 banner。

## §2 详情区快捷操作（预置会话 → 编辑 → 派给 agent）

### 核心机制：零新后端端点 ⭐

`StartAutofixGoalRun` 创建修复 run 时**已经开了一个 discussion 会话**并把 `chat_session_id`
写回 goal_run（`SetGoalRunChatSession`）。所以：

```
issue.metadata.autofix.latest_goal_run_id
  → goalRunOptions 拿到 goalRun（响应含 chat_session_id）
  → api.sendChatMessage(goalRun.chat_session_id, <预置且用户编辑过的会话>)
  → 该会话绑的是 squad leader / 总控 PMO
  → 总控收到 → 决定下一步（重规划/派子任务/开 PR/补信息）
```

**复用现有 `sendChatMessage`，零新端点、零新机制。** 完全贴合你"把会话带给模型、让模型决定"。
agent 在它环境里用 `gh`/`git`/`multica repo checkout` 决定怎么做——multica 只编排、不碰 repo、不写 main
（贴合 requirement 决策 2/3）。

> 派发后给一个 toast + "去助理页看进展"的跳转（复用现有 jump-to-assistant）。

### 交互形态

详情区三态 banner 下方加一行**「快捷操作」**按钮。点任一按钮 → **内联展开一个预填 textarea
（可编辑）+ 「派发」按钮**（不弹模态，跟内联新建一致）。编辑确认 → sendChatMessage → 收起。

```
┌─ 选中 issue 详情 ───────────────────────────┐
│ MUL-14  登录按钮点击无反应                    │
│ [指派] [项目]                                 │
│ ┌─ 🔴 执行错误 ───────────────────────────┐ │
│ │ 编译失败：handler 缺少 import            │ │
│ └─────────────────────────────────────────┘ │
│ 快捷操作:  [重试修复] [补充信息] [新建工作树] │
│            [完成 issue] [自由跟进]            │
│ ┌─ (点"重试修复"后内联展开) ──────────────┐ │
│ │ 重新尝试修复这个问题：上次编译失败，     │ │ ← 预置文本,可编辑
│ │ 请先修正 handler 的 import 再继续。      │ │
│ │                            [取消] [派发] │ │
│ └─────────────────────────────────────────┘ │
│ [图片] [描述]                                 │
└───────────────────────────────────────────────┘
```

### 预置按钮集（名称待调，先定语义 + 预置文本 + 适用态）

| 按钮 | 预置会话（可编辑） | 主要适用态 |
|------|------------------|-----------|
| **重试修复** | "重新尝试修复这个问题。{失败原因/上次结果}，请据此调整后再跑一遍。" | failed / needs_info |
| **补充信息** | "补充以下信息帮助你复现/修复：\n（在这里写复现步骤、环境、期望行为…）" | needs_info |
| **新建工作树** | "为这个问题在本仓库新建一个独立的工作树/分支来修复（按本仓库约定）。" | 任意（你明确要的） |
| **完成 issue** ⭐ | "我确认这个 issue 已经修复完成。请按本仓库约定收尾——你来判断走 PR（推分支+开 PR 合入主干）还是其它合适的方式，把改动落定。" | running / completed / 任意 |
| **自由跟进** | （空白）用户自己写任意意图。 | 任意 |

- **「完成 issue」= 用户的确认信号，收尾方式交给模型** ⭐（你的明确要求）：点它表示"我确认这个 issue
  完成了"，预置一段"确认完成、请收尾"的会话派给 agent，**由 agent 判断走 PR 还是别的方式**落定改动。
  multica 不直接写 main、不写死"必须开 PR"——只把确认信号 + 意图带给模型（贴合决策 2/3）。
  替代了原稿里的"提交/合并"按钮——更贴合"我确认完成"的语义，且把"怎么合"完全交给模型。
- **预置文本带上下文**：failed→塞 `failure_reason`，needs_info→塞 `needs_info_reason`，让用户编辑前就有料。
- **无 goal_run 时**（not_started）：快捷操作整体灰掉 + 提示"先指派 agent + 绑工作目录以启动修复"
  （因为没有会话可派）。

### 改动点

1. `autofix-issues-page.tsx`：`IssueDetailColumn` 加 `QuickActions` 子组件——按钮行 + 内联编辑框。
   预置文本按 status 计算（带 reason）。派发调 `api.sendChatMessage(goalRun.chat_session_id, text)`。
2. `packages/core/chat/`：若 `sendChatMessage` 还没有 mutation 封装，加一个 `useSendChatMessage`
   （现在是 usage-site 直接调 api）；或直接调 `api.sendChatMessage`（更省）。
3. i18n：`autofix_page.quick_actions.*`（按钮名 + 预置模板 + 派发/取消/toast）4 locale。
4. 派发成功后 toast +（可选）自动跳助理页该会话。

## §3 破坏性风险

1. **颜色语义迁移**：needs_info 从红→琥珀，已有用户对"红=要注意"的认知不变（failed 接红）。纯样式。
2. **预置文本是模板不是写死指令**：只是填进可编辑框，用户能改，最终由 agent 决定——不违背"模型决定"。
3. **chat_session 可能不存在**：老 goal_run 若没 discussion 会话（理论上 StartAutofixGoalRun 都建了），
   `chat_session_id` 为空时快捷操作灰掉，不报错（fail-soft）。
4. **API 兼容**：goalRun 响应的 `chat_session_id` / `failure_reason` 按可选读，缺失降级。

## §4 本轮不做

- 不真做"直接 merge main"动作（走 agent 派会话）。
- 不加 goal_run 新枚举（failed 已存在，只是前端没渲染）。
- 不为快捷操作建新后端端点（复用 sendChatMessage）。
- 不碰 worktree 后端逻辑（agent 用现有 `multica repo checkout` 自行决定）。
