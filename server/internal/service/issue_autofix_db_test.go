package service

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

// autofixDBFixture is the minimal world an autofix goal_run needs: a workspace,
// a user (creator), a project, and (optionally) a live PMO agent the planner can
// resolve. Fixtures are inserted with raw SQL — the same approach the handler
// suite uses — so the test does not couple to a dozen Queries signatures.
type autofixDBFixture struct {
	pool        *pgxpool.Pool
	queries     *db.Queries
	svc         *GoalService
	workspaceID pgtype.UUID
	userID      pgtype.UUID
	projectID   pgtype.UUID
	// agentID is the live PMO agent id when the fixture was built withPMO; empty
	// otherwise. Issues are assigned to it to satisfy the autofix gate's
	// "assigned to an agent" condition.
	agentID string
}

func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	u, err := util.ParseUUID(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return u
}

// newAutofixDBFixture connects to the test database (skips when unreachable),
// builds the service, and inserts a workspace + user + project. withPMO controls
// whether a live planner agent exists — the autofix gate's authority.
func newAutofixDBFixture(t *testing.T, withPMO bool) *autofixDBFixture {
	t.Helper()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://sumo@localhost:5432/multica?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("database not available: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database not reachable: %v", err)
	}
	t.Cleanup(pool.Close)

	queries := db.New(pool)
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	taskSvc := NewTaskService(queries, pool, hub, bus)
	svc := NewGoalService(queries, pool, bus, taskSvc)

	var userID, workspaceID, projectID string

	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Autofix Test', 'autofix-test-' || gen_random_uuid() || '@multica.ai')
		RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID) })

	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, issue_prefix)
		VALUES ('Autofix Test WS', 'autofix-' || substr(gen_random_uuid()::text,1,8), 'AFX')
		RETURNING id`).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	// Deleting the workspace cascades to project / goal_run / squad / issue.
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID) })

	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`,
		workspaceID, userID); err != nil {
		t.Fatalf("insert member: %v", err)
	}

	if err := pool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, 'Autofix Project')
		RETURNING id`, workspaceID).Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	var agentID string
	if withPMO {
		var runtimeID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
			VALUES ($1, 'Autofix Runtime', 'cloud', 'autofix_runtime', 'online', 'autofix', '{}'::jsonb, now())
			RETURNING id`, workspaceID).Scan(&runtimeID); err != nil {
			t.Fatalf("insert runtime: %v", err)
		}
		if err := pool.QueryRow(ctx, `
			INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id)
			VALUES ($1, 'Autofix PMO', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3)
			RETURNING id`,
			workspaceID, runtimeID, userID).Scan(&agentID); err != nil {
			t.Fatalf("insert pmo agent: %v", err)
		}
	}

	return &autofixDBFixture{
		pool:        pool,
		queries:     queries,
		svc:         svc,
		workspaceID: mustUUID(t, workspaceID),
		userID:      mustUUID(t, userID),
		projectID:   mustUUID(t, projectID),
		agentID:     agentID,
	}
}

// insertIssue inserts a minimal issue via raw SQL and returns the loaded db.Issue.
// projectBound=false leaves project_id NULL (the "no project" path).
// agentAssigned=true assigns the issue to the fixture's PMO agent (assignee_type
// 'agent'), which is the second half of the autofix gate.
func (f *autofixDBFixture) insertIssue(t *testing.T, projectBound, agentAssigned bool) db.Issue {
	t.Helper()
	ctx := context.Background()

	var projectArg any
	if projectBound {
		projectArg = uuidToStringForTest(f.projectID)
	}

	var assigneeType, assigneeID any
	if agentAssigned {
		if f.agentID == "" {
			t.Fatal("insertIssue(agentAssigned=true) requires a withPMO fixture")
		}
		assigneeType = "agent"
		assigneeID = f.agentID
	}

	// Per-workspace issue.number must be unique; derive the next one rather than
	// hardcoding 1 (a test may insert several issues into the same workspace).
	var nextNumber int
	if err := f.pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1`,
		uuidToStringForTest(f.workspaceID),
	).Scan(&nextNumber); err != nil {
		t.Fatalf("compute next issue number: %v", err)
	}

	var issueID string
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, description, status, priority, creator_type, creator_id, position, number, project_id, assignee_type, assignee_id, metadata)
		VALUES ($1, 'Login button does nothing', 'Clicking login is a no-op', 'todo', 'none', 'member', $2, 0, $3, $4, $5, $6, '{}'::jsonb)
		RETURNING id`,
		uuidToStringForTest(f.workspaceID), uuidToStringForTest(f.userID), nextNumber, projectArg, assigneeType, assigneeID,
	).Scan(&issueID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}

	issue, err := f.queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          mustUUID(t, issueID),
		WorkspaceID: f.workspaceID,
	})
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	return issue
}

