package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

// Autofix metadata lives under issue.metadata.autofix (a nested JSONB object).
// It tracks the goal_run(s) spawned to auto-fix an issue plus the GitHub issue
// they filed and any "needs more info" reason a verify agent reported back.
//
// Shape (mirrors packages/core/issues/autofix.ts autofixMetadataSchema):
//
//	issue.metadata.autofix = {
//	  "goal_run_ids":       ["<uuid>", ...],   // full history, append-only
//	  "latest_goal_run_id": "<uuid>",          // most recent, for jump-to
//	  "github":             { "issue_number": 1234, "issue_url": "..." },
//	  "needs_info_reason":  "<text>"           // set when verify reports no-repro
//	}
//
// The helpers below are PURE: they take the issue's decoded metadata map and
// return a NEW map with the autofix blob mutated. A single thin persist wrapper
// (persistAutofixMetadata) writes the resulting autofix object back via the
// existing Queries.SetIssueMetadataKey (key = "autofix"). This bypasses the
// HTTP handler's primitive-only value validation on purpose — the autofix value
// is a structured object, and the DB CHECK only requires the top-level metadata
// column to be an object ≤ 8KB, which a nested object satisfies.
const autofixMetadataKey = "autofix"

// AutofixGithub is the GitHub issue reference recorded by the "create GitHub
// issue" subtask.
type AutofixGithub struct {
	IssueNumber int    `json:"issue_number,omitempty"`
	IssueURL    string `json:"issue_url,omitempty"`
}

// AutofixMetadata is the decoded shape of issue.metadata.autofix.
type AutofixMetadata struct {
	GoalRunIDs      []string       `json:"goal_run_ids"`
	LatestGoalRunID string         `json:"latest_goal_run_id,omitempty"`
	Github          *AutofixGithub `json:"github,omitempty"`
	PRURL           string         `json:"pr_url,omitempty"`
	NeedsInfoReason string         `json:"needs_info_reason,omitempty"`
}

// readAutofixMetadata extracts the autofix blob from a decoded issue metadata
// map. A missing / malformed blob degrades to the empty value with a non-nil
// (empty) GoalRunIDs slice so callers never nil-check before appending.
func readAutofixMetadata(metadata map[string]any) AutofixMetadata {
	empty := AutofixMetadata{GoalRunIDs: []string{}}
	if metadata == nil {
		return empty
	}

	raw, ok := metadata[autofixMetadataKey]
	if !ok {
		return empty
	}

	// Round-trip through JSON: the map came from json.Unmarshal into any, so
	// the nested blob is itself a map[string]any. Re-encoding then decoding
	// into the typed struct is the simplest faithful conversion.
	buf, err := json.Marshal(raw)
	if err != nil {
		return empty
	}

	var out AutofixMetadata
	if err := json.Unmarshal(buf, &out); err != nil {
		return empty
	}
	if out.GoalRunIDs == nil {
		out.GoalRunIDs = []string{}
	}
	return out
}

// appendAutofixGoalRun appends goalRunID to the autofix history and sets it as
// the latest. Idempotent: a goalRunID already present is not appended twice,
// but it is always promoted to latest_goal_run_id.
func appendAutofixGoalRun(metadata map[string]any, goalRunID string) AutofixMetadata {
	autofix := readAutofixMetadata(metadata)

	present := false
	for _, id := range autofix.GoalRunIDs {
		if id == goalRunID {
			present = true
			break
		}
	}
	if !present {
		autofix.GoalRunIDs = append(autofix.GoalRunIDs, goalRunID)
	}
	autofix.LatestGoalRunID = goalRunID

	return autofix
}

// setAutofixGithub records the GitHub issue reference filed for this issue.
func setAutofixGithub(metadata map[string]any, number int, url string) AutofixMetadata {
	autofix := readAutofixMetadata(metadata)
	autofix.Github = &AutofixGithub{IssueNumber: number, IssueURL: url}
	return autofix
}

// setAutofixPR records the pull request URL opened by the "open PR" subtask. This
// is the artifact the frontend surfaces on the issue's "completed" state.
func setAutofixPR(metadata map[string]any, url string) AutofixMetadata {
	autofix := readAutofixMetadata(metadata)
	autofix.PRURL = url
	return autofix
}

// setAutofixNeedsInfo records the verify agent's "needs more info" reason. This
// is the signal the frontend pairs with a goal_run `partial` status to derive
// the "needs_info" product state (vs a true partial failure).
func setAutofixNeedsInfo(metadata map[string]any, reason string) AutofixMetadata {
	autofix := readAutofixMetadata(metadata)
	autofix.NeedsInfoReason = reason
	return autofix
}

