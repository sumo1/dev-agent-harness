"use client";

import { useCallback, useMemo, useRef, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Plus,
  X as XIcon,
  ExternalLink,
  ImageOff,
  RotateCcw,
  MessageSquarePlus,
  GitBranchPlus,
  CheckCircle2,
  Send,
  Play,
} from "lucide-react";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  issueListOptions,
  issueAttachmentsOptions,
  issueKeys,
} from "@multica/core/issues/queries";
import { useCreateIssue, useUpdateIssue } from "@multica/core/issues/mutations";
import { goalRunOptions } from "@multica/core/goals/queries";
import { chatMessagesOptions, pendingChatTaskOptions } from "@multica/core/chat/queries";
import { agentListOptions } from "@multica/core/workspace/queries";
import { ChatMessageList } from "../../chat/components/chat-message-list";
import { ChatInput } from "../../chat/components/chat-input";
import { TaskStream } from "../../tasks/components/task-stream";
import { GoalStatusTree } from "../../assistant/components/goal-status-tree";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { ListTree, ChevronDown } from "lucide-react";
import type { GoalRun, GoalSubtask } from "@multica/core/types";
import { deriveAutofixStatus, parseAutofixMetadata } from "@multica/core/issues";
import { paths, useWorkspaceSlug } from "@multica/core/paths";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import type {
  Attachment,
  Issue,
  IssueAssigneeType,
  UpdateIssueRequest,
} from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useNavigation } from "../../navigation";
import {
  ContentEditor,
  type ContentEditorRef,
  ReadonlyContent,
  useFileDropZone,
  FileDropOverlay,
} from "../../editor";
import { PillButton } from "../../common/pill-button";
import { AssigneePicker } from "./pickers/assignee-picker";
import { ProjectPicker } from "../../projects/components/project-picker";
import { StatusIcon } from "./status-icon";
import { useT } from "../../i18n";
import { createLogger } from "@multica/core/logger";

const logger = createLogger("issues.autofix-page");

const EMPTY_ATTACHMENTS: Attachment[] = [];

/**
 * Issue mode page (issue-github-autofix design §2). Three columns:
 *
 *   left  = the global app sidebar (already in layout; not rendered here)
 *   middle = issue list, with an inline "New" entry at the top + a per-row
 *            autofix three-state dot
 *   right  = the selected issue's detail: images / attachments + description
 *            + the autofix three-state + a "jump to assistant session" button
 *
 * Selection is local component state (mirrors tasks-page — no store), keyed off
 * the existing workspace issue-list query (`["issues", wsId, …]`). The autofix
 * status is derived per-row via S1's `deriveAutofixStatus`, reading
 * `issue.metadata.autofix` defensively.
 */
export function AutofixIssuesPage() {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();

  const { data: issues = [], isLoading } = useQuery(issueListOptions(wsId));

  // Selected issue + whether the inline create form is expanded. Both are
  // ephemeral UI state, so they live here, not in a store.
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);

  const selected = useMemo(
    () => issues.find((i) => i.id === selectedId) ?? null,
    [issues, selectedId],
  );

  const handleSelect = useCallback((id: string) => {
    setSelectedId(id);
    setCreating(false);
  }, []);

  return (
    // h-full (not h-screen): the page mounts below the app top bar, so 100vh
    // would push each column's scroll-container end off-screen.
    <div className="flex h-full min-h-0">
      {/* Middle: issue list */}
      <div className="flex min-h-0 w-80 shrink-0 flex-col border-r bg-muted/20">
        <div className="flex items-center justify-between border-b px-4 py-3">
          <h2 className="text-sm font-semibold">{t(($) => $.page.breadcrumb_title)}</h2>
          <Button
            variant="ghost"
            size="icon-sm"
            className="rounded-full"
            aria-label={t(($) => $.autofix_page.new_issue)}
            title={t(($) => $.autofix_page.new_issue)}
            onClick={() => {
              setCreating((v) => !v);
              setSelectedId(null);
            }}
          >
            <Plus className="size-4" />
          </Button>
        </div>

        <div className="flex-1 overflow-y-auto">
          {creating && (
            <InlineCreateForm
              onClose={() => setCreating(false)}
              onCreated={(id) => {
                setCreating(false);
                setSelectedId(id);
              }}
            />
          )}

          <div className="p-2">
            {isLoading ? (
              <p className="px-2 py-6 text-center text-xs text-muted-foreground">
                {t(($) => $.autofix_page.loading)}
              </p>
            ) : issues.length === 0 ? (
              <p className="px-2 py-6 text-center text-xs text-muted-foreground">
                {t(($) => $.page.empty_title)}
              </p>
            ) : (
              issues.map((issue) => (
                <IssueListRow
                  key={issue.id}
                  issue={issue}
                  active={selectedId === issue.id}
                  onSelect={() => handleSelect(issue.id)}
                />
              ))
            )}
          </div>
        </div>
      </div>

      {/* Right: selected issue detail */}
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        {selected ? (
          <IssueDetailColumn issue={selected} />
        ) : (
          <div className="flex flex-1 items-center justify-center p-6">
            <p className="max-w-xs text-center text-sm text-muted-foreground">
              {t(($) => $.autofix_page.empty_detail)}
            </p>
          </div>
        )}
      </div>
    </div>
  );
}

