---
name: paste-image-relative-url-broken-on-desktop
description: 粘贴/上传的图片在桌面端无法访问——LocalStorage 返回站点相对 /uploads/… URL,桌面端渲染进程 origin(:5173)≠ API origin(:8080),相对路径打到 vite 拿到 index.html 当图片。修法:resolveAssetUrl 把 /uploads/… 相对路径按 API base 绝对化
metadata:
  type: project
---

## 现象

新建 issue 粘贴图片后"图片无法访问"。

## 根因(跨 origin,只在桌面端炸)

- `LocalStorage.Upload`(无 `LOCAL_UPLOAD_BASE_URL` 时)返回**站点相对** URL:`/uploads/workspaces/<ws>/<file>.png`(`server/internal/storage/local.go:152`)。
- 编辑器把这个相对路径塞进图片 `src`(`<Attachment url=src>` → `<img src>`)。
- **Web**:渲染进程和 API 同 origin,`/uploads/…` 正常加载。
- **桌面端**:渲染进程在 `http://localhost:5173`(electron-vite),API + 文件服务在 `http://localhost:8080`。相对 `/uploads/…` 解析到 `:5173` → vite 返回 **SPA fallback `index.html`(Content-Type: text/html)** 当图片 → 坏图。这就是"无法访问"。

实证:`curl :5173/uploads/<key>.png` → 200 但 `text/html`;`curl :8080/uploads/<key>.png` → 200 `image/png`。

## 修法(单一 chokepoint)

新增纯函数 `resolveAssetUrl`(`packages/core/api/asset-url.ts`,导出子路径 `@multica/core/api/asset-url`):
- `/uploads/…` 相对路径 → 前缀 API base(`getApi().getBaseUrl()`,桌面端是 :8080)。
- 绝对 URL(S3/CloudFront/已设 `LOCAL_UPLOAD_BASE_URL` 的 LocalStorage)/ `blob:` / `data:` 预览 → 原样透传。
- singleton 未初始化(隔离单测)→ try/catch 返回原值,不抛进 render。

应用点:`packages/views/editor/attachment.tsx` 的 `normalize()`——**live 编辑器节点(image-view.tsx)和只读 markdown(readonly-content.tsx)都走 `<Attachment>`**,改这一处全覆盖。

## 关键工程决策

- **走专用子路径 `@multica/core/api/asset-url` 而不是 `@multica/core/api` 桶**:`@multica/core/api` 被 34+ 个 views 测试 `vi.mock`,往桶里加新导出会让所有 mock 报 "No X export defined"。专用子路径不被 mock,真函数在测试里跑(singleton 未初始化→透传),零 mock 改动。**给被广泛 mock 的 barrel 加导出前先想这点。**
- pure core `resolveAssetUrlWithBase(url, base)` 注入 base 便于单测;`resolveAssetUrl(url)` 是读 singleton 的薄封装。

## 同类教训

又一个**只在桌面端暴露**的 bug(呼应 [[desktop-e2e-found-metadata-schema-strips-autofix]]):凡是"相对 URL / 同 origin 假设"在 web 上无感,在桌面端(渲染与 API 分 origin)必炸。目标端是 `apps/desktop`([[desktop-is-the-target-end]]),这类必须在桌面端验。

关联:[[desktop-e2e-found-metadata-schema-strips-autofix]]、[[desktop-is-the-target-end]]。
