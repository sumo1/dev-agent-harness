import type {
  ChannelProvider,
  SessionCommand,
  SessionCommandId,
  SessionCommandScope,
  WorkItemKind,
} from "../types";

export interface SessionCommandContext {
  workItemKind?: WorkItemKind;
  channelProvider?: ChannelProvider;
  automationProvider?: ChannelProvider;
}

export const SESSION_COMMANDS: SessionCommand[] = [
  {
    id: "retry",
    kind: "prompt_shortcut",
    scopes: ["goal", "issue", "assistant"],
    label: "Retry",
    message_template: "Please retry based on the visible conversation history. Explain what changed before continuing.",
  },
  {
    id: "continue",
    kind: "prompt_shortcut",
    scopes: ["goal", "issue", "assistant"],
    label: "Continue",
    message_template: "Continue from the current conversation state.",
  },
  {
    id: "interrupt",
    kind: "runtime_control",
    scopes: ["goal", "issue", "assistant"],
    label: "Interrupt",
    requires_running_run: true,
  },
  {
    id: "cancel",
    kind: "runtime_control",
    scopes: ["goal", "issue", "assistant"],
    label: "Cancel",
    requires_running_run: true,
  },
  {
    id: "dispatch_as_goal",
    kind: "workflow_transition",
    scopes: ["channel:openclaw"],
    label: "Dispatch as Goal",
  },
  {
    id: "dispatch_as_issue",
    kind: "workflow_transition",
    scopes: ["channel:openclaw"],
    label: "Dispatch as Issue",
  },
  {
    id: "continue_in_assistant",
    kind: "workflow_transition",
    scopes: ["channel:openclaw"],
    label: "Continue in Assistant",
  },
  {
    id: "sync_openclaw_automations",
    kind: "automation_control",
    scopes: ["automation:openclaw"],
    label: "Sync OpenClaw Automations",
  },
  {
    id: "pause_openclaw_automation",
    kind: "automation_control",
    scopes: ["automation:openclaw"],
    label: "Pause",
  },
  {
    id: "resume_openclaw_automation",
    kind: "automation_control",
    scopes: ["automation:openclaw"],
    label: "Resume",
  },
  {
    id: "edit_openclaw_automation",
    kind: "automation_control",
    scopes: ["automation:openclaw"],
    label: "Edit",
  },
  {
    id: "delete_openclaw_automation",
    kind: "automation_control",
    scopes: ["automation:openclaw"],
    label: "Delete",
  },
];

export function getSessionCommands(context: SessionCommandContext): SessionCommand[] {
  const scopes = new Set<SessionCommandScope>(["all"]);
  if (context.workItemKind) scopes.add(context.workItemKind);
  if (context.channelProvider) scopes.add(`channel:${context.channelProvider}`);
  if (context.automationProvider) scopes.add(`automation:${context.automationProvider}`);

  return SESSION_COMMANDS.filter((command) => command.scopes.some((scope) => scopes.has(scope)));
}

export function getSessionCommand(id: SessionCommandId): SessionCommand | undefined {
  return SESSION_COMMANDS.find((command) => command.id === id);
}
