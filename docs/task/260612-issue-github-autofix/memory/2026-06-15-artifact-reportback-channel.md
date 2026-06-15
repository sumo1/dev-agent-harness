---
name: autofix-artifact-reportback-channel
description: 修补 N1/N4 产物回报缺口——agent 建的 GitHub issue / 开的 PR 经 `multica goal report` 回流到 issue.metadata.autofix；completed 态读 pr_url 不读 github.issue_url
metadata:
  type: project
---

## 缺口（首版落地遗漏的一半）

首版 autofix（commit a72a8e3）把 `ReportAutofixGithub` helper 写好了、有 DB 测试,
但**生产代码零调用点**:N1(建 GitHub issue)、N4(出 PR)是 execute 子任务,
agent 跑完 `gh issue create` / `gh pr create` 后**没有任何 CLI verb / handler 接收它回报的
number/url**。后果链:`autofix.github` 永远写不进 → `deriveAutofixStatus` 的 completed 态
返回的 `prUrl` 拿不到值;而且字段还错位——completed 应给 **PR** url,代码却读
`github.issue_url`(N1 的产物,不是 N4 的)。

`partial→needs_info` 那条回报路径反而是通的(`finalizeGoalRun` 里接了 `ReportAutofixNeedsInfo`)。

## 修法(对齐既有 verdict/decide 模式,零迁移)

1. **CLI**:新增 `multica goal report <subtask-id> --github-issue-number/--github-issue-url/--pr-url`
   (照 `goal verdict`/`goal decide` 写,POST `/api/goals/subtasks/{id}/report`)。
2. **handler**:`ReportSubtaskArtifact` 走 `parseSubtaskScope` → `GoalService.ReportAutofixSubtaskArtifact`。
3. **service**:`ReportAutofixSubtaskArtifact` 从 subtask 解析 goal_run(workspace-gated,像 SubmitVerdict),
   再调 `ReportAutofixGithub`/新增的 `ReportAutofixPR`。metadata 加 `pr_url` 字段 + `setAutofixPR`。
4. **prompt**:execute prompt 在 `task.GoalAutofix && GoalSubtaskID != ""` 时注入
   `multica goal report <自己的-subtask-id> ...` 命令(generic,非 autofix run 时 server 端 no-op)。
   规划引导也加一行点名这个回报通道。**仍不硬编码 `gh` 模板**。
5. **TS**:`deriveAutofixStatus` completed 态改读 `autofix.pr_url`(不是 `github.issue_url`)。

## 关键接线点(改 4 处才透传 GoalAutofix)

`GoalSubtaskContext.Autofix`(service/goal.go)→ daemon claim 映射(daemon.go ~1589
`resp.GoalAutofix = gc.Autofix`)→ `AgentTaskResponse.GoalAutofix`(agent.go)→
daemon `Task.GoalAutofix`(types.go)。漏一处则 execute prompt 拿不到 flag、回报通道不出现。

**autofix 判别器单一来源**:`dispatchSubtask` 里用 `resolveAutofixIssue(ctx, run.WorkspaceID, run.ID)`
判断是否 autofix run——和 Report* helper 用的是同一个 metadata `@>` 反查,不另造标记。

## 坑

- 测试里 `CreateGoalSubtask` 的 `DependsOn` 传 `[]pgtype.UUID{}` 会 **pgx 编码失败**
  (`cannot find encode plan for text[]`),要传 `nil`(见 goal.go:777 同样传 nil)。

关联:[[repo-ssot-persist-and-judgment-landed]]、[[which-model-ran-it-attribution]]。
