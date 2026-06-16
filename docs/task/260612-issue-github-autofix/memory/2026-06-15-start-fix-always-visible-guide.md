---
name: start-fix-button-always-visible-with-guide
description: 修"Issue 详情快捷操作按钮都不见了"——「启动修复」门槛(绑project+指派agent)太严+二选一硬分支，导致随手建的 issue 只剩一句灰提示、零按钮。改成按钮常显，点击若不达标内联引导补绑
metadata:
  type: project
---

## 现象
用户："Issue 里之前说的主动执行按钮、合并/完成这些按钮为什么都不见了？"

## 根因（门槛 + 入口不匹配）
QuickActions 的 not_started 分支是**二选一硬分支**：
- 满足 `canStartAutofix`(绑 project + 指派 agent/squad) → 显示「启动修复」
- 否则 → **只显示一句灰提示，一个按钮都没有**

而 Issue 页的内联新建表单（之前简化过）**不让选 project/agent**，所以随手建的 issue 永远不达标
→ 永远落到 else → 按钮全不见。重试/完成/合并那一组又只在"有 chat_session(修复已启动)"才显示，
没启动过自然也到不了。门槛和入口对不上。

## 修法（用户拍板：按钮常显，缺啥点了引导补）
- not_started 分支：**「启动修复」按钮总是渲染**（不再因不达标而消失）。
- 点击时：达标 → `startAutofix.mutate()`；不达标 → `setShowStartGuide(true)` 内联展示引导文案，
  按缺失项分三句（缺 both / 缺 project / 缺 agent），指向详情头部已有的 assignee/project pickers。
  **不发会 400 的请求。**
- 重试/补充/完成/合并 那组**保持"有 chat_session 才显示"**（这些是对进行中修复发指令，没 run 无对象）。
  用户确认这个前提合理。
- 新增 i18n `quick_actions.{start_need_both,start_need_project,start_need_agent}` × 4 locale。

## 实机验证
bare issue(无 project 无 agent)：以前只剩灰提示 → 现在「启动修复」按钮常显 ✅；点它 → 弹引导
"请先在上方绑定工作目录并指派一个 agent，再启动修复。" ✅，不发请求。page 测试 + parity 全过。

## 教训
**简化新建入口时一并放宽下游门槛**：把"建 issue 时选 project/agent"砍掉，却没同步放宽"启动修复要求
project+agent"，等于把功能锁死。入口和门槛要一起设计。按钮"消失"比"禁用+提示"更让用户困惑——
宁可常显+引导，别凭条件静默隐藏。

关联：[[quick-actions-and-failed-state-landed]]、[[pick-folder-git-detect-and-start-fix-landed]]、[[desktop-is-the-target-end]]。
