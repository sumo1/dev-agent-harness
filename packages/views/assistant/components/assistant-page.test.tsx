// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, cleanup, waitFor } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enChat from "../../locales/en/chat.json";
import type { GoalRun } from "@multica/core/types";

const TEST_RESOURCES = { en: { chat: enChat } };

// ---------------------------------------------------------------------------
// Mocks. The assistant page is store-driven; the locator (issue → "jump to
// assistant session") arrives as a query param through the shared
// NavigationAdapter. This spec verifies: entering the page with a
// `goal_run_id` param resolves it to the goal_run's discussion chat_session_id
// and calls setActiveSession(...) exactly once.
// ---------------------------------------------------------------------------

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

// Chat store: callable selector + .getState() (Zustand dual shape).
const { mockSetActiveSession, chatState } = vi.hoisted(() => ({
  mockSetActiveSession: vi.fn(),
  chatState: { activeSessionId: null as string | null },
}));

vi.mock("@multica/core/chat", () => {
  const getState = () => ({
    activeSessionId: chatState.activeSessionId,
    setActiveSession: mockSetActiveSession,
  });
  const useChatStore = (selector: (s: ReturnType<typeof getState>) => unknown) =>
    selector(getState());
  return { useChatStore: Object.assign(useChatStore, { getState }) };
});

vi.mock("@multica/core/auth", () => {
  const getState = () => ({ user: { id: "user-1" } });
  const useAuthStore = (selector: (s: ReturnType<typeof getState>) => unknown) =>
    selector(getState());
  return { useAuthStore: Object.assign(useAuthStore, { getState }) };
});

vi.mock("@multica/core/agents", () => ({
  useAgentPresenceDetail: () => ({ availability: "online" }),
}));

const {
  mockGetGoal,
  mockListChatSessions,
  mockListChatMessages,
  mockGetPendingChatTask,
  mockListAgents,
  mockListMembers,
  mockListRuntimes,
} = vi.hoisted(() => ({
  mockGetGoal: vi.fn(),
  mockListChatSessions: vi.fn(),
  mockListChatMessages: vi.fn(),
  mockGetPendingChatTask: vi.fn(),
  mockListAgents: vi.fn(),
  mockListMembers: vi.fn(),
  mockListRuntimes: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    getGoal: mockGetGoal,
    listChatSessions: mockListChatSessions,
    listChatMessages: mockListChatMessages,
    getPendingChatTask: mockGetPendingChatTask,
    listAgents: mockListAgents,
    listMembers: mockListMembers,
    listRuntimes: mockListRuntimes,
  },
}));

// Navigation: searchParams is controllable per-test via the shared ref.
const { navSearch } = vi.hoisted(() => ({
  navSearch: { params: new URLSearchParams() },
}));

vi.mock("../../navigation", () => ({
  useNavigation: () => ({
    push: vi.fn(),
    replace: vi.fn(),
    pathname: "/ws-1/assistant",
    searchParams: navSearch.params,
    getShareableUrl: (p: string) => `https://app.multica.com${p}`,
  }),
  NavigationProvider: ({ children }: { children: React.ReactNode }) => children,
}));

// Child components hit Tiptap / scroll APIs — stub to keep the spec on the
// page's locator logic, not the chat surface.
vi.mock("../../chat/components/chat-message-list", () => ({
  ChatMessageList: () => null,
}));
vi.mock("../../chat/components/chat-input", () => ({
  ChatInput: () => null,
}));
vi.mock("./session-list", () => ({ SessionList: () => null }));
vi.mock("./new-session-dialog", () => ({ NewSessionDialog: () => null }));
vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ uploadWithToast: vi.fn() }),
}));
vi.mock("@multica/core/chat/mutations", () => ({
  useCreateChatSession: () => ({ mutateAsync: vi.fn() }),
}));

import { AssistantPage } from "./assistant-page";

const GOAL_RUN: GoalRun = {
  id: "goal-1",
  workspace_id: "ws-1",
  squad_id: "sq-1",
  chat_session_id: "session-from-goal",
  title: "Autofix issue",
  goal: "Fix it",
  status: "executing",
  subtasks: [],
  planning_task_id: "",
  summary_task_id: "",
  confirmed_at: "",
  project_id: "",
  persist_task_id: "",
  can_persist: false,
  coordinator_name: "",
  coordinator_runtime_name: "",
  coordinator_runtime_provider: "",
  coordinator_model: "",
  created_at: "",
  updated_at: "",
};

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <AssistantPage />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe("AssistantPage session locator", () => {
  beforeEach(() => {
    navSearch.params = new URLSearchParams();
    chatState.activeSessionId = null;
    mockSetActiveSession.mockReset();
    mockGetGoal.mockReset().mockResolvedValue(GOAL_RUN);
    mockListChatSessions.mockResolvedValue([]);
    mockListChatMessages.mockResolvedValue([]);
    mockGetPendingChatTask.mockResolvedValue(null);
    mockListAgents.mockResolvedValue([]);
    mockListMembers.mockResolvedValue([]);
    mockListRuntimes.mockResolvedValue([]);
  });

  afterEach(() => cleanup());

  it("resolves goal_run_id → discussion session and selects it", async () => {
    navSearch.params = new URLSearchParams({ goal_run_id: "goal-1" });
    renderPage();

    await waitFor(() => {
      expect(mockGetGoal).toHaveBeenCalledWith("goal-1");
      expect(mockSetActiveSession).toHaveBeenCalledWith("session-from-goal");
    });
  });

  it("selects a direct session_id without fetching a goal_run", async () => {
    navSearch.params = new URLSearchParams({ session_id: "session-direct" });
    renderPage();

    await waitFor(() => {
      expect(mockSetActiveSession).toHaveBeenCalledWith("session-direct");
    });
    expect(mockGetGoal).not.toHaveBeenCalled();
  });

  it("does not select any session when no locator param is present", async () => {
    renderPage();

    // Let queries settle; nothing should select a session.
    await waitFor(() => {
      expect(mockListChatSessions).toHaveBeenCalled();
    });
    expect(mockSetActiveSession).not.toHaveBeenCalled();
    expect(mockGetGoal).not.toHaveBeenCalled();
  });
});
