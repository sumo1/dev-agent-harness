# 需求拆解：260612-issue-github-autofix

> 上游：[`requirement.md`](./requirement.md) ｜ 下游：`design.md`（本轮不写）、`plan/step-*.md`
> 本文只做**可独立验收的子任务拆解 + 串/并行依赖判定**，不落具体实现。

## 拆解原则

按 README 的判定：一组子任务标**并行**当且仅当①文件范围互斥 ②约束显式 ③验收独立，
三条全满足。否则串行。本任务横跨 DB / Go 后端 / 共享 view / daemon-prompt 四层，
天然分出可并行的"地基层"和必须串行的"链路层"。

---

## 子任务清单

### S1 — DB 地基：issue↔goal_run 链接 + 修复运行三态语义

- **要点**：issue 表加 `goal_run_id`（nullable FK），支撑 issue→修复运行→助理会话跳转；
  goal_run 状态枚举加"需要补充信息"语义（候选名 `needs_info`，与 partial/failed 并列，
  由验证 agent 回报触发，非 server 推断）。
- **可改文件**：`server/migrations/` 新迁移、`server/pkg/db/queries/issue.sql` / `goal.sql`、sqlc 重生成。
- **产出**：迁移 up/down、新增查询（按 issue 查 goal_run、按 goal_run 反查 issue）、生成代码。
- **验收**：迁移可 up/down；`make sqlc` 无 drift；新状态值在 CHECK 约束内。
- **依赖**：无（地基）。

### S2 — 建 GitHub issue 子任务（agent 跑 `gh`，server 只编排）

- **要点**：multica 原生 issue 创建后，派一个子任务给 agent → agent 在 project 仓库目录用
  `gh issue create` 建真实 GitHub issue → 回报 URL/number → 存回 multica issue。
  **server 不调 GitHub API。** prompt 只给思路 + "读本仓库既有约定"，不写死命令。
- **可改文件**：`server/internal/service/goal.go`（或新建一类轻量 dispatch）、
  `server/internal/daemon/prompt.go`（新 prompt 段）、issue 上存 github 回报字段（依赖 S1 同迁移或独立列）。
- **产出**：新任务类型 / dispatch 路径 + claim 映射 + prompt 段 + 回报落库。
- **验收**：Go 机制测试（任务被派发、claim 携带 project 仓库目录、回报字段落库）；
  prompt 单测断言不含硬编码 `gh issue create` 模板。
- **依赖**：S1（存 github 回报字段）。

### S3 — 自动修复运行：issue→goal_run（修复 / 验证 / 出 PR 三节点）

- **要点**：issue 创建即触发（复用 `EnqueueTaskForIssue` 事件链路），起一个绑 project 的
  goal_run，issue 内容作为输入，PMO 规划三节点（修复 in worktree → E2E 验证 → 出 PR）。
  worktree 复用 `multica repo checkout`。
- **可改文件**：`server/internal/service/goal.go`、`server/internal/handler/issue.go`（触发点）、
  `server/internal/daemon/prompt.go`（修复 / 出 PR 节点 prompt 思路）。
- **产出**：触发逻辑 + goal_run 创建 + 三节点规划引导 prompt + 出 PR 节点（agent `git push` + `gh pr create`）。
- **验收**：Go 机制测试（issue 创建 → goal_run 落库、绑 project、根节点派发；出 PR 节点 prompt 含
  "用本仓库方言开 PR"思路、不含死脚本）；状态流转到三态。
- **依赖**：S1（goal_run_id 回写 issue + needs_info 态）、S2（建 issue 在前）。

### S4 — 「Issue」三栏页（中=列表+新建，右=详情）+ 图片粘贴 + 删旧入口

- **要点**：放出 sidebar 的「Issue」菜单项；新建 `IssuePage`（`packages/views/issues/`），
  中栏 = issue 列表 + 状态点 + 顶部内联新建（粘贴图片+一段话，**不弹模态**），
  右栏 = 选中 issue 详情（图片/描述/三态/跳助理会话入口）；补 paste 事件；
  删掉现有 create / quick-create 模态触发点；web + desktop 双端挂路由。
