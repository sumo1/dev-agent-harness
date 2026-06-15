# 技术方案：runtime skills unified entry

> 上游：[`requirement.md`](./requirement.md)、[`breakdown.md`](./breakdown.md)
> 本文定：产品信息架构、数据结构、API/daemon 边界、冲突模型、实施阶段。

## §0 基线校准

### 现有能力

- `SkillsPage` 当前以 workspace skills 为主列表，使用 `skillListOptions(wsId)`。
- 页面已经读取 `runtimeListOptions(wsId)`，但 runtime 只用于展示 imported origin，不是主维度。
- `CreateSkillDialog` 里已有 `RuntimeLocalSkillImportPanel`。
- `RuntimeLocalSkillImportPanel` 已支持：
  - 选择一个 runtime。
  - 通过 daemon 请求 runtime local skills。
  - 批量把 runtime local skill import 成 workspace skill。
- runtime local skills 现有 API 是异步 request/poll：
  - `POST /api/runtimes/{id}/local-skills`
  - `GET /api/runtimes/{id}/local-skills/{requestId}`
  - `POST /api/runtimes/{id}/local-skills/import`
  - `GET /api/runtimes/{id}/local-skills/import/{requestId}`
- daemon 已按 provider 映射本地 skill root，例如 Claude、Codex、Copilot、OpenCode、Cursor、Kiro 等。

### 现有缺口

- runtime local skill list 只有 summary：`key/name/description/source_path/provider/file_count`。
- 没有读取 runtime local skill bundle 详情的 API。
- 没有从 workspace skill 同步安装到 runtime 的 API。
- 没有 durable binding 记录，无法稳定判断“这个 runtime 上的 skill 是否由平台管理、是否已经漂移”。
- UI 还是 workspace-first，不是 runtime-first。

## §1 核心产品决策

### 决策 A：Skills 页改成 Runtime Skill Hub

`Skills` 页的第一语义应该是：

> 我有哪些运行时，每个运行时现在能用哪些 skills，平台能把哪些 skills 同步进去。

不是：

> 平台里有几条 skill 配置。

workspace skill 仍然存在，但角色降为“可编辑模板和管理层”。runtime local skill 是运行时实际可发现的能力。

### 决策 B：runtime 本地文件系统仍是真实安装结果

不要把 runtime local skills 全量落库当唯一事实来源。这会变成一个假的真相：数据库说有，Claude Code/Codex 本地未必真的有。

正确结构是：

```text
runtime local filesystem  = 安装事实
workspace skill           = 平台模板
runtime_skill_binding     = 两者关系与同步状态
```

页面展示时重新扫描或使用短期缓存，再和 workspace/binding 合并。

### 决策 C：binding 是必须的数据结构，不要靠 metadata 硬撑

现有 `skill.metadata.origin` 可以表达“这个 workspace skill 从哪个 runtime 导入过”，但它表达不了：

- 同一个 workspace skill 同步到多个 runtime。
- 某个 runtime 上的同名 skill 是否由平台管理。
- 上次同步 hash 和当前 runtime hash 是否一致。
- 冲突原因和最后同步时间。

继续塞 metadata 会让所有状态判断变成字符串考古。这里应该加一张明确的 binding 表。

建议表：`skill_runtime_binding`

```sql
skill_runtime_binding (
  id uuid primary key,
  workspace_id uuid not null,
  skill_id uuid not null,
  runtime_id uuid not null,
  provider text not null,
  runtime_skill_key text not null,
  source_path text,
  sync_status text not null,
  last_synced_hash text,
  last_seen_hash text,
  last_synced_at timestamptz,
  last_seen_at timestamptz,
  conflict_reason text,
  created_at timestamptz not null,
  updated_at timestamptz not null,
  unique (skill_id, runtime_id),
  unique (runtime_id, runtime_skill_key)
)
```

`runtime_skill_key` 必须使用 daemon 侧的规范化 key，不允许前端自己拼路径。

## §2 页面交互设计

### 信息架构

桌面端使用三栏：

```text
┌───────────────┬──────────────────────────────┬──────────────────────────────┐
│ Runtime list  │ Skill list                   │ Skill detail                 │
│               │                              │                              │
│ All runtimes  │ Search / filters / actions   │ SKILL.md / files / status    │
│ Claude Code   │ skill rows                   │ sync / import / edit actions │
│ Codex         │                              │                              │
│ ...           │                              │                              │
└───────────────┴──────────────────────────────┴──────────────────────────────┘
```

移动端降级为：

```text
Runtime tabs -> Skill list -> Detail drawer
```

### 左栏：runtime list

每个 runtime 显示：

