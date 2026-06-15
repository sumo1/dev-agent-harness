# 任务拆解：runtime skills unified entry

> 上游：[`requirement.md`](./requirement.md)
> 下游：[`design.md`](./design.md)、后续 `plan/step-*.md`

## 1. 核心判断

✅ 值得做。

原因很简单：skills 本质上是运行时能力，不是平台里一张孤立的配置表。现在平台已经有了 workspace skill，也有 runtime local skill import 的半条链路。继续沿着“平台单独维护 skills”走，会让 Claude Code、Codex、后续 runtime 的真实能力越来越难判断。

这次应该把数据关系理顺：

```text
workspace skill        runtime skill root
      │                       │
      └──── binding/sync ─────┘
```

workspace skill 是可编辑模板，runtime local skill 是实际安装结果，binding/sync 记录两者关系和漂移。

## 2. 子任务拆解

### S1：统一 skill 状态模型

目标：定义 workspace skill、runtime local skill、binding 之间的合并模型。

交付：

- TypeScript domain model。
- Go response model。
- 状态派生纯函数。
- bundle hash 规则。

验收：

- 能从 `workspace skills + runtime local skills + bindings` 派生出页面需要的行。
- 不依赖前端临时 if/else 猜状态。

### S2：runtime local skill detail

目标：在现有 runtime local skills list/import 之外，补齐详情读取能力。

交付：

- server request/poll API。
- daemon 读取 bundle。
- core query 封装。

验收：

- 用户点击 runtime local skill 后能看到 `SKILL.md` 和附属文件。
- server 仍不直接读本机文件。
- 超大文件、路径穿越、符号链接等边界沿用现有 daemon 限制。

### S3：workspace skill 同步到 runtime

目标：新增从平台 skill 安装/同步到 runtime local skill root 的反向链路。

交付：

- sync request/poll API。
- daemon 写入 runtime skill root。
- 冲突策略：跳过、覆盖、另存为。
- binding 状态更新。

验收：

- 单个 skill 能同步到多个 runtime。
- 同名冲突不会静默覆盖。
- 同步成功后 runtime 重新扫描能识别为 `managed_synced`。

### S4：Skills 页面交互改造

目标：把现有 Skills 页从 workspace 列表改成 Runtime Skill Hub。

交付：

- runtime-first 三栏布局。
- runtime 列表、skill 列表、skill 详情。
- 新建 skill + 选择 runtime + 同步。
- 状态筛选和冲突提示。

验收：

- 用户一眼能看出 Claude Code 和 Codex 各有哪些 skills。
- 用户能点开任意 skill 看详情。
- 用户能从页面发起“一键同步到 runtime”。

### S5：验证与回归

目标：验证 UI、API、daemon 三段链路。

交付：

- 单元测试：状态派生、bundle hash、路径校验、冲突策略。
- API 测试：detail/sync request lifecycle。
- desktop E2E：通过 computer-use-harness 验证本地客户端流程。

验收：

- 不影响现有 skill 创建、编辑、导入、agent assignment。
- 不重启或污染当前控制面实例。

## 3. 串并行关系

```text
S1 统一状态模型
│
├─ S2 runtime detail API
├─ S3 workspace -> runtime sync API
│
└─ S4 页面交互改造
    │
    ▼
S5 验证与回归
```

- S1 必须先做。数据结构不定，后面 UI 和 API 都会乱。
- S2/S3/S4 可以在 S1 契约固定后并行。
- S5 必须最后做，尤其 desktop E2E 要验证候选 worktree，不碰当前控制面。

## 4. 文件范围预判

### 前端

- `packages/views/skills/components/*`
- `packages/views/locales/*/skills.json`
- `packages/core/runtimes/*`
- `packages/core/workspace/*`
- `packages/core/types/*`

### 后端

- `server/internal/handler/runtime_local_skills.go`
- `server/internal/handler/daemon.go`
- `server/internal/daemon/local_skills.go`
- `server/internal/daemon/daemon.go`
- `server/pkg/db/migrations/*`
- `server/pkg/db/query/*`

### 验证

- `packages/views/skills/**/*.test.tsx`
- `server/internal/handler/*runtime_local_skills*_test.go`
- `server/internal/daemon/local_skills_test.go`
- desktop E2E 证据写入 task memory。

## 5. 关键风险

- 把 runtime install 和 agent assignment 混为一谈。
- server 直接读写用户本机 skill 目录。
- 同名 skill 静默覆盖。
- Codex 的 `CODEX_HOME` 和 task/worktree 环境不一致。
- 只读取 `SKILL.md`，忽略 skill bundle 里的其他文件。
- 离线 runtime 的旧数据被误认为当前真实状态。
