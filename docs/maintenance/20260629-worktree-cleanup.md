# Worktree 清理记录 - 2026-06-29

## 背景

清理项目中遗留的 git worktree，统一代码分支到 main。

## 清理前状态

共 5 个 worktree（包括主工作树）：

1. **主工作树** - `/Users/sumo/workplace/opensource/multica/multica-sumo` (main 分支)
2. **docs-action-index** - `/Users/sumo/workplace/opensource/multica/multica-sumo-docs-action-index` (codex/docs-action-index 分支) - 孤立历史
3. **semantic-subtask-prompts** - `/Users/sumo/workplace/opensource/multica/multica-sumo-semantic-subtask-prompts` (codex/semantic-subtask-prompts 分支) - 孤立历史
4. **agent-harness-session-architecture** - `.dogfood-worktrees/agent-harness-session-architecture-20260616-160317` (codex/dogfood-agent-harness-session-architecture-20260616-160317 分支) - 已合并到 main，但有未提交修改
5. **runtime-skills-unified-entry** - `.dogfood-worktrees/runtime-skills-unified-entry-20260615-165218` (codex/dogfood-runtime-skills-unified-entry-20260615-165218 分支) - 已合并到 main，但有未提交修改

## 执行的操作

### 1. 已合并分支的处理

两个 dogfood worktree 的分支已经合并到 main，但包含未提交的工作进行中（WIP）修改：

#### agent-harness-session-architecture (f3d490684)
- **修改内容**：lobster 运行时、agent session 架构、OpenClaw 通道
- **关键文件**：
  - 新增 `packages/core/channels/` - OpenClaw 通道实现
  - 新增 `packages/core/session-commands/` - 会话命令注册
  - 新增 `packages/views/common/agent-session/` - Agent 会话面板组件
  - 新增 `packages/views/lobster/` - Lobster 运行时页面
  - 新增 `server/internal/handler/openclaw_channel.go` - OpenClaw 后端处理
  - 新增 110 个 computer-use trace 文件
- **提交**：`git commit -m "WIP: lobster runtime and agent session architecture changes"`
- **删除**：`git worktree remove`

#### runtime-skills-unified-entry (4b3723015)
- **修改内容**：运行时技能统一入口、技能详情查看器修复
- **关键文件**：
  - 修改 `packages/views/skills/components/skills-page.tsx`
  - 修改 `packages/views/skills/components/file-viewer.tsx`
  - 修改 `server/internal/handler/runtime_local_skills.go`
  - 新增 26 个 computer-use trace 文件和截图
- **提交**：`git commit -m "WIP: runtime skills unified entry changes"`
- **删除**：`git worktree remove`

### 2. 孤立历史分支的处理

两个孤立历史分支（与 main 无共同祖先）：

#### codex/docs-action-index
- **最新提交**：7b0da2b13 - feat(goal): expose runtime handoff context
- **关键内容**：任务文档体系、workflow 知识组织
- **评估结果**：内容已存在于 main 分支的更新版本中
- **操作**：直接删除 worktree（尝试合并产生 100+ 冲突）

#### codex/semantic-subtask-prompts
- **最新提交**：9ab0986a1 - feat(goal): hide workflow topology from subtask prompts
- **关键内容**：语义化子任务提示、工作流拓扑隐藏
- **评估结果**：功能已在 main 分支中以不同形式实现
- **操作**：直接删除 worktree

### 3. 分支清理决策

由于两个孤立历史分支：
- 与 main 无共同 git 祖先
- 合并会产生大量冲突（100+ 文件）
- 内容已在 main 分支中存在（通过不同的提交路径）
- 继续维护这些分支没有价值

决定：**直接删除 worktree**，不进行合并。

## 清理后状态

只保留主工作树：
```
/Users/sumo/workplace/opensource/multica/multica-sumo (main)
```

## 保留的代码

两个 dogfood 分支的 WIP 修改已提交到各自分支，虽然 worktree 已删除，但提交历史保留在分支中：
- `codex/dogfood-agent-harness-session-architecture-20260616-160317` @ f3d490684
- `codex/dogfood-runtime-skills-unified-entry-20260615-165218` @ 4b3723015

如需恢复这些实验性功能，可以：
```bash
git checkout codex/dogfood-agent-harness-session-architecture-20260616-160317
# 或
git cherry-pick f3d490684
```

## 后续建议

1. **避免孤立历史分支**：未来创建分支应从 main 或其他已有分支分出，避免 `git checkout --orphan`
2. **及时清理 worktree**：实验完成后及时合并或删除，避免累积
3. **WIP 提交规范**：工作进行中的 worktree 应定期提交或 stash，避免删除时丢失工作
4. **dogfood worktree 命名**：已有规范 `.dogfood-worktrees/{feature-name}-{timestamp}`，继续遵守

## 影响评估

- ✅ 主工作树干净，无遗留修改
- ✅ 所有重要功能代码已在 main 或分支中保留
- ✅ 实验性功能（lobster、OpenClaw）的提交历史已保存
- ✅ 孤立历史分支的内容已在 main 中以更新形式存在
- ⚠️ 如果有依赖这些 worktree 的本地脚本或配置，需要更新路径