- 名称，例如 `Claude Code`、`Codex`。
- provider。
- 在线状态。
- skill 数量。
- 上次扫描时间。
- 是否支持 local skills。
- 是否有待处理 sync。

特殊入口：

- `All runtimes`：看所有 runtime 的 skill 矩阵。
- `Workspace library`：看平台内尚未同步到任何 runtime 的 skills。

### 中栏：skill list

列表按当前 runtime 过滤。每行显示：

- skill 名称和描述。
- provider/source path。
- 文件数。
- 状态 badge。
- 最近同步或扫描时间。
- 主要动作。

筛选：

- All
- Local only
- Workspace only
- Synced
- Drifted
- Conflict
- Unsupported/offline

搜索范围：

- name
- description
- runtime key
- source path

### 右栏：skill detail

详情区域显示：

- `SKILL.md` 预览。
- 文件树和文件内容。
- runtime source path。
- workspace skill 信息。
- binding 状态。
- 被哪些 agents 使用。
- 同步历史摘要。

动作按状态变化：

| 状态 | 主要动作 |
|------|----------|
| runtime local only | Import to workspace、Sync to other runtimes |
| workspace only | Sync to runtimes、Edit、Delete |
| managed synced | Edit workspace、Resync、Open runtime path |
| managed drifted | Compare、Pull from runtime、Push workspace version |
| conflict | Rename and sync、Overwrite、Skip |
| unsupported/offline | Retry when runtime online |

### 新建 skill 流程

新建不应该只弹一个“创建 workspace skill”的表单。需要变成四步：

1. 编辑 skill：name、description、`SKILL.md`、附属文件。
2. 选择目标 runtime：多选 Claude Code、Codex 等。
3. 预检查：显示将写入的 runtime key、路径、冲突。
4. 执行同步：展示每个 runtime 的进度和结果。

默认行为：

- 只创建 workspace skill 时，不写 runtime。
- 勾选 runtime 后，创建成功立即发起 sync。
- 有冲突时默认跳过，必须用户显式选择覆盖或另存为。

### 一键同步

支持两种入口：

- 单个 workspace skill 同步到多个 runtime。
- 当前 runtime 下批量同步多个 workspace skills。

同步前必须有 dry-run 结果：

```text
Claude Code
  computer-use-desktop-e2e: will update
  dev-agent-harness-self-dogfooding: already synced

Codex
  computer-use-desktop-e2e: conflict, local skill is unmanaged
```

没有 dry-run 的“一键覆盖”是坏设计，迟早把用户本地技能目录写坏。

## §3 状态模型

页面不要直接靠 if/else 拼状态。先统一合并成一类行：

```ts
type RuntimeSkillViewStatus =
  | "runtime_local"
  | "workspace_only"
  | "managed_synced"
  | "managed_drifted"
  | "conflict"
  | "unsupported"
  | "offline";

type RuntimeSkillViewRow = {
  runtimeId: string | null;
  provider: string | null;
  skillId: string | null;
  runtimeSkillKey: string | null;
  name: string;
  description: string;
  sourcePath: string | null;
  fileCount: number;
  status: RuntimeSkillViewStatus;
  lastSyncedHash: string | null;
  lastSeenHash: string | null;
  conflictReason: string | null;
};
```

状态派生规则：

| 条件 | 状态 |
|------|------|
| runtime 不支持 local skills | `unsupported` |
| runtime 不在线或请求超时 | `offline` |
| 只有 runtime local skill，无 binding | `runtime_local` |
| 只有 workspace skill，无 runtime 安装 | `workspace_only` |
| 有 binding，`last_synced_hash == last_seen_hash` | `managed_synced` |
| 有 binding，hash 不一致 | `managed_drifted` |
| runtime 有同名 skill，但 binding 指向不同 workspace skill 或 unmanaged | `conflict` |

hash 使用 canonical bundle：

```text
hash = sha256(
  sorted files by path,
  each path normalized with slash,
  each content normalized exactly as bytes
)
```

不要只 hash `SKILL.md`。skill 是 bundle，不是单文件。

## §4 API 和 daemon 边界

### 保留现有 list/import API

现有“runtime -> workspace import”继续保留，只是从弹窗能力升级为页面主动作之一。

### 新增 detail API

读取 runtime local skill 详情仍然走 request/poll：

```http
POST /api/runtimes/{runtimeId}/local-skills/detail
{
  "skill_key": "computer-use-desktop-e2e"
}

GET /api/runtimes/{runtimeId}/local-skills/detail/{requestId}
```

daemon 执行：

```text
loadRuntimeLocalSkillBundle(provider, skill_key)
```

返回：

