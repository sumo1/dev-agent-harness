"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  openClawAutomationsOptions,
  openClawChannelStatusOptions,
  openClawConversationOptions,
  openClawConversationsOptions,
} from "@multica/core/channels/openclaw-queries";
import {
  useDispatchOpenClawConversation,
  useOpenClawAutomationCommand,
  useSendOpenClawMessage,
  useSyncOpenClawAutomations,
} from "@multica/core/channels/openclaw-mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { getSessionCommands } from "@multica/core/session-commands";
import type {
  OpenClawAutomationCommand,
  OpenClawConversationSummary,
  OpenClawDispatchTarget,
  RuntimeContext,
  SessionCommand,
  SessionCommandId,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { Bot, CalendarClock, CirclePause, CirclePlay, MessageSquare, RefreshCw, Send } from "lucide-react";
import { useNavigation } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";
import { ProviderLogo } from "../../runtimes/components/provider-logo";
import { AgentSessionPanel, CommandBar, ContextBar } from "../../common/agent-session";

function EmptyState({ text }: { text: string }) {
  return (
    <div className="flex min-h-36 items-center justify-center px-4 text-center text-sm text-muted-foreground">
      {text}
    </div>
  );
}

function ConversationRow({
  conversation,
  selected,
  onSelect,
}: {
  conversation: OpenClawConversationSummary;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        "flex w-full flex-col gap-1 border-b px-4 py-3 text-left text-sm transition-colors hover:bg-accent/50",
        selected && "bg-accent/60",
      )}
    >
      <div className="flex min-w-0 items-center gap-2">
        <MessageSquare className="size-4 shrink-0 text-muted-foreground" />
        <span className="min-w-0 flex-1 truncate font-medium">{conversation.title}</span>
        <span className="shrink-0 text-xs text-muted-foreground tabular-nums">{conversation.message_count}</span>
      </div>
      <div className="line-clamp-2 pl-6 text-xs text-muted-foreground">
        {conversation.last_message_preview ?? conversation.status}
      </div>
    </button>
  );
}

