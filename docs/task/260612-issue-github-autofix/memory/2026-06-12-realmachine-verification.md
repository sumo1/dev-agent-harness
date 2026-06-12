# 真机验证（桌面端 Multica Canary）

> 日期：2026-06-12 ｜ 候选 worktree，桌面端实机（requirement 铁律：端到端验证在 apps/desktop 实机跑）

## 怎么验的（正确姿势）

用本机 **computer-use-harness**（`~/workplace/opensource/computer-use-harness`，CLI-first，AX-first）驱动桌面端 **Multica Canary**，**不是**裸 AppleScript 盲点坐标，也**不是**浏览器 web。

```sh
HELPER=native/mac-helper/.build/debug/computer-use-mac-helper
node dist/cli/index.js observe --app "Multica Canary" --mac-helper "$HELPER"
node dist/cli/index.js click  --app "Multica Canary" --keyword "<AX名>" --mac-helper "$HELPER"
node dist/cli/index.js key    --app "Multica Canary" --key "c" --mac-helper "$HELPER"
```

- app 名是 **"Multica Canary"**（DESKTOP_APP_SUFFIX 派生），AX 进程名是 "Electron"。
- 候选桌面端：renderer :14863，连候选后端 :18943，daemon profile `desktop-localhost-18943`，与控制面（8080）隔离。
- 登录走 dev：邮箱任意 + 验证码 **888888**。

## 验证结论

✅ **S4 三栏 Issue 页在桌面端实机渲染正确**（截图实证）：
- sidebar「工作区」组出现 **Issues** 项（任务→Issues→角色→助理→自动化→用量），i18n label = "Issues"。
- 中栏：header "Issues" + 顶部「+」内联新建按钮（非模态）+ issue 列表（`WS-1` 带状态点）。
- 右栏：详情区（"选择一个 issue 查看详情"）。
- 旧「新建 issue」sidebar 按钮已移除；全局 `c` 快捷键现导航到 `/{ws}/issues`（实机按 c 验证跳转成功）。

## 两个踩坑（沉淀）

1. **electron-vite 起桌面端会用陈旧 renderer bundle**：首次起候选桌面端时 sidebar 不含 Issues（跑的是旧 bundle）。修：停桌面端 → `rm -rf apps/desktop/node_modules/.vite apps/desktop/out` → 重起，才吃到当前 worktree 源码。**dogfood 候选桌面端必须清 .vite 缓存重起**，否则验的是旧码。
2. **Electron 缺 electron 本体 dist**：候选 `make setup-worktree` 时 pnpm 拦了 electron 的 postinstall（"Ignored build scripts"），`node_modules/.pnpm/electron@*/node_modules/electron` 缺 `dist`+`path.txt`，起桌面端报 "Electron failed to install correctly"。本机离线下载失败时，**软链控制面同版本 dist** 即可：`ln -s <ctrl>/electron/dist <cand>/electron/dist && cp path.txt`。

## 仍待手动确认（坐标点击 friction，非产品缺陷）

- 内联新建表单展开后的 **agent/project picker 选择 → 提交 → 触发 autofix goal_run** 全链，未在实机完整点完（"+"按钮坐标点击命中不稳）。后端触发逻辑已被 `TestStartAutofixGoalRun_*` 真库集成测试覆盖；UI 渲染已截图确认。建议下次用 AX keyword 绑定（拿到 picker 的精确 AX 名）而非坐标点完这一段。

关联 [[dogfood-reuses-control-plane-db]]、[[2026-06-12-autofix-gate-must-be-optin]]。
