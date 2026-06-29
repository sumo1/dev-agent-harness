package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issueposition"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type openClawChannelStatusResponse struct {
	Provider       string                      `json:"provider"`
	DisplayName    string                      `json:"display_name"`
	Status         string                      `json:"status"`
	ExecutablePath *string                     `json:"executable_path"`
	Version        *string                     `json:"version"`
	RuntimeID      *string                     `json:"runtime_id"`
	LastSyncedAt   *string                     `json:"last_synced_at"`
	LastError      *string                     `json:"last_error"`
	Capabilities   openClawChannelCapabilities `json:"capabilities"`
}

type openClawChannelCapabilities struct {
	Conversations bool `json:"conversations"`
	Automations   bool `json:"automations"`
	NativeWrite   bool `json:"native_write"`
}

type openClawConversationSummaryResponse struct {
	ID                 string  `json:"id"`
	Title              string  `json:"title"`
	Status             string  `json:"status"`
	LastMessagePreview *string `json:"last_message_preview"`
	MessageCount       int     `json:"message_count"`
	UpdatedAt          *string `json:"updated_at"`
	ExternalURL        *string `json:"external_url,omitempty"`
}

type openClawConversationMessageResponse struct {
	ID        string  `json:"id"`
	Role      string  `json:"role"`
	Content   string  `json:"content"`
	CreatedAt *string `json:"created_at"`
}

type openClawConversationDetailResponse struct {
	openClawConversationSummaryResponse
	Messages []openClawConversationMessageResponse `json:"messages"`
}

type openClawConversationListResponse struct {
	Conversations []openClawConversationSummaryResponse `json:"conversations"`
	LastSyncedAt  *string                               `json:"last_synced_at"`
	LastError     *string                               `json:"last_error"`
}

type openClawAutomationResponse struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Schedule    *string `json:"schedule"`
	Status      string  `json:"status"`
	LastRunAt   *string `json:"last_run_at"`
	NextRunAt   *string `json:"next_run_at"`
	ExternalURL *string `json:"external_url,omitempty"`
}

type openClawAutomationListResponse struct {
	Automations  []openClawAutomationResponse `json:"automations"`
	LastSyncedAt *string                      `json:"last_synced_at"`
	LastError    *string                      `json:"last_error"`
}

type openClawDispatchRequest struct {
	Target       string `json:"target"`
	Title        string `json:"title"`
	Instructions string `json:"instructions"`
}

type openClawDispatchResponse struct {
	Target  string  `json:"target"`
	Status  string  `json:"status"`
	ID      *string `json:"id"`
	Path    *string `json:"path"`
	Message string  `json:"message"`
}

type openClawSendMessageRequest struct {
	Message string `json:"message"`
}

func (h *Handler) GetOpenClawChannelStatus(w http.ResponseWriter, r *http.Request) {
	status := h.openClawChannelStatus(r.Context())
	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) ListOpenClawConversations(w http.ResponseWriter, r *http.Request) {
	items, errText := h.openClawConversations(r.Context())
	now := time.Now().UTC().Format(time.RFC3339)
	writeJSON(w, http.StatusOK, openClawConversationListResponse{
		Conversations: items,
		LastSyncedAt:  &now,
		LastError:     errText,
	})
}

func (h *Handler) GetOpenClawConversation(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "conversationId"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "conversation id is required")
		return
	}
	detail, err := h.openClawConversation(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (h *Handler) SendOpenClawConversationMessage(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "conversationId"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "conversation id is required")
		return
	}
	var req openClawSendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	message := strings.TrimSpace(req.Message)
	if message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}
	detail, err := h.openClawSendMessage(r.Context(), id, message)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversation": detail})
}

