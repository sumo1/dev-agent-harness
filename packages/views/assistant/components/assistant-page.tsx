"use client";

import { useCallback, useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useChatStore } from "@multica/core/chat";
import { useAuthStore } from "@multica/core/auth";
import { chatSessionsOptions, chatMessagesOptions, pendingChatTaskOptions, chatKeys } from "@multica/core/chat/queries";
import { goalRunOptions } from "@multica/core/goals/queries";
import { ASSISTANT_GOAL_RUN_PARAM, ASSISTANT_SESSION_PARAM } from "@multica/core/paths";
import { useNavigation } from "../../navigation";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { useAgentPresenceDetail } from "@multica/core/agents";
import { useCreateChatSession } from "@multica/core/chat/mutations";
import { api } from "@multica/core/api";
import { ChatMessageList } from "../../chat/components/chat-message-list";
import { ChatInput } from "../../chat/components/chat-input";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { SessionList } from "./session-list";
import { NewSessionDialog } from "./new-session-dialog";
import { GoalStatusTree } from "./goal-status-tree";
import { TaskStream } from "../../tasks/components/task-stream";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { Button } from "@multica/ui/components/ui/button";
import { ListTree, ChevronDown } from "lucide-react";
import { useT } from "../../i18n";
import { createLogger } from "@multica/core/logger";
import type { ChatMessage, GoalRun, GoalSubtask } from "@multica/core/types";

const logger = createLogger("assistant.page");

