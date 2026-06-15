import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Issue } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

// ---------------------------------------------------------------------------
// Mocks — follow the repo convention: mock @multica/core data layer, never
// next/* or react-router-dom. The real `autofix.ts` is NOT mocked so the
// three-state dot mapping is genuinely exercised.
// ---------------------------------------------------------------------------

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/logger", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/logger")>();
  return {
    ...actual,
    createLogger: () => ({ info: vi.fn(), warn: vi.fn(), error: vi.fn(), debug: vi.fn() }),
  };
});

// Query options are mocked so each returns a stable queryKey + a queryFn that
// resolves from the per-test fixtures. The component calls useQuery on these.
const fixtures = vi.hoisted(() => ({
  issues: [] as unknown[],
  attachments: [] as unknown[],
  goalRun: null as unknown,
}));

vi.mock("@multica/core/issues/queries", () => ({
  issueListOptions: () => ({
    queryKey: ["issues", "ws-1", "list"],
    queryFn: () => Promise.resolve(fixtures.issues),
  }),
  issueAttachmentsOptions: (issueId: string) => ({
    queryKey: ["issues", "attachments", issueId],
    queryFn: () => Promise.resolve(fixtures.attachments),
  }),
  issueKeys: {
    list: (wsId: string) => ["issues", wsId, "list"],
  },
}));

vi.mock("@multica/core/goals/queries", () => ({
  goalRunOptions: (_wsId: string, id: string) => ({
    queryKey: ["goals", "ws-1", "run", id],
    queryFn: () => Promise.resolve(fixtures.goalRun),
  }),
}));

const mockCreateMutateAsync = vi.hoisted(() => vi.fn());
const mockUpdateMutate = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/issues/mutations", () => ({
  useCreateIssue: () => ({ mutateAsync: mockCreateMutateAsync, isPending: false }),
  useUpdateIssue: () => ({ mutate: mockUpdateMutate, isPending: false }),
}));

// Stub the two pickers to a single button that fires `onUpdate` with a fixed
// agent / project. The real pickers' popover internals (Base UI + member/agent/
// squad/project queries) aren't what this page test asserts — the wiring is:
// does picking an agent reach createIssue / useUpdateIssue with the right
// field shape. Mock by the relative paths the page imports.
vi.mock("./pickers/assignee-picker", () => ({
  AssigneePicker: (props: {
    assigneeType: string | null;
    assigneeId: string | null;
    onUpdate: (u: { assignee_type?: string | null; assignee_id?: string | null }) => void;
  }) => (
    <button
      type="button"
      aria-label="pick-agent"
      data-assignee-type={props.assigneeType ?? ""}
      data-assignee-id={props.assigneeId ?? ""}
      onClick={() => props.onUpdate({ assignee_type: "agent", assignee_id: "agent-7" })}
    />
  ),
}));

vi.mock("../../projects/components/project-picker", () => ({
  ProjectPicker: (props: {
    projectId: string | null;
    onUpdate: (u: { project_id?: string | null }) => void;
  }) => (
    <button
      type="button"
      aria-label="pick-project"
      data-project-id={props.projectId ?? ""}
      onClick={() => props.onUpdate({ project_id: "proj-3" })}
    />
  ),
}));

const mockUploadWithToast = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ uploadWithToast: mockUploadWithToast, upload: vi.fn(), uploading: false }),
}));

const sendChatMessageMock = vi.hoisted(() => vi.fn(() => Promise.resolve({})));
const startAutofixMock = vi.hoisted(() =>
  vi.fn(() => Promise.resolve({ goal_run_id: "g-new" })),
);
vi.mock("@multica/core/api", () => ({
  api: { sendChatMessage: sendChatMessageMock, startAutofix: startAutofixMock },
}));

vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