- **可改文件**：`packages/views/issues/`（新）、`packages/views/layout/app-sidebar.tsx`、
  `apps/web/app/[workspaceSlug]/(dashboard)/issues/`、`apps/desktop` routes、
  `packages/core/paths/paths.ts`、i18n locales、删旧入口的触发点文件。
- **产出**：三栏页、内联新建+粘贴、状态点、详情区、跳转入口、双端路由。
- **验收**：`packages/views/` 测试（列表渲染、三态点、粘贴触发上传、新建提交调 createIssue）；
  `pnpm typecheck` 0 error；旧入口移除后无悬挂引用。
- **依赖**：S1（详情区读三态 + 跳转用 goal_run_id）。前端骨架可与 S2/S3 并行开发，
  但**联调验收**依赖 S1 字段就位。

### S5 — issue → 助理会话跳转（可路由的会话定位）

- **要点**：助理页支持"按 goal_run / session id 直达对应会话历史"；issue 详情的跳转入口
  经 `useNavigation().push()` 跳过去并选中会话。
- **可改文件**：`packages/views/assistant/`（会话定位）、`packages/core/paths/paths.ts`、
  导航参数透传、issue 详情跳转按钮（落在 S4 页里）。
- **产出**：助理页接受会话定位参数 + 选中逻辑、issue→助理跳转链路。
- **验收**：`packages/views/` 测试（带定位参数进助理页 → 选中正确会话）；
  从 issue 详情点跳转 → 落到对应 goal_run 的 discussion 会话。
- **依赖**：S1（goal_run_id）、S3（goal_run 存在才有会话可跳）、S4（跳转入口在 issue 详情里）。

---

## 串 / 并行依赖图

```
            S1 (DB 地基) ──────┬──────────┬──────────┐
              │               │          │          │
              ▼               ▼          ▼          │
            S2 (建 GH issue) ─▶ S3 (自动修复运行) ─┐ │
                                          │        │ │
                                          ▼        ▼ ▼
                                        S5 (跳转) ◀ S4 (Issue 三栏页)
```

- **第 1 阶段（地基，串行前置）**：**S1** 单独先跑。它是 issue↔goal_run 链接 + 三态语义的
  唯一来源，所有下游都依赖它的字段就位。
- **第 2 阶段（可并行）**：S1 完成后，**S2（建 GH issue 子任务）** 与 **S4 前端骨架** 可并行——
  文件范围互斥（后端 service/prompt vs 前端 views），约束各自显式，验收独立。
- **第 3 阶段（串行链路）**：**S3** 依赖 S1+S2（建 issue 在修复前），**S5** 依赖 S1+S3+S4
  （要有 goal_run 和跳转入口才能联调）。

> **判定结论**：只有 S2 与 S4-骨架 满足三条可并行；其余因字段 / 链路依赖必须串行。
> 宁可 S3/S5 串行跑稳，也不拆细到互相缠绕。

---

## 风险点（design 阶段要重点处理）

1. **agent 环境凭证假设**：整套方案押在"agent 机器上有可用的 `gh auth` + git push 权限"。
   若环境没配，建 issue / 出 PR 子任务会失败——需要清晰的失败回报（落到"需要补充信息"或独立错误态），
   而不是静默卡住。
2. **"需要补充信息"态的触发契约**：必须由验证 agent 的结构化回报驱动（没复现/没发现问题），
   不能让 server 凭超时或空结果瞎猜。延续 goal_decision 的"回报驱动判断"模式。
3. **删旧入口的悬挂引用**：现有 create / quick-create 模态可能在多处被触发（issue 列表、
   命令面板、快捷键），删之前要全量搜引用，避免留下死按钮。
4. **issue→goal_run 一对多**：一个 issue 可能被多次触发修复（首次没复现、补充信息后再来）。
   `goal_run_id` 单列可能不够，design 要定清是"最新一次"还是历史多次（倾向最新 + 历史可查）。