/** Per-state color + short label for the autofix status, shared by the list
 *  badge and (potentially) other surfaces. */
const AUTOFIX_STATE_STYLE: Record<
  ReturnType<typeof deriveAutofixStatus>["state"],
  { dot: string; text: string }
> = {
  completed: { dot: "bg-success", text: "text-success" },
  failed: { dot: "bg-destructive", text: "text-destructive" },
  needs_info: { dot: "bg-warning", text: "text-warning" },
  running: { dot: "bg-primary", text: "text-primary" },
  not_started: { dot: "bg-muted-foreground/40", text: "text-muted-foreground" },
};

/**
 * List-row autofix status badge. Unlike the old dot-only version, this fetches
 * the issue's latest goal_run (when it has one) so the state is REAL, not always
 * "not_started" — that was the bug that made every fixing issue look unstarted.
 * The query shares `goalRunOptions`' cache key with the detail column, so
 * selecting the issue doesn't refetch. A short text label rides next to the dot
 * so states are distinguishable without hovering. Issues that never started a
 * fix show just a faint dot (no label noise).
 */
function AutofixBadge({ issue }: { issue: Issue }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const autofix = useMemo(() => parseAutofixMetadata(issue), [issue]);
  const latestRunId = autofix.latest_goal_run_id ?? "";
  const { data: goalRun } = useQuery({
    ...goalRunOptions(wsId, latestRunId),
    enabled: !!wsId && !!latestRunId,
  });

  const status = deriveAutofixStatus(issue, goalRun);
  const style = AUTOFIX_STATE_STYLE[status.state];
  const label = t(($) => $.autofix_page.state[status.state]);

  // No fix ever started → just a faint dot, no label (keeps the list quiet).
  if (status.state === "not_started") {
    return (
      <span
        className={cn("size-1.5 shrink-0 rounded-full", style.dot)}
        title={label}
        aria-label={label}
        data-autofix-state={status.state}
      />
    );
  }

  return (
    <span
      className="flex shrink-0 items-center gap-1"
      data-autofix-state={status.state}
      title={label}
    >
      <span className={cn("size-1.5 rounded-full", style.dot)} aria-hidden />
      <span className={cn("text-[10px] font-medium whitespace-nowrap", style.text)}>
        {label}
      </span>
    </span>
  );
}

/** Middle-column row: status icon + identifier + title + autofix dot. */
function IssueListRow({
  issue,
  active,
  onSelect,
}: {
  issue: Issue;
  active: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        "mb-1 flex w-full items-center gap-2 rounded-md px-2 py-2 text-left transition-colors hover:bg-muted/60",
        active && "bg-muted",
      )}
    >
      <StatusIcon status={issue.status} className="!size-3.5 shrink-0" />
      <span className="min-w-0 flex-1 truncate text-sm text-foreground">
        {issue.identifier ? `${issue.identifier} ` : ""}
        {issue.title}
      </span>
      <AutofixBadge issue={issue} />
    </button>
  );
}

