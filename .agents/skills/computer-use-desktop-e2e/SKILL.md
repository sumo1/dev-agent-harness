---
name: computer-use-desktop-e2e
description: Use when validating the Electron desktop app, native macOS UI flows, screenshots, Accessibility state, trace evidence, or end-to-end verification that requires operating the local desktop client through computer-use-harness.
metadata:
  short-description: Verify desktop UI with computer-use-harness
  platform: macos
---

# Computer-Use Desktop E2E

Use this skill before desktop E2E verification for this repository.

## Core Rule

If the target surface is `apps/desktop`, do not rely on browser-only checks.
Use `computer-use-harness` to operate or observe the real macOS desktop client,
unless the task is purely non-UI or the user explicitly says not to run desktop
automation.

```text
candidate desktop -> computer-use CLI -> JSON trace evidence
```

## Harness Location

Resolve the harness directory in this order:

```bash
COMPUTER_USE_HARNESS_DIR=${COMPUTER_USE_HARNESS_DIR:-/Users/sumo/workplace/opensource/computer-use-harness}
```

If that path is missing, stop and report that `computer-use-harness` is not
available on this machine.

Before using details not listed here, read:

```bash
$COMPUTER_USE_HARNESS_DIR/SKILL.md
```

That file is the source of truth for the `computer-use` CLI surface.

## Preflight

```bash
computer-use version || node "$COMPUTER_USE_HARNESS_DIR/dist/cli/index.js" version
```

If the command or dist build is missing:

```bash
cd "$COMPUTER_USE_HARNESS_DIR"
npm install
npm run build
cd native/mac-helper
swift build
```

The helper binary is normally:

```bash
$COMPUTER_USE_HARNESS_DIR/native/mac-helper/.build/debug/computer-use-mac-helper
```

macOS real runs require Accessibility permission. Screenshot/OCR/vision flows
may also require Screen Recording. If the CLI returns `PERMISSION_REQUIRED`,
tell the user exactly which permission is missing and stop.

## Verification Flow

1. Confirm the candidate desktop is running.

   For self-dogfooding worktrees, prefer:

   ```bash
   make -C <candidate-worktree> start-desktop-worktree
   ```

2. Inspect available apps and capabilities.

   ```bash
   computer-use apps --pretty
   computer-use capabilities --app "Dev Agent Harness Canary" --pretty
   ```

   If the app name includes a worktree suffix, use that full name.

3. Observe first, then act.

   ```bash
   computer-use observe --app "<desktop-app-name>" --mac-helper <helper> --pretty
   computer-use click --app "<desktop-app-name>" --keyword "<visible control>" --mac-helper <helper> --pretty
   computer-use type --app "<desktop-app-name>" --text "<text>" --mac-helper <helper> --pretty
   computer-use key --app "<desktop-app-name>" --key Enter --mac-helper <helper> --pretty
   ```

4. Use predefined use cases when the workflow exists.

   ```bash
   computer-use usecases list --pretty
   computer-use usecases dry-run <id> --pretty
   computer-use usecases run <id> --fake --pretty
   computer-use usecases run <id> --mac-helper <helper> --pretty
   ```

5. Always read the latest trace after a real run.

   ```bash
   computer-use trace --last --pretty
   ```

## Evidence Contract

For any desktop E2E claim, include:

- whether `computer-use` was used;
- target app name;
- command or use case ID;
- trace path / trace ID;
- screenshot, AX, or extracted state evidence when available;
- if skipped, the concrete reason.

## References

- `docs/step-e2e-testing/使用-computer-use-验证桌面端.md`
- `/Users/sumo/workplace/opensource/computer-use-harness/SKILL.md`
