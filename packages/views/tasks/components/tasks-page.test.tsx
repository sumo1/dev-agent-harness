// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, cleanup, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enChat from "../../locales/en/chat.json";

const TEST_RESOURCES = { en: { chat: enChat } };

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

const { mockCreateTask, mockListTasks, mockListAgents, mockGetGoal } = vi.hoisted(() => ({
  mockCreateTask: vi.fn(),
  mockListTasks: vi.fn(),
  mockListAgents: vi.fn(),
  mockGetGoal: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    createTask: mockCreateTask,
    listTasks: mockListTasks,
    listAgents: mockListAgents,
    getGoal: mockGetGoal,
    sendChatMessage: vi.fn(),
    cancelTaskById: vi.fn().mockResolvedValue(undefined),
  },
}));

// The discussion column renders ChatInput, which reads the singleton chat
// store. Provide a minimal stand-in so it doesn't throw "store not initialised".
vi.mock("@multica/core/chat", () => {
  const state = {
    activeSessionId: null,
    selectedAgentId: null,
    inputDrafts: {} as Record<string, string>,
    setInputDraft: vi.fn(),
    clearInputDraft: vi.fn(),
  };
  const useChatStore = Object.assign(
    (selector?: (s: typeof state) => unknown) => (selector ? selector(state) : state),
    { getState: () => state },
  );
  return {
    useChatStore,
    registerChatStore: vi.fn(),
    DRAFT_NEW_SESSION: "__new__",
  };
});

import { TasksPage } from "./tasks-page";

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>
        <TasksPage />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

beforeEach(() => {
  mockListTasks.mockResolvedValue([]);
  mockListAgents.mockResolvedValue([
    { id: "agent-1", name: "Coder", workspace_id: "ws-1" },
    { id: "agent-2", name: "Reviewer", workspace_id: "ws-1" },
  ]);
  mockCreateTask.mockResolvedValue({
    goal: {
      id: "goal-1",
      workspace_id: "ws-1",
      squad_id: "sq-1",
      chat_session_id: "chat-1",
      title: "Refactor login",
      goal: "Refactor it",
      status: "discussion",
      subtasks: [],
      created_at: "",
      updated_at: "",
    },
    discussion_chat_id: "chat-1",
  });
  mockGetGoal.mockResolvedValue({
    id: "goal-1",
    workspace_id: "ws-1",
    squad_id: "sq-1",
    chat_session_id: "chat-1",
    title: "Refactor login",
    goal: "Refactor it",
    status: "discussion",
    subtasks: [],
    created_at: "",
    updated_at: "",
  });
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("TasksPage", () => {
  it("shows the empty state with a create entry (no form)", async () => {
    renderPage();
    // Empty state CTA — conversational create, no title/goal/member form.
    expect(await screen.findByText("Create a task or pick one from the list.")).toBeInTheDocument();
    expect(screen.queryByPlaceholderText(/Describe what you want done/)).not.toBeInTheDocument();
  });

  it("creates a task conversationally — clicking + opens an empty discussion", async () => {
    renderPage();
    // Both the header + (aria-label) and the empty-state CTA carry "New task".
    // Either creates with no form payload; the goal is described in the chat.
    const [firstButton] = await screen.findAllByRole("button", { name: "New task" });
    await userEvent.click(firstButton!);

    await waitFor(() => {
      expect(mockCreateTask).toHaveBeenCalledWith({});
    });
  });

  it("shows a not-found state when a selected task fails to load (deleted out from under a stale list)", async () => {
    mockListTasks.mockResolvedValue([
      {
        id: "goal-gone",
        workspace_id: "ws-1",
        squad_id: "sq-1",
        chat_session_id: "",
        title: "Ghost task",
        goal: "",
        status: "completed",
        subtasks: [],
        created_at: "",
        updated_at: "",
      },
    ]);
    // The goal was deleted: fetching it 404s (getGoal throws).
    mockGetGoal.mockRejectedValue(new Error("API error: 404 Not Found"));

    renderPage();
    const row = await screen.findByText("Ghost task");
    await userEvent.click(row);

    // The detail panel must surface an explicit not-found state, NOT the same
    // blank "pick a task" empty state (which would look like a dead click).
    expect(
      await screen.findByText("This task couldn't be loaded — it may have been deleted."),
    ).toBeInTheDocument();

    // "Back to list" clears the selection → back to the empty state.
    await userEvent.click(screen.getByRole("button", { name: "Back to list" }));
    expect(
      await screen.findByText("Create a task or pick one from the list."),
    ).toBeInTheDocument();
  });

  it("lists existing tasks with a localized status", async () => {
    mockListTasks.mockResolvedValue([
      {
        id: "goal-9",
        workspace_id: "ws-1",
        squad_id: "sq-9",
        chat_session_id: "",
        title: "Ship onboarding",
        goal: "",
        status: "executing",
        subtasks: [],
        created_at: "",
        updated_at: "",
      },
    ]);
    renderPage();
    expect(await screen.findByText("Ship onboarding")).toBeInTheDocument();
    // Raw enum is localized, not rendered verbatim.
    expect(screen.getByText("Executing")).toBeInTheDocument();
    expect(screen.queryByText("executing")).not.toBeInTheDocument();
  });
});