/** Right column: attachments/images + description + autofix three-state +
 *  jump-to-assistant button. */
function IssueDetailColumn({ issue }: { issue: Issue }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const slug = useWorkspaceSlug();
  const { push } = useNavigation();
  const updateIssue = useUpdateIssue();

  // Assigning an AGENT to a PROJECT-bound issue is what triggers (or
  // re-triggers) the server-side auto-fix flow. Both pickers route through the
  // existing optimistic `useUpdateIssue` mutation; the three-state banner above
  // reflects the resulting goal_run on the next relevant event.
  const handleUpdate = useCallback(
    (updates: Partial<UpdateIssueRequest>) => {
      updateIssue.mutate(
        { id: issue.id, ...updates },
        {
          onError: (err) =>
            toast.error(
              err instanceof Error && err.message
                ? err.message
                : t(($) => $.detail.update_failed),
            ),
        },
      );
    },
    [issue.id, updateIssue, t],
  );

  // Live goal_run for the three-state. `latest_goal_run_id` is the most recent
  // run; absent runs leave the status at not_started.
  const autofix = useMemo(() => parseAutofixMetadata(issue), [issue]);
  const latestRunId = autofix.latest_goal_run_id ?? "";
  const { data: goalRun } = useQuery({
    ...goalRunOptions(wsId, latestRunId),
    enabled: !!wsId && !!latestRunId,
  });
  const status = deriveAutofixStatus(issue, goalRun);

  const { data: attachments = EMPTY_ATTACHMENTS } = useQuery(issueAttachmentsOptions(issue.id));
  const images = useMemo(
    () => attachments.filter((a) => a.content_type?.startsWith("image/")),
    [attachments],
  );

  // Jump to the assistant session for this issue's autofix. We hand the
  // assistant page the `latest_goal_run_id`; it resolves that to the goal_run's
  // discussion chat_session_id and selects it (useChatStore.activeSessionId).
  // The button is disabled when there's no run yet (autofix never started), so
  // latestRunId is non-empty here.
  const handleJumpToAssistant = useCallback(() => {
    if (!slug || !latestRunId) return;
    logger.info("jump-to-assistant", { issueId: issue.id, latestRunId });
    push(paths.workspace(slug).assistant({ goalRunId: latestRunId }));
  }, [slug, latestRunId, issue.id, push]);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* Fixed header: identity + pickers + state + quick actions. */}
      <div className="shrink-0 border-b px-5 py-4">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <p className="text-xs text-muted-foreground">{issue.identifier}</p>
            <h1 className="mt-0.5 text-base font-semibold text-foreground">{issue.title}</h1>
          </div>
          {goalRun?.chat_session_id && (
            <Button
              variant="ghost"
              size="sm"
              className="h-7 shrink-0 gap-1.5 text-xs text-muted-foreground"
              onClick={handleJumpToAssistant}
              title={t(($) => $.autofix_page.jump_to_assistant)}
            >
              <ExternalLink className="size-3.5" />
              {t(($) => $.autofix_page.jump_to_assistant)}
            </Button>
          )}
        </div>

        {/* Assignee + project pickers. Picking an agent on a project-bound
            issue is what arms the auto-fix flow server-side. */}
        <div className="mt-3 flex flex-wrap items-center gap-1.5">
          <AssigneePicker
            assigneeType={issue.assignee_type}
            assigneeId={issue.assignee_id}
            onUpdate={handleUpdate}
            triggerRender={<PillButton />}
            align="start"
          />
          <ProjectPicker
            projectId={issue.project_id}
            onUpdate={handleUpdate}
            triggerRender={<PillButton />}
            align="start"
          />
        </div>

        <AutofixStateBanner status={status} />

        <QuickActions
          issue={issue}
          status={status}
          chatSessionId={goalRun?.chat_session_id ?? ""}
        />
      </div>

      {/* Body. When the issue has an auto-fix run, the detail IS the closed loop:
          the agent's replies + planning/subtask execution streams + a live input
          to keep the conversation going — no need to leave for the assistant
          page (which is now just a record). Without a run, show the static
          description/images. */}
      {goalRun?.chat_session_id ? (
        <IssueConversation goal={goalRun} />
      ) : (
        <div className="flex flex-1 flex-col gap-4 overflow-y-auto px-5 py-4">
          {images.length > 0 && (
            <div className="flex flex-wrap gap-2">
              {images.map((img) => (
                // eslint-disable-next-line @next/next/no-img-element
                <img
                  key={img.id}
                  src={img.download_url || img.url}
                  alt={img.filename}
                  className="h-28 w-28 rounded-md border object-cover"
                />
              ))}
            </div>
          )}

          {issue.description ? (
            <ReadonlyContent content={issue.description} attachments={attachments} />
          ) : (
            <p className="flex items-center gap-1.5 text-sm text-muted-foreground">
              <ImageOff className="size-4" />
              {t(($) => $.autofix_page.no_description)}
            </p>
          )}
        </div>
      )}
    </div>
  );
}

