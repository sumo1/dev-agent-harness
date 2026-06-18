package handler

import (
	"context"
	"encoding/json"
	"testing"
)

// TestClaimAgentTask_GoalSubtasksRunInParallelOnSameAgent locks in the parallel
// execution fix: two INDEPENDENT goal_subtask tasks assigned to the SAME agent
// must both be claimable while the first is still running. Goal subtasks carry
// none of issue/chat/autopilot links (so they used to fall into the
// "quick-create-shape" serialization bucket and run one-at-a-time), but they are
// independent DAG nodes — the engine must execute them concurrently even when one
// coder owns several. The fix excludes goal_subtask_id-bearing rows from that
// bucket. This is the difference between a PMO's parallel fan-out actually
// running in parallel vs silently collapsing to sequential on a single coder.
func TestClaimAgentTask_GoalSubtasksRunInParallelOnSameAgent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Parallel subtask runtime")
	agentID, _ := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Parallel subtask agent")

	// A goal_run + two independent (depends_on:[]) subtasks on the same agent.
	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'parallel-subtask-squad', '', $2, $3) RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM squad WHERE id = $1`, squadID) })

	var goalRunID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO goal_run (workspace_id, squad_id, creator_id, title, goal, status)
		VALUES ($1, $2, $3, 'Parallel goal', 'fan out', 'executing') RETURNING id
	`, testWorkspaceID, squadID, testUserID).Scan(&goalRunID); err != nil {
		t.Fatalf("create goal_run: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM goal_run WHERE id = $1`, goalRunID) })

	mkSubtask := func(seq int, title string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO goal_subtask (goal_run_id, seq, title, spec, assignee_agent_id, status)
			VALUES ($1, $2, $3, 'do it', $4, 'running') RETURNING id
		`, goalRunID, seq, title, agentID).Scan(&id); err != nil {
			t.Fatalf("create subtask: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM goal_subtask WHERE id = $1`, id) })
		return id
	}
	subA := mkSubtask(1, "node A")
	subB := mkSubtask(2, "node B")

	// Enqueue a task for each subtask (issue/chat/autopilot all NULL, like the
	// real CreateGoalSubtaskTask query).
	mkTask := func(subtaskID string) {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, goal_subtask_id)
			VALUES ($1, $2, NULL, 'queued', 5, $3)
		`, agentID, runtimeID, subtaskID); err != nil {
			t.Fatalf("create subtask task: %v", err)
		}
	}
	mkTask(subA)
	mkTask(subB)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE goal_subtask_id IN ($1, $2)`, subA, subB)
	})

	agentUUID := parseUUID(agentID)

	// First claim succeeds.
	first, err := testHandler.Queries.ClaimAgentTask(ctx, agentUUID)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	// Second claim MUST also succeed while the first is 'dispatched' — this is
	// the whole point: independent subtasks on one agent run concurrently.
	second, err := testHandler.Queries.ClaimAgentTask(ctx, agentUUID)
	if err != nil {
		t.Fatalf("second claim must succeed (goal subtasks run in parallel on one agent), got error: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("second claim returned the same task as the first (%v) — both subtasks should be claimable", first.ID)
	}
}

// TestClaimAgentTask_QuickCreateStillSerializes is the guard rail for the fix
// above: excluding goal_subtask tasks from the quick-create-shape bucket must
// NOT widen the hole to real quick-create tasks. Two quick-create-shaped tasks
// (all source FKs NULL, NO goal_subtask_id) on the same agent must still
// serialize — claiming the first blocks the second — otherwise a user mashing
// the create button races over "most recent issue by this agent".
func TestClaimAgentTask_QuickCreateStillSerializes(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Quick-create serial runtime")
	agentID, _ := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Quick-create serial agent")

	ctxJSON, _ := json.Marshal(map[string]string{"quick_create_prompt": "make a thing"})
	mkQuickCreate := func() {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, context)
			VALUES ($1, $2, NULL, 'queued', 5, $3)
		`, agentID, runtimeID, ctxJSON); err != nil {
			t.Fatalf("create quick-create task: %v", err)
		}
	}
	mkQuickCreate()
	mkQuickCreate()
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID) })

	agentUUID := parseUUID(agentID)

	first, err := testHandler.Queries.ClaimAgentTask(ctx, agentUUID)
	if err != nil {
		t.Fatalf("first quick-create claim: %v", err)
	}
	if first.ID.Valid == false {
		t.Fatal("first quick-create claim returned no task")
	}
	// Second claim MUST be blocked while the first quick-create is dispatched.
	_, err = testHandler.Queries.ClaimAgentTask(ctx, agentUUID)
	if err == nil {
		t.Fatal("second quick-create claim must be blocked while the first is in flight (serialization), but it succeeded")
	}
}
