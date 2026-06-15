# 需求记录：runtime skills unified entry

> 日期：2026-06-15
> 上游输入：用户口述需求
> 下游：[`breakdown.md`](./breakdown.md)、[`design.md`](./design.md)

## 1. 原始需求

配置页左侧菜单中的 `Skills` 列表需要和运行时结合在一起。

当前系统里运行时可能有多个，例如 Claude Code、Codex。`Skills` 页面不应该只维护一份平台内部的 skills 列表，而应该分运行时读取这些运行时已经绑定或安装的 skills。用户需要能看到：

- 每个运行时分别有哪些 skills。
- 每个 skill 的内容是什么，可以点进去看详情。
- 哪些 skill 是运行时本地已有的。
- 哪些 skill 是平台里自定义创建的。
- 新建 skill 后，可以一键同步或绑定到各个运行时。

核心思路是：`Skills` 不再单独维护一套孤立资源，而是作为统一入口，查看和管理各运行时的 skills。

## 2. 理解确认

基于现有信息，本任务要解决的真问题是：

> 让配置页 `Skills` 成为 Runtime Skill Hub。它统一展示 Claude Code、Codex 等运行时的本地 skills，同时保留平台内可编辑 skills 作为模板和管理层，并提供从平台 skill 同步安装到运行时的能力。

这不是简单地把现有 `RuntimeLocalSkillImportPanel` 放大。现有能力只有“从运行时导入到 workspace skill”；新需求还需要“从 workspace skill 同步到运行时”，并且要能持续判断两边是否一致。

## 3. 术语边界

### Runtime local skill

运行时本地实际可发现、可执行的 skill。它的事实来源是运行时本地 skill 目录，例如：

- Claude Code：`~/.claude/skills`
- Codex：`$CODEX_HOME/skills` 或 `~/.codex/skills`

服务端不能直接读写这些目录，必须继续通过 daemon 异步执行。

### Workspace skill

平台内保存的 skill。它可以被编辑、版本化、展示，也可以作为同步到各运行时的模板。

### Runtime install / sync

把 workspace skill 写入某个 runtime 的本地 skill root，让该运行时真正能发现这个 skill。

### Agent assignment

现有系统中 agent 使用某个 workspace skill 的关系。它不是 runtime install。

这两个概念必须分开。否则后续会出现一个坏味道：用户以为“给 agent 绑定了 skill”，但 Claude Code 或 Codex 的本地 runtime 实际没有这个 skill。

## 4. 成功标准

- `Skills` 页面能按 runtime 查看 skills，而不是只显示 workspace skills。
- 用户能看到 runtime local skill 的详情，包括 `SKILL.md` 和 bundle 文件树。
- 用户能新建 workspace skill，并选择一个或多个 runtime 同步安装。
- 用户能看出每个 skill 在每个 runtime 上的状态：未安装、已同步、运行时有漂移、同名冲突、运行时不支持或离线。
- 所有本地文件读写仍由 daemon 处理，server 不直接碰用户本机目录。
- 现有 workspace skill 列表、skill 详情、agent assignment 不能被破坏。

## 5. 本轮只做

- 记录需求边界。
- 设计信息架构、交互方式、数据结构和 API/daemon 边界。
- 拆出后续可实施的任务阶段。

## 6. 本轮不做

- 不实现 UI。
- 不改 API。
- 不改数据库。
- 不启动候选 worktree。
- 不跑 E2E 验证。
