"use client";

import { useCallback, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Plus, X as XIcon, ExternalLink, ImageOff } from "lucide-react";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueListOptions, issueAttachmentsOptions } from "@multica/core/issues/queries";
import { useCreateIssue, useUpdateIssue } from "@multica/core/issues/mutations";
import { goalRunOptions } from "@multica/core/goals/queries";
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

/** A status dot mapping the autofix three-state (plus the running interstitial)
 *  to a color. Unknown future states fall through `deriveAutofixStatus` to
 *  "running", so this switch never crashes (enum-drift rule). */
function AutofixDot({ issue }: { issue: Pick<Issue, "metadata"> }) {
  const { t } = useT("issues");
  // The list query doesn't carry the goal_run object; the dot is derived from
  // metadata alone. `deriveAutofixStatus` treats "metadata references a run but
  // the run isn't loaded" as not_started, which is the correct list-level
  // display (a colored "in flight" dot needs the run's live status, fetched
  // only in the detail column).
  const status = deriveAutofixStatus(issue);

  const color =
    status.state === "completed"
      ? "bg-success"
      : status.state === "needs_info"
        ? "bg-destructive"
        : status.state === "running"
          ? "bg-primary"
          : "bg-muted-foreground/40";

  const label = t(($) => $.autofix_page.state[status.state]);

  return (
    <span
      className={cn("size-1.5 shrink-0 rounded-full", color)}
      title={label}
      aria-label={label}
      data-autofix-state={status.state}
    />
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
      <AutofixDot issue={issue} />
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
    <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
      <div className="border-b px-5 py-4">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <p className="text-xs text-muted-foreground">{issue.identifier}</p>
            <h1 className="mt-0.5 text-base font-semibold text-foreground">{issue.title}</h1>
          </div>
          <Button
            variant="outline"
            size="sm"
            className="h-7 shrink-0 gap-1.5 text-xs"
            onClick={handleJumpToAssistant}
            disabled={!latestRunId}
            title={t(($) => $.autofix_page.jump_to_assistant)}
          >
            <ExternalLink className="size-3.5" />
            {t(($) => $.autofix_page.jump_to_assistant)}
          </Button>
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
      </div>

      <div className="flex flex-col gap-4 px-5 py-4">
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
      <div className="mt-3 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive">
        <p className="font-medium">{t(($) => $.autofix_page.state.needs_info)}</p>
        <p className="mt-1 whitespace-pre-wrap text-destructive/80">{status.reason}</p>
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
