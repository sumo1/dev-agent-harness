package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// goalVerifyWorkflow builds a confirmed goal: execute A → verify V(reviews A) →
// execute B(after V). Returns the service, queries, and the three subtask ids.
func goalVerifyWorkflow(t *testing.T, ctx context.Context) (
	*service.GoalService, *service.TaskService, *db.Queries,
	db.GoalRun, pgtype.UUID, pgtype.UUID, pgtype.UUID,
) {
	t.Helper()
	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)
	goalSvc := service.NewGoalService(queries, testPool, bus, taskSvc)
	registerGoalListeners(bus, goalSvc)

	squadID, agentID := goalTestSquad(t, ctx, queries)

	run, subtasks, err := goalSvc.CreateGoal(
		ctx,
		parseUUID(testWorkspaceID), squadID, parseUUID(testUserID),
		pgtype.UUID{},
		"Verified goal", "Build with adversarial review",
		[]service.SubtaskSpec{
			{Seq: 1, Title: "Build", Spec: "build it", AssigneeAgentID: agentID},
			{Seq: 2, Title: "Review", Spec: "review the build", AssigneeAgentID: agentID, DependsOn: []int32{1}, Kind: "verify"},
			{Seq: 3, Title: "Ship", Spec: "ship it", AssigneeAgentID: agentID, DependsOn: []int32{2}},
		},
		true,
	)
	if err != nil {
		t.Fatalf("CreateGoal: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM goal_run WHERE id = $1`, run.ID)
	})

	var a, v, b pgtype.UUID
	for _, st := range subtasks {
		switch st.Seq {
		case 1:
			a = st.ID
		case 2:
			v = st.ID
		case 3:
			b = st.ID
		}
	}
	return goalSvc, taskSvc, queries, run, a, v, b
}

// TestGoalVerifyPassUnblocksDownstream: A completes → verify V dispatched → V
// passes → V completed → B (downstream of V) dispatches.
func TestGoalVerifyPassUnblocksDownstream(t *testing.T) {
	ctx := context.Background()
	goalSvc, taskSvc, queries, _, subA, subV, subB := goalVerifyWorkflow(t, ctx)

	// A is the only root → running. V and B pending.
	assertSubtaskStatus(t, ctx, queries, subA, "running")
	assertSubtaskStatus(t, ctx, queries, subV, "pending")
	assertSubtaskStatus(t, ctx, queries, subB, "pending")

	// Complete A → verify V unlocks + dispatches.
	taskA := drainGoalSubtaskTask(t, ctx, queries, subA)
	if _, err := taskSvc.CompleteTask(ctx, taskA.ID, []byte(`{"output":"built"}`), "", ""); err != nil {
		t.Fatalf("CompleteTask A: %v", err)
	}
	assertSubtaskStatus(t, ctx, queries, subA, "completed")
	assertSubtaskStatus(t, ctx, queries, subV, "running")
	assertSubtaskStatus(t, ctx, queries, subB, "pending")

	// Verifier reports pass (via the service, as the CLI would), then its task
	// completes → V finalized completed, B unlocks.
	if _, err := goalSvc.SubmitVerdict(ctx, parseUUID(testWorkspaceID), subV, "pass", ""); err != nil {
		t.Fatalf("SubmitVerdict pass: %v", err)
	}
	taskV := drainGoalSubtaskTask(t, ctx, queries, subV)
	if _, err := taskSvc.CompleteTask(ctx, taskV.ID, []byte(`{"verdict":"pass"}`), "", ""); err != nil {
		t.Fatalf("CompleteTask V: %v", err)
	}

	vRow, _ := queries.GetGoalSubtask(ctx, subV)
	if vRow.Status != "completed" || !vRow.Verdict.Valid || vRow.Verdict.String != "pass" {
		t.Fatalf("verify node: expected completed/pass, got %s/%v", vRow.Status, vRow.Verdict)
	}
	assertSubtaskStatus(t, ctx, queries, subB, "running")

	var ctxRawB []byte
	if err := testPool.QueryRow(ctx,
		`SELECT context FROM agent_task_queue WHERE goal_subtask_id=$1 ORDER BY created_at DESC LIMIT 1`, subB,
	).Scan(&ctxRawB); err != nil {
		t.Fatalf("load B task context: %v", err)
	}
	var ctxB service.GoalSubtaskContext
	if err := json.Unmarshal(ctxRawB, &ctxB); err != nil {
		t.Fatalf("unmarshal B context: %v", err)
	}
	if !strings.Contains(ctxB.UpstreamOutput, "built") {
		t.Fatalf("B's source material should include the reviewed producer output, got %q", ctxB.UpstreamOutput)
	}
	if !strings.Contains(ctxB.UpstreamOutput, "Verdict: pass") {
		t.Fatalf("B's source material should include the verifier verdict, got %q", ctxB.UpstreamOutput)
	}
}

