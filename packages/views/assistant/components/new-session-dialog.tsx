"use client";

import { useState, useEffect } from "react";
import { ChevronRight, MessageSquare } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { RuntimePicker } from "../../agents/components/runtime-picker";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";
import type { Agent, MemberWithUser, RuntimeDevice } from "@multica/core/types";

export function NewSessionDialog({
  open,
  onOpenChange,
  agents,
  runtimes,
  runtimesLoading,
  members,
  currentUserId,
  onCreateSession,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  agents: Agent[];
  runtimes: RuntimeDevice[];
  runtimesLoading: boolean;
  members: MemberWithUser[];
  currentUserId: string | null;
  onCreateSession: (agentId: string, runtimeId: string) => Promise<void>;
}) {
  const { t } = useT("chat");
  // Agent is OPTIONAL: "" means "no agent → use the workspace default chat
  // agent" (a plain passthrough to the runtime). The runtime is what matters.
  const [selectedAgentId, setSelectedAgentId] = useState("");
  const [selectedRuntimeId, setSelectedRuntimeId] = useState("");
  const [isCreating, setIsCreating] = useState(false);

  // 当对话框打开时，重置选择。Default to NO agent (the default chat agent) so
  // the user only has to pick a runtime — matching "just talk to a runtime".
  useEffect(() => {
    if (open) {
      setSelectedAgentId("");
      setSelectedRuntimeId("");
    }
  }, [open]);

  const handleCreate = async () => {
    if (!selectedRuntimeId) return;

    setIsCreating(true);
    try {
      // Empty agentId → server resolves the workspace default chat agent.
      await onCreateSession(selectedAgentId, selectedRuntimeId);
    } finally {
      setIsCreating(false);
    }
  };

  // A chat session binds its runtime immutably at create time, so an offline
  // runtime would produce a conversation that can never run. Block create when
  // the selected runtime is not online (the picker also disables offline rows).
  // Agent is optional — only the runtime is required.
  const selectedRuntime = runtimes.find((r) => r.id === selectedRuntimeId);
  const runtimeOnline = selectedRuntime?.status === "online";
  const canCreate = !!selectedRuntimeId && runtimeOnline && !isCreating;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {/* Cap the dialog height and make it a column so a long agent/runtime
          list scrolls INSIDE instead of overflowing the viewport (which made
          the centered box bleed off the top + bottom edges). */}
      <DialogContent className="flex max-h-[85vh] flex-col sm:max-w-[500px]">
        <DialogHeader>
          <DialogTitle>{t(($) => $.new_dialog.title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.new_dialog.description)}
          </DialogDescription>
        </DialogHeader>

        <div className="-mx-4 flex-1 space-y-6 overflow-y-auto px-4 py-4">
          {/* 运行时选择 — 这是必选项，对话直接发给所选运行时 */}
          <RuntimePicker
            runtimes={runtimes}
            runtimesLoading={runtimesLoading}
            members={members}
            currentUserId={currentUserId}
            selectedRuntimeId={selectedRuntimeId}
            onSelect={setSelectedRuntimeId}
            blockOffline
          />

          {/* 智能体选择 — 可选。默认不绑定（直接对话）；想用某个角色再选。 */}
          <details className="group">
            <summary className="flex cursor-pointer list-none items-center gap-2 text-sm text-muted-foreground select-none">
              <ChevronRight className="size-4 transition-transform group-open:rotate-90" />
              {selectedAgentId
                ? agents.find((a) => a.id === selectedAgentId)?.name ??
                  t(($) => $.new_dialog.agent_label)
                : t(($) => $.new_dialog.agent_optional)}
            </summary>
            <div className="mt-2 space-y-2">
              {/* 默认（不绑定智能体） */}
              <button
                type="button"
                onClick={() => setSelectedAgentId("")}
                className={`flex w-full items-center gap-3 rounded-lg border px-4 py-3 text-left transition-colors ${
                  selectedAgentId === ""
                    ? "border-primary bg-accent"
                    : "border-border hover:bg-accent/50"
                }`}
              >
                <div className="flex size-8 shrink-0 items-center justify-center rounded-full bg-muted">
                  <MessageSquare className="size-4 text-muted-foreground" />
                </div>
                <div className="flex-1 min-w-0">
                  <div className="font-medium truncate">
                    {t(($) => $.new_dialog.agent_none)}
                  </div>
                  <div className="text-xs text-muted-foreground truncate">
                    {t(($) => $.new_dialog.agent_none_hint)}
                  </div>
                </div>
                {selectedAgentId === "" && (
                  <div className="h-2 w-2 rounded-full bg-primary" />
                )}
              </button>

              {agents.filter((a) => !a.archived_at).map((agent) => (
                <button
                  key={agent.id}
                  type="button"
                  onClick={() => setSelectedAgentId(agent.id)}
                  className={`flex w-full items-center gap-3 rounded-lg border px-4 py-3 text-left transition-colors ${
                    selectedAgentId === agent.id
                      ? "border-primary bg-accent"
                      : "border-border hover:bg-accent/50"
                  }`}
                >
                  <ActorAvatar actorType="agent" actorId={agent.id} size={32} />
                  <div className="flex-1 min-w-0">
                    <div className="font-medium truncate">{agent.name}</div>
                    {agent.instructions && (
                      <div className="text-xs text-muted-foreground truncate">
                        {agent.instructions}
                      </div>
                    )}
                  </div>
                  {selectedAgentId === agent.id && (
                    <div className="h-2 w-2 rounded-full bg-primary" />
                  )}
                </button>
              ))}
            </div>
          </details>
        </div>

        <DialogFooter>
          <Button
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={isCreating}
          >
            {t(($) => $.new_dialog.cancel)}
          </Button>
          <Button
            onClick={handleCreate}
            disabled={!canCreate}
          >
            {isCreating ? t(($) => $.new_dialog.creating) : t(($) => $.new_dialog.create)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