/**
 * The issue's auto-fix conversation — the closed-loop work surface. Renders the
 * goal_run's discussion messages (the agent's replies) with the planning +
 * summary execution streams interleaved at the confirm gate, a status-tree
 * popover to inspect/switch subtasks, and a pinned input to keep talking — all
 * the same primitives the tasks/assistant pages use. The user resolves the
 * issue here; the assistant page is just a mirror/record.
 */
function IssueConversation({ goal }: { goal: GoalRun }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const sessionId = goal.chat_session_id;
  const { data: messages = [] } = useQuery(chatMessagesOptions(sessionId));
  const { data: pendingTask } = useQuery(pendingChatTaskOptions(sessionId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const [activeSubtaskId, setActiveSubtaskId] = useState<string | null>(null);

  const resolveAgentName = useCallback(
    (id: string) => agents.find((a) => a.id === id)?.name,
    [agents],
  );
  const activeSubtask = goal.subtasks.find((s) => s.id === activeSubtaskId) ?? null;

  const handleSend = useCallback(
    async (content: string) => {
      try {
        await api.sendChatMessage(sessionId, content);
      } catch (e) {
        logger.error("issue conversation send failed", e);
      }
    },
    [sessionId],
  );
  const handleStop = useCallback(() => {
    if (pendingTask?.task_id) api.cancelTaskById(pendingTask.task_id).catch(() => {});
  }, [pendingTask?.task_id]);

  const done = goal.subtasks.filter((s) => s.status === "completed").length;
  const total = goal.subtasks.length;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* Status-tree switcher (subtasks). Read-only; act via quick actions above. */}
      {total > 0 && (
        <div className="flex shrink-0 items-center justify-end border-b px-4 py-1.5">
          <Popover>
            <PopoverTrigger
              render={<Button variant="outline" size="sm" className="h-7 gap-1.5 text-xs" />}
            >
              <ListTree className="h-3.5 w-3.5" />
              {t(($) => $.autofix_page.execution)}
              <span className="font-mono tabular-nums text-muted-foreground/70">
                {done}/{total}
              </span>
              <ChevronDown className="h-3 w-3 opacity-60" />
            </PopoverTrigger>
            <PopoverContent align="end" className="max-h-[70vh] w-[360px] overflow-y-auto p-0">
              <GoalStatusTree
                goal={goal}
                resolveAgentName={resolveAgentName}
                selectedSubtaskId={activeSubtaskId}
                onSelectMain={() => setActiveSubtaskId(null)}
                onSelectSubtask={setActiveSubtaskId}
              />
            </PopoverContent>
          </Popover>
        </div>
      )}

      <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
        {activeSubtask ? (
          <SubtaskStream subtask={activeSubtask} t={t} />
        ) : (
          <ChatMessageList
            messages={messages}
            pendingTask={pendingTask}
            availability={undefined}
            timelineInsert={
              goal.planning_task_id
                ? {
                    afterTs: goal.confirmed_at,
                    content: (
                      <div className="space-y-3 border-y py-3">
                        <TaskStream
                          taskId={goal.planning_task_id}
                          running={goal.status === "planning"}
                        />
                        {goal.summary_task_id && (
                          <TaskStream
                            taskId={goal.summary_task_id}
                            running={goal.status === "executing"}
                          />
                        )}
                      </div>
                    ),
                  }
                : undefined
            }
          />
        )}
      </div>

      <ChatInput
        onSend={handleSend}
        onUploadFile={async () => null}
        onStop={handleStop}
        isRunning={!!pendingTask?.task_id}
        disabled={false}
      />
    </div>
  );
}