// persistAutofixMetadata writes the autofix blob back to the issue via the
// existing single-key atomic SetIssueMetadataKey query (key = "autofix"). The
// workspace_id is the tenant/authorization gate; the caller must have already
// resolved the issue for the user.
func (s *GoalService) persistAutofixMetadata(
	ctx context.Context,
	issueID, workspaceID pgtype.UUID,
	autofix AutofixMetadata,
) (db.Issue, error) {
	value, err := json.Marshal(autofix)
	if err != nil {
		return db.Issue{}, err
	}

	return s.Queries.SetIssueMetadataKey(ctx, db.SetIssueMetadataKeyParams{
		ID:          issueID,
		WorkspaceID: workspaceID,
		Key:         autofixMetadataKey,
		Value:       value,
	})
}

// ShouldAutofixIssue is the cheap gate for the auto-fix flow. Two conditions,
// both required:
//
//   - the issue is bound to a project (a project carries the repo the agents
//     fix in), and
//   - the issue is assigned to an agent (or agent squad) — i.e. someone
//     deliberately routed it to automation.
//
// The assignee condition is what keeps auto-fix opt-in: a human creating an
// ordinary project issue (no agent assignee) does NOT silently spawn a fix
// goal_run. Only issues explicitly handed to an agent enter the flow. Whether
// the workspace has a plannable PMO is decided authoritatively by
// StartAutofixGoalRun (it resolves the planner and errors if none), so this
// stays a cheap structural check rather than duplicating planner resolution.
func (s *GoalService) ShouldAutofixIssue(issue db.Issue) bool {
	if !issue.ProjectID.Valid {
		return false
	}

	assignedToAgent := issue.AssigneeType.Valid &&
		(issue.AssigneeType.String == "agent" || issue.AssigneeType.String == "squad") &&
		issue.AssigneeID.Valid

	return assignedToAgent
}

// StartAutofixGoalRun spins up an auto-fix goal_run for a freshly created issue:
// it resolves the workspace PMO, builds a dynamic squad, creates a goal_run
// (bound to the issue's project) straight in 'planning', opens a discussion chat
// (so the jump-to-assistant entry can resolve a session), dispatches an autofix
// planning task to the PMO, and records the run id on issue.metadata.autofix.
//
// The PMO never executes here — it plans the fixed 4-node DAG (file GitHub issue
// → fix → verify → open PR) which the engine then dispatches. The backend calls
// no LLM and no GitHub API; all real work happens inside agents.
//
// Returns the created run. On any failure the caller treats it as "autofix did
// not start" — the issue is already created and must not be rolled back.
func (s *GoalService) StartAutofixGoalRun(
	ctx context.Context,
	issue db.Issue,
) (db.GoalRun, error) {
	if !issue.ProjectID.Valid {
		return db.GoalRun{}, fmt.Errorf("autofix requires a project-bound issue")
	}

	pmo, err := s.resolvePlannerAgent(ctx, issue.WorkspaceID)
	if err != nil {
		return db.GoalRun{}, fmt.Errorf("resolve planner: %w", err)
	}

	title := issue.Title
	goal := buildAutofixGoalText(issue)

	squadName := s.uniqueSquadName(ctx, issue.WorkspaceID, title+" 自动修复小队")
	squad, err := s.Queries.CreateSquad(ctx, db.CreateSquadParams{
		WorkspaceID: issue.WorkspaceID,
		Name:        squadName,
		Description: "Issue 自动修复动态小队（leader=PMO 规划层）",
		LeaderID:    pmo.ID,
		CreatorID:   issue.CreatorID,
	})
	if err != nil {
		return db.GoalRun{}, fmt.Errorf("create autofix squad: %w", err)
	}

	run, err := s.Queries.CreateGoalRun(ctx, db.CreateGoalRunParams{
		WorkspaceID: issue.WorkspaceID,
		SquadID:     squad.ID,
		CreatorID:   issue.CreatorID,
		Title:       title,
		Goal:        goal,
		Status:      pgtype.Text{String: "planning", Valid: true},
		ProjectID:   issue.ProjectID,
	})
	if err != nil {
		return db.GoalRun{}, fmt.Errorf("create autofix goal run: %w", err)
	}

	// Open the discussion chat so the issue → assistant jump-to entry
	// (latest_goal_run_id → GetGoalRunByChatSession) has a session to resolve.
	chat, cerr := s.Queries.CreateDiscussionChatSession(ctx, db.CreateDiscussionChatSessionParams{
		WorkspaceID: issue.WorkspaceID,
		AgentID:     pmo.ID,
		CreatorID:   issue.CreatorID,
		Title:       "自动修复：" + title,
		GoalRunID:   run.ID,
	})
	if cerr == nil {
		if updated, uerr := s.Queries.SetGoalRunChatSession(ctx, db.SetGoalRunChatSessionParams{
			ID:            run.ID,
			ChatSessionID: chat.ID,
		}); uerr == nil {
			run = updated
		}
	}

	s.broadcastGoalRun(ctx, run)

	if derr := s.dispatchPlanningTaskWithMode(ctx, run, squad, true); derr != nil {
		// Planning dispatch failed (leader offline/archived): mark the run failed
		// so the issue's autofix state reflects reality instead of stranding it in
		// 'planning'. The issue itself stays created.
		if _, ferr := s.Queries.CompleteGoalRun(ctx, db.CompleteGoalRunParams{
			ID:            run.ID,
			Status:        "failed",
			FailureReason: pgtype.Text{String: derr.Error(), Valid: true},
		}); ferr == nil {
			if failed, gerr := s.Queries.GetGoalRun(ctx, run.ID); gerr == nil {
				run = failed
				s.broadcastGoalRun(ctx, run)
			}
		}
		return run, fmt.Errorf("dispatch autofix planning: %w", derr)
	}

	slog.Info("autofix goal run started",
		"goal_run_id", util.UUIDToString(run.ID),
		"issue_id", util.UUIDToString(issue.ID),
		"project_id", util.UUIDToString(issue.ProjectID),
	)
	return run, nil
}

