# 自动修复触发门必须 opt-in（project + agent 指派），不能只看 project_id

> 日期：2026-06-12 ｜ 阶段：S3' 实现后整体验证发现的真回归

## 结论

`GoalService.ShouldAutofixIssue` 的触发门**两个条件都要**：

```go
issue.ProjectID.Valid
  && issue.AssigneeType ∈ {"agent","squad"} && issue.AssigneeID.Valid
```

只判 `project_id.Valid` 是**破坏性 bug**：生产里大量正常 issue 都绑 project，
会让"创建任何 project issue"都静默起一个修复 goal_run + 新建一个动态 squad。
这违反 "Never break userspace"。

## 怎么发现的

整体验证时 `internal/handler` 包**全量跑 FAIL、单跑 PASS** —— 典型测试间状态污染。
失败的是 `TestClaimTask_LeaderGetsBriefing`：它 claim 到的 instructions 是
"planning 风格 + 无成员 squad"，不是它自己排的 squad issue 任务。

根因链：某个在它之前跑的 handler 测试通过 `CreateIssue` 建了**绑 project** 的 issue
→ 旧门 `project_id.Valid` 命中 → autofix 起了个新 squad + planning task 排进
leader 队列且测试没清理 → Briefing 测试 claim 时抢到了这个残留 planning task。

> 这个污染是**真 bug 的信号**，不是测试噪音。门收窄后污染消失。

## 验证方法（可复用）

判断"是我引入的回归"还是"预存在失败"：
1. `git stash push -u -- server/ packages/ apps/` 把改动全藏起。
2. 在干净测试库跑同一组测试，记录 base 失败集。
3. `git stash pop` 恢复，对比。净 delta 才是你的责任。

本任务净回归 = 0。预存在失败（base 同样挂，与本任务无关）：
- `cmd/server` 的 12 个 comment/router 测试（环境性）。
- `internal/service/TestScanRoleDir`（testdata frontmatter，base 快照里就坏）。
- `pkg/agent` Hermes/Kimi（5s 超时，跑外部 ACP 二进制，flaky）。

## 测试隔离 vs 运行时隔离（dogfood）

dogfood 候选**运行时**复用控制面活库 `multica`（见 user memory `dogfood-reuses-control-plane-db`）。
但**跑 Go 测试**要用独立临时库（`multica_dogfood_test_tmp`），否则测试写的 fixture
会污染活库、且测试间互相踩。两者不矛盾：运行时复用真数据，测试用一次性库。
