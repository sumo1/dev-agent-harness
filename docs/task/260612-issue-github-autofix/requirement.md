# 需求文档：Issue 录入 → GitHub Issue → 自动修复 → PR

> 任务 ID: 260612-issue-github-autofix
> 状态: **需求已对齐**（本轮只整理需求 + 任务拆解，不写代码、不做详细技术方案）
> 录入日期: 2026-06-12

## 一句话

工作区新增「Issue」菜单页：用户粘贴图片 + 一段话录入 issue，issue 推到 GitHub；
后台自动起一个修复运行（worktree 改代码 → 端到端验证 → 出 PR），三态可视；
点 issue 跳到助理页对应会话历史看执行细节。

---

## 需求原文（用户口述，整理）

1. **新建 issue**：在新建界面支持**图片粘贴上传** + 录入一段话，把内容**作为一个 GitHub issue 创建**上去。
2. **自动修复**：有一个任务**监控新增的 issue**，去**新建一个 worktree 修复它**，并进行**端到端验证**。
3. **修复任务三态**：
   - **完成**
   - **未启动**
   - **需要补充信息**（例：没有复现、没有发现问题）
4. **完成态 → 合并**：完成态时增加一个功能，可以把 worktree 的内容**合并回主干**。
5. **助理页详情**：在助理菜单的详情里，能看到**每个子任务的执行信息**。
6. **新增「Issue」工作区菜单**：
   - 删掉**现在新建 issue 的入口**。
   - 新菜单是**三栏结构**。
   - 含 issue 列表、每个 issue 的状态、新建 issue 入口。
7. **跳转**：点列表里的 issue → 跳到助理页面中**对应的会话历史**。

---

## 理解确认（已与用户对齐的决策）

### 决策 1：issue 真推到 GitHub Issues（不是 multica 口语意义的 issue）

用户明确选择「真推到 GitHub Issues」。multica 原生 issue 仍落库作为主真相，
但要在真实 GitHub 仓库里创建对应 issue。

### 决策 2：修复完成后走 GitHub PR（不直接写 main）

用户明确选择「走 GitHub PR」。multica **不直接 `git push` 到 main**——
agent 推一个分支 + 开 PR，合并交给用户在 GitHub 上点。规避了写用户主干的最高风险。

### 决策 3：GitHub 的所有写操作都由 agent 子任务完成，后端不碰 GitHub API ⭐

用户原话：「我们可以直接启动一个生成 PR 的子任务吗？最好不要使用脚本的方式去写死，
让大模型自己处理，因为大模型通常会有环境。」

这与本工程铁律完全一致：**后端永不调 LLM、server 不碰 repo；所有 AI 活和 repo 写
都是派任务给 agent，agent 在 daemon 机器上、在 repo 目录里、用它自己环境里的
`gh` / `git` 凭证干。**

因此：
- 建 GitHub issue = 派一个子任务给 agent → agent 跑 `gh issue create`。
- 出 PR = 派一个 execute 子任务 → agent 在修复用的 worktree 里 `git push` + `gh pr create`。
- **不内置 GitHub API client、不存 App 私钥、daemon 不持有 push 凭证。** 这些坑全归 agent 环境。
- 具体姿势（分支命名、PR 模板、CI 约定）让模型按本仓库既有方言自己定，**不写死脚本**
  （延续 goal_persist 的"契约是工程方言、不内置模板"思路）。

### 决策 4：三栏 = 全局 app 菜单 + 列表 + 详情

经两轮纠正后定稿的布局（与助理页 / 任务页版式一致）：

```
┌─────────┬──────────────────────────┬─────────────────────────┐
│ 全局菜单 │ issue 列表 + 新建入口     │ 选中 issue 详情          │
│ (sidebar)│ (列表顶部"新建"按钮,      │ (图片 + 描述             │
│ ·Inbox  │  点开在中栏内建,          │  + 修复运行三态状态      │
│ ·Tasks  │  不弹独立模态)            │  + 跳助理会话入口)       │
│ ·Issue ←│                          │                         │
│ ·助理   │  · MUL-12  ● 完成        │                         │
│ ·...    │  · MUL-13  ○ 未启动      │                         │
│         │  · MUL-14  ◐ 需补充信息  │                         │
└─────────┴──────────────────────────┴─────────────────────────┘
```

- **左 = 现有全局 sidebar**（`packages/views/layout/app-sidebar.tsx`），放出隐藏的「Issue」项。
- **中 = issue 列表 + 状态点 + 新建入口**。新建按钮在列表顶部，点了在中栏内联建，
  **不弹独立模态**——这就是「删掉现在新建 issue 入口」：旧的 create / quick-create
  模态触发点撤掉，统一收进这里。
- **右 = 选中 issue 详情**：图片、描述、修复运行三态、跳助理会话入口。

---

## 探查到的现状基线（写需求前的事实校准，2026-06-12）

### ✅ 已存在、可直接复用

