---
name: verify-rearm-ignores-depends-on-bug
description: 实机验证并行+跨模型 review 时发现真 bug——verify 节点 reject 重审时无视 depends_on，立刻 redispatch 自己，抢在被审节点重跑完成前开审，基于陈旧产出重复 reject，撞 attempt 上限误杀正在 running 的 execute 节点 → goal 卡死。handleVerifyCompleted goal.go:1398-1405
metadata:
  type: project
---

## ✅ 已修复(2026-06-16)
见 [[llm-driven-failure-reject-decisions]]:reject 不再自己重跑/抢跑,改派 goal_decision
给主链路 PMO 决策;`dispatchSubtask` 加 `depsSatisfied` 依赖门结构性杜绝抢跑;attempt
不再做 fail 闸。实机复跑已确认 reject→决策→收敛不卡死。本文保留作根因记录。

## 背景
实机验证「规划并行 fan-out + 同 coder 并发 + 跨模型 review」三档改动。
目标=URL 短链服务(3 块独立 execute + 1 verify + 1 收口)。PMO(claude)拆出正确的
5 节点 fan-out DAG，verify 节点正确派给 codex（跨模型）。前半段全部验证通过：
- 并行：seq1/2/3 同一 coder、同一秒 started、task 层并发 completed ✅
- 跨模型：verify task 真派到 Codex runtime，做了真 24×tool 的对抗审查，跑端到端联调
  发现 CLI↔server 路径契约不匹配，给出 reject ✅（同模型自审很可能漏掉的集成 bug）

## 真 BUG：verify reject 重审无视 depends_on，抢跑 + 误杀
`handleVerifyCompleted`（`server/internal/service/goal.go:1350`）reject 分支：
- 1372-1385：re-arm 每个被审 dep（RearmGoalSubtask）并**立即 dispatchSubtask**。
- 1398-1405：`if anyRetried` → **立即 re-arm 并 redispatch verify 自己**。

问题：verify 被立刻重新派发，而被审的 seq1/2/3 这时刚 re-arm 成 running、**还没重新完成**。
实测：reject 后第二轮 seq1/2/3 和 seq4(verify) **同一秒 16:55:40 一起 dispatched+started**。
verify 不遵守自己的 `depends_on`（应等被审节点重新 completed 才由 unblockDownstream 触发）。

### 危害链（实测复现）
1. 第一轮 verify reject（基于第一轮真实产出，合理）。
2. 被审节点 re-arm（attempt→2/2 上限）+ verify 立即 redispatch（抢跑）。
3. codex verify 跑得快，基于**还没写完的新产出**又一次 reject。
4. 这次 reject 时 dep.Attempt==MaxAttempts → 走 1386-1396 直接 FailGoalSubtask
   （"rejected by verifier, out of attempts"）。
5. 结果：seq2/seq3 被判 **failed，但它们的 execute task 实际还在 running**（孤儿任务，
   completed=-）。goal 停在 executing 不收敛，seq5 永久 pending（依赖 failed 节点）→ **卡死**。

## 修复方向（未实施，待确认）
reject 重审必须尊重 DAG：reject 后只 re-arm 被审 execute 节点，**verify 节点回到
pending**，由被审节点重新 completed 后的 `unblockDownstream` 自然重新 ready+dispatch
verify——而不是在 1400 行立即 redispatch。这样 verify 永远审的是"最新已完成产出"，
也不会在被审节点还在 running 时撞 attempt 上限误杀。

附带要查：FailGoalSubtask 误杀正在 running 的节点时，应同时 cancel 那条孤儿 task
（否则 task 继续跑、completed 回调还会再动一次状态）。

## 证据
goal_id=4f1a6bb3-6adb-4e7c-85d9-f321e99b6410（E2E 验证用，可清理）。
验证手段：computer-use 操作桌面端确认 UI（Electron 键盘输入受限，沿用既有结论），
真实任务经 API 创建+confirm，由真 PMO daemon 规划/调度，全程读 DB + /api/goals 核对。

关联：[[llm-decompose-via-leader-task]]、并行 fan-out + claim 串行闸 + 跨模型 review 三档
（260615 那轮，prompt.go buildGoalPlanningPrompt + agent.sql ClaimAgentTask +
squad_briefing.go roster provider 标识）。
