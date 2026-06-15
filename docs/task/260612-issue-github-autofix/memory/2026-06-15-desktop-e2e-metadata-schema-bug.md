---
name: desktop-e2e-found-metadata-schema-strips-autofix
description: 桌面端实机 E2E 揪出的真 bug——IssueMetadataSchema 只收基本类型值,把嵌套的 metadata.autofix 整个打回 {},导致跳转按钮永远禁用、三态永远 not_started。单测没覆盖到因为它断言的是错误行为
metadata:
  type: project
---

## 怎么发现的

API 层 E2E（CLI→handler→DB→派生函数）全绿,但**目标端是桌面客户端**(见 [[desktop-is-the-target-end]])。
在 Electron 上用 computer-use 真操作:登录(用户帮输验证码,window 拿不到 key focus 是 computer-use
在本机的已知限制)→ 进 Issue 三栏页 → 选 issue → 点「打开助理会话」。**按钮 `enabled=False`**,
即使 DB 里 `issue.metadata.autofix.latest_goal_run_id` 已写入。

## 根因(纯前端 schema bug,后端/CLI 都对)

`packages/core/api/schemas.ts` 的 `IssueMetadataSchema`:

```ts
z.record(z.string(), z.union([z.string(), z.number(), z.boolean()])).default({})
```

值只允许基本类型。但 autofix 流程经 `SetIssueMetadataKey` 往 `metadata.autofix` 写的是**嵌套对象**。
zod record 的值校验拒绝对象 → `parseWithFallback` 整条 issue 回退 → **metadata 变成 `{}`**。
于是 `parseAutofixMetadata` 永远拿到空 → `latestRunId=""` → 跳转按钮 `disabled`、`deriveAutofixStatus`
永远 `not_started`。**整个 autofix 三态 + 跳转在桌面端从来没真正工作过。**

## 为什么单测没抓到

`schemas.test.ts` 里有一条 `it("rejects metadata with non-primitive values (nested object)")`——
它**断言的就是这个 bug 行为**(把"拒绝嵌套对象"当成正确)。测试锁住了错误契约,绿灯反而是假象。
这跟之前 completed 态读 `github.issue_url` 当 PR url 的错位是同一类:测试编码了 bug。

## 修法

值 union 增加对象分支(其余 key 仍是基本类型,契约不破):

```ts
z.record(z.string(), z.union([z.string(), z.number(), z.boolean(), z.record(z.string(), z.unknown())])).default({})
```

把那条"rejects nested object"测试改成"accepts nested autofix blob + 基本类型 key 仍透传"。
HMR 后桌面端按钮立刻 `enabled=True`、状态从"未启动"变"进行中",点击 → 窗口标题翻到「Assistant」、
会话列表选中"自动修复：天为什么是蓝的"——S5 跳转链路实机跑通。

## 教训

- **API 层验证 ≠ 端到端**:这个 bug 在 CLI/handler/service/DB/派生函数全绿的情况下依然存在,
  因为它在"API 响应 → 前端 zod parse"那一跳,只有真在桌面端点按钮才暴露。目标端永远是 `apps/desktop`。
- **写 metadata 的特性要同步放宽读它的 schema**。后端往一个历史上"primitive-only"的 JSONB 里塞结构化对象时,
  共享的响应 schema 是会把它默默吃掉的——这正是 API Response Compatibility 那套(parse-don't-cast + fail-soft)
  的反面:fail-soft 退到 `{}` 把功能给 fail 没了。
- computer-use 在本机对 Electron **AXPress 点击好使,但键盘输入打不进**(window 不成为 key window),
  登录这类需要键入的步骤让用户代劳;点击/导航/observe 类全自动。

关联:[[autofix-artifact-reportback-channel]]、[[desktop-is-the-target-end]]、[[repo-ssot-persist-and-judgment-landed]]。