export function LobsterPage() {
  const { t } = useT("lobster");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const navigation = useNavigation();
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [message, setMessage] = useState("");
  const [instructions, setInstructions] = useState("");
  const [pendingDispatchId, setPendingDispatchId] = useState<SessionCommandId | null>(null);

  const { data: status, isLoading: statusLoading } = useQuery(openClawChannelStatusOptions(wsId));
  const { data: conversations, isLoading: conversationsLoading } = useQuery(openClawConversationsOptions(wsId));
  const { data: conversation } = useQuery(openClawConversationOptions(wsId, selectedId));
  const { data: automations, isLoading: automationsLoading } = useQuery(openClawAutomationsOptions(wsId));
  const syncAutomations = useSyncOpenClawAutomations();
  const sendMessage = useSendOpenClawMessage();
  const dispatchConversation = useDispatchOpenClawConversation();
  const automationCommand = useOpenClawAutomationCommand();

  const conversationItems = conversations?.conversations ?? [];
  const automationItems = automations?.automations ?? [];
  const selectedConversation = conversation?.id ? conversation : null;
  const connected = status?.status === "connected";
  const runtimeContext = useMemo<RuntimeContext>(() => ({
    work_item: {
      kind: "assistant",
      id: selectedId ?? "openclaw",
      title: selectedConversation?.title ?? t(($) => $.page.title),
    },
    workspace: {
      id: wsId,
      name: wsId,
    },
    runtime: status?.runtime_id
      ? {
          id: status.runtime_id,
          provider: "openclaw",
          name: status.display_name ?? "OpenClaw",
        }
      : undefined,
    channel: {
      provider: "openclaw",
      channel: "lobster",
      external_conversation_id: selectedId ?? undefined,
    },
    custom_blocks: [],
  }), [selectedConversation?.title, selectedId, status?.display_name, status?.runtime_id, t, wsId]);
  const dispatchCommands = useMemo<SessionCommand[]>(() => {
    const labels: Partial<Record<SessionCommandId, string>> = {
      dispatch_as_goal: t(($) => $.actions.dispatch_goal),
      dispatch_as_issue: t(($) => $.actions.dispatch_issue),
      continue_in_assistant: t(($) => $.actions.continue_assistant),
    };
    return getSessionCommands({ channelProvider: "openclaw" })
      .filter((command) =>
        command.id === "dispatch_as_goal" ||
        command.id === "dispatch_as_issue" ||
        command.id === "continue_in_assistant",
      )
      .map((command) => ({ ...command, label: labels[command.id] ?? command.label }));
  }, [t]);

  useEffect(() => {
    if (!selectedId && conversationItems.length > 0) {
      setSelectedId(conversationItems[0]?.id ?? null);
    }
  }, [conversationItems, selectedId]);

  const statusText = useMemo(() => {
    if (statusLoading) return "...";
    switch (status?.status) {
      case "connected":
        return t(($) => $.page.connected);
      case "error":
        return t(($) => $.page.error);
      default:
        return t(($) => $.page.disconnected);
    }
  }, [status?.status, statusLoading, t]);

  const dispatch = (target: OpenClawDispatchTarget, commandId: SessionCommandId) => {
    if (!selectedId) return;
    setPendingDispatchId(commandId);
    dispatchConversation.mutate(
      {
        conversationId: selectedId,
        data: {
          target,
          title: selectedConversation?.title,
          instructions,
        },
      },
      {
        onSuccess: (result) => {
          if (result.path) {
            if (target === "assistant" && result.id) {
              navigation.push(wsPaths.assistant({ sessionId: result.id }));
              return;
            }
            if (target === "issue" && result.id) {
              navigation.push(wsPaths.issueDetail(result.id));
              return;
            }
            if (target === "goal") {
              navigation.push(wsPaths.tasks());
            }
          }
        },
        onSettled: () => setPendingDispatchId(null),
      },
    );
  };

  const handleDispatchCommand = (command: SessionCommand) => {
    const targetByCommand: Partial<Record<SessionCommandId, OpenClawDispatchTarget>> = {
      dispatch_as_goal: "goal",
      dispatch_as_issue: "issue",
      continue_in_assistant: "assistant",
    };
    const target = targetByCommand[command.id];
    if (target) dispatch(target, command.id);
  };

  const runAutomationCommand = (automationId: string, command: OpenClawAutomationCommand) => {
    automationCommand.mutate({ automationId, command });
  };

  const submitMessage = () => {
    if (!selectedId || !message.trim()) return;
    sendMessage.mutate(
      { conversationId: selectedId, message: message.trim() },
      { onSuccess: () => setMessage("") },
    );
  };

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader className="justify-between px-5">
        <div className="flex min-w-0 items-center gap-2">
          <ProviderLogo provider="openclaw" className="size-4" />
          <h1 className="text-sm font-medium">{t(($) => $.page.title)}</h1>
          <span className="hidden truncate text-xs text-muted-foreground sm:inline">
            {t(($) => $.page.subtitle)}
          </span>
          <Badge variant={connected ? "default" : "secondary"} className="h-5 rounded-sm px-1.5 text-[11px]">
            {statusText}
          </Badge>
        </div>
        <Button
          size="sm"
          variant="outline"
          disabled={syncAutomations.isPending}
          onClick={() => syncAutomations.mutate()}
        >
          <RefreshCw className={cn("mr-1 size-3.5", syncAutomations.isPending && "animate-spin")} />
          {t(($) => $.page.sync)}
        </Button>
      </PageHeader>

      <ContextBar context={runtimeContext} />
      {(status?.last_error || (!connected && !status?.last_error)) && (
        <div className="border-b px-5 py-1.5 text-xs text-muted-foreground">
          {status?.last_error ? (
            <span className="text-destructive">{status.last_error}</span>
          ) : (
            <span>{t(($) => $.page.native_unavailable)}</span>
          )}
        </div>
      )}

      <div className="grid min-h-0 flex-1 grid-cols-1 overflow-hidden lg:grid-cols-[320px_minmax(0,1fr)]">
        <section className="min-h-0 border-b lg:border-b-0 lg:border-r">
          <div className="flex h-9 items-center justify-between border-b px-4">
            <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
              <MessageSquare className="size-3.5" />
              {t(($) => $.sections.conversations)}
            </div>
            <span className="text-xs text-muted-foreground tabular-nums">{conversationItems.length}</span>
          </div>
          <div className="max-h-64 overflow-y-auto lg:max-h-none lg:h-[calc(100%-2.25rem)]">
            {conversationsLoading ? (
              <div className="space-y-2 p-4">
                {Array.from({ length: 4 }).map((_, i) => (
                  <Skeleton key={i} className="h-16 w-full" />
                ))}
              </div>
            ) : conversationItems.length === 0 ? (
              <EmptyState text={t(($) => $.empty.conversations)} />
            ) : (
              conversationItems.map((item) => (
                <ConversationRow
                  key={item.id}
                  conversation={item}
                  selected={item.id === selectedId}
                  onSelect={() => setSelectedId(item.id)}
                />
              ))
            )}
          </div>
        </section>

        <main className="grid min-h-0 grid-rows-[minmax(0,1fr)_auto] overflow-hidden">
          <div className="grid min-h-0 grid-cols-1 overflow-hidden xl:grid-cols-[minmax(0,1fr)_360px]">
            <section className="min-h-0 border-b xl:border-b-0 xl:border-r">
              <div className="flex h-9 items-center gap-2 border-b px-4 text-xs font-medium text-muted-foreground">
                <Bot className="size-3.5" />
                {t(($) => $.sections.conversation_detail)}
              </div>
              <div className="h-[calc(100%-2.25rem)] overflow-y-auto p-4">
                {!selectedId ? (
                  <EmptyState text={t(($) => $.empty.select_conversation)} />
                ) : !selectedConversation ? (
                  <div className="space-y-3">
                    <Skeleton className="h-5 w-48" />
                    <Skeleton className="h-24 w-full" />
                    <Skeleton className="h-20 w-3/4" />
                  </div>
                ) : (
                  <AgentSessionPanel
                    messages={selectedConversation.messages.map((item) => ({
                      id: item.id,
                      role: item.role,
                      content: item.content,
                      created_at: item.created_at,
                    }))}
                    empty={t(($) => $.empty.select_conversation)}
                  />
                )}
              </div>
            </section>

            <aside className="min-h-0 overflow-y-auto">
              <section className="border-b">
                <div className="flex h-9 items-center gap-2 border-b px-4 text-xs font-medium text-muted-foreground">
                  <Send className="size-3.5" />
                  {t(($) => $.sections.dispatch)}
                </div>
                <div className="space-y-3 p-4">
                  <Textarea
                    value={instructions}
                    onChange={(event) => setInstructions(event.target.value)}
                    placeholder={t(($) => $.fields.instructions)}
                    className="min-h-24 resize-none text-sm"
                  />
                  <CommandBar
                    commands={dispatchCommands}
                    disabled={!selectedId || dispatchConversation.isPending}
                    pendingCommandId={pendingDispatchId}
                    onCommand={handleDispatchCommand}
                  />
                  {dispatchConversation.data?.message && (
                    <p className="text-xs text-muted-foreground">{dispatchConversation.data.message}</p>
                  )}
                </div>
              </section>

              <section>
                <div className="flex h-9 items-center gap-2 border-b px-4 text-xs font-medium text-muted-foreground">
                  <CalendarClock className="size-3.5" />
                  {t(($) => $.sections.automations)}
                </div>
                <div className="divide-y">
                  {automationsLoading ? (
                    <div className="space-y-2 p-4">
                      <Skeleton className="h-14 w-full" />
                      <Skeleton className="h-14 w-full" />
                    </div>
                  ) : automationItems.length === 0 ? (
                    <EmptyState text={t(($) => $.empty.automations)} />
                  ) : (
                    automationItems.map((item) => (
                      <div key={item.id} className="px-4 py-3 text-sm">
                        <div className="flex items-center justify-between gap-2">
                          <span className="min-w-0 truncate font-medium">{item.title}</span>
                          <div className="flex shrink-0 items-center gap-1">
                            <Badge variant="secondary" className="h-5 rounded-sm px-1.5 text-[11px]">
                              {item.status}
                            </Badge>
                            <Button
                              size="icon"
                              variant="ghost"
                              className="size-7"
                              disabled={automationCommand.isPending}
                              onClick={() =>
                                runAutomationCommand(item.id, item.status === "active" ? "pause" : "resume")
                              }
                              aria-label={
                                item.status === "active"
                                  ? t(($) => $.actions.pause)
                                  : t(($) => $.actions.resume)
                              }
                            >
                              {item.status === "active" ? (
                                <CirclePause className="size-3.5" />
                              ) : (
                                <CirclePlay className="size-3.5" />
                              )}
                            </Button>
                          </div>
                        </div>
                        <div className="mt-1 text-xs text-muted-foreground">
                          {item.schedule ?? t(($) => $.automations.schedule_empty)}
                        </div>
                      </div>
                    ))
                  )}
                </div>
              </section>
            </aside>
          </div>

          <div className="flex items-end gap-2 border-t p-3">
            <Textarea
              value={message}
              onChange={(event) => setMessage(event.target.value)}
              placeholder={t(($) => $.fields.message)}
              disabled={!connected || !selectedId}
              className="max-h-32 min-h-10 resize-none text-sm"
            />
            <Button
              size="icon"
              disabled={!connected || !selectedId || !message.trim() || sendMessage.isPending}
              onClick={submitMessage}
              aria-label={t(($) => $.actions.send)}
            >
              <Send className="size-4" />
            </Button>
          </div>
        </main>
      </div>
    </div>
  );
}
