"use client";

import type { SessionCommand, SessionCommandId } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  CirclePause,
  CirclePlay,
  MessageSquare,
  Pause,
  Pencil,
  RefreshCw,
  Send,
  Square,
  Target,
  Trash2,
  X,
} from "lucide-react";

const COMMAND_ICON: Partial<Record<SessionCommandId, typeof RefreshCw>> = {
  retry: RefreshCw,
  continue: Send,
  interrupt: Pause,
  cancel: X,
  dispatch_as_goal: Target,
  dispatch_as_issue: Square,
  continue_in_assistant: MessageSquare,
  sync_openclaw_automations: RefreshCw,
  pause_openclaw_automation: CirclePause,
  resume_openclaw_automation: CirclePlay,
  edit_openclaw_automation: Pencil,
  delete_openclaw_automation: Trash2,
};

export function CommandBar({
  commands,
  onCommand,
  disabled,
  pendingCommandId,
}: {
  commands: SessionCommand[];
  onCommand: (command: SessionCommand) => void;
  disabled?: boolean;
  pendingCommandId?: SessionCommandId | null;
}) {
  return (
    <div className="flex flex-wrap gap-2">
      {commands.map((command) => {
        const Icon = COMMAND_ICON[command.id] ?? Send;
        const pending = pendingCommandId === command.id;

        return (
          <Button
            key={command.id}
            size="sm"
            variant="outline"
            className="h-8 gap-1.5 text-xs"
            disabled={disabled || pending}
            onClick={() => onCommand(command)}
          >
            <Icon className={pending ? "size-3.5 animate-spin" : "size-3.5"} />
            {command.label}
          </Button>
        );
      })}
    </div>
  );
}
