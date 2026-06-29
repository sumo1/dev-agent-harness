export type WorkItemKind = "goal" | "issue" | "assistant";

export type RuntimeProvider = "codex" | "claude_code" | "openclaw" | string;

export interface RuntimeExternalRef {
  provider: RuntimeProvider;
  kind: string;
  id: string;
  url?: string;
}

export interface AgentSession {
  id: string;
  workspace_id: string;
  work_item_id: string;
  work_item_kind: WorkItemKind;
  title: string;
  runtime_id?: string;
  agent_id?: string;
  status: "idle" | "running" | "waiting" | "failed" | "completed" | "cancelled";
  context_snapshot_id: string;
  created_at: string;
  updated_at: string;
}

export interface RuntimeRun {
  id: string;
  session_id: string;
  runtime_id: string;
  provider: RuntimeProvider;
  status: "queued" | "running" | "succeeded" | "failed" | "cancelled";
  task_queue_job_id?: string;
  external_ref?: RuntimeExternalRef;
  started_at?: string;
  ended_at?: string;
}

export interface RuntimeContextBlock {
  id: string;
  title: string;
  content: string;
}

export interface RuntimeContext {
  work_item: {
    kind: WorkItemKind;
    id: string;
    title: string;
    description?: string;
  };
  workspace: {
    id: string;
    name: string;
  };
  project?: {
    id: string;
    name: string;
    local_directory?: string;
    git_remote?: string;
  };
  runtime?: {
    id: string;
    provider: RuntimeProvider;
    name: string;
  };
  agent?: {
    id: string;
    name: string;
  };
  channel?: {
    provider: ChannelProvider;
    channel: string;
    external_conversation_id?: string;
    external_message_id?: string;
  };
  custom_blocks: RuntimeContextBlock[];
}

export type ChannelProvider = "openclaw" | string;

export interface ChannelSurface {
  id: string;
  workspace_id: string;
  provider: ChannelProvider;
  runtime_id?: string;
  display_name: string;
  status: "connected" | "disconnected" | "error";
  external_ref?: RuntimeExternalRef;
}

export type SessionCommandKind =
  | "prompt_shortcut"
  | "runtime_control"
  | "workflow_transition"
  | "automation_control";

export type SessionCommandId =
  | "retry"
  | "continue"
  | "interrupt"
  | "cancel"
  | "dispatch_as_goal"
  | "dispatch_as_issue"
  | "continue_in_assistant"
  | "sync_openclaw_automations"
  | "pause_openclaw_automation"
  | "resume_openclaw_automation"
  | "edit_openclaw_automation"
  | "delete_openclaw_automation";

export type SessionCommandScope =
  | "all"
  | WorkItemKind
  | `channel:${ChannelProvider}`
  | `automation:${ChannelProvider}`;

export interface SessionCommand {
  id: SessionCommandId;
  kind: SessionCommandKind;
  scopes: SessionCommandScope[];
  label: string;
  message_template?: string;
  requires_running_run?: boolean;
}