export function AssistantPage() {
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const setActiveSession = useChatStore((s) => s.setActiveSession);
  const qc = useQueryClient();
  const { searchParams } = useNavigation();

  const { t } = useT("chat");

  // Session locator on entry (issue → "jump to assistant session"). Either a
  // direct `session_id`, or a `goal_run_id` resolved to that goal_run's
  // discussion `chat_session_id`. Both arrive as query params through the
  // shared NavigationAdapter — no next/react-router import here.
  const localeSessionParam = searchParams.get(ASSISTANT_SESSION_PARAM) ?? "";
  const goalRunParam = searchParams.get(ASSISTANT_GOAL_RUN_PARAM) ?? "";

  const { data: locatorGoalRun } = useQuery({
    ...goalRunOptions(wsId, goalRunParam),
    enabled: !!wsId && !!goalRunParam,
  });

  // Resolve the locator to a concrete session id: a direct session_id wins;
  // otherwise the goal_run's discussion chat_session_id (once loaded).
  const resolvedLocatorSessionId =
    localeSessionParam || (locatorGoalRun?.chat_session_id ?? "");

  // Select the located session once. We track the value we last applied so a
  // user navigating away from it within the same mount isn't yanked back.
  const [appliedLocatorSession, setAppliedLocatorSession] = useState<string | null>(null);

  useEffect(() => {
    if (!resolvedLocatorSessionId) return;
    if (resolvedLocatorSessionId === appliedLocatorSession) return;
    setActiveSession(resolvedLocatorSessionId);
    setAppliedLocatorSession(resolvedLocatorSessionId);
  }, [resolvedLocatorSessionId, appliedLocatorSession, setActiveSession]);

  // 新建会话对话框状态
  const [showNewSessionDialog, setShowNewSessionDialog] = useState(false);


  // 获取所有会话
  const { data: sessions = [] } = useQuery(chatSessionsOptions(wsId));

  // 获取当前会话的消息
  const { data: rawMessages } = useQuery(
    chatMessagesOptions(activeSessionId ?? ""),
  );
  const messages = activeSessionId ? rawMessages ?? [] : [];

  // 获取当前会话的运行状态
  const { data: pendingTask } = useQuery(
    pendingChatTaskOptions(activeSessionId ?? ""),
  );
  const pendingTaskId = pendingTask?.task_id ?? null;

  // 获取所有 agents
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  // 获取所有 members
  const { data: members = [] } = useQuery(memberListOptions(wsId));

  // 获取所有运行时
  const { data: runtimes = [], isLoading: runtimesLoading } = useQuery(runtimeListOptions(wsId));

  // 当前会话对应的 agent
  const currentSession = sessions.find((s) => s.id === activeSessionId);
  const currentAgent = agents.find((a) => a.id === currentSession?.agent_id);

  // Goal-run execution view: when the active session IS the located goal_run's
  // discussion session (the issue → "open assistant session" jump), surface the
  // planning + subtask execution streams alongside the chat — otherwise an
  // autofix issue's output was invisible here. Read-only (no intervention); the
  // tasks page remains the place to act on a goal.
  const goalForSession =
    locatorGoalRun && locatorGoalRun.chat_session_id === activeSessionId
      ? locatorGoalRun
      : null;
  const [activeSubtaskId, setActiveSubtaskId] = useState<string | null>(null);
  const resolveAgentName = useCallback(
    (id: string) => agents.find((a) => a.id === id)?.name,
    [agents],
  );

  // Agent 可用性状态
  const presenceDetail = useAgentPresenceDetail(wsId, currentAgent?.id);
  const availability = presenceDetail === "loading" ? undefined : presenceDetail.availability;

  const { uploadWithToast } = useFileUpload(api);
  const createSession = useCreateChatSession();

  // 发送消息
  const handleSend = useCallback(
    async (content: string, attachmentIds?: string[]) => {
      if (!activeSessionId) {
        logger.warn("handleSend: no active session");
        return;
      }

      logger.info("sendMessage", { sessionId: activeSessionId, contentLength: content.length });

      try {
        await api.sendChatMessage(activeSessionId, content, attachmentIds);
      } catch (error) {
        logger.error("sendMessage failed", { error });
      }
    },
    [activeSessionId],
  );

  // 上传文件
  const handleUploadFile = useCallback(
    async (file: File) => {
      if (!activeSessionId) {
        logger.warn("handleUploadFile: no active session");
        return null;
      }

      return uploadWithToast(file, { chatSessionId: activeSessionId });
    },
    [activeSessionId, uploadWithToast],
  );

  // 停止任务
  const handleStop = useCallback(() => {
    if (!pendingTaskId) {
      logger.debug("handleStop: no pending task");
      return;
    }

    logger.info("cancelTask", { taskId: pendingTaskId });
    api.cancelTaskById(pendingTaskId).catch((err) => {
      logger.warn("cancelTask failed", { error: err });
    });
  }, [pendingTaskId]);

  // 选择会话
  const handleSelectSession = useCallback(
    (sessionId: string) => {
      setActiveSession(sessionId);
    },
    [setActiveSession],
  );

  // 创建新会话
  const handleCreateSession = useCallback(
    async (agentId: string, runtimeId: string) => {
      logger.info("createSession", { agentId, runtimeId });

      try {
        const session = await createSession.mutateAsync({
          agent_id: agentId,
          title: "",
          runtime_id: runtimeId,
        });

        // 预先设置空消息列表，避免加载闪烁
        qc.setQueryData<ChatMessage[]>(chatKeys.messages(session.id), []);

        // 切换到新会话
        setActiveSession(session.id);

        // 关闭对话框
        setShowNewSessionDialog(false);

        logger.info("createSession success", { sessionId: session.id });
      } catch (error) {
        logger.error("createSession failed", { error });
      }
    },
    [createSession, qc, setActiveSession],
  );

  return (
    // h-full (not h-screen): mounts below the app top bar / tab strip, so 100vh
    // would push the bottom of each scroll column off-screen.
    <div className="flex h-full min-h-0">
      {/* 左侧会话列表 */}
      <SessionList
        sessions={sessions}
        agents={agents}
        runtimes={runtimes}
        activeSessionId={activeSessionId}
        onSelectSession={handleSelectSession}
        onNewSession={() => setShowNewSessionDialog(true)}
      />

      {/* 右侧主体区域：纯聊天（目标模式已独立到「任务」页）。min-h-0 是关键：
          flex 列里的 flex-1 子项默认不会缩到内容高度以下，缺了它内部
          ChatMessageList 的 overflow-y-auto 就没有可滚动的有界高度 → 不能滚动。 */}
      <div className="flex-1 min-h-0 flex flex-col border-l">
        {activeSessionId ? (
          <>
            {/* Goal-run execution header: status tree + subtask switcher. Only
                for a session bound to a located goal_run (autofix / task jump). */}
            {goalForSession && (
              <GoalExecutionHeader
                goal={goalForSession}
                resolveAgentName={resolveAgentName}
                activeSubtaskId={activeSubtaskId}
                onSelectMain={() => setActiveSubtaskId(null)}
                onSelectSubtask={setActiveSubtaskId}
              />
            )}

            {/* 消息列表 - 复用现有组件。A selected subtask shows its own stream;
                otherwise the chat, with the planning/summary streams interleaved
                at the confirm gate (same model as the tasks page). This wrapper
                MUST be a flex column: ChatMessageList's scroll root is
                `flex-1 overflow-y-auto`, which only gets a bounded height (→ can
                scroll) when its parent is a flex column with min-h-0. A plain
                block here kills the scroll. */}
            <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
              {goalForSession && activeSubtaskId ? (
                <SubtaskStreamView
                  goal={goalForSession}
                  subtaskId={activeSubtaskId}
                />
              ) : (
                <ChatMessageList
                  messages={messages}
                  pendingTask={pendingTask}
                  availability={availability}
                  timelineInsert={
                    goalForSession?.planning_task_id
                      ? {
                          afterTs: goalForSession.confirmed_at,
                          content: (
                            <div className="space-y-3 border-y py-3">
                              <TaskStream
                                taskId={goalForSession.planning_task_id}
                                running={goalForSession.status === "planning"}
                                emptyHint={t(($) => $.task_page.planning_hint)}
                              />
                              {goalForSession.summary_task_id && (
                                <TaskStream
                                  taskId={goalForSession.summary_task_id}
                                  running={goalForSession.status === "executing"}
                                  emptyHint={t(($) => $.task_page.summarizing)}
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

            {/* 输入区域 - 复用现有组件 */}
            <ChatInput
              onSend={handleSend}
              onUploadFile={handleUploadFile}
              onStop={handleStop}
              isRunning={!!pendingTaskId}
              disabled={false}
              noAgent={!currentAgent}
              agentName={currentAgent?.name}
            />
          </>
        ) : (
          <div className="flex-1 flex items-center justify-center">
            <div className="text-center space-y-2">
              <h3 className="text-lg font-semibold text-muted-foreground">
                {t(($) => $.window.no_previous)}
              </h3>
              <p className="text-sm text-muted-foreground">
                {t(($) => $.empty_state.returning_subtitle)}
              </p>
            </div>
          </div>
        )}
      </div>

      {/* 新建会话对话框 */}
      <NewSessionDialog
        open={showNewSessionDialog}
        onOpenChange={setShowNewSessionDialog}
        agents={agents}
        runtimes={runtimes}
        runtimesLoading={runtimesLoading}
        members={members}
        currentUserId={user?.id ?? null}
        onCreateSession={handleCreateSession}
      />
    </div>
  );
}

/** Header bar for a goal-run-backed session: a progress chip that opens the
 *  status tree (reused from the tasks page). Read-only here — no intervention
 *  handlers — so the assistant stays a viewer; act on the goal from the tasks
 *  page. Clicking a subtask switches the content below to its execution stream. */
function GoalExecutionHeader({
  goal,
  resolveAgentName,
  activeSubtaskId,
  onSelectMain,
  onSelectSubtask,
}: {
  goal: GoalRun;
  resolveAgentName: (id: string) => string | undefined;
  activeSubtaskId: string | null;
  onSelectMain: () => void;
  onSelectSubtask: (id: string) => void;
}) {
  const { t } = useT("chat");
  const total = goal.subtasks.length;
  const done = goal.subtasks.filter((s) => s.status === "completed").length;

  return (
    <div className="flex items-center justify-between gap-2 border-b px-4 py-2">
      <span className="truncate text-xs text-muted-foreground">{goal.title || goal.goal}</span>
      <Popover>
        <PopoverTrigger
          render={<Button variant="outline" size="sm" className="h-7 shrink-0 gap-1.5 text-xs" />}
        >
          <ListTree className="h-3.5 w-3.5" />
          {t(($) => $.task_page.status_tree)}
          {total > 0 && (
            <span className="font-mono tabular-nums text-muted-foreground/70">
              {done}/{total}
            </span>
          )}
          <ChevronDown className="h-3 w-3 opacity-60" />
        </PopoverTrigger>
        <PopoverContent align="end" className="max-h-[70vh] w-[360px] overflow-y-auto p-0">
          <GoalStatusTree
            goal={goal}
            resolveAgentName={resolveAgentName}
            selectedSubtaskId={activeSubtaskId}
            onSelectMain={onSelectMain}
            onSelectSubtask={onSelectSubtask}
          />
        </PopoverContent>
      </Popover>
    </div>
  );
}

/** Read-only execution stream for one subtask (title + spec + transcript). */
function SubtaskStreamView({ goal, subtaskId }: { goal: GoalRun; subtaskId: string }) {
  const { t } = useT("chat");
  const subtask: GoalSubtask | undefined = goal.subtasks.find((s) => s.id === subtaskId);
  if (!subtask) return null;
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
          <p className="text-xs text-muted-foreground">{t(($) => $.task_page.planning_hint)}</p>
        )}
      </div>
    </div>
  );
}