func uuidToStringForTest(u pgtype.UUID) string {
	return util.UUIDToString(u)
}

// insertUnplannableAgent inserts an ARCHIVED agent (with a runtime, since
// agent.runtime_id is NOT NULL). It can be an issue assignee — the autofix
// gate's "assigned to an agent" check only inspects assignee_type/id — but
// resolvePlannerAgent skips archived agents, so it is not a plannable PMO.
// Returns the agent id.
func (f *autofixDBFixture) insertUnplannableAgent(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	var runtimeID string
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
		VALUES ($1, 'Archived Assignee Runtime', 'cloud', 'autofix_runtime', 'online', 'autofix', '{}'::jsonb, now())
		RETURNING id`, uuidToStringForTest(f.workspaceID)).Scan(&runtimeID); err != nil {
		t.Fatalf("insert runtime: %v", err)
	}
	var agentID string
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id, archived_at)
		VALUES ($1, 'Archived Assignee', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3, now())
		RETURNING id`,
		uuidToStringForTest(f.workspaceID), runtimeID, uuidToStringForTest(f.userID)).Scan(&agentID); err != nil {
		t.Fatalf("insert archived agent: %v", err)
	}
	return agentID
}

// TestStartAutofixGoalRun_WithProjectAndPMO is the happy path: a project-bound
// issue in a workspace with a live PMO produces a goal_run bound to the project,
// and LinkAutofixGoalRun writes latest_goal_run_id back to issue.metadata.autofix.
func TestStartAutofixGoalRun_WithProjectAndPMO(t *testing.T) {
	f := newAutofixDBFixture(t, true)
	ctx := context.Background()

	issue := f.insertIssue(t, true, true)
	if !f.svc.ShouldAutofixIssue(issue) {
		t.Fatal("project-bound, agent-assigned issue should pass the autofix gate")
	}

	// An otherwise-identical issue with no agent assignee must NOT pass — autofix
	// stays opt-in to issues deliberately routed to automation.
	unassigned := f.insertIssue(t, true, false)
	if f.svc.ShouldAutofixIssue(unassigned) {
		t.Fatal("project-bound but unassigned issue must NOT pass the autofix gate")
	}

	run, err := f.svc.StartAutofixGoalRun(ctx, issue)
	if err != nil {
		t.Fatalf("StartAutofixGoalRun: %v", err)
	}

	// goal_run row exists, bound to the issue's project.
	loaded, err := f.queries.GetGoalRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("load goal run: %v", err)
	}
	if !loaded.ProjectID.Valid || loaded.ProjectID != f.projectID {
		t.Fatalf("goal run not bound to project: %+v", loaded.ProjectID)
	}
	if loaded.WorkspaceID != f.workspaceID {
		t.Fatal("goal run workspace mismatch")
	}

	// Link writes latest_goal_run_id back to issue metadata.
	if err := f.svc.LinkAutofixGoalRun(ctx, issue, run.ID); err != nil {
		t.Fatalf("LinkAutofixGoalRun: %v", err)
	}

	reloaded, err := f.queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          issue.ID,
		WorkspaceID: f.workspaceID,
	})
	if err != nil {
		t.Fatalf("reload issue: %v", err)
	}
	autofix := readAutofixMetadata(decodeIssueMetadata(reloaded.Metadata))
	if autofix.LatestGoalRunID != util.UUIDToString(run.ID) {
		t.Fatalf("latest_goal_run_id = %q, want %q", autofix.LatestGoalRunID, util.UUIDToString(run.ID))
	}
	if len(autofix.GoalRunIDs) != 1 || autofix.GoalRunIDs[0] != util.UUIDToString(run.ID) {
		t.Fatalf("goal_run_ids = %v, want [%s]", autofix.GoalRunIDs, util.UUIDToString(run.ID))
	}
}