// Navigation: a shared push spy so we can assert the "jump to assistant"
// wiring. We never mock next/* or react-router-dom — only the shared adapter.
const mockPush = vi.hoisted(() => vi.fn());
vi.mock("../../navigation", () => ({
  useNavigation: () => ({
    push: mockPush,
    replace: vi.fn(),
    pathname: "/ws-slug/issues",
    searchParams: new URLSearchParams(),
    getShareableUrl: (p: string) => `https://app.multica.com${p}`,
  }),
  NavigationProvider: ({ children }: { children: unknown }) => children,
}));

// Keep the real `paths` builder; only stub the slug hook so the detail column
// can build the assistant path without a WorkspaceSlugProvider.
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return { ...actual, useWorkspaceSlug: () => "ws-slug" };
});

// Mock the editor module: ContentEditor exposes the imperative ref the page
// uses (getMarkdown / uploadFile), and a fake "paste" button that routes a file
// through onUploadFile — exactly what the real editor's handlePaste does.
const editorMarkdown = vi.hoisted(() => ({ current: "" }));
vi.mock("../../editor", () => {
  const React = require("react");
  return {
    ContentEditor: React.forwardRef(
      (
        props: {
          onUpdate?: (md: string) => void;
          onUploadFile?: (f: File) => Promise<unknown>;
          placeholder?: string;
        },
        ref: React.Ref<unknown>,
      ) => {
        React.useImperativeHandle(ref, () => ({
          getMarkdown: () => editorMarkdown.current,
          clearContent: () => {
            editorMarkdown.current = "";
          },
          focus: vi.fn(),
          blur: vi.fn(),
          uploadFile: (f: File) => props.onUploadFile?.(f),
          hasActiveUploads: () => false,
        }));
        return (
          <div>
            <textarea
              aria-label="editor"
              placeholder={props.placeholder}
              onChange={(e) => {
                editorMarkdown.current = e.target.value;
                props.onUpdate?.(e.target.value);
              }}
            />
            <button
              type="button"
              aria-label="paste-image"
              onClick={() =>
                props.onUploadFile?.(new File(["x"], "shot.png", { type: "image/png" }))
              }
            />
          </div>
        );
      },
    ),
    ReadonlyContent: ({ content }: { content: string }) => <div>{content}</div>,
    useFileDropZone: () => ({ isDragOver: false, dropZoneProps: {} }),
    FileDropOverlay: () => null,
  };
});

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const issueDefaults = {
  workspace_id: "ws-1",
  description: null as string | null,
  priority: "none" as const,
  assignee_type: null,
  assignee_id: null,
  creator_type: "member" as const,
  creator_id: "user-1",
  parent_issue_id: null,
  project_id: null,
  position: 0,
  start_date: null,
  due_date: null,
  metadata: {},
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

function issue(over: Partial<Issue> & { id: string; number: number; identifier: string; title: string }): Issue {
  return { ...issueDefaults, status: "todo", ...over } as Issue;
}

// ---------------------------------------------------------------------------
// Import after mocks
// ---------------------------------------------------------------------------

import { AutofixIssuesPage } from "./autofix-issues-page";

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 }, mutations: { retry: false } },
  });
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>
        <AutofixIssuesPage />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("AutofixIssuesPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    fixtures.issues = [];
    fixtures.attachments = [];
    fixtures.goalRun = null;
    editorMarkdown.current = "";
  });

  it("renders the issue list", async () => {
    fixtures.issues = [
      issue({ id: "i1", number: 1, identifier: "TES-1", title: "Login broken" }),
      issue({ id: "i2", number: 2, identifier: "TES-2", title: "Crash on save" }),
    ];
    renderPage();
    expect(await screen.findByText(/Login broken/)).toBeInTheDocument();
    expect(screen.getByText(/Crash on save/)).toBeInTheDocument();
  });

  it("maps the autofix three-state to the correct list dot", async () => {
    fixtures.issues = [
      // not_started: no autofix metadata
      issue({ id: "i1", number: 1, identifier: "TES-1", title: "Fresh" }),
      // completed: latest_goal_run_id present (list-level derivation has no
      // run object, so it shows not_started — the colored running/completed
      // dot needs the run; verified in the detail column test below). Here we
      // assert the metadata-only derivation for a needs_info-style row stays
      // not_started without a loaded run.
      issue({
        id: "i2",
        number: 2,
        identifier: "TES-2",
        title: "Has run",
        metadata: { autofix: { goal_run_ids: ["g1"], latest_goal_run_id: "g1" } } as never,
      }),
    ];
    renderPage();
    await screen.findByText(/Fresh/);

    const dots = document.querySelectorAll("[data-autofix-state]");
    // Both rows derive not_started at the list level (no goal_run object loaded
    // in the list query) — the dot never crashes on metadata drift.
    expect(dots.length).toBe(2);
    dots.forEach((d) => expect(d.getAttribute("data-autofix-state")).toBe("not_started"));
  });

  it("derives completed state in the detail column when the goal_run is loaded", async () => {
    fixtures.issues = [
      issue({
        id: "i1",
        number: 1,
        identifier: "TES-1",
        title: "Has PR",
        metadata: {
          autofix: {
            goal_run_ids: ["g1"],
            latest_goal_run_id: "g1",
            // N1 filed the upstream GitHub issue; N4 opened the PR. The completed
            // banner must link the PR (pr_url), not the upstream issue url.
            github: { issue_url: "https://github.com/o/r/issues/4" },
            pr_url: "https://github.com/o/r/pull/9",
          },
        } as never,
      }),
    ];
    fixtures.goalRun = { status: "completed" };
    renderPage();

    fireEvent.click(await screen.findByText(/Has PR/));
    // "Auto-fix completed" now appears in BOTH the list badge and the detail
    // banner — at least one, and the PR link is the banner-specific proof.
    expect((await screen.findAllByText("Auto-fix completed")).length).toBeGreaterThan(0);
    expect(screen.getByText("View pull request")).toBeInTheDocument();
  });

  it("jump-to-assistant pushes the assistant path carrying the latest goal_run_id", async () => {
    fixtures.issues = [
      issue({
        id: "i1",
        number: 1,
        identifier: "TES-1",
        title: "Has run",
        metadata: {
          autofix: { goal_run_ids: ["g1"], latest_goal_run_id: "g1" },
        } as never,
      }),
    ];
    renderPage();

    fireEvent.click(await screen.findByText(/Has run/));
    fireEvent.click(await screen.findByText("Open assistant session"));

    expect(mockPush).toHaveBeenCalledWith("/ws-slug/assistant?goal_run_id=g1");
  });

  it("jump-to-assistant button is disabled when there is no goal_run yet", async () => {
    fixtures.issues = [
      issue({ id: "i1", number: 1, identifier: "TES-1", title: "No run" }),
    ];
    renderPage();

    fireEvent.click(await screen.findByText(/No run/));
    const btn = await screen.findByText("Open assistant session");
    expect(btn.closest("button")).toBeDisabled();
  });

  it("failed goal_run renders the 执行错误/failed banner (not 'in progress')", async () => {
    fixtures.issues = [
      issue({
        id: "i1",
        number: 1,
        identifier: "TES-1",
        title: "Broke",
        metadata: {
          autofix: { goal_run_ids: ["g1"], latest_goal_run_id: "g1" },
        } as never,
      }),
    ];
    // failed run with a failed subtask carrying the reason.
    fixtures.goalRun = {
      status: "failed",
      chat_session_id: "cs-1",
      subtasks: [{ status: "failed", failure_reason: "compile error in handler" }],
    };
    renderPage();

    fireEvent.click(await screen.findByText(/Broke/));
    // "Auto-fix failed" now shows in both the list badge and the banner; the
    // failure reason is the banner-specific proof.
    expect((await screen.findAllByText("Auto-fix failed")).length).toBeGreaterThan(0);
    expect(screen.getByText("compile error in handler")).toBeInTheDocument();
    // It must NOT show the in-progress banner.
    expect(screen.queryByText("Auto-fix in progress")).not.toBeInTheDocument();
  });

  it("list badge shows the REAL state of an issue with a goal_run (not always not_started)", async () => {
    // The bug: the list dot was derived without the goal_run object, so every
    // issue with a fix showed "not_started". The badge now fetches the run.
    fixtures.issues = [
      issue({
        id: "i1",
        number: 1,
        identifier: "TES-1",
        title: "Fixing",
        metadata: {
          autofix: { goal_run_ids: ["g1"], latest_goal_run_id: "g1" },
        } as never,
      }),
    ];
    fixtures.goalRun = { status: "failed", chat_session_id: "cs-1", subtasks: [] };
    renderPage();

    // The list row's badge resolves to the failed state (data attr on the badge).
    const badge = await waitFor(() => {
      const el = document.querySelector('[data-autofix-state="failed"]');
      if (!el) throw new Error("badge not failed yet");
      return el;
    });
    expect(badge).toBeTruthy();
    // And it must NOT be stuck on not_started.
    expect(
      document.querySelector('[data-autofix-state="not_started"]'),
    ).toBeNull();
  });

  it("quick action dispatches the (edited) preset to the goal_run's chat session", async () => {
    fixtures.issues = [
      issue({
        id: "i1",
        number: 1,
        identifier: "TES-1",
        title: "Fix me",
        metadata: {
          autofix: { goal_run_ids: ["g1"], latest_goal_run_id: "g1" },
        } as never,
      }),
    ];
    fixtures.goalRun = { status: "running", chat_session_id: "cs-1" };
    renderPage();

    fireEvent.click(await screen.findByText(/Fix me/));
    // Open the "Complete issue" quick action → preset textarea appears.
    fireEvent.click(await screen.findByText("Complete issue"));
    const textarea = await screen.findByPlaceholderText(/Edit the message/);
    // The preset is pre-filled (the user can edit it).
    expect((textarea as HTMLTextAreaElement).value).toContain("I confirm this issue");
    // Dispatch → goes to the goal_run's chat session.
    fireEvent.click(screen.getByText("Send"));
    await waitFor(() =>
      expect(sendChatMessageMock).toHaveBeenCalledWith(
        "cs-1",
        expect.stringContaining("I confirm this issue"),
      ),
    );
  });

  it("quick actions are disabled (hint shown) when the run has no chat session", async () => {
    fixtures.issues = [
      issue({
        id: "i1",
        number: 1,
        identifier: "TES-1",
        title: "No session",
        metadata: {
          autofix: { goal_run_ids: ["g1"], latest_goal_run_id: "g1" },
        } as never,
      }),
    ];
    fixtures.goalRun = { status: "running", chat_session_id: "" };
    renderPage();

    fireEvent.click(await screen.findByText(/No session/));
    expect(
      await screen.findByText(/Assign an agent and bind a working directory/),
    ).toBeInTheDocument();
    expect(screen.queryByText("Complete issue")).not.toBeInTheDocument();
  });

  it("not_started + eligible (project + agent) shows 启动修复 and calls startAutofix", async () => {
    fixtures.issues = [
      issue({
        id: "i1",
        number: 1,
        identifier: "TES-1",
        title: "Ready to fix",
        project_id: "proj-1",
        assignee_type: "agent",
        assignee_id: "agent-1",
        // No autofix metadata yet → not_started.
      }),
    ];
    fixtures.goalRun = null;
    renderPage();

    fireEvent.click(await screen.findByText(/Ready to fix/));
    const startBtn = await screen.findByText("Start fix");
    fireEvent.click(startBtn);
    await waitFor(() => expect(startAutofixMock).toHaveBeenCalledWith("i1"));
  });

  it("not_started + NOT eligible (no project/agent) shows the hint, no Start fix", async () => {
    fixtures.issues = [
      issue({ id: "i1", number: 1, identifier: "TES-1", title: "Bare" }),
    ];
    fixtures.goalRun = null;
    renderPage();

    fireEvent.click(await screen.findByText(/Bare/));
    expect(
      await screen.findByText(/Assign an agent and bind a working directory/),
    ).toBeInTheDocument();
    expect(screen.queryByText("Start fix")).not.toBeInTheDocument();
  });

  it("paste routes the file through the upload path", async () => {
    mockUploadWithToast.mockResolvedValue({ id: "att-1", url: "u", link: "u" });
    renderPage();
    await screen.findByText("Select an issue to see its details.");

    // Open the inline create form (the + button in the list header).
    fireEvent.click(screen.getByLabelText("New issue"));
    // Simulate a paste: the editor's paste handler calls onUploadFile.
    fireEvent.click(await screen.findByLabelText("paste-image"));

    await waitFor(() => expect(mockUploadWithToast).toHaveBeenCalledTimes(1));
    expect(mockUploadWithToast).toHaveBeenCalledWith(expect.any(File));
  });

  it("inline create submit calls createIssue with title + description + attachment_ids", async () => {
    mockUploadWithToast.mockResolvedValue({ id: "att-1", url: "u", link: "u" });
    mockCreateMutateAsync.mockResolvedValue({ id: "new-1" });
    renderPage();
    await screen.findByText("Select an issue to see its details.");

    fireEvent.click(screen.getByLabelText("New issue"));
    // Paste an image so an attachment id is collected.
    fireEvent.click(await screen.findByLabelText("paste-image"));
    await waitFor(() => expect(mockUploadWithToast).toHaveBeenCalled());
    // Type a paragraph.
    fireEvent.change(screen.getByLabelText("editor"), {
      target: { value: "App crashes when I click save" },
    });
    // Submit.
    fireEvent.click(screen.getByText("Create"));

    await waitFor(() => expect(mockCreateMutateAsync).toHaveBeenCalledTimes(1));
    // No agent/project chosen → those fields are omitted (undefined).
    expect(mockCreateMutateAsync).toHaveBeenCalledWith({
      title: "App crashes when I click save",
      description: "App crashes when I click save",
      attachment_ids: ["att-1"],
      assignee_type: undefined,
      assignee_id: undefined,
      project_id: undefined,
    });
  });

  it("inline create with a chosen agent + project sends assignee + project_id", async () => {
    mockCreateMutateAsync.mockResolvedValue({ id: "new-2" });
    renderPage();
    await screen.findByText("Select an issue to see its details.");

    fireEvent.click(screen.getByLabelText("New issue"));
    fireEvent.change(await screen.findByLabelText("editor"), {
      target: { value: "Button does nothing" },
    });
    // Pick "who fixes this" (agent) and the project via the toolbar pills.
    fireEvent.click(screen.getByLabelText("pick-agent"));
    fireEvent.click(screen.getByLabelText("pick-project"));
    fireEvent.click(screen.getByText("Create"));

    await waitFor(() => expect(mockCreateMutateAsync).toHaveBeenCalledTimes(1));
    expect(mockCreateMutateAsync).toHaveBeenCalledWith({
      title: "Button does nothing",
      description: "Button does nothing",
      attachment_ids: undefined,
      assignee_type: "agent",
      assignee_id: "agent-7",
      project_id: "proj-3",
    });
  });

  it("detail-column assignee change updates the existing issue via useUpdateIssue", async () => {
    fixtures.issues = [
      issue({ id: "i1", number: 1, identifier: "TES-1", title: "Needs an agent" }),
    ];
    renderPage();

    // Select the issue, then assign an agent from the detail column.
    fireEvent.click(await screen.findByText(/Needs an agent/));
    fireEvent.click(await screen.findByLabelText("pick-agent"));

    expect(mockUpdateMutate).toHaveBeenCalledTimes(1);
    // mutate(payload, options) — assert only the payload (first arg).
    expect(mockUpdateMutate).toHaveBeenCalledWith(
      { id: "i1", assignee_type: "agent", assignee_id: "agent-7" },
      expect.anything(),
    );
  });
});
