import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import type { OpenClawAutomationCommand, OpenClawDispatchRequest } from "../types";
import { openClawChannelKeys } from "./openclaw-queries";

export function useSendOpenClawMessage() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: ({ conversationId, message }: { conversationId: string; message: string }) =>
      api.sendOpenClawConversationMessage(conversationId, message),
    onSettled: (_data, _error, vars) => {
      qc.invalidateQueries({ queryKey: openClawChannelKeys.conversation(wsId, vars.conversationId) });
      qc.invalidateQueries({ queryKey: openClawChannelKeys.conversations(wsId) });
    },
  });
}

export function useDispatchOpenClawConversation() {
  return useMutation({
    mutationFn: ({ conversationId, data }: { conversationId: string; data: OpenClawDispatchRequest }) =>
      api.dispatchOpenClawConversation(conversationId, data),
  });
}

export function useSyncOpenClawAutomations() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: () => api.syncOpenClawAutomations(),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: openClawChannelKeys.automations(wsId) });
      qc.invalidateQueries({ queryKey: openClawChannelKeys.status(wsId) });
    },
  });
}

export function useOpenClawAutomationCommand() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: ({ automationId, command }: { automationId: string; command: OpenClawAutomationCommand }) =>
      api.runOpenClawAutomationCommand(automationId, command),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: openClawChannelKeys.automations(wsId) });
    },
  });
}
