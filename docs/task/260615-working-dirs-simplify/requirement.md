# 需求文档：把「项目」精简为轻量「工作目录」配置

> 任务 ID: 260615-working-dirs-simplify
> 状态: **需求已对齐**（本轮只整理需求 + 方向，设计见 design.md，不写代码）
> 录入日期: 2026-06-15

## 一句话

用户觉得「项目」太重。把它**重构（reframe）为一个轻量「工作目录」配置面**：在 Configure 区设置一个
本地目录（路径 + 所在 daemon），可选绑定一个 git 工程；issue 创建时从这些目录里选。
**底层仍 CRUD 现有的 `project + project_resource`，只是 UI 只暴露"名字 + 本地目录 + 可选 git"，
藏掉 status/priority/lead/看板那套仪式。** daemon / autofix / goal_persist / worktree 零改动。

## 需求原文（用户口述，整理）

> "这个项目太复杂了，我们把项目的目录的内容给精简一下，或者重新做一个功能：在配置里面可以去
> 设置一些本地文件的目录。本地文件目录可能绑定了一些 git 工程，这时候其他的就可以选择这些目录了，
> 在 issue 创建的时候。"

拆出三个诉求：
1. 「项目」当前太复杂，要**精简**。
2. 在**配置（settings/configure）**里能设置**本地文件目录**。
3. 本地目录**可选绑定 git 工程**；issue 创建时从这些目录里**选**。

## 已对齐的决策

### 决策 A：重构 project 为轻量目录，不新建并列实体 ⭐

用户选「重构 project 为轻量目录（推荐）」，否定了"新建 workspace_local_directory 并列表"。

理由（写需求前三路 Explore 实读得出）：
- `project` 表里**被 daemon/autofix 真正消费的只有 `title` + 它挂的 `project_resource`**
  （本地目录 `local_path`+`daemon_id` / git `url`）。
- `status / priority / lead_type / lead_id / icon / description` + 整个 ProjectsPage 看板
  —— **自动化一个字段都不读，纯 UI 仪式**。
- `project_resource` **本身就是用户描述的那个东西**："一个本地目录、绑 daemon、可选挂 git"。
- worktree checkout、role_sync、goal_persist、issue 创建的 project picker、goal_run 绑定
  —— 全挂在 `project_id → project_resource` 这条链上。
- 新建并列实体要把 daemon/goal/autofix 改成"读两个绑定源"、还要迁 `issue.project_id` /
  `goal_run.project_id` —— 典型"为概念干净引入重复"，风险高、回报低。

所以：**保留 project + project_resource 底层不动，只换前端表达。**

### 决策 B：一个「工作目录」1:1 绑定 ⭐

用户选「1:1（推荐）」：一个工作目录条目 = 一个本地路径 + 最多一个 git 仓。
- 贴合用户心智，issue 创建选择时不歧义。
- 底层 `project_resource` 仍是 1:N，UI 只呈现/编辑主资源（一个 `local_directory` +
  可选一个 `github_repo`）。多资源的老数据不破坏，只是新建/编辑走 1:1 表单。

## 现状基线（2026-06-15 三路 Explore 实读）

### 三个重叠概念（真问题：别再加第四个）

1. `project` + `project_resource` —— 完整，autofix 在用。
2. settings 里**已存在** `RepositoriesTab`，往 `workspace.repos`（JSONB）存：**只有 url + 描述，
   没有本地路径/daemon 绑定**。⚠️ `workspace.repos` **被 daemon 消费**（注册时下发给 repo cache，
   `daemon.go:416`），不是纯死 UI —— 合并它要迁移 + 兼容，不能直接删。
3. 用户现在想加的"本地目录配置"。

**好品味的终点是收敛，不是并列四份。**

### project_resource 的两种资源（载荷）

- `local_directory`: `{ local_path, daemon_id, label }` —— 绝对路径 + 哪台 daemon 机器。
- `github_repo`: `{ url, default_branch_hint }` —— git 仓 URL。

### project 被消费 vs 纯仪式

| 字段/能力 | 谁用 | 性质 |
|----------|------|------|
| `project_resource.local_path / daemon_id` | daemon worktree、goal_persist、role_sync、attachProjectContext | **载重** |
| `project_resource.github_repo.url` | worktree checkout、autofix 出 PR | **载重** |
| `project.title` | issue picker 显示、日志 | **载重（标识）** |
| `issue.project_id` / `goal_run.project_id`（nullable FK） | 绑定/分组 | **载重（可空）** |
| `project.status/priority/lead_*/icon/description` | 仅 ProjectsPage 看板 | **仪式，可藏** |
| ProjectsPage 看板/进度环/搜索 | 纯 UI | **仪式，可藏** |
| `task_usage_*` 的 project_id 物化列 | dashboard 按项目切分 token | 保留（不碍事） |

### 入口现状

- 侧边栏「项目」菜单刚恢复（本轮前一步）。
- issue 创建用 `ProjectPicker`（`packages/views/projects/components/project-picker.tsx`）→
  `GET /api/projects`。
- Configure 区现有：运行时、Skills、设置。settings 内有 tabs（含 RepositoriesTab、GitHubTab）。

## 范围边界

**做：**
- 一个轻量「工作目录」配置面（Configure 区，或 settings 内一个 tab —— 设计定），
  CRUD 底层 `project + project_resource`，只暴露 名字 + 本地目录(路径+daemon) + 可选 git。
- issue 创建的 picker 复用同一数据源（底层不变），文案/呈现按"工作目录"对齐。
- 隐藏 project 的 status/priority/lead/看板仪式（或在轻量面里不显示）。
- 决定 `RepositoriesTab` 与新面的关系（合并 / 并存 / 废弃）—— 设计阶段定，注意 daemon 消费。

**不做：**
- 不新建并列实体、不迁 `issue.project_id` / `goal_run.project_id`。
- 不动 daemon / autofix / goal_persist / worktree / role_sync 的后端消费逻辑。
- 不删 `project` / `project_resource` 表与字段（仪式字段只是 UI 不显示，留空即可）。
- 多资源 1:N 的复杂 UI（按 1:1 表单做）。

## 待设计阶段定的问题（design.md 回答）

1. 轻量面放哪：Configure 区独立页（跟运行时并列）还是 settings 内一个 tab？
2. 「项目」这个词要不要在 UI 全量改叫「工作目录」？还是保留 project 词、只精简内容？
3. `RepositoriesTab`（workspace.repos，daemon 在用）怎么处理——合并进来需不需要兼容/迁移？
4. 本地目录的 `daemon_id`：用户怎么选 daemon？daemon 现在不上报它能访问哪些目录（只报 runtimes）——
   是让用户手填路径 + 选已注册 daemon，还是要 daemon 端补上报目录能力？
