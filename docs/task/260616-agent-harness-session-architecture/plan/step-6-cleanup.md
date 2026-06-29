# step-6: 清理耦合和命名债

> 所属任务: 260616-agent-harness-session-architecture ｜ 依赖: step-5 ｜ 并行组: 独立串行

## 施工契约（给执行 Agent）

### 范围

- 可改文件:
  - 旧 Issue -> Task 隐式入口相关文件
  - 文案 locale
  - docs memory
  - tests
- 不可改文件 / 冻结边界:
  - 不删除内部 task_queue。
  - 不破坏旧数据读取。

### 产出清单

- 删除 Issue 默认创建复杂任务的隐式链路。
- 清理产品层“Task”泛化文案。
- 增加回归测试锁住：
  - Issue direct run 不创建 goal_run。
  - retry 追加消息。
  - interrupt 在三类 session 可用。
- 在 `memory/` 记录关键迁移结论和被否决方案。

### 约束

- 清理只能在新路径验证后执行。
- 对旧数据只做兼容读取，不做危险迁移。

## 验收契约（给验收）

### 代码结构验证

- [ ] grep 不再出现 Issue 默认走 Goal/Task 的调用链。
- [ ] internal `task_queue` 仍可用，但 UI/API 文案不混用。
- [ ] docs 更新说明新分层。

### 命令验收

| 命令 | 通过标准 |
|------|---------|
| `pnpm --filter @multica/views typecheck` | 0 error |
| `pnpm --filter @multica/core typecheck` | 0 error |
| `cd server && go test ./internal/handler ./internal/daemon` | 0 fail |

