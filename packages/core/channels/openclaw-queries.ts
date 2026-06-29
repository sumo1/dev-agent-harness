import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const openClawChannelKeys = {
  all: (wsId: string) => ["channels", wsId, "openclaw"] as const,
  status: (wsId: string) => [...openClawChannelKeys.all(wsId), "status"] as const,
  conversations: (wsId: string) => [...openClawChannelKeys.all(wsId), "conversations"] as const,
  conversation: (wsId: string, id: string) =>
    [...openClawChannelKeys.all(wsId), "conversation", id] as const,
  automations: (wsId: string) => [...openClawChannelKeys.all(wsId), "automations"] as const,
};

export function openClawChannelStatusOptions(wsId: string) {
  return queryOptions({
    queryKey: openClawChannelKeys.status(wsId),
    queryFn: () => api.getOpenClawChannelStatus(),
  });
}

export function openClawConversationsOptions(wsId: string) {
  return queryOptions({
    queryKey: openClawChannelKeys.conversations(wsId),
    queryFn: () => api.listOpenClawConversations(),
  });
}

export function openClawConversationOptions(
  wsId: string,
  id: string | null | undefined,
) {
  return queryOptions({
    queryKey: openClawChannelKeys.conversation(wsId, id ?? ""),
    queryFn: () => api.getOpenClawConversation(id ?? ""),
    enabled: !!id,
  });
}

export function openClawAutomationsOptions(wsId: string) {
  return queryOptions({
    queryKey: openClawChannelKeys.automations(wsId),
    queryFn: () => api.listOpenClawAutomations(),
  });
}
