# S3' — 自动修复 goal_run（建GH issue / 修复 / 验证 / 出PR 四节点）

> 依赖：S1'（metadata helper） ｜ 与 S4 并行（文件互斥：server vs views）

## 施工契约（怎么做）

1. **触发点**：`server/internal/handler/issue.go` 创建路径末尾（≈2264，现 `EnqueueTaskForIssue` 附近）。
   - 新增判定 `shouldAutofixIssue(issue)`：issue 绑了 project + workspace 有可规划的 PMO/squad。
   - 满足则调 `GoalService` 起 autofix goal_run；不满足**静默跳过**（issue 正常建，前端显示"未启动"）。
2. **起 goal_run**：复用 `GoalService` 现有 CreateTask/StartPlanning 链路：
   - 绑 issue 的 `project_id`；goal 文本 = issue 标题 + 描述（+ 附件引用）。
   - 起成功后用 S1' helper 把 `goal_run_id` 写回 `issue.metadata.autofix`。
3. **规划引导 prompt**（`server/internal/daemon/prompt.go`，注入 PMO 规划段，**不写死脚本**）：
   引导 PMO 规划出四节点 DAG（depends_on 串成序）：
   - N1 建 GitHub issue：思路"在 project 仓库目录用本机环境的 GitHub CLI 把这个 issue 推上去，回报 number+url"。
   - N2 修复：思路"在 worktree 改代码"。依赖 N1。
   - N3 端到端验证：思路"按本仓库既有 E2E 方式验证；若无法复现/未发现问题，明确回报需要补充信息"。依赖 N2。
   - N4 出 PR：思路"按本仓库方言 push 分支 + 开 PR，body 引用 N1 的 issue number，回报 PR url"。依赖 N3。
   - prompt 只给"思路 + 先读本仓库既有约定"，**禁止硬编码 `gh issue create` / `gh pr create` 模板**。
4. **回报落库**：验证 agent 报 needs_info → S1' `setAutofixNeedsInfo`；N1/N4 回报 → `setAutofixGithub` / PR url 进 metadata。

## 验收契约（怎么算做完）

- Go 机制测试：issue 创建（绑 project + 有 PMO）→ goal_run 落库、绑 project、metadata.autofix.latest_goal_run_id 写回。
- Go 测试：issue 无 project / 无 PMO → 不起 goal_run、不报错、issue 正常建。
- prompt 单测：规划引导段含"建 GitHub issue / 出 PR / 引用既有约定"思路，**断言不含** `gh issue create` / `gh pr create` 字面模板。
- `ResolveTaskWorkspaceID` 对这些节点能解析出 workspace（复用 goal_subtask FK 路径，无需新分支）。
- `go build ./...` + `go test ./internal/...` 通过。

## 边界

- 只碰 `server/internal/{handler,service,daemon}`，**不碰** `packages/`。
- 不调 LLM、不调 GitHub API、不在 server 持有 push 凭证。
- 不加 goal_run 枚举值。