/** Read-only stream for one subtask (title + spec + failure + transcript). */
function SubtaskStream({
  subtask,
  t,
}: {
  subtask: GoalSubtask;
  t: ReturnType<typeof useT<"issues">>["t"];
}) {
  return (
    <div className="min-h-0 flex-1 overflow-y-auto p-3 text-sm">
      <div className="border-b pb-2">
        <h4 className="mb-1 font-medium text-foreground">{subtask.title}</h4>
        <p className="whitespace-pre-wrap text-xs text-muted-foreground">{subtask.spec}</p>
      </div>
      {subtask.failure_reason && (
        <div className="mt-2 rounded border border-destructive/40 bg-destructive/5 p-2 text-xs text-destructive">
          {subtask.failure_reason}
        </div>
      )}
      <div className="mt-3">
        {subtask.task_id ? (
          <TaskStream taskId={subtask.task_id} running={subtask.status === "running"} />
        ) : (
          <p className="text-xs text-muted-foreground">{t(($) => $.autofix_page.no_description)}</p>
        )}
      </div>
    </div>
  );
}

/** Autofix three-state banner. `switch`-equivalent has a default branch (the
 *  trailing not_started/running case) so enum drift downgrades, not crashes. */
function AutofixStateBanner({
  status,
}: {
  status: ReturnType<typeof deriveAutofixStatus>;
}) {
  const { t } = useT("issues");

  if (status.state === "completed") {
    return (
      <div className="mt-3 rounded-md border border-success/30 bg-success/5 px-3 py-2 text-xs text-success">
        <p className="font-medium">{t(($) => $.autofix_page.state.completed)}</p>
        {status.prUrl && (
          <a
            href={status.prUrl}
            target="_blank"
            rel="noreferrer"
            className="mt-1 inline-flex items-center gap-1 underline"
          >
            <ExternalLink className="size-3" />
            {t(($) => $.autofix_page.view_pr)}
          </a>
        )}
      </div>
    );
  }

  if (status.state === "needs_info") {
    return (
      <div className="mt-3 rounded-md border border-warning/30 bg-warning/5 px-3 py-2 text-xs text-warning">
        <p className="font-medium">{t(($) => $.autofix_page.state.needs_info)}</p>
        <p className="mt-1 whitespace-pre-wrap text-warning/80">{status.reason}</p>
      </div>
    );
  }

  if (status.state === "failed") {
    return (
      <div className="mt-3 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive">
        <p className="font-medium">{t(($) => $.autofix_page.state.failed)}</p>
        {status.reason && (
          <p className="mt-1 whitespace-pre-wrap text-destructive/80">{status.reason}</p>
        )}
      </div>
    );
  }

  const color =
    status.state === "running"
      ? "border-primary/30 bg-primary/5 text-primary"
      : "border-border bg-muted/30 text-muted-foreground";
  return (
    <div className={cn("mt-3 rounded-md border px-3 py-2 text-xs", color)}>
      {t(($) => $.autofix_page.state[status.state])}
    </div>
  );
}