```json
{
  "status": "completed",
  "skill": {
    "key": "computer-use-desktop-e2e",
    "name": "computer-use-desktop-e2e",
    "description": "...",
    "source_path": "~/.codex/skills/computer-use-desktop-e2e",
    "provider": "codex",
    "content": "...SKILL.md...",
    "files": [
      { "path": "references/foo.md", "content": "..." }
    ],
    "hash": "sha256:..."
  }
}
```

### 新增 sync API

从 workspace skill 写入 runtime：

```http
POST /api/runtimes/{runtimeId}/local-skills/sync
{
  "skill_id": "...",
  "runtime_skill_key": "computer-use-desktop-e2e",
  "mode": "dry_run" | "apply",
  "conflict_policy": "skip" | "overwrite" | "rename"
}

GET /api/runtimes/{runtimeId}/local-skills/sync/{requestId}
```

daemon 执行：

1. 根据 provider 找 root。
2. 规范化 `runtime_skill_key`。
3. 读取目标目录当前状态。
4. dry-run 返回 diff/conflict。
5. apply 时写入临时目录。
6. 原子 rename 到目标目录。
7. 返回 `source_path`、`hash`、`written_files`。

### 安全边界

- server 只创建请求和持久化结果，不直接读写 runtime 本地文件。
- daemon 负责路径校验、bundle 大小限制、文件数量限制。
- `runtime_skill_key` 禁止绝对路径、`..`、空路径。
- 写入前必须确认 provider 支持 local skills。
- 离线 runtime 只能排队或失败，不能假装成功。

## §5 数据流

### 扫描 runtime skills

```text
用户点击 Refresh
  -> POST list request
  -> daemon heartbeat pop request
  -> daemon scan runtime skill root
  -> daemon report summaries
  -> UI merge workspace skills + bindings + runtime summaries
```

### 查看详情

```text
用户点击 runtime local skill
  -> POST detail request(skill_key)
  -> daemon load bundle
  -> UI 展示 SKILL.md + files
```

### 新建并同步

```text
用户创建 workspace skill
  -> POST /api/skills
  -> 对每个选中 runtime 发起 dry_run
  -> 用户确认冲突策略
  -> 对每个 runtime 发起 apply sync
  -> daemon 写入 runtime skill root
  -> server upsert skill_runtime_binding
  -> UI refresh runtime inventory
```

### 运行时漂移

```text
runtime 被用户手工修改 skill
  -> 下次扫描返回新 hash
  -> last_seen_hash != last_synced_hash
  -> 状态变 managed_drifted
  -> 用户选择 Pull from runtime 或 Push workspace version
```

## §6 实施阶段

### Phase 1：只读 Runtime Skill Hub

目标：先把页面主维度改对。

- 重构 `SkillsPage` 为 runtime-first。
- 保留现有 workspace skill 列表能力。
- 复用现有 runtime local skills list。
- 增加 view model 纯函数。

不做同步写入。

### Phase 2：runtime local detail

目标：补齐点进去看详情。

- 新增 detail request/poll store。
- daemon 复用 `loadRuntimeLocalSkillBundle`。
- UI detail panel 支持 runtime bundle。

### Phase 3：workspace -> runtime sync

目标：真正完成一键同步。

- 新增 binding 表。
- 新增 sync request/poll store。
- daemon 写入 runtime root。
- UI 加 dry-run、冲突处理、进度展示。

### Phase 4：批量与体验收口

目标：把单点能力变成可用工作流。

- 多 runtime 批量同步。
- drift compare。
- pull from runtime。
- 同步结果 toast 和 activity。
- desktop E2E 验证。

## §7 破坏性风险

1. **误伤现有 workspace skills 页面。** 先用 view model 包一层，不改底层 skill CRUD 语义。
2. **runtime install 和 agent assignment 混淆。** 文案和类型都要分开，不用同一个字段表达两件事。
3. **本地目录写坏。** sync 必须 dry-run，默认 skip conflict，apply 用临时目录加原子替换。
4. **Codex 环境错位。** daemon 使用 runtime 进程环境里的 `CODEX_HOME`，不要前端/server 猜。
5. **离线 runtime 状态误导。** timeout/offline 必须显式显示，不能拿旧扫描结果当当前真相。
6. **多文件 skill 丢内容。** detail/sync 都按 bundle 处理，不只处理 `SKILL.md`。

## §8 本轮不做

- 不做 marketplace。
- 不做 skill 版本历史。
- 不做跨 workspace 共享。
- 不做自动后台定时扫描。
- 不把 agent assignment 改成 runtime install。
- 不让 server 直接读写 `~/.claude` 或 `~/.codex`。