// LinkAutofixGoalRun records goalRunID on the issue's metadata.autofix history
// and promotes it to latest_goal_run_id. Called right after StartAutofixGoalRun
// so the frontend can derive the issue's autofix state and jump to the run.
func (s *GoalService) LinkAutofixGoalRun(
	ctx context.Context,
	issue db.Issue,
	goalRunID pgtype.UUID,
) error {
	metadata := decodeIssueMetadata(issue.Metadata)
	autofix := appendAutofixGoalRun(metadata, util.UUIDToString(goalRunID))
	_, err := s.persistAutofixMetadata(ctx, issue.ID, issue.WorkspaceID, autofix)
	return err
}

// resolveAutofixIssue finds the issue whose metadata.autofix.latest_goal_run_id
// points at goalRunID, scoped to workspaceID. Uses the existing metadata @>
// containment filter on ListIssues — no new query, no reverse FK. Returns
// (issue, true) on a unique-enough hit; (_, false) when no issue is linked
// (e.g. an ordinary task-mode goal, or the link was never written).
func (s *GoalService) resolveAutofixIssue(
	ctx context.Context,
	workspaceID, goalRunID pgtype.UUID,
) (db.Issue, bool) {
	filter, err := json.Marshal(map[string]any{
		autofixMetadataKey: map[string]any{
			"latest_goal_run_id": util.UUIDToString(goalRunID),
		},
	})
	if err != nil {
		return db.Issue{}, false
	}

	rows, err := s.Queries.ListIssues(ctx, db.ListIssuesParams{
		WorkspaceID:    workspaceID,
		Limit:          1,
		MetadataFilter: filter,
	})
	if err != nil || len(rows) == 0 {
		return db.Issue{}, false
	}

	// ListIssuesRow is a superset of Issue; re-read the full row by id so the
	// caller gets the canonical Issue shape (and current metadata) for the write.
	issue, err := s.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          rows[0].ID,
		WorkspaceID: workspaceID,
	})
	if err != nil || !issue.ID.Valid {
		return db.Issue{}, false
	}
	return issue, true
}

// ReportAutofixNeedsInfo records a verify agent's "could not reproduce / needs
// more info" report on the linked issue's metadata. The frontend pairs this with
// a goal_run `partial` status to render the "needs more info" product state. A
// no-op when the goal_run is not an autofix run (no linked issue).
func (s *GoalService) ReportAutofixNeedsInfo(
	ctx context.Context,
	workspaceID, goalRunID pgtype.UUID,
	reason string,
) {
	issue, ok := s.resolveAutofixIssue(ctx, workspaceID, goalRunID)
	if !ok {
		return
	}
	metadata := decodeIssueMetadata(issue.Metadata)
	autofix := setAutofixNeedsInfo(metadata, strings.TrimSpace(reason))
	if _, err := s.persistAutofixMetadata(ctx, issue.ID, issue.WorkspaceID, autofix); err != nil {
		slog.Error("autofix: persist needs-info", "error", err, "issue_id", util.UUIDToString(issue.ID))
	}
}

