# E2E verification memory

Date: 2026-06-16

## Verification target

- Worktree: `/Users/sumo/workplace/opensource/multica/multica-sumo/.dogfood-worktrees/agent-harness-session-architecture-20260616-160317`
- Branch: `codex/dogfood-agent-harness-session-architecture-20260616-160317`
- Backend: `http://localhost:18186`
- Web renderer: `http://localhost:13106`
- Desktop renderer: `http://localhost:14106`
- Desktop app: `Dev Agent Harness Canary agent_harness_session_architecture_20260616_160317`
- Desktop PID: `70186`
- Workspace: `c5a9d445-2406-4f8b-927e-42bb99d2984b`
- Test user: `autofix-e2e@multica.ai`

The candidate server, daemon, and desktop were started from the candidate
worktree. The control-plane checkout and desktop were not restarted or used for
verification.

## Commands that passed

```text
git diff --check
pnpm --filter @multica/core typecheck
pnpm --filter @multica/views typecheck
pnpm --filter @multica/desktop typecheck
cd server && go test ./internal/daemon -run 'TestBuildPromptInjectsRuntimeContext|TestBuildQuickCreatePromptRules'
cd server && go test ./internal/handler -run 'TestOpenClaw|Test.*Issue|Test.*Rerun|Test.*Autofix'
cd server && go test ./internal/handler ./internal/daemon
pnpm --filter @multica/core exec vitest run session-commands/registry.test.ts
cd server && go test ./pkg/agent -count=1 -parallel=1
```

Note: `go test ./pkg/agent -count=1` has a known parallel-resource failure mode
in this environment. The low-parallelism run passed and is the evidence used
here.

## Desktop evidence

`computer-use-harness` was used against the candidate Electron process:

```text
computer-use version
computer-use doctor --pid 70186 --pretty
computer-use observe --pid 70186 --summary --text --pretty
computer-use trace --last --pretty
```

Trace:

```text
/Users/sumo/workplace/opensource/multica/multica-sumo/.dogfood-worktrees/agent-harness-session-architecture-20260616-160317/.computer-use/traces/trace_action_dd01823c-4b7e-4059-a1e2-3ac3cf060ab7.jsonl
```

The harness resolved the candidate process and saw one window:

```text
pid: 70186
app name: Dev Agent Harness Canary agent_harness_session_architecture_20260616_160317
window title: Agents
permissions: accessibility=granted, screenRecording=granted
```

Limitation: the desktop observation still only exposed the Electron
`AXApplication` root and the app/window title. It did not expose inner controls
or readable page text. Therefore this run proves candidate desktop process,
permissions, window resolution, API/WS activity, and trace capture, but it is
not a full visual UI interaction proof.

## Case 1: Goal DAG/session path

A minimal explicit Goal was created through candidate API with one confirmed
subtask, avoiding the PMO auto-decompose LLM path while still exercising the
Goal DAG execution path.

```text
goal_run_id: 413f3b27-609f-48c4-ab03-25f273635de8
goal title: E2E goal DAG evidence 20260616T140221Z
goal status: failed
goal_subtask_id: df292c13-af01-483e-ac2a-53d87095eb0c
latest task_id: 3847cd56-849b-4c1c-ab1a-ae14b9e15d7e
subtask status: failed
attempt: 2 / 2
```

Evidence:

- API `POST /api/goals` returned `status=executing` with a `goal_subtask`.
- DB `goal_run` moved to `failed` after both attempts failed.
- DB `agent_task_queue` rows were linked through `goal_subtask_id`.
- API `GET /api/goals/413f3b27-609f-48c4-ab03-25f273635de8` returned the subtask
  with `task_id=3847cd56-849b-4c1c-ab1a-ae14b9e15d7e`.
- API `GET /api/tasks/3847cd56-849b-4c1c-ab1a-ae14b9e15d7e/messages` returned
  one task message: `Not logged in · Please run /login`.
- Server/daemon log showed `goal created`, `goal subtask dispatched`, `task claimed`,
  `task started`, `task failed`, and the automatic retry dispatch.

Conclusion: the complex-task path remains a Goal DAG path. The run failed only
because the local Claude runtime is not logged in, not because the Goal DAG
plumbing regressed.

## Case 2: Issue direct session path

An assigned backlog Issue was created, then explicitly run through
`POST /api/issues/{id}/run`.

```text
issue_id: 9c688407-a6fc-4cae-bb41-a1cb05b2b4de
identifier: WS-2
task_id: 63e8d7b7-63f4-412f-a315-8b9b23130bca
runtime_id: 5cf720d0-6a38-4927-973d-5dff037ac1ee
status: failed
```

Evidence:

- API `POST /api/issues` created the Issue without creating a Goal.
- API `POST /api/issues/9c688407-a6fc-4cae-bb41-a1cb05b2b4de/run` returned
  `202` and a direct `agent_task_queue` task.
