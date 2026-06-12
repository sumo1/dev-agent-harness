# S4 — Issue 三栏页 + 图片粘贴 + 删旧入口

> 依赖：S1'（详情读三态 + 跳转用 latest_goal_run_id） ｜ 与 S3' 并行（views vs server）

## 施工契约（怎么做）

1. **放出 sidebar 菜单**：`packages/views/layout/app-sidebar.tsx` 的 `workspaceNav` 加 `{ key: "issues", labelKey: "issues", icon: <选个图标> }`。
2. **新建三栏页** `packages/views/issues/components/issues-page.tsx`（参考 `tasks/components/tasks-page.tsx` 版式）：
   - 左 = 现有全局 sidebar（已在 layout，不重复造）。
   - 中 = issue 列表（复用现成 list query，key `["issues", wsId]`）+ 顶部内联"新建"入口 + 每行三态点。
   - 右 = 选中 issue 详情：图片/描述 + 三态（用 S1' `deriveAutofixStatus`）+ "跳助理会话"按钮（S5 接）。
   - 选中态用本地 component state（对齐 tasks-page，不引 store）。
3. **内联新建（不弹模态）**：中栏顶部展开内联表单，粘贴图片 + 一段话：
   - 复用 `useFileUpload`/`uploadWithToast` → `attachment_ids`。
   - 复用 `useFileDropZone` 拖拽；**新增 paste**：ContentEditor 上 `onPaste` 抓 `clipboardData.files`/image items → 同一 `uploadFile` 路径。
   - 提交调现成 `createIssue` mutation。
4. **删旧入口**（4 处，删前 grep 确认无悬挂）：
   - sidebar "New Issue" 按钮（`app-sidebar.tsx` ≈605）。
   - 全局 `c` 快捷键（≈465）→ 改为导航到 `/{ws}/issues` 并聚焦内联新建（或直接移除快捷键打开模态的行为）。
   - search 命令面板触发 `openCreateIssueWithPreference`。
   - （子 issue 入口 `openCreateSubIssue` 保留——那是 issue 详情内的功能，不属"新建 issue 主入口"。需用户确认是否一并收口，默认保留。）
5. **双端挂路由**：web `apps/web/app/[workspaceSlug]/(dashboard)/issues/page.tsx` import 共享 `IssuesPage`；desktop `routes.tsx` 加 `/:slug/issues`。
6. **i18n**：locales 加 issues 页新键（en/zh-Hans/ko/ja）。

## 验收契约（怎么算做完）

- `packages/views/` 测试：列表渲染、三态点正确映射、paste 触发上传、内联新建提交调 `createIssue`。
- `pnpm typecheck` 0 error；删旧入口后 `grep` 无悬挂引用（`openCreateIssueWithPreference` / `quick-create-issue` 调用点清零或改向）。
- 零 `next/*` / `react-router-dom` 导入（共享包铁律）。
- 桌面端实机：sidebar 出现 Issue 项，点进是三栏，粘贴图片能上传，新建落库。

## 边界

- 只碰 `packages/views/issues`、`packages/views/layout/app-sidebar.tsx`、双端路由文件、locales、删旧入口触发点。
- **不碰** server。跳转按钮的实际跳转逻辑留给 S5。