/**
 * Quick actions — preset, editable conversations dispatched to the issue's
 * auto-fix agent. Each button opens an inline editable textarea pre-filled with
 * an intent; the user tweaks it and dispatches. We send to the goal_run's
 * discussion chat_session (the coordinator/PMO), reusing the existing
 * `api.sendChatMessage` — no new backend endpoint. The agent decides HOW to act
 * (open a PR, ask for info, retry, merge…); multica only carries the intent.
 *
 * Disabled when there's no chat_session yet (no run to talk to). The action set
 * is status-aware but every action stays available — the user may follow up at
 * any point. Presets carry the failure / needs-info reason as context so the
 * editable text starts useful.
 */
type QuickActionKey = "retry" | "needs_info" | "new_worktree" | "complete" | "freeform";

const QUICK_ACTION_ICON: Record<QuickActionKey, typeof RotateCcw> = {
  retry: RotateCcw,
  needs_info: MessageSquarePlus,
  new_worktree: GitBranchPlus,
  complete: CheckCircle2,
  freeform: MessageSquarePlus,
};

function QuickActions({
  issue,
  status,
  chatSessionId,
}: {
  issue: Issue;
  status: ReturnType<typeof deriveAutofixStatus>;
  chatSessionId: string;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const [openKey, setOpenKey] = useState<QuickActionKey | null>(null);
  const [draft, setDraft] = useState("");
  // Whether the "start fix needs project/agent" guide is revealed (shown after
  // the user clicks Start while the issue isn't yet eligible).
  const [showStartGuide, setShowStartGuide] = useState(false);

  const sendMessage = useMutation({
    mutationFn: (content: string) => api.sendChatMessage(chatSessionId, content),
  });

  // Manual "start fix" for a not_started issue. Unlike the other actions this
  // CREATES the goal_run (there's no session yet), so it hits the dedicated
  // endpoint rather than sendChatMessage. On success the issue list/detail
  // refetch picks up the new run → the state flips to running.
  const startAutofix = useMutation({
    mutationFn: () => api.startAutofix(issue.id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });
      toast.success(t(($) => $.autofix_page.quick_actions.start_sent));
    },
    onError: (err) =>
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.autofix_page.quick_actions.start_failed),
      ),
  });

  // Eligible to start a fix = project-bound + assigned to an agent/squad (mirror
  // of the server gate ShouldAutofixIssue). When eligible but no run yet, we show
  // a one-click "start fix" instead of the inert hint.
  const canStartAutofix =
    !!issue.project_id &&
    (issue.assignee_type === "agent" || issue.assignee_type === "squad") &&
    !!issue.assignee_id;

  // The reason snippet woven into a preset, sourced from the live status.
  const reason =
    status.state === "failed" || status.state === "needs_info" ? status.reason : "";

  const presetFor = useCallback(
    (key: QuickActionKey): string => {
      const base = t(($) => $.autofix_page.quick_actions.preset[key]);
      if (key === "retry" && reason) {
        // "{reason}" placeholder → " <prefix> <reason>". Keep it one editable blob.
        const prefix = t(($) => $.autofix_page.quick_actions.reason_prefix);
        return base.replace("{reason}", ` ${prefix} ${reason}`);
      }
      return base.replace("{reason}", "");
    },
    [t, reason],
  );

  const openAction = useCallback(
    (key: QuickActionKey) => {
      setOpenKey(key);
      setDraft(presetFor(key));
    },
    [presetFor],
  );

  const dispatch = useCallback(async () => {
    const content = draft.trim();
    if (!content || !chatSessionId) return;
    try {
      await sendMessage.mutateAsync(content);
      toast.success(t(($) => $.autofix_page.quick_actions.sent));
      setOpenKey(null);
      setDraft("");
    } catch {
      toast.error(t(($) => $.autofix_page.quick_actions.send_failed));
    }
  }, [draft, chatSessionId, sendMessage, t]);

  // No run / no discussion session yet → the only action is "start fix". The
  // button is ALWAYS shown (so it never silently disappears); when the issue
  // isn't yet eligible, clicking surfaces a guide naming exactly what to bind
  // (the working-dir + assignee pickers sit right above this in the detail
  // header), instead of firing a request that 400s.
  if (!chatSessionId) {
    const missingProject = !issue.project_id;
    const missingAgent = !(
      (issue.assignee_type === "agent" || issue.assignee_type === "squad") &&
      !!issue.assignee_id
    );
    const onStartClick = () => {
      if (canStartAutofix) {
        startAutofix.mutate();
      } else {
        // Reveal the guide; the pickers to satisfy it are in the header above.
        setShowStartGuide(true);
      }
    };
    return (
      <div className="mt-3 space-y-1.5">
        <Button
          size="sm"
          className="h-7 gap-1.5 text-xs"
          onClick={onStartClick}
          disabled={startAutofix.isPending}
        >
          <Play className="size-3.5" />
          {t(($) => $.autofix_page.quick_actions.start)}
        </Button>
        {showStartGuide && !canStartAutofix && (
          <p className="text-xs text-warning">
            {missingProject && missingAgent
              ? t(($) => $.autofix_page.quick_actions.start_need_both)
              : missingProject
                ? t(($) => $.autofix_page.quick_actions.start_need_project)
                : t(($) => $.autofix_page.quick_actions.start_need_agent)}
          </p>
        )}
      </div>
    );
  }

  const actions: QuickActionKey[] = [
    "retry",
    "needs_info",
    "new_worktree",
    "complete",
    "freeform",
  ];

  return (
    <div className="mt-3">
      <p className="mb-1.5 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
        {t(($) => $.autofix_page.quick_actions.title)}
      </p>
      <div className="flex flex-wrap gap-1.5">
        {actions.map((key) => {
          const Icon = QUICK_ACTION_ICON[key];
          return (
            <Button
              key={key}
              variant={openKey === key ? "default" : "outline"}
              size="sm"
              className="h-7 gap-1.5 text-xs"
              onClick={() => (openKey === key ? setOpenKey(null) : openAction(key))}
            >
              <Icon className="size-3.5" />
              {t(($) => $.autofix_page.quick_actions[key])}
            </Button>
          );
        })}
      </div>

      {openKey && (
        <div className="mt-2 rounded-md border bg-background p-2">
          <Textarea
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder={t(($) => $.autofix_page.quick_actions.placeholder)}
            className="min-h-[80px] resize-y text-sm"
            autoFocus
          />
          <div className="mt-2 flex items-center justify-end gap-2">
            <Button
              variant="ghost"
              size="sm"
              className="h-7 text-xs"
              onClick={() => {
                setOpenKey(null);
                setDraft("");
              }}
            >
              {t(($) => $.autofix_page.quick_actions.cancel)}
            </Button>
            <Button
              size="sm"
              className="h-7 gap-1.5 text-xs"
              onClick={dispatch}
              disabled={!draft.trim() || sendMessage.isPending}
            >
              <Send className="size-3.5" />
              {t(($) => $.autofix_page.quick_actions.send)}
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}

/**
 * Inline create form at the top of the middle column (NOT a modal). The user
 * pastes an image + writes a paragraph; submit calls the existing createIssue
 * mutation with the uploaded files as `attachment_ids`.
 *
 * Paste + drop both route through `ContentEditor.onUploadFile` (the editor's
 * `handlePaste` / `handleDrop` ProseMirror plugin grabs `clipboardData.files` /
 * `dataTransfer.files` and inserts an image node), and `useFileDropZone` adds a
 * drop overlay at the form level. The uploaded attachment ids are collected as
 * they resolve so submit can bind them to the new issue.
 */
function InlineCreateForm({
  onClose,
  onCreated,
}: {
  onClose: () => void;
  onCreated: (issueId: string) => void;
}) {
  const { t } = useT("issues");
  const createIssue = useCreateIssue();
  const editorRef = useRef<ContentEditorRef>(null);

  const [hasContent, setHasContent] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  // "Who fixes this" + which project. An AGENT assignee on a PROJECT-bound
  // issue is what triggers auto-fix once created; both are ephemeral form
  // state, so they live here (no store, no draft persistence).
  const [assigneeType, setAssigneeType] = useState<IssueAssigneeType | null>(null);
  const [assigneeId, setAssigneeId] = useState<string | null>(null);
  const [projectId, setProjectId] = useState<string | null>(null);
  // Attachment ids collected from paste/drop/button uploads, in arrival order.
  const attachmentIdsRef = useRef<string[]>([]);

  const { uploadWithToast, uploading } = useFileUpload(api, (err) =>
    toast.error(err.message),
  );

  // Every successful upload (paste, drop, button) flows through here, so we
  // capture the attachment id alongside inserting the image into the editor.
  const handleUploadFile = useCallback(
    async (file: File) => {
      const result = await uploadWithToast(file);
      if (result) attachmentIdsRef.current.push(result.id);
      return result;
    },
    [uploadWithToast],
  );

  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => editorRef.current?.uploadFile(f)),
  });

  const submit = useCallback(async () => {
    const md = editorRef.current?.getMarkdown()?.trim() ?? "";
    if (!md || submitting || uploading) return;
    setSubmitting(true);
    try {
      // First non-empty line becomes the title; the whole paragraph is the
      // description so the autofix goal_run gets the full context.
      const firstLine = md.split("\n").find((l) => l.trim().length > 0) ?? md;
      const title = firstLine.replace(/^#+\s*/, "").trim().slice(0, 200) || md.slice(0, 200);
      const created = await createIssue.mutateAsync({
        title,
        description: md,
        attachment_ids: attachmentIdsRef.current.length
          ? [...attachmentIdsRef.current]
          : undefined,
        // Bind "who fixes this" + project so the new issue can arm auto-fix.
        // `undefined` (not null) so the create request omits absent fields.
        assignee_type: assigneeType ?? undefined,
        assignee_id: assigneeId ?? undefined,
        project_id: projectId ?? undefined,
      });
      onCreated(created.id);
    } catch (e) {
      logger.error("inline create issue failed", e);
      toast.error(t(($) => $.autofix_page.create_failed));
    } finally {
      setSubmitting(false);
    }
  }, [submitting, uploading, createIssue, onCreated, assigneeType, assigneeId, projectId, t]);

  return (
    <div className="border-b bg-background p-2">
      <div
        {...dropZoneProps}
        className="relative flex min-h-[100px] flex-col rounded-md border bg-background p-2"
      >
        <ContentEditor
          ref={editorRef}
          placeholder={t(($) => $.autofix_page.create_placeholder)}
          onUpdate={(md) => setHasContent(md.trim().length > 0)}
          onUploadFile={handleUploadFile}
          onSubmit={submit}
          debounceMs={150}
        />
        {isDragOver && <FileDropOverlay />}
      </div>

      {/* Pick "who fixes this" (agent) + project — same pills the create modal
          uses. An agent on a project-bound issue is what arms auto-fix. */}
      <div className="mt-2 flex flex-wrap items-center gap-1.5">
        <AssigneePicker
          assigneeType={assigneeType}
          assigneeId={assigneeId}
          onUpdate={(u) => {
            setAssigneeType(u.assignee_type ?? null);
            setAssigneeId(u.assignee_id ?? null);
          }}
          triggerRender={<PillButton />}
          align="start"
        />
        <ProjectPicker
          projectId={projectId}
          onUpdate={(u) => setProjectId(u.project_id ?? null)}
          triggerRender={<PillButton />}
          align="start"
        />
      </div>

      <div className="mt-2 flex items-center justify-end gap-2">
        <Button
          variant="ghost"
          size="sm"
          className="h-7 gap-1 text-xs"
          onClick={onClose}
        >
          <XIcon className="size-3.5" />
          {t(($) => $.autofix_page.cancel)}
        </Button>
        <Button
          size="sm"
          className="h-7 text-xs"
          onClick={submit}
          disabled={!hasContent || submitting || uploading}
        >
          {submitting
            ? t(($) => $.autofix_page.creating)
            : uploading
              ? t(($) => $.autofix_page.uploading)
              : t(($) => $.autofix_page.create)}
        </Button>
      </div>
    </div>
  );
}