// ReportAutofixGithub records the GitHub issue reference (filed by the N1 node)
// on the linked issue's metadata. A no-op when the goal_run is not an autofix
// run (no linked issue).
func (s *GoalService) ReportAutofixGithub(
	ctx context.Context,
	workspaceID, goalRunID pgtype.UUID,
	number int,
	url string,
) {
	issue, ok := s.resolveAutofixIssue(ctx, workspaceID, goalRunID)
	if !ok {
		return
	}
	metadata := decodeIssueMetadata(issue.Metadata)
	autofix := setAutofixGithub(metadata, number, strings.TrimSpace(url))
	if _, err := s.persistAutofixMetadata(ctx, issue.ID, issue.WorkspaceID, autofix); err != nil {
		slog.Error("autofix: persist github ref", "error", err, "issue_id", util.UUIDToString(issue.ID))
	}
}

// ReportAutofixPR records the pull request URL (opened by the N4 node) on the
// linked issue's metadata. A no-op when the goal_run is not an autofix run (no
// linked issue).
func (s *GoalService) ReportAutofixPR(
	ctx context.Context,
	workspaceID, goalRunID pgtype.UUID,
	url string,
) {
	issue, ok := s.resolveAutofixIssue(ctx, workspaceID, goalRunID)
	if !ok {
		return
	}
	metadata := decodeIssueMetadata(issue.Metadata)
	autofix := setAutofixPR(metadata, strings.TrimSpace(url))
	if _, err := s.persistAutofixMetadata(ctx, issue.ID, issue.WorkspaceID, autofix); err != nil {
		slog.Error("autofix: persist pr url", "error", err, "issue_id", util.UUIDToString(issue.ID))
	}
}

// ReportAutofixSubtaskArtifact is the subtask-scoped entry used by the
// `multica goal report <subtask-id>` CLI: the N1 (file GitHub issue) and N4
// (open PR) nodes report the artifacts they produced. It resolves the subtask's
// goal_run (workspace-gated, like SubmitVerdict), then records whatever artifact
// fields were supplied onto the linked issue's metadata.autofix.
//
// Both artifacts are optional: N1 reports the github issue number+url, N4 reports
// the PR url. A report against a non-autofix run is silently a no-op (the
// underlying Report* helpers find no linked issue). Returns an error only on a
// bad subtask id / workspace mismatch so the agent gets a clear CLI failure.
func (s *GoalService) ReportAutofixSubtaskArtifact(
	ctx context.Context,
	workspaceID, subtaskID pgtype.UUID,
	githubIssueNumber int,
	githubIssueURL string,
	prURL string,
) error {
	st, err := s.Queries.GetGoalSubtask(ctx, subtaskID)
	if err != nil {
		return fmt.Errorf("load subtask: %w", err)
	}
	run, err := s.Queries.GetGoalRun(ctx, st.GoalRunID)
	if err != nil {
		return fmt.Errorf("load goal run: %w", err)
	}
	if run.WorkspaceID != workspaceID {
		return fmt.Errorf("subtask does not belong to workspace")
	}

	if n := strings.TrimSpace(githubIssueURL); n != "" || githubIssueNumber > 0 {
		s.ReportAutofixGithub(ctx, workspaceID, run.ID, githubIssueNumber, githubIssueURL)
	}
	if strings.TrimSpace(prURL) != "" {
		s.ReportAutofixPR(ctx, workspaceID, run.ID, prURL)
	}
	return nil
}

// decodeIssueMetadata decodes the issue.metadata JSONB column into a map for the
// pure autofix helpers. A missing / malformed blob degrades to an empty map.
func decodeIssueMetadata(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return map[string]any{}
	}
	return m
}

// buildAutofixGoalText assembles the goal text the PMO decomposes: the issue
// title + description. The PMO reads the project repo conventions itself; this
// only needs to convey what to fix.
func buildAutofixGoalText(issue db.Issue) string {
	var b strings.Builder
	b.WriteString(issue.Title)
	if issue.Description.Valid {
		if desc := strings.TrimSpace(issue.Description.String); desc != "" {
			b.WriteString("\n\n")
			b.WriteString(desc)
		}
	}
	return b.String()
}
