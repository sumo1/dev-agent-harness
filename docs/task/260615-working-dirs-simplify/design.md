# 技术方案：把「项目」精简为轻量「工作目录」配置

> 上游：[`requirement.md`](./requirement.md)
> 下游：`plan/step-*.md`（双契约）
> 本文定：数据结构边界、UI 形态、串/并行依赖、破坏性风险。基线见 requirement §现状。

## §0 设计取舍（已定）

| 问题 | 决策 | 理由 |
|------|------|------|
| 实现方向 | **重构 project 为轻量目录**，底层不动 | autofix/daemon 只吃 title + project_resource；status/priority/lead 是纯 UI 仪式 |
| 绑定基数 | **1:1**（一本地路径 + 可选一 git 仓） | 贴合心智；底层 project_resource 仍 1:N，UI 只编辑主资源 |
| 路径录入 | **手填绝对路径 + 选已注册 daemon** | 零后端改动；路径/仓库有效性靠实际跑任务时暴露，不预校验 |
| 放哪 | **Configure 区独立页「工作目录」**，跟 运行时/Skills 并列 | 这是配置不是工作流；ProjectsPage 看板从主菜单移除 |
| 词 | UI 改叫「工作目录 / Working Directory」 | "项目"过载；底层 project 词保留在代码/DB |
| RepositoriesTab | **本轮并存，不动**（标记后续收敛） | `workspace.repos` 被 daemon repo cache 消费，合并需迁移+兼容，单独一轮 |

## §1 数据结构：零迁移，复用现有

**不加表、不加列、不迁数据。** 一个「工作目录」= 一个 `project` 行 + 它的 `project_resource`：

```
WorkingDirectory (UI 概念)         project (DB，复用)
├─ name              ───────────►  project.title
├─ localPath         ───────────►  project_resource[type=local_directory].resource_ref.local_path
├─ daemonId          ───────────►  project_resource[type=local_directory].resource_ref.daemon_id
└─ gitRepoUrl (可选) ───────────►  project_resource[type=github_repo].resource_ref.url
   project.status/priority/lead/icon/description → 建时取默认值，UI 永不显示
```

- 新建工作目录 = `POST /api/projects`（title=name，bundled resources：一个 local_directory +
  可选一个 github_repo）。现有 CreateProject 已支持 bundled resources。
- 编辑 = `PUT /api/projects/{id}`（改 title）+ `PUT/POST/DELETE /api/projects/{id}/resources/*`
  维护那两条资源。
- 删除 = `DELETE /api/projects/{id}`（级联删 resource）。
- **issue picker / goal_run 绑定 / autofix / worktree / goal_persist / role_sync 全部零改动**——
  它们读的就是 project + project_resource，实体没变。

> **good taste**：用现有数据结构让"新功能"消失成"换一张脸"。新增的全是前端 + 一个可选的薄
> API 包装，后端业务逻辑一行不动。

## §2 前端形态

### 新页：`packages/views/working-dirs/`（共享 views）

仿 `runtimes/` 的 list+detail+CRUD 形态（最干净的现有范例）：

- `WorkingDirsPage`：列出当前 workspace 的工作目录（= 复用 `projectListOptions` →
  `GET /api/projects`，只挑/呈现带 local_directory 资源的）。
  每行：名字 · 本地路径 · daemon 名 · git 仓(有则显示)。
- 内联/弹层**新建/编辑表单**（1:1）：名字、本地路径(文本)、daemon(从 `runtimeListOptions`
  派生的 daemon 列表选)、git 仓 URL(可选)。提交映射到 project + 2 条 resource 的 CRUD。
- 删除确认。

### 数据/逻辑层 `packages/core/working-dirs/`

- 薄封装：`workingDirListOptions`（包 `projectListOptions` + 把每个 project 的 resources
  规约成 1:1 的 `{name, localPath, daemonId, gitRepoUrl, projectId}` 视图模型）。
- mutations：`useCreateWorkingDir / useUpdateWorkingDir / useDeleteWorkingDir`——内部编排
  现有 project + project_resource 的 api 调用（建 project→建 resources；改名→改 resources）。
- **零新后端端点**（除非编排太碎，再加一个 `POST /api/working-dirs` 聚合端点；先不加）。

### 入口调整（`app-sidebar.tsx`）

- Configure 区 `configureNav` 加 `{ key: "workingDirs", icon: FolderGit }`，放运行时前/后。
- **「项目」从 workspace 主菜单移除**（回到隐藏）——它被工作目录页取代。ProjectsPage 路由保留
  可达（pin / 直达 URL），不删代码。
- paths.ts 加 `workingDirs: () => /{ws}/working-dirs`（单词路由，合规）。

### issue 创建 picker

- `ProjectPicker` 文案对齐"工作目录"（label/placeholder），数据源不变。
- 可选：picker 里每项显示 本地路径/git 仓 副标题，让用户知道选的是哪个目录。

## §3 daemon 选择（路径录入）

- 表单的 daemon 下拉 = 从 `runtimeListOptions(wsId)` 聚合出**去重的 daemon 列表**
  （runtime 带 `daemon_id` + device_name；同一 daemon 多 runtime 折叠成一条）。
- 本地路径**纯手填绝对路径**，不预校验存在性。无效路径在实际跑任务（worktree/persist）时
  由 agent 报错冒上来——延续"runtime 真实错误透传"原则，不在配置期假装校验。

## §4 串/并行依赖图

```
        S1 (core/working-dirs 视图模型 + mutations 编排)  ── 地基,无迁移
          │
    ┌─────┴───────────────┐
    ▼                      ▼
  S2 (WorkingDirsPage      S3 (sidebar 入口调整 +
      list+form+CRUD)          paths + 「项目」移回隐藏)
    │                      │
    └──────────┬───────────┘
               ▼
             S4 (issue picker 文案对齐 + 可选副标题)
```

- **S1 地基**：视图模型规约（project+resources ↔ 1:1 WorkingDirVM）+ 编排 mutations。纯 core，
  有单测（规约/拆分逻辑）。
- **S2 / S3 并行**：页面 vs 入口，文件互斥。
- **S4 收尾**：picker 文案，依赖 S1 视图模型稳定。

## §5 破坏性风险

1. **RepositoriesTab 不要误删/误合**：`workspace.repos` 被 daemon repo cache 消费
   （`daemon.go:416` 下发，注册时带 ReposVersion）。本轮**并存不动**，只在 design 标注后续收敛
   需迁移 + daemon 兼容。
2. **老 project 数据（多资源/带 status 的）**：轻量页按 1:1 呈现——一个 project 若有多个
   local_directory，UI 只显示主资源（position 最小那条），编辑只动主资源，**不破坏**其余。
   带 status/priority 的老 project 照常显示在工作目录列表（仪式字段忽略）。
3. **「项目」移回隐藏**：ProjectsPage 仍可达（防止有人 pin 了 / 有直达链接）；只从默认菜单移除。
   不删路由/组件。
4. **issue.project_id / goal_run.project_id 语义不变**：picker 选的还是 project，只是 UI 叫
   工作目录。autofix 触发门「绑 project + 指派 agent」逻辑零改动。
5. **API 兼容**：新视图模型读 project/resource 响应仍走 zod parse-don't-cast；resource_ref 的
   JSONB 按可选字段防御。

## §6 本轮不做

- 不动 daemon 上报（不加"上报可选目录"能力）。
- 不合并/迁移 RepositoriesTab（workspace.repos）。
- 不删 project/project_resource 表与字段、不删 ProjectsPage。
- 不迁 issue/goal_run 的 project_id。
- 工作目录多绑定（1:N）的 UI。