// TestStartAutofixGoalRun_NoProject: an issue without a project never enters the
// autofix flow — the gate rejects it. (The handler skips silently.)
func TestStartAutofixGoalRun_NoProject(t *testing.T) {
	f := newAutofixDBFixture(t, true)
	issue := f.insertIssue(t, false, true)

	if f.svc.ShouldAutofixIssue(issue) {
		t.Fatal("issue without a project must NOT pass the autofix gate")
	}

	// And StartAutofixGoalRun refuses (defensive — the handler gates first).
	if _, err := f.svc.StartAutofixGoalRun(context.Background(), issue); err == nil {
		t.Fatal("StartAutofixGoalRun should error for a project-less issue")
	}
}

// TestStartAutofixGoalRun_NoPMO: a project-bound issue in a workspace with no
// live planner agent returns an error (no goal_run started) — the handler treats
// this as "autofix did not start" and does NOT surface an error to the user.
func TestStartAutofixGoalRun_NoPMO(t *testing.T) {
	f := newAutofixDBFixture(t, false)
	// Give the issue an agent assignee that has NO runtime — it satisfies the
	// gate's "assigned to an agent" condition but resolvePlannerAgent skips it
	// (no runtime), so no plannable PMO exists.
	f.agentID = f.insertUnplannableAgent(t)
	issue := f.insertIssue(t, true, true)

	// The cheap gate still passes (project bound + agent assigned); whether a
	// *plannable* PMO exists is decided authoritatively by StartAutofixGoalRun.
	if !f.svc.ShouldAutofixIssue(issue) {
		t.Fatal("project-bound, agent-assigned issue should pass the cheap gate even without a plannable PMO")
	}

	if _, err := f.svc.StartAutofixGoalRun(context.Background(), issue); err == nil {
		t.Fatal("StartAutofixGoalRun should error when no planner agent exists")
	}

	// No goal_run rows for this workspace.
	runs, err := f.queries.ListGoalRunsForWorkspace(context.Background(), db.ListGoalRunsForWorkspaceParams{
		WorkspaceID: f.workspaceID,
		Limit:       100,
	})
	if err != nil {
		t.Fatalf("list goal runs: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("expected no goal runs, got %d", len(runs))
	}
}

// TestReportAutofix_RoundTrip exercises the report-back resolver: after linking a
// run to an issue, ReportAutofixGithub / ReportAutofixNeedsInfo find the issue by
// its metadata.autofix.latest_goal_run_id and write the fields back.
func TestReportAutofix_RoundTrip(t *testing.T) {
	f := newAutofixDBFixture(t, true)
	ctx := context.Background()

	issue := f.insertIssue(t, true, true)
	run, err := f.svc.StartAutofixGoalRun(ctx, issue)
	if err != nil {
		t.Fatalf("StartAutofixGoalRun: %v", err)
	}
	if err := f.svc.LinkAutofixGoalRun(ctx, issue, run.ID); err != nil {
		t.Fatalf("LinkAutofixGoalRun: %v", err)
	}

	f.svc.ReportAutofixGithub(ctx, f.workspaceID, run.ID, 4242, "https://github.com/o/r/issues/4242")
	f.svc.ReportAutofixNeedsInfo(ctx, f.workspaceID, run.ID, "could not reproduce on main")

	reloaded, err := f.queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          issue.ID,
		WorkspaceID: f.workspaceID,
	})
	if err != nil {
		t.Fatalf("reload issue: %v", err)
	}
	autofix := readAutofixMetadata(decodeIssueMetadata(reloaded.Metadata))

	if autofix.Github == nil || autofix.Github.IssueNumber != 4242 ||
		autofix.Github.IssueURL != "https://github.com/o/r/issues/4242" {
		t.Fatalf("github not recorded: %+v", autofix.Github)
	}
	if autofix.NeedsInfoReason != "could not reproduce on main" {
		t.Fatalf("needs_info_reason = %q", autofix.NeedsInfoReason)
	}
	// The history must survive the writes.
	if autofix.LatestGoalRunID != util.UUIDToString(run.ID) {
		t.Fatalf("report-back clobbered latest_goal_run_id: %q", autofix.LatestGoalRunID)
	}

	// A run id with no linked issue is a silent no-op (must not panic / error).
	f.svc.ReportAutofixNeedsInfo(ctx, f.workspaceID, mustUUID(t, "00000000-0000-0000-0000-000000000001"), "noop")
}

