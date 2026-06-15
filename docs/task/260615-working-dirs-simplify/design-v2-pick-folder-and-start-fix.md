# 设计增量 v2：工作目录"只选文件夹" + Issue"启动修复"快捷操作

> 所属任务: 260615-working-dirs-simplify（工作目录）+ 260612-issue-github-autofix（启动修复）
> 上游: [`design.md`](./design.md)（v1 落地的工作目录页）
> 本文定: 两个用户反馈的增量。本轮只设计，未执行。

---

## 增量 A：工作目录新建表单简化为"只选一个本地文件夹"

### 用户原话
> "简化一下，它只需要选择我本地的一个文件夹就行了，其他的都不需要选。名称可以用文件夹名，
> 本地路径有了就有了，git 仓库可以用绑定的文件夹对应的 git 仓库，机器默认我本机就好，
> 不需要选其他的。我们的项目就只管理这一个内容。"

### 现状（v1 表单，4 个字段都要手填）
名称 / 机器下拉 / 本地路径文本 / git URL 文本 —— 太重。

### 探查结论：要的能力几乎全现成 ⭐

| 能力 | 状态 | 位置 |
|------|------|------|
| 原生文件夹选择器（Electron） | ✅ 现成 | `apps/desktop/src/main/local-directory.ts` IPC `local-directory:pick` |
| 选完返回 path **+ basename** | ✅ 现成 | 同上（picker 已返回 basename） |
| 目录校验（存在+可读写） | ✅ 现成 | `local-directory:validate` |
| 本机 daemon_id | ✅ 现成 | `daemonAPI.getStatus().daemonId`（轮询本地 /health 拿到） |
| preload 暴露 | ✅ 现成 | `window.desktopAPI.pickDirectory()` / `validateLocalDirectory()` |
| **读文件夹的 git remote** | ❌ 要建一小步 | 见下 |

### 落地设计

**新建表单（桌面端）从"填 4 个字段" → "选一个文件夹"：**

```
┌─ 新建工作目录 ─────────────────┐
│  [ 选择本地文件夹… ]            │   ← 点 → window.desktopAPI.pickDirectory()
│                                 │
│  (选完后展示，全部自动填、可改)  │
│  名称:     multica-sumo         │   ← basename，自动
│  路径:     /Users/.../multica   │   ← picker path，自动（只读展示）
│  机器:     本机 (Mac Studio)    │   ← 本地 daemon_id，自动
│  Git 仓库: github.com/.../x.git │   ← 自动探测，探到就填、探不到留空
│                       [取消][创建]│
└─────────────────────────────────┘
```

- **名称** = picker 返回的 basename，自动；用户可改（仍是 project.title）。
- **路径** = picker 返回的 path，只读展示（不再手打绝对路径）。
- **机器** = `daemonAPI.getStatus().daemonId`，默认本机，**不再给下拉**（用户原话"默认我本机就好"）。
- **Git 仓库** = 自动探测该文件夹的 `git config --get remote.origin.url`，探到就填、探不到留空（非 git 目录也能建）。

**唯一要新建的一小步：读 git remote。**
- 最简：desktop 主进程加一个 IPC `local-directory:git-remote`（`git -C <path> config --get remote.origin.url`），
  和现成的 `pick`/`validate` 并列在 `local-directory.ts`。纯本地、桌面端、不碰后端。
- 暴露 `window.desktopAPI.detectGitRemote(path)`。

**数据落地不变**：仍是 `useCreateWorkingDir` → project + local_directory resource(+可选 github_repo)。
后端零改动。

### Web 端怎么办（desktop-only 的边界）
浏览器选不了文件夹、拿不到绝对路径、连不上本地 daemon。所以：
- **桌面端**：一键选文件夹（本设计）。
- **Web 端**：保留 v1 的手填表单（路径+机器+git URL），或直接提示"请在桌面端配置工作目录"。
  用 `window.desktopAPI` 是否存在来分支（`platform` 能力探测）。
- 这跟"目标端是桌面端"一致（[[desktop-is-the-target-end]]），web 退化不阻塞主路径。

### "项目只管理这一个内容"
用户明确：工作目录就是项目的全部。v1 已经把 status/priority/lead 那套仪式藏掉了，
本增量进一步把新建简化为"选文件夹"。底层仍是 project + project_resource（不动）。

---

## 增量 B：Issue 详情加"启动修复"快捷操作

### 用户原话
> "issue 中，我们是不是应该还有一个快捷操作，就是启动修复"

### 现状缺口（已核实，真缺口）
autofix **只在 issue 创建那一刻自动触发一次**（`handler/issue.go:2277` `maybeStartAutofix`，
门 = 绑 project + 指派 agent/squad）。**没有任何"对已存在 issue 手动启动修复"的入口** —— 全仓零结果。

后果：一个 `not_started` 的 issue（创建时没绑 agent/project，或当时没触发），用户**事后绑好了
agent+project 也没有"开始"按钮**，只能删了重建。详情区 not_started 态只有一句静态提示，然后就没了。

### 为什么它和别的快捷操作不一样（关键）
其它快捷操作（重试/补充/完成）是 `sendChatMessage` 派会话给**已存在**的 goal_run 会话。
但"启动修复"时**还没有 goal_run、没有会话**，所以**不能复用 sendChatMessage** ——
得调一个真正"创建 autofix goal_run"的动作。

`StartAutofixGoalRun` + `LinkAutofixGoalRun` 逻辑**全现成**，只是现在只被创建路径内部调用，**没暴露成端点**。

### 落地设计

1. **后端**：暴露手动触发端点 `POST /api/issues/{id}/autofix`：
   - 走 issue loader（`loadIssueForUser`）解析 issue + 鉴权。
   - 复用现成 `ShouldAutofixIssue` 门（没绑 project/agent → 400 带原因）。
   - 复用 `StartAutofixGoalRun` + `LinkAutofixGoalRun`。
   - 返回新 goal_run id。**逻辑全现成，只差一层 handler + 路由。**
2. **前端**：not_started 态（且已绑 agent+project）显示**「启动修复」按钮**（一键，**不带预置会话框**——
   没会话可派）。点完转入 running。
   - 没绑 agent/project 时按钮禁用 + 现有提示「先指派 agent 并绑定工作目录」。
3. 这个按钮和现有快捷操作**互补**：not_started → 「启动修复」(新)；有 run 后 → 重试/补充/完成(已有)。

### 与铁律一致
后端不调 LLM、不碰 repo——「启动修复」只是创建 goal_run + 派规划任务给 PMO（现成链路）。
端点只编排，真正的修复仍是 agent 在环境里干。

---

## 破坏性风险

- **增量 A 是 desktop-only**：必须用能力探测（`window.desktopAPI` 在否）分支，web 端不能崩。
- **git remote 探测**失败（非 git 目录 / 无 remote）→ 留空，不报错，仍可建工作目录。
- **增量 B 的门**：`ShouldAutofixIssue` 现在要 agent/squad assignee + project。手动端点要把"为什么没触发"
  的原因明确返回（缺 project / 缺 agent），别静默失败——否则用户点了没反应又像坏了。
- API 兼容：新端点 + 新 IPC 都是增量，老客户端不受影响。

## 本轮不做
- 不改后端 project/project_resource 模型。
- 不做 web 端的文件夹选择（浏览器做不到）。
- 不做跨机器 daemon 选择（默认本机；多机留后续）。
