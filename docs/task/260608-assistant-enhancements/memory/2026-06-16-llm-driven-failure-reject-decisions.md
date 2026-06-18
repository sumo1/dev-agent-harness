---
name: llm-driven-failure-reject-decisions
description: 把 goal 编排里所有"接下来怎么办"的语义决策(execute 失败 / verify reject)从硬编码规则改成统一派 goal_decision 给主链路 PMO 决定(retry/reshape/proceed/abort);机械编排(deps→ready→dispatch、rollup)仍规则驱动。修了 verify reject 抢跑 bug + 加全局 DAG 快照 + 断点续跑(会话恢复+增量指令)。实机验证通过。
metadata:
  type: project
---

## 背景
用户诉求:"一定要用 LLM 主链路驱动,别用规则驱动。每个子任务执行结果给到主链路,让主链路决定下一步——它就知道还有逻辑/子进程没跑完。" 起因是 [[verify-rearm-ignores-depends-on-bug]] 那个抢跑死锁。

## 有品味的边界(没无脑全 LLM 化)
- **机械编排留规则**:deps 全 completed/skipped → ready → dispatch、全终结 → rollup partial/failed/completed。确定、高频,交给 LLM 又慢又会漏派。
- **语义决策走主链路 LLM**:节点失败 / verify reject → 派 goal_decision 给 PMO 判断。低频、要看全局。
- 划错这条线就是上面那个 bug 的根因(把语义判断写成 attempt 计数规则)。

## 四块改动(server/internal/service/goal.go 为主)
1. **失败统一走主链路**:删 `SyncSubtaskFromTask` 的 `attempt<max 自动重试` + `handleVerifyCompleted` reject 分支的计数重试/抢跑。任何 execute 失败 / verify reject 一律 `dispatchDecisionTask`。decide 动作扩展为 `retry|reshape|proceed|abort`(新增 retry=不改 spec 重跑)。attempt 变成给 PMO 看的信息,不再是 fail 闸。
2. **决策上下文带全局 DAG 快照**:`GoalDecisionContext` 加 `Trigger`(failure/reject)、`RejectReason`、`Attempts`、`DagSnapshot`(`buildDagSnapshot` 渲染每个节点 `[status](kind)title`,▶ 标被判节点)。PMO 看得见还有谁在 running。
3. **机械依赖门防抢跑**:`dispatchSubtask` 开头加 `depsSatisfied` 闸——任何节点(尤其 verify)deps 没全 completed/skipped 就退回 pending 不报错,等 `unblockDownstream` 自然重触发。**唯一咽喉点**,结构上杜绝抢跑。
4. **断点续跑**:新 query `GetLastSubtaskTaskSession`(按 goal_subtask_id 找上次 session_id/work_dir);daemon claim 给 goal_subtask 也接 prior_session(同 runtime 续跑)。`GoalSubtaskContext.RerunFeedback` 带 verifier 拒绝理由,prompt 提示"这是重跑,接着上次会话改,别从零写"。

## verify reject 的特殊建模
reject 时判决对象是 verify 节点本身(parked pending),但 `DecideSubtask` 检测 `kind==verify && status==pending && verdict==reject` → `rerunReviewedThenVerify`:重跑**被审的 execute 节点**(带 reject 理由作 RerunFeedback),verify 留 pending,被审节点重新 completed 后依赖门自动重新 ready+dispatch verify。reject 理由存在 verdict 的 `result` JSON(`{"reason":...}`),不是 failure_reason——读的时候要从 result 取。

## 实机验证(全过)
goal=字符串工具库(slugify+truncate 并行 + codex 严格审查):
- 并行 fan-out:seq1+seq2 同 coder 同秒 started 并发 ✅
- 依赖门:seq2 先 completed 时 verify 仍 pending,等 seq1 也 completed 才 dispatch ✅(抢跑根治)
- codex reject:给出真 bug(slugify max_length 契约不一致)→ 派 goal_decision `trigger=reject` 带 reject 理由 + 全局 DAG 快照 + attempts=1 ✅;被审节点保持 completed 没被自动重跑 ✅
- PMO 用 `multica goal decide ... proceed` 决策 ✅

## 又抓到一个 bug 并修(幂等)
PMO agent 在一个决策任务里**调了两次 `goal decide proceed`**。第二次撞上已 resolved 的节点,重新 dispatch 了 verify。修:`DecideSubtask` 加幂等闸——只在 `status==failed`(execute 失败)或 `verify+pending+verdict==reject` 时才 enact,否则 no-op。测试 `TestGoalDecideIsIdempotent`。

## 测试
新增/改:`TestGoalVerifyRejectAsksCoordinator`、`TestGoalVerifyRejectRetryRerunsReviewed`、`TestGoalDecideIsIdempotent`(cmd/server/goal_verify_test.go)、`TestGoalFailureNoAutoRetryAsksImmediately`(goal_listeners_test.go,顺带断言 DAG 快照)、`TestClaimGoalSubtaskResumesPriorSession`(handler/goal_persist_claim_test.go)。删了锁旧抢跑行为的 `TestGoalVerifyRejectRerunsReviewed`。cmd/server+service+handler+daemon 全过,vet 干净。
注意:goal decide CLI 在 daemon bundled CLI(apps/desktop/resources/bin/multica),改了 CLI 要 `node apps/desktop/scripts/bundle-cli.mjs` 重新 bundle,daemon 会因 version mismatch 自动重启加载。

关联:[[verify-rearm-ignores-depends-on-bug]](本轮修复它)、[[llm-decompose-via-leader-task]]、[[pmo-summary-closeout]]。
