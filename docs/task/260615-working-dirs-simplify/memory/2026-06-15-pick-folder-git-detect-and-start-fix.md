---
name: pick-folder-git-detect-and-start-fix-landed
description: 补齐两件——① 工作目录选文件夹后自动探测 git remote 填进 git 绑定（新 Electron IPC）② issue「启动修复」手动触发端点+按钮（POST /api/issues/{id}/autofix）。桌面端实机验证
metadata:
  type: project
---

## 做了什么（R1–R3，全绿，桌面端实机）

### R1 — 工作目录"选文件夹"补 git remote 自动探测（增量 A 的缺口）
fork 之前把"选文件夹→自动填名称/路径/本机"做了，但**漏了"自动读文件夹的 git 仓库"**。补上：
- **Electron 主进程**新增 IPC `local-directory:git-remote`（`apps/desktop/src/main/local-directory.ts`）：
  跑 `git -C <path> config --get remote.origin.url`。退出码映射：0+url→ok；1→no_remote；128→not_git；
  其它→error。非 git 目录/无 remote 都返回 ok=false（留空，不报错）。
- preload 暴露 `window.desktopAPI.detectGitRemote(path)`（+ index.d.ts 类型）。
- views platform wrapper `detectGitRemote`（web/无 shell → unsupported，留空）。
- `working-dirs-page.tsx`：选完文件夹后调它，探到的 url 进 `gitRepoUrl` state → 进 WorkingDirForm；
  表单显示只读的 git 绑定行（有→显示 url，无→"无 git 仓库（仅本地文件夹）"）。
- 后端零改动（仍是 project + local_directory + 可选 github_repo resource）。

### R2 — issue「启动修复」快捷操作（补了一条完全缺失的链路）
autofix 原本**只在 issue 创建那一刻自动触发一次**，事后没有手动入口——not_started 的 issue
绑好 agent/project 也只能删了重建。补上：
- **后端** `POST /api/issues/{id}/autofix`（`StartAutofix` in issue.go）：loadIssueForUser →
  `ShouldAutofixIssue` 门（不满足返回 **400 带原因**，不像创建时 fail-soft 静默）→ `StartAutofixGoalRun`
  + `LinkAutofixGoalRun` → 202 + goal_run_id。**全复用现成 service 原语，只加 handler+路由。**
- **前端** `api.startAutofix` + QuickActions 在 not_started 分支：eligible（project + agent/squad assignee，
  镜像 server 门）→ 一键「启动修复」按钮（无会话框，因为还没 goal_run）；不 eligible → 原提示。
  成功后 invalidate issue list → 状态翻 running。

### 为什么「启动修复」不能复用 sendChatMessage
其它快捷操作（重试/补充/完成）派给**已存在**的 goal_run 会话。"启动修复"时**还没有 goal_run/会话**，
必须真正创建 run → 专用端点。这是它和别的快捷操作唯一的结构差异。

## 实机验证（桌面端 computer-use + 活库）
- R1：新建工作目录对话框显示「选择文件夹…」picker；detectGitRemote 三分支实测（multica repo→
  `git@github.com:sumo1/dev-agent-harness.git`；/tmp→空；fresh init 无 origin→空），表单永远拿到干净结果。
- R2：seed 一个 not_started + eligible(project+agent) 的 issue → 详情显示「启动修复」按钮（无 hint）→
  点击 → `POST /api/issues/{id}/autofix` **202** + 服务日志 `autofix goal run started` + issue.metadata.autofix
  写入新 goal_run_id（StartAutofixGoalRun+LinkAutofixGoalRun 都跑了）✅。
- 不 eligible 的 issue → 仍显示提示、无「启动修复」按钮（前端测试覆盖）。

## 坑/教训
- **改 Electron preload/main 必须重启 desktop dev**（rebuild 主进程），HMR 不覆盖主进程的新 IPC。
- 改了 Go（新端点）必须 rebuild + restart server。
- 新端点的 4xx 要带明确原因（缺 project/agent）——用户主动点的按钮，静默失败 = 像坏了
  （和创建时 fail-soft 相反）。

## 测试
- core/views typecheck、desktop typecheck(node+web)、go build+vet 全过。
- views 页测试 15 项（+ eligible→Start fix→调 startAutofix、not-eligible→hint 无按钮）；parity 4-locale；
  autofix service DB 测试（gate+start+link 早覆盖）全过。

设计文档：`design-v2-pick-folder-and-start-fix.md`。
关联：[[working-dirs-landed-zero-migration]]、[[quick-actions-and-failed-state-landed]]、[[desktop-is-the-target-end]]。