// TestGoalVerifyRejectAsksCoordinator: A completes → V dispatched → V rejects →
// the engine does NOT auto-rerun (that抢跑'd reviewed nodes and mis-killed running
// work). Instead it dispatches a goal_decision task to the coordinator and parks
// the verify node at 'pending'. A stays completed (untouched), B stays pending.
func TestGoalVerifyRejectAsksCoordinator(t *testing.T) {
	ctx := context.Background()
	goalSvc, taskSvc, queries, _, subA, subV, subB := goalVerifyWorkflow(t, ctx)

	// Drive A → V running.
	taskA := drainGoalSubtaskTask(t, ctx, queries, subA)
	if _, err := taskSvc.CompleteTask(ctx, taskA.ID, []byte(`{"output":"v1"}`), "", ""); err != nil {
		t.Fatalf("CompleteTask A: %v", err)
	}
	assertSubtaskStatus(t, ctx, queries, subV, "running")

	aBefore, _ := queries.GetGoalSubtask(ctx, subA)

	// Verifier rejects → coordinator is asked (goal_decision dispatched); NO auto-rerun.
	if _, err := goalSvc.SubmitVerdict(ctx, parseUUID(testWorkspaceID), subV, "reject", "missing tests"); err != nil {
		t.Fatalf("SubmitVerdict reject: %v", err)
	}
	taskV := drainGoalSubtaskTask(t, ctx, queries, subV)
	if _, err := taskSvc.CompleteTask(ctx, taskV.ID, []byte(`{"verdict":"reject"}`), "", ""); err != nil {
		t.Fatalf("CompleteTask V: %v", err)
	}

	// The reviewed node A is UNTOUCHED (not auto-rerun, no attempt bump). This is
	// the key anti-抢跑 invariant: nothing re-runs until the coordinator decides.
	aAfter, _ := queries.GetGoalSubtask(ctx, subA)
	if aAfter.Status != "completed" {
		t.Fatalf("reviewed node A must stay completed until the coordinator decides, got %q", aAfter.Status)
	}
	if aAfter.Attempt != aBefore.Attempt {
		t.Fatalf("reviewed node A must NOT get an attempt bump before any decision (%d → %d)", aBefore.Attempt, aAfter.Attempt)
	}
	// The verify node is parked at pending — the dependency gate keeps it from
	// re-running until A is re-done and it is re-armed.
	assertSubtaskStatus(t, ctx, queries, subV, "pending")
	assertSubtaskStatus(t, ctx, queries, subB, "pending")

	// A goal_decision task for the verify node must be in flight for the coordinator.
	if _, err := queries.GetActiveDecisionTaskForSubtask(ctx, subV.String()); err != nil {
		t.Fatalf("a goal_decision task must be dispatched on reject, got none: %v", err)
	}
}

// TestGoalVerifyRejectRetryRerunsReviewed: after a reject, the coordinator
// decides `retry` → the reviewed node A re-runs (fresh attempt, with the
// reviewer's feedback as an incremental instruction), and the verify node stays
// parked at pending until A completes again (the dependency gate re-arms it).
func TestGoalVerifyRejectRetryRerunsReviewed(t *testing.T) {
	ctx := context.Background()
	goalSvc, taskSvc, queries, _, subA, subV, subB := goalVerifyWorkflow(t, ctx)

	taskA := drainGoalSubtaskTask(t, ctx, queries, subA)
	if _, err := taskSvc.CompleteTask(ctx, taskA.ID, []byte(`{"output":"v1"}`), "", ""); err != nil {
		t.Fatalf("CompleteTask A: %v", err)
	}
	if _, err := goalSvc.SubmitVerdict(ctx, parseUUID(testWorkspaceID), subV, "reject", "missing tests"); err != nil {
		t.Fatalf("SubmitVerdict reject: %v", err)
	}
	taskV := drainGoalSubtaskTask(t, ctx, queries, subV)
	if _, err := taskSvc.CompleteTask(ctx, taskV.ID, []byte(`{"verdict":"reject"}`), "", ""); err != nil {
		t.Fatalf("CompleteTask V: %v", err)
	}

	// Coordinator decides retry (judged node is the verify node V).
	if _, err := goalSvc.DecideSubtask(ctx, parseUUID(testWorkspaceID), subV, "retry", ""); err != nil {
		t.Fatalf("DecideSubtask retry: %v", err)
	}

	// A re-runs; V stays parked at pending (gate holds it until A completes again).
	assertSubtaskStatus(t, ctx, queries, subA, "running")
	assertSubtaskStatus(t, ctx, queries, subV, "pending")
	assertSubtaskStatus(t, ctx, queries, subB, "pending")

	// A's re-run task must carry the reviewer's feedback as an incremental instruction.
	var ctxRawA []byte
	if err := testPool.QueryRow(ctx,
		`SELECT context FROM agent_task_queue WHERE goal_subtask_id=$1 ORDER BY created_at DESC LIMIT 1`, subA,
	).Scan(&ctxRawA); err != nil {
		t.Fatalf("load A rerun task context: %v", err)
	}
	var ctxA service.GoalSubtaskContext
	if err := json.Unmarshal(ctxRawA, &ctxA); err != nil {
		t.Fatalf("unmarshal A context: %v", err)
	}
	if !strings.Contains(ctxA.RerunFeedback, "missing tests") {
		t.Fatalf("A's re-run must carry the reviewer's feedback, got %q", ctxA.RerunFeedback)
	}

	// Now A completes again → the dependency gate re-arms + redispatches V (it
	// must NOT have run while A was still running — the anti-抢跑 invariant).
	taskA2 := drainGoalSubtaskTask(t, ctx, queries, subA)
	if _, err := taskSvc.CompleteTask(ctx, taskA2.ID, []byte(`{"output":"v2 with tests"}`), "", ""); err != nil {
		t.Fatalf("CompleteTask A rerun: %v", err)
	}
	assertSubtaskStatus(t, ctx, queries, subA, "completed")
	assertSubtaskStatus(t, ctx, queries, subV, "running") // re-armed only AFTER A finished
}