- DB `issue.metadata` remained `{}`.
- DB query for matching `goal_run.title/goal` returned no rows for this Issue.
- API `GET /api/issues/9c688407-a6fc-4cae-bb41-a1cb05b2b4de/task-runs` returned
  task `63e8d7b7-63f4-412f-a315-8b9b23130bca`.
- API `GET /api/tasks/63e8d7b7-63f4-412f-a315-8b9b23130bca/messages` matched DB
  `task_message`: `Not logged in · Please run /login`.
- Server/daemon log showed `task enqueued`, `issue rerun enqueued`, `task claimed`,
  `task started`, `task failed`.

Conclusion: Issue execution is a direct issue session. It no longer implicitly
creates or routes through a complex Goal workflow.

## Case 3: Assistant retry and runtime control

An assistant chat session was created with the Codex runtime, and the retry
shortcut text was sent as a visible user chat message.

```text
chat_session_id: 4487d5d3-477b-43ab-9fea-8e98cac5d61d
message_id: ac92edc3-2d17-418d-b045-d9f15e5e73c5
task_id: eeb35a3d-6629-43c2-9dbd-d4df52d76887
final task status: cancelled
```

Evidence:

- DB `chat_message` contains a visible user message:
  `Please retry based on the visible conversation history. Explain what changed before continuing.`
- API `GET /api/chat/sessions/4487d5d3-477b-43ab-9fea-8e98cac5d61d/messages`
  returned the same user message.
- DB `agent_task_queue.chat_session_id` links the run to the chat session and has
  no `issue_id`.
- API `POST /api/tasks/eeb35a3d-6629-43c2-9dbd-d4df52d76887/cancel` returned
  `status=cancelled`.
- Server/daemon log showed task messages streaming, then `task cancelled`.
- `packages/core/session-commands/registry.ts` models `retry` as
  `prompt_shortcut` and `interrupt/cancel` as `runtime_control`; registry tests
  passed.

Conclusion: retry is represented as a visible prompt shortcut message, while
cancel/interrupt are runtime controls against the active run.

## Case 4: OpenClaw/Lobster channel

Native OpenClaw is not installed on this machine:

```text
GET /api/channels/openclaw/status
status: disconnected
last_error: openclaw executable not found on PATH
capabilities: conversations=false, automations=false, native_write=false
```

The unavailable native connector was recorded as an environment limitation.
Synthetic dispatch fallback was then verified through candidate API:

```text
conversation_id: conv-e2e-20260616T134900Z
dispatch_as_issue -> issue_id: c682d7cb-3803-4d04-8df5-2c7009206577
dispatch_as_goal -> goal_run_id: 8c5306b2-f072-4621-ab87-4a10c30e3c57
continue_in_assistant -> unsupported, because no OpenClaw runtime is registered
```

Evidence:

- `GET /api/channels/openclaw/conversations` returned an empty list plus the
  native executable error.
- `GET /api/channels/openclaw/automations` returned an empty list plus the same
  native executable error.
- `POST /api/channels/openclaw/conversations/{id}/dispatch` with target `issue`
  created an Issue whose description contains:

```text
<channel_context>
provider: openclaw
channel: lobster
external_conversation_id: conv-e2e-20260616T134900Z
</channel_context>
```

- Dispatch with target `goal` created `goal_run_id=8c5306b2-f072-4621-ab87-4a10c30e3c57`
  and stored the same `channel_context` in `goal_run.goal`.
- Dispatch with target `assistant` returned `unsupported`, which is correct
  without a registered OpenClaw runtime.
- `WorkItemKind` remains exactly `"goal" | "issue" | "assistant"` in
  `packages/core/types/agent-session.ts`; OpenClaw is represented as
  runtime/channel/automation provider, not a fourth work item kind.

Conclusion: OpenClaw is modeled as an external channel/runtime source. Native
sync is blocked by missing local executable, but fallback dispatch and
`channel_context` propagation work.

## RuntimeContext prompt evidence

Prompt injection is covered by `server/internal/daemon/prompt_test.go`.

The test verifies:

- Issue prompt contains `<runtime_context>`, `work_item_kind: issue`,
  `work_item_id`, `workspace_id`, `runtime_id`, `runtime_provider`, and
  `task_queue_job_id`.
- Goal prompt contains `work_item_kind: goal`, goal id/title/description.
- Assistant prompt contains `work_item_kind: assistant`, chat session id, and
  chat content.

The targeted daemon prompt tests passed in this worktree.

## Residual risks

- Desktop UI extraction is still weak: `computer-use` can resolve and trace the
  candidate app, but AX/visual text currently exposes only the Electron root.
  Full click-through verification of ContextBar/CommandBar/Lobster controls
  remains blocked on better desktop accessibility extraction.
- Native OpenClaw was not available on PATH. Real conversation sync and
  automation command forwarding were not exercised against a live OpenClaw
  process.
- The Goal and Issue runtime attempts failed because the local Claude runtime is
  not logged in. This is environment auth state, not a regression in dispatch,
  DB linking, or transcript plumbing.
- The verification database now contains deliberate evidence records listed
  above. Do not treat them as product seed data.