// TestReportAutofixSubtaskArtifact_RoundTrip exercises the subtask-scoped report
// channel (`multica goal report <subtask-id>`): the N1/N4 nodes report against
// their OWN subtask id, and the service resolves the goal_run from the subtask
// before writing the github issue ref / PR url onto the linked issue.
func TestReportAutofixSubtaskArtifact_RoundTrip(t *testing.T) {
	f := newAutofixDBFixture(t, true)
	ctx := context.Background()

	issue := f.insertIssue(t, true, true)
	run, err := f.svc.StartAutofixGoalRun(ctx, issue)
	if err != nil {
		t.Fatalf("StartAutofixGoalRun: %v", err)
	}
	if err := f.svc.LinkAutofixGoalRun(ctx, issue, run.ID); err != nil {
		t.Fatalf("LinkAutofixGoalRun: %v", err)
	}

	// Seed a subtask under the run (the N1/N4 node the agent reports against).
	st, err := f.queries.CreateGoalSubtask(ctx, db.CreateGoalSubtaskParams{
		GoalRunID:       run.ID,
		Seq:             1,
		Title:           "File the GitHub issue",
		Spec:            "Open a GitHub issue and report its number+url",
		AssigneeAgentID: mustUUID(t, f.agentID),
		DependsOn:       nil,
		Status:          "running",
		MaxAttempts:     3,
		Kind:            "execute",
	})
	if err != nil {
		t.Fatalf("create subtask: %v", err)
	}

	// N1 reports the filed issue, N4 reports the PR — both against the subtask id.
	if err := f.svc.ReportAutofixSubtaskArtifact(ctx, f.workspaceID, st.ID, 77, "https://github.com/o/r/issues/77", ""); err != nil {
		t.Fatalf("report github artifact: %v", err)
	}
	if err := f.svc.ReportAutofixSubtaskArtifact(ctx, f.workspaceID, st.ID, 0, "", "https://github.com/o/r/pull/78"); err != nil {
		t.Fatalf("report pr artifact: %v", err)
	}

	reloaded, err := f.queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          issue.ID,
		WorkspaceID: f.workspaceID,
	})
	if err != nil {
		t.Fatalf("reload issue: %v", err)
	}
	autofix := readAutofixMetadata(decodeIssueMetadata(reloaded.Metadata))

	if autofix.Github == nil || autofix.Github.IssueNumber != 77 ||
		autofix.Github.IssueURL != "https://github.com/o/r/issues/77" {
		t.Fatalf("github not recorded via subtask report: %+v", autofix.Github)
	}
	if autofix.PRURL != "https://github.com/o/r/pull/78" {
		t.Fatalf("pr_url not recorded via subtask report: %q", autofix.PRURL)
	}

	// A workspace mismatch must error (authorization gate), not silently write.
	otherWS := mustUUID(t, "00000000-0000-0000-0000-0000000000ff")
	if err := f.svc.ReportAutofixSubtaskArtifact(ctx, otherWS, st.ID, 0, "", "https://x/pull/9"); err == nil {
		t.Fatal("expected workspace-mismatch report to error")
	}
}

// Compile-time guard: the report-back JSON filter shape matches what the writers
// persist (a regression here would silently break resolveAutofixIssue).
func TestAutofixMetadataFilterShape(t *testing.T) {
	t.Parallel()
	buf, err := json.Marshal(map[string]any{
		autofixMetadataKey: map[string]any{"latest_goal_run_id": "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(buf); got != `{"autofix":{"latest_goal_run_id":"x"}}` {
		t.Fatalf("filter shape drifted: %s", got)
	}
}
