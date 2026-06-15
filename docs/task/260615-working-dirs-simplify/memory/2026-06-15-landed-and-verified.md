---
name: working-dirs-landed-zero-migration
description: 「项目」精简为轻量「工作目录」已落地+桌面端实机验证——零迁移，前端把 project+project_resource 1:1 规约成 WorkingDir，后端/daemon/autofix 一行没动
metadata:
  type: project
---

## 做了什么（S1–S4，全绿，实机验证过）

把重的「项目」reframe 成 Configure 区的轻量「工作目录」：名字 + 本地目录(路径+daemon) + 可选 git，1:1。
**零迁移、零后端业务改动**——纯前端换脸。

- **S1** `packages/core/working-dirs/`：
  - `model.ts` 纯函数 `toWorkingDir(project, resources)` 把 project+project_resource 规约成 1:1
    `WorkingDir{projectId,name,localPath,daemonId,gitRepoUrl,localResourceId,gitResourceId}`；
    多资源取 position 最小的主资源，malformed ref 不崩。`daemonChoicesFromRuntimes` 从 runtimes
    聚合去重出 daemon 机器列表（daemon 不单独上报目录，复用 runtime 的 daemon_id）。
  - `queries.ts` `useWorkingDirs`（包 projectListOptions + 每 project useQueries 拉 resources）、
    `useDaemonChoices`（包 runtimeListOptions）。
  - `mutations.ts` `useCreate/Update/DeleteWorkingDir` 编排现有 project + project_resource API：
    建=POST /api/projects 带 bundled resources；改=改 title + reconcile 两条 resource；删=删 project（级联）。
  - **零新后端端点。** 单测 9 项覆盖规约/拆分/daemon 聚合。
- **S2** `packages/views/working-dirs/components/working-dirs-page.tsx`：list + 1:1 Dialog 表单
  （名字/机器下拉/本地路径/git 可选）+ AlertDialog 删除确认。
- **S3** sidebar `configureNav` 加 `工作目录`(FolderGit)，`项目` 从 workspaceNav 移回隐藏
  （ProjectsPage 路由/组件保留可达）；paths 加 `workingDirs()→/{ws}/working-dirs`（workspace-scoped，
  非全局，合规）；web+desktop 双端挂路由。
- **S4** issue 创建的 `ProjectPicker` 文案对齐——共享 `projects.picker` 三键改"工作目录"措辞，零组件改动。

## i18n 三处登记（新 namespace 的完整套路）

加新 namespace `working-dirs` 要同步改三处，否则 parity.test 或运行时缺 bundle：
1. 4 个 locale 各一个 `working-dirs.json`（en/zh-Hans/ja/ko）。
2. `packages/views/locales/index.ts`：import + RESOURCES 每 locale 一条 `"working-dirs": xxxWorkingDirs`。
3. `packages/views/i18n/resources-types.ts`：`import type` + `I18nResources` 里加 `"working-dirs": typeof workingDirs`。
另：sidebar nav label 走现有 `layout` namespace 的 `nav.working_dirs`（4 locale 都加）。
`LocaleResources` 是 `Record<string,...>` 松类型，不用改。

## 实机验证（桌面端，computer-use）

- sidebar Configure 区出现「工作目录」、「项目」消失 ✅
- Working Dirs 页渲染：现有 AI-GAME(role sync test) project 被正确规约成 1:1 工作目录显示（名字+路径+无仓库）✅
- 「新建工作目录」Dialog 表单四字段齐全（名字/机器下拉/本地路径/git 可选）✅
- create 编排走真 API（POST /api/projects 带 2 条 bundled resource）→ resource_count:2 ✅
- reload 后新建的「E2E WorkDir · git · /tmp/e2e-wd」出现在列表，git 徽章正确（有 github_repo）✅

## 坑

- 改了 package.json exports（core 加 `./working-dirs`、views 加 `./working-dirs/components`）→
  **vite 启动时缓存 exports，必须重启 desktop dev** 才认新子路径（同 [[paste-image-relative-url-broken-on-desktop]] 那次教训）。
- sidebar test mock 的 `paths.workspace()` 和 `useWorkspacePaths()` 两个对象都要补 `workingDirs` key，
  否则 `p[item.key]()` is not a function。

## 没动（边界）

后端 project/project_resource 表与字段、daemon 上报、autofix/goal_persist/worktree/role_sync、
issue/goal_run 的 project_id、RepositoriesTab(workspace.repos)、ProjectsPage（仍可达）。

关联：[[desktop-is-the-target-end]]、[[paste-image-relative-url-broken-on-desktop]]。