func (h *Handler) DispatchOpenClawConversation(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	conversationID := strings.TrimSpace(chi.URLParam(r, "conversationId"))
	if conversationID == "" {
		writeError(w, http.StatusBadRequest, "conversation id is required")
		return
	}
	var req openClawDispatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	detail, err := h.openClawConversation(r.Context(), conversationID)
	if err != nil {
		detail = syntheticOpenClawConversation(conversationID)
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = detail.Title
	}
	if title == "" {
		title = "OpenClaw conversation"
	}
	body := openClawDispatchBody(detail, req.Instructions)

	switch req.Target {
	case "assistant":
		h.dispatchOpenClawToAssistant(w, r, workspaceUUID, parseUUID(userID), title, body)
	case "goal":
		h.dispatchOpenClawToGoal(w, r, workspaceUUID, parseUUID(userID), title, body)
	case "issue":
		h.dispatchOpenClawToIssue(w, r, workspaceUUID, userID, title, body)
	default:
		writeError(w, http.StatusBadRequest, "unsupported dispatch target")
	}
}

func (h *Handler) ListOpenClawAutomations(w http.ResponseWriter, r *http.Request) {
	items, errText := h.openClawAutomations(r.Context())
	now := time.Now().UTC().Format(time.RFC3339)
	writeJSON(w, http.StatusOK, openClawAutomationListResponse{
		Automations:  items,
		LastSyncedAt: &now,
		LastError:    errText,
	})
}

func (h *Handler) SyncOpenClawAutomations(w http.ResponseWriter, r *http.Request) {
	h.ListOpenClawAutomations(w, r)
}

