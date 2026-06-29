import type { WorkItemKind } from "./agent-session";

export type OpenClawChannelStatusValue = "connected" | "disconnected" | "error";

export interface OpenClawChannelStatus {
  provider: "openclaw";
  display_name: string;
  status: OpenClawChannelStatusValue;
  executable_path: string | null;
  version: string | null;
  runtime_id: string | null;
  last_synced_at: string | null;
  last_error: string | null;
  capabilities: {
    conversations: boolean;
    automations: boolean;
    native_write: boolean;
  };
}

export interface OpenClawConversationSummary {
  id: string;
  title: string;
  status: string;
  last_message_preview: string | null;
  message_count: number;
  updated_at: string | null;
  external_url?: string | null;
}

export interface OpenClawConversationMessage {
  id: string;
  role: "user" | "assistant" | "system" | string;
  content: string;
  created_at: string | null;
}

export interface OpenClawConversationDetail extends OpenClawConversationSummary {
  messages: OpenClawConversationMessage[];
}

export interface OpenClawConversationListResponse {
  conversations: OpenClawConversationSummary[];
  last_synced_at: string | null;
  last_error: string | null;
}

export type OpenClawDispatchTarget = WorkItemKind;

export interface OpenClawDispatchRequest {
  target: OpenClawDispatchTarget;
  title?: string;
  instructions?: string;
}

export interface OpenClawDispatchResponse {
  target: OpenClawDispatchTarget;
  status: "created" | "queued" | "unsupported";
  id: string | null;
  path: string | null;
  message: string;
}

export interface OpenClawSendMessageResponse {
  conversation: OpenClawConversationDetail;
}

export interface OpenClawAutomation {
  id: string;
  title: string;
  schedule: string | null;
  status: "active" | "paused" | "disabled" | "unknown" | string;
  last_run_at: string | null;
  next_run_at: string | null;
  external_url?: string | null;
}

export interface OpenClawAutomationListResponse {
  automations: OpenClawAutomation[];
  last_synced_at: string | null;
  last_error: string | null;
}

export type OpenClawAutomationCommand = "pause" | "resume" | "edit" | "delete";

export interface OpenClawAutomationCommandResponse {
  automation_id: string;
  command: OpenClawAutomationCommand;
  status: "ok" | "unsupported";
  message: string;
}