// TestGoalDecideIsIdempotent locks the idempotency guard: a coordinator agent
// can call `multica goal decide` more than once in a single decision task. The
// SECOND call must be a no-op — the node already moved on, so re-enacting would
// re-dispatch / re-skip a resolved node (the live E2E hit exactly this: a double
// `proceed` re-dispatched an already-accepted verify node).
func TestGoalDecideIsIdempotent(t *testing.T) {
	ctx := context.Background()
	goalSvc, taskSvc, queries, _, subA, subV, subB := goalVerifyWorkflow(t, ctx)

	taskA := drainGoalSubtaskTask(t, ctx, queries, subA)
	if _, err := taskSvc.CompleteTask(ctx, taskA.ID, []byte(`{"output":"v1"}`), "", ""); err != nil {
		t.Fatalf("CompleteTask A: %v", err)
	}
	if _, err := goalSvc.SubmitVerdict(ctx, parseUUID(testWorkspaceID), subV, "reject", "needs work"); err != nil {
		t.Fatalf("SubmitVerdict reject: %v", err)
	}
	taskV := drainGoalSubtaskTask(t, ctx, queries, subV)
	if _, err := taskSvc.CompleteTask(ctx, taskV.ID, []byte(`{"verdict":"reject"}`), "", ""); err != nil {
		t.Fatalf("CompleteTask V: %v", err)
	}

	// First proceed: accept the reviewed work → verify completes as pass, B unblocks.
	if _, err := goalSvc.DecideSubtask(ctx, parseUUID(testWorkspaceID), subV, "proceed", ""); err != nil {
		t.Fatalf("DecideSubtask proceed (1st): %v", err)
	}
	vRow, _ := queries.GetGoalSubtask(ctx, subV)
	if vRow.Status != "completed" {
		t.Fatalf("after 1st proceed, verify should be completed, got %q", vRow.Status)
	}
	assertSubtaskStatus(t, ctx, queries, subB, "running")

	// Count verify tasks after the first decision.
	var vTasksBefore int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue WHERE goal_subtask_id = $1`, subV,
	).Scan(&vTasksBefore); err != nil {
		t.Fatalf("count verify tasks: %v", err)
	}

	// Second proceed on the SAME node: must be a no-op (node already resolved).
	if _, err := goalSvc.DecideSubtask(ctx, parseUUID(testWorkspaceID), subV, "proceed", ""); err != nil {
		t.Fatalf("DecideSubtask proceed (2nd): %v", err)
	}
	// Verify node must NOT have been re-dispatched or reverted.
	vRow2, _ := queries.GetGoalSubtask(ctx, subV)
	if vRow2.Status != "completed" {
		t.Fatalf("2nd proceed must be a no-op: verify status changed to %q", vRow2.Status)
	}
	var vTasksAfter int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue WHERE goal_subtask_id = $1`, subV,
	).Scan(&vTasksAfter); err != nil {
		t.Fatalf("count verify tasks after: %v", err)
	}
	if vTasksAfter != vTasksBefore {
		t.Fatalf("2nd proceed must NOT dispatch another verify task: %d → %d", vTasksBefore, vTasksAfter)
	}
}