func (h *Handler) RunOpenClawAutomationCommand(w http.ResponseWriter, r *http.Request) {
	automationID := strings.TrimSpace(chi.URLParam(r, "automationId"))
	command := strings.TrimSpace(chi.URLParam(r, "commandId"))
	if automationID == "" || command == "" {
		writeError(w, http.StatusBadRequest, "automation id and command are required")
		return
	}
	if command != "pause" && command != "resume" && command != "edit" && command != "delete" {
		writeError(w, http.StatusBadRequest, "unsupported automation command")
		return
	}
	if err := runOpenClawNative(r.Context(), "automations", command, automationID, "--json"); err != nil {
		writeJSON(w, http.StatusOK, map[string]string{
			"automation_id": automationID,
			"command":       command,
			"status":        "unsupported",
			"message":       err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"automation_id": automationID,
		"command":       command,
		"status":        "ok",
		"message":       "OpenClaw automation command forwarded.",
	})
}

func (h *Handler) openClawChannelStatus(ctx context.Context) openClawChannelStatusResponse {
	execPath, err := openClawExecutable()
	var pathPtr *string
	var versionPtr *string
	var errPtr *string
	status := "connected"
	capabilities := openClawChannelCapabilities{
		Conversations: true,
		Automations:   true,
		NativeWrite:   true,
	}
	if err != nil {
		status = "disconnected"
		msg := err.Error()
		errPtr = &msg
		capabilities = openClawChannelCapabilities{}
	} else {
		pathPtr = &execPath
		if out, verr := runOpenClawOutput(ctx, "--version"); verr == nil {
			version := strings.TrimSpace(string(out))
			versionPtr = &version
		}
	}
	runtimeID := h.firstOpenClawRuntimeID(ctx)
	return openClawChannelStatusResponse{
		Provider:       "openclaw",
		DisplayName:    "OpenClaw",
		Status:         status,
		ExecutablePath: pathPtr,
		Version:        versionPtr,
		RuntimeID:      runtimeID,
		LastError:      errPtr,
		Capabilities:   capabilities,
	}
}

func (h *Handler) firstOpenClawRuntimeID(ctx context.Context) *string {
	workspaceID := ctxWorkspaceID(ctx)
	if workspaceID == "" {
		return nil
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return nil
	}
	runtimes, err := h.Queries.ListAgentRuntimes(ctx, wsUUID)
	if err != nil {
		return nil
	}
	for _, runtime := range runtimes {
		if runtime.Provider == "openclaw" {
			id := uuidToString(runtime.ID)
			return &id
		}
	}
	return nil
}

func (h *Handler) openClawConversations(ctx context.Context) ([]openClawConversationSummaryResponse, *string) {
	out, err := runOpenClawOutput(ctx, "conversations", "list", "--json")
	if err != nil {
		msg := err.Error()
		return []openClawConversationSummaryResponse{}, &msg
	}
	items, err := parseOpenClawConversationList(out)
	if err != nil {
		msg := err.Error()
		return []openClawConversationSummaryResponse{}, &msg
	}
	return items, nil
}

func (h *Handler) openClawConversation(ctx context.Context, id string) (openClawConversationDetailResponse, error) {
	out, err := runOpenClawOutput(ctx, "conversations", "get", id, "--json")
	if err != nil {
		return openClawConversationDetailResponse{}, err
	}
	return parseOpenClawConversationDetail(out)
}

func (h *Handler) openClawSendMessage(ctx context.Context, id, message string) (openClawConversationDetailResponse, error) {
	out, err := runOpenClawOutput(ctx, "conversations", "send", id, "--message", message, "--json")
	if err != nil {
		return openClawConversationDetailResponse{}, err
	}
	return parseOpenClawConversationDetail(out)
}

func (h *Handler) openClawAutomations(ctx context.Context) ([]openClawAutomationResponse, *string) {
	out, err := runOpenClawOutput(ctx, "automations", "list", "--json")
	if err != nil {
		msg := err.Error()
		return []openClawAutomationResponse{}, &msg
	}
	items, err := parseOpenClawAutomationList(out)
	if err != nil {
		msg := err.Error()
		return []openClawAutomationResponse{}, &msg
	}
	return items, nil
}

func (h *Handler) dispatchOpenClawToAssistant(w http.ResponseWriter, r *http.Request, workspaceID, userID pgtype.UUID, title, body string) {
	runtimeID := h.firstOpenClawRuntimeID(r.Context())
	if runtimeID == nil {
		writeJSON(w, http.StatusOK, openClawDispatchResponse{
			Target:  "assistant",
			Status:  "unsupported",
			Message: "No OpenClaw runtime is registered in this workspace.",
		})
		return
	}
	runtimeUUID := parseUUID(*runtimeID)
	agentID, err := h.resolveOrCreateDefaultChatAgent(r.Context(), workspaceID, runtimeUUID, userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	session, err := h.Queries.CreateChatSession(r.Context(), db.CreateChatSessionParams{
		WorkspaceID: workspaceID,
		AgentID:     agentID,
		CreatorID:   userID,
		Title:       title,
		RuntimeID:   runtimeUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create assistant session")
		return
	}
	msg, err := h.Queries.CreateChatMessage(r.Context(), db.CreateChatMessageParams{
		ChatSessionID: session.ID,
		Role:          "user",
		Content:       body,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to seed assistant session")
		return
	}
	_ = h.Queries.TouchChatSession(r.Context(), session.ID)
	id := uuidToString(session.ID)
	path := "/assistant?session_id=" + id
	h.publishChat(protocol.EventChatMessage, uuidToString(workspaceID), "member", uuidToString(userID), id, protocol.ChatMessagePayload{
		ChatSessionID: id,
		MessageID:     uuidToString(msg.ID),
		Role:          "user",
		Content:       body,
		CreatedAt:     timestampToString(msg.CreatedAt),
	})
	writeJSON(w, http.StatusCreated, openClawDispatchResponse{
		Target:  "assistant",
		Status:  "created",
		ID:      &id,
		Path:    &path,
		Message: "Assistant session created from OpenClaw conversation.",
	})
}

func (h *Handler) dispatchOpenClawToGoal(w http.ResponseWriter, r *http.Request, workspaceID, userID pgtype.UUID, title, goal string) {
	run, chat, err := h.GoalService.CreateTask(r.Context(), workspaceID, userID, title, goal, nil, pgtype.UUID{})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id := uuidToString(run.ID)
	chatID := uuidToString(chat.ID)
	path := "/tasks"
	writeJSON(w, http.StatusCreated, openClawDispatchResponse{
		Target:  "goal",
		Status:  "created",
		ID:      &id,
		Path:    &path,
		Message: "Goal created from OpenClaw conversation. Discussion chat: " + chatID,
	})
}

func (h *Handler) dispatchOpenClawToIssue(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID, userID, title, description string) {
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create issue")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)
	number, err := qtx.IncrementIssueCounter(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create issue")
		return
	}
	position, err := issueposition.NextTopPosition(r.Context(), tx, workspaceID, "todo")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create issue")
		return
	}
	creatorType, creatorID := h.resolveActor(r, userID, uuidToString(workspaceID))
	issue, err := qtx.CreateIssue(r.Context(), db.CreateIssueParams{
		WorkspaceID: workspaceID,
		Title:       title,
		Description: strToText(description),
		Status:      "todo",
		Priority:    "none",
		CreatorType: creatorType,
		CreatorID:   parseUUID(creatorID),
		Position:    position,
		Number:      number,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create issue")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create issue")
		return
	}
	prefix := h.getIssuePrefix(r.Context(), workspaceID)
	resp := issueToResponse(issue, prefix)
	h.publish(protocol.EventIssueCreated, uuidToString(workspaceID), creatorType, creatorID, map[string]any{"issue": resp})
	id := uuidToString(issue.ID)
	path := "/issues/" + id
	writeJSON(w, http.StatusCreated, openClawDispatchResponse{
		Target:  "issue",
		Status:  "created",
		ID:      &id,
		Path:    &path,
		Message: "Issue created from OpenClaw conversation.",
	})
}

func openClawExecutable() (string, error) {
	if custom := strings.TrimSpace(os.Getenv("MULTICA_OPENCLAW_PATH")); custom != "" {
		if _, err := os.Stat(custom); err != nil {
			return "", fmt.Errorf("openclaw executable not found at %q: %w", custom, err)
		}
		return custom, nil
	}
	path, err := exec.LookPath("openclaw")
	if err != nil {
		return "", fmt.Errorf("openclaw executable not found on PATH")
	}
	return path, nil
}

func runOpenClawNative(ctx context.Context, args ...string) error {
	_, err := runOpenClawOutput(ctx, args...)
	return err
}

func runOpenClawOutput(ctx context.Context, args ...string) ([]byte, error) {
	execPath, err := openClawExecutable()
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, execPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if runCtx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("openclaw command timed out")
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("openclaw %s failed: %s", strings.Join(args, " "), msg)
	}
	return out, nil
}

func parseOpenClawConversationList(raw []byte) ([]openClawConversationSummaryResponse, error) {
	values, err := decodeObjectList(raw, "conversations")
	if err != nil {
		return nil, err
	}
	out := make([]openClawConversationSummaryResponse, 0, len(values))
	for i, value := range values {
		id := firstString(value, "id", "conversation_id", "session_id")
		if id == "" {
			id = fmt.Sprintf("conversation-%d", i+1)
		}
		title := firstString(value, "title", "name", "summary")
		if title == "" {
			title = "OpenClaw conversation"
		}
		preview := optionalString(firstString(value, "last_message_preview", "preview", "last_message"))
		updated := optionalString(firstString(value, "updated_at", "last_message_at", "created_at"))
		out = append(out, openClawConversationSummaryResponse{
			ID:                 id,
			Title:              title,
			Status:             defaultString(firstString(value, "status"), "unknown"),
			LastMessagePreview: preview,
			MessageCount:       firstInt(value, "message_count", "messages_count"),
			UpdatedAt:          updated,
			ExternalURL:        optionalString(firstString(value, "url", "external_url")),
		})
	}
	return out, nil
}

func parseOpenClawConversationDetail(raw []byte) (openClawConversationDetailResponse, error) {
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return openClawConversationDetailResponse{}, fmt.Errorf("parse openclaw conversation JSON: %w", err)
	}
	if nested, ok := value["conversation"].(map[string]any); ok {
		value = nested
	}
	summaries, _ := parseOpenClawConversationList(mustMarshalJSON([]map[string]any{value}))
	detail := openClawConversationDetailResponse{Messages: []openClawConversationMessageResponse{}}
	if len(summaries) > 0 {
		detail.openClawConversationSummaryResponse = summaries[0]
	}
	if messages, ok := value["messages"].([]any); ok {
		for i, rawMsg := range messages {
			msg, ok := rawMsg.(map[string]any)
			if !ok {
				continue
			}
			id := firstString(msg, "id", "message_id")
			if id == "" {
				id = fmt.Sprintf("message-%d", i+1)
			}
			detail.Messages = append(detail.Messages, openClawConversationMessageResponse{
				ID:        id,
				Role:      defaultString(firstString(msg, "role", "type"), "assistant"),
				Content:   defaultString(firstString(msg, "content", "text", "message"), ""),
				CreatedAt: optionalString(firstString(msg, "created_at", "timestamp")),
			})
		}
	}
	detail.MessageCount = len(detail.Messages)
	return detail, nil
}

func syntheticOpenClawConversation(id string) openClawConversationDetailResponse {
	return openClawConversationDetailResponse{
		openClawConversationSummaryResponse: openClawConversationSummaryResponse{
			ID:           id,
			Title:        "OpenClaw conversation",
			Status:       "unknown",
			MessageCount: 0,
		},
		Messages: []openClawConversationMessageResponse{},
	}
}

func parseOpenClawAutomationList(raw []byte) ([]openClawAutomationResponse, error) {
	values, err := decodeObjectList(raw, "automations")
	if err != nil {
		return nil, err
	}
	out := make([]openClawAutomationResponse, 0, len(values))
	for i, value := range values {
		id := firstString(value, "id", "automation_id", "task_id")
		if id == "" {
			id = fmt.Sprintf("automation-%d", i+1)
		}
		title := firstString(value, "title", "name")
		if title == "" {
			title = "OpenClaw automation"
		}
		out = append(out, openClawAutomationResponse{
			ID:          id,
			Title:       title,
			Schedule:    optionalString(firstString(value, "schedule", "cron", "cron_expression")),
			Status:      defaultString(firstString(value, "status"), "unknown"),
			LastRunAt:   optionalString(firstString(value, "last_run_at", "last_fired_at")),
			NextRunAt:   optionalString(firstString(value, "next_run_at")),
			ExternalURL: optionalString(firstString(value, "url", "external_url")),
		})
	}
	return out, nil
}

func decodeObjectList(raw []byte, key string) ([]map[string]any, error) {
	var array []map[string]any
	if err := json.Unmarshal(raw, &array); err == nil {
		return array, nil
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("parse openclaw JSON: %w", err)
	}
	rawItems, ok := object[key].([]any)
	if !ok {
		return []map[string]any{}, nil
	}
	items := make([]map[string]any, 0, len(rawItems))
	for _, rawItem := range rawItems {
		if item, ok := rawItem.(map[string]any); ok {
			items = append(items, item)
		}
	}
	return items, nil
}

func openClawDispatchBody(detail openClawConversationDetailResponse, instructions string) string {
	var b strings.Builder
	b.WriteString("<channel_context>\n")
	b.WriteString("provider: openclaw\n")
	b.WriteString("channel: lobster\n")
	b.WriteString("external_conversation_id: " + detail.ID + "\n")
	b.WriteString("</channel_context>\n\n")
	if trimmed := strings.TrimSpace(instructions); trimmed != "" {
		b.WriteString(trimmed + "\n\n")
	}
	for _, msg := range detail.Messages {
		if strings.TrimSpace(msg.Content) == "" {
			continue
		}
		b.WriteString(msg.Role + ": " + msg.Content + "\n\n")
	}
	if len(detail.Messages) == 0 {
		b.WriteString("Continue from the OpenClaw conversation: " + detail.Title)
	}
	return strings.TrimSpace(b.String())
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := m[key]; ok {
			switch v := value.(type) {
			case string:
				if strings.TrimSpace(v) != "" {
					return v
				}
			case fmt.Stringer:
				return v.String()
			}
		}
	}
	return ""
}

func firstInt(m map[string]any, keys ...string) int {
	for _, key := range keys {
		switch v := m[key].(type) {
		case float64:
			return int(v)
		case int:
			return v
		}
	}
	return 0
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func optionalString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func mustMarshalJSON(v any) []byte {
	out, _ := json.Marshal(v)
	return out
}