| 能力 | 位置 | 说明 |
|------|------|------|
| issue 实体 + 创建链路 | `issue` 表、`POST /api/issues`、`server/internal/handler/issue.go` | status 枚举、polymorphic assignee、project_id 齐全 |
| 附件上传 + 拖拽 | `upload-file → attachment_ids`、`useFileDropZone`、`create-issue.tsx` | **拖拽已支持**；缺的是 paste 事件 |
| worktree 创建 | `multica repo checkout`、`server/internal/daemon/repocache/cache.go` | 每任务一条 `agent/{name}/{task-id}` 分支，跑在 daemon 机器仓库里 |
| goal 执行引擎全套 | `goal_run`（discussion→planning→executing→completed/partial/failed）、`goal_subtask` DAG、`server/internal/service/goal.go` | **正好就是"修复 + 验证 + 看执行流"** |
| 执行实时流渲染 | `TimelineView`（`packages/views/common/task-transcript/`） | 助理页 / 任务页已复用，看每个子任务执行信息 |
| GitHub webhook ingest（**单向只读**） | `server/internal/handler/github.go`、migration 079 | 处理 `installation` / `pull_request` / `check_suite`；自动 link PR↔issue、PR 合并自动推进 issue |
| 会话链路骨架 | `chat_session` 已有 `goal_run_id` / `goal_subtask_id`（migration 114/115） | 助理页能渲染会话详情 |
| project → 本地仓库 | `project_resource` resource_type=`local_directory` / `github_repo` | agent 跑在仓库目录里的地基 |

### ❌ 已确认的缺口（本任务要建的）

| 缺口 | 证据 | 影响 |
|------|------|------|
| **GitHub 写能力完全空白** | 无 App 私钥、无 `go-github`、daemon 无 push 凭证、所有 github handler 全只读 | **但按决策 3 不在 server 建**——全部走 agent 子任务，缺口被绕过 |
| issue 上无 `goal_run_id` | issue 表无此列 | issue → 修复运行 → 助理会话跳转断链，需补字段 |
| 图片**粘贴**（paste 事件） | 现仅 `useFileDropZone` 拖拽 | 录入要支持 Ctrl/Cmd+V 贴图 |
| 「Issue」三栏页 | 现有 create/quick-create 是模态，无独立列表页 | 要新建中+右两栏页面 |
| 修复运行的"需要补充信息"终态 | goal_run 状态枚举无此语义 | 要加一个由验证 agent 回报触发的终态 |
| issue → 助理会话跳转 | 助理页无"按 session id 直达"的路由参数 | 需补可路由的会话定位 |

---

## 修正后的端到端数据流

```
① 新建 issue（中栏内联：粘贴图片 + 一段话）
   └─ multica 原生 issue 落库（附件走 upload-file）
   └─ 派"建 GitHub issue"子任务给 agent → agent 跑 `gh issue create`
      → 回报 issue URL/number → 存回 multica issue

② 事件触发修复运行（issue 创建即触发，复用 EnqueueTaskForIssue 链路）
   └─ 起一个绑 project 的 goal_run，issue 内容作为输入，PMO 规划：
      节点A 修复（在 worktree 里改代码）
      节点B 端到端验证
      节点C 出 PR（push 分支 + gh pr create）
   └─ 全程执行流 / 状态 / 助理详情白嫖现有 goal 引擎

③ 修复任务三态（由验证 agent 的回报驱动，server 不猜）：
   - 未启动        = 修复运行尚未起（pending / 未创建 goal_run）
   - 完成          = 验证跑通 + PR 成功，回报 PR URL
   - 需要补充信息  = agent 报"没复现 / 没发现问题" → 新增终态语义

④ 完成态 → 走 GitHub PR：agent push 分支 + gh pr create，
   合并交给用户在 GitHub 点；webhook 把 PR / CI 状态同步回 issue（已现成）

⑤ 助理页详情：复用 TimelineView，看每个子任务执行流（已现成）

⑥ issue → 助理会话跳转：issue.goal_run_id → 定位 goal_run 的 discussion 会话 → 助理页直达
```

---

## 全局约束（继承既有铁律）

- **端形态：桌面客户端优先**（`apps/desktop`），端到端验证在桌面端实机跑。
- **后端永不调 LLM**：所有 AI 活 = 派任务给 agent（daemon → agent → runtime）。
- **server 不碰 repo / 不碰 GitHub API**：建 issue、push、开 PR 全是 agent 在 daemon 机器上用自身环境凭证完成。
- **不写死脚本**：GitHub 操作的具体姿势让模型按本仓库方言定，prompt 只给"思路 + 先读本工程既有约定"。
- **API Response Compatibility**：新增响应字段走 zod parse-don't-cast + 默认值，fail-soft。
- **共享优先**：「Issue」页是 web + desktop 共享的 `@multica/views` 组件，零 `next/*` / `react-router-dom` 导入。

---

## 本轮范围边界

**做：** 新建 issue（含粘贴）+ 「Issue」三栏页 + 建 GitHub issue 子任务 + 自动修复运行
（修复/验证/出 PR 三节点）+ 三态 + issue→助理会话跳转 + 删旧入口。

**不做（明确推迟）：**
- multica 直接 `git push` 到 main（已选走 PR，不写主干）。
- server 侧 GitHub API client / App 私钥 / installation token（已选全走 agent）。
- 真正的定时轮询（用事件驱动；批量节流如有需要下一轮）。
- 聊天消息粒度的 PR 状态深链（webhook 自动 link 已够）。
