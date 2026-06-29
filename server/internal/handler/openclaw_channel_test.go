package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestOpenClawDispatchBodyIncludesChannelContext(t *testing.T) {
	body := openClawDispatchBody(openClawConversationDetailResponse{
		openClawConversationSummaryResponse: openClawConversationSummaryResponse{
			ID:    "conv-1",
			Title: "Incident thread",
		},
		Messages: []openClawConversationMessageResponse{
			{ID: "msg-1", Role: "user", Content: "Fix the failing flow."},
			{ID: "msg-2", Role: "assistant", Content: "I found the missing context."},
		},
	}, "Preserve the original acceptance criteria.")

	for _, want := range []string{
		"<channel_context>",
		"provider: openclaw",
		"channel: lobster",
		"external_conversation_id: conv-1",
		"Preserve the original acceptance criteria.",
		"user: Fix the failing flow.",
		"assistant: I found the missing context.",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("dispatch body missing %q:\n%s", want, body)
		}
	}
}

func TestDispatchOpenClawConversationToAssistantSeedsContextMessage(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture unavailable")
	}
	t.Setenv("MULTICA_OPENCLAW_PATH", "/definitely/missing/openclaw-for-test")

	ctx := context.Background()
	var originalDefaultAgent sql.NullString
	if err := testPool.QueryRow(ctx, `
		SELECT default_chat_agent_id::text FROM workspace WHERE id = $1
	`, testWorkspaceID).Scan(&originalDefaultAgent); err != nil {
		t.Fatalf("load original default chat agent: %v", err)
	}
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, 'OpenClaw Test Runtime', 'cloud', 'openclaw', 'online',
			'OpenClaw test runtime', '{}'::jsonb, now())
		RETURNING id
	`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("create openclaw runtime: %v", err)
	}

	var sessionID string
	var agentID string
	t.Cleanup(func() {
		if sessionID != "" {
			_, _ = testPool.Exec(ctx, `DELETE FROM chat_session WHERE id = $1`, sessionID)
		}
		if agentID != "" && (!originalDefaultAgent.Valid || agentID != originalDefaultAgent.String) {
			if originalDefaultAgent.Valid {
				_, _ = testPool.Exec(ctx, `UPDATE workspace SET default_chat_agent_id = $2 WHERE id = $1`, testWorkspaceID, originalDefaultAgent.String)
			} else {
				_, _ = testPool.Exec(ctx, `UPDATE workspace SET default_chat_agent_id = NULL WHERE id = $1 AND default_chat_agent_id = $2`, testWorkspaceID, agentID)
			}
			_, _ = testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID)
		}
		_, _ = testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	req := withURLParam(
		newRequest(http.MethodPost, "/api/channels/openclaw/conversations/conv-seed/dispatch", map[string]any{
			"target":       "assistant",
			"title":        "OpenClaw seed test",
			"instructions": "Carry this context into the assistant session.",
		}),
		"conversationId",
		"conv-seed",
	)
	req = withOpenClawTestWorkspaceCtx(t, req)
	w := httptest.NewRecorder()

	testHandler.DispatchOpenClawConversation(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("dispatch assistant: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp openClawDispatchResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID == nil || *resp.ID == "" {
		t.Fatalf("response did not include assistant session id: %+v", resp)
	}
	sessionID = *resp.ID
	if err := testPool.QueryRow(ctx, `SELECT agent_id::text FROM chat_session WHERE id = $1`, sessionID).Scan(&agentID); err != nil {
		t.Fatalf("load chat session agent: %v", err)
	}

	var content string
	if err := testPool.QueryRow(ctx, `
		SELECT content FROM chat_message
		WHERE chat_session_id = $1 AND role = 'user'
		ORDER BY created_at ASC
		LIMIT 1
	`, sessionID).Scan(&content); err != nil {
		t.Fatalf("load seeded chat message: %v", err)
	}
	for _, want := range []string{
		"<channel_context>",
		"provider: openclaw",
		"channel: lobster",
		"external_conversation_id: conv-seed",
		"Carry this context into the assistant session.",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("seeded assistant message missing %q:\n%s", want, content)
		}
	}
}

func withOpenClawTestWorkspaceCtx(t *testing.T, req *http.Request) *http.Request {
	t.Helper()

	memberRow, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      util.MustParseUUID(testUserID),
		WorkspaceID: util.MustParseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load test member row: %v", err)
	}

	return req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, memberRow))
}
