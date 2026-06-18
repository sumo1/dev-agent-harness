package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// GoalService orchestrates a "goal" — a PMO-led, multi-role unit of work that
// decomposes into a DAG of subtasks. It owns the goal_run / goal_subtask
// lifecycle and dispatches ready subtasks by reusing the existing task queue
// (mirroring how AutopilotService dispatches via agent_task_queue).
//
// PMO = squad.leader_id; executing roles = squad members (agents). The service
// is a state machine + scheduler, not an agent: "who plans" and "who executes"
// are ordinary agents reached through the task path.
type GoalService struct {
	Queries   *db.Queries
	TxStarter TxStarter
	Bus       *events.Bus
	TaskSvc   *TaskService
}

func NewGoalService(q *db.Queries, tx TxStarter, bus *events.Bus, taskSvc *TaskService) *GoalService {
	return &GoalService{Queries: q, TxStarter: tx, Bus: bus, TaskSvc: taskSvc}
}

// GoalSubtaskContextType marks a task as a goal-subtask execution job. The
// daemon claim handler detects this in the task's context JSONB and routes to
// the free-prompt builder (same shape as quick-create: no issue to fetch, the
// spec is the prompt).
const GoalSubtaskContextType = "goal_subtask"

// GoalSubtaskContext is stored in agent_task_queue.context for a dispatched
// subtask. It carries everything the daemon needs to build the prompt without
// an issue: the goal headline (so the role knows the bigger picture) and the
// subtask spec (what THIS role must achieve).
type GoalSubtaskContext struct {
	Type          string `json:"type"`
	GoalRunID     string `json:"goal_run_id"`
	GoalSubtaskID string `json:"goal_subtask_id"`
	WorkspaceID   string `json:"workspace_id"`
	GoalTitle     string `json:"goal_title"`
	SubtaskTitle  string `json:"subtask_title"`
	Spec          string `json:"spec"`
	// Kind is 'execute' (default) or 'verify'. Verify nodes adversarially
	// review the upstream output below and report a pass/reject verdict.
	Kind string `json:"kind,omitempty"`
	// ReviewTarget carries the work product the verify node must judge: the
	// reviewed node's title + result. Empty for execute nodes.
	ReviewTarget string `json:"review_target,omitempty"`
	// UpstreamOutput carries the source material selected for this execute node,
	// so it receives prior work products directly instead of re-deriving them.
	// The JSON name is kept for compatibility; prompts present it as task input,
	// not as workflow topology. Empty for root nodes and for verify nodes.
	UpstreamOutput string `json:"upstream_output,omitempty"`
	// HandoffBrief frames UpstreamOutput for the executing role: what this task
	// should do with the injected source material, without relying on a temporary
	// document handoff.
	HandoffBrief string `json:"handoff_brief,omitempty"`
	// ProjectID is the bound project (when any). The executing agent runs inside
	// its repo (claim surfaces ProjectResources) so it can read the project's
	// existing contracts/conventions and align its output. Empty when unbound.
	ProjectID string `json:"project_id,omitempty"`
	// Autofix marks this subtask as part of an issue auto-fix run, so the daemon
	// surfaces the `multica goal report` artifact channel (filed GitHub issue /
	// opened PR) to the execute prompt. False for ordinary task-mode subtasks.
	Autofix bool `json:"autofix,omitempty"`
	// RerunFeedback is the incremental instruction for a re-run: when a
	// coordinator retries/reshapes a node after a reviewer rejected it, this
	// carries the reviewer's reason so the (session-resumed) agent fixes the
	// specific points instead of starting over. Empty on first run.
	RerunFeedback string `json:"rerun_feedback,omitempty"`
}

// GoalPlanningContextType marks a task as a goal-planning job: the squad leader
// (PMO) decomposes the goal into subtasks and submits the plan via the CLI.
const GoalPlanningContextType = "goal_planning"

// GoalPlanningContext is stored in agent_task_queue.context for a planning task.
// The daemon claim handler injects the squad roster so the planner knows which
// role agents are available; this context carries the goal text + run id.
type GoalPlanningContext struct {
	Type        string `json:"type"`
	GoalRunID   string `json:"goal_run_id"`
	WorkspaceID string `json:"workspace_id"`
	SquadID     string `json:"squad_id"`
	GoalTitle   string `json:"goal_title"`
	Goal        string `json:"goal"`
	// ProjectID is the bound project (when any). The planner agent runs inside
	// its repo (claim surfaces ProjectResources) so it can read the project's
	// existing docs/task/*/plan contracts and align to that project's dialect
	// when writing each node's spec. Empty when the task is not bound.
	ProjectID string `json:"project_id,omitempty"`
	// Autofix marks this planning task as an issue auto-fix run. The daemon
	// surfaces it to the planning prompt builder so the PMO is steered to plan
	// the fixed 4-node DAG (file GitHub issue → fix → verify → open PR) instead
	// of a free-form decomposition. Empty/false for ordinary task-mode goals.
	Autofix bool `json:"autofix,omitempty"`
}

// GoalSummaryContextType marks a task as a goal-summary job: once every subtask
// is terminal, the PMO (squad leader) reads all subtask outputs and writes the
// final deliverable. This is the PMO's "收口/汇总" step — the planning task only
// plans + dispatches, so without this the main-session stream stops at "node
// running" and the user never sees a final result.
const GoalSummaryContextType = "goal_summary"

// GoalSummaryContext is stored in agent_task_queue.context for a summary task.
// SubtaskDigest is the assembled outputs of all subtasks (built server-side,
// like buildReviewTarget) so the PMO can synthesize without DB access.
type GoalSummaryContext struct {
	Type          string `json:"type"`
	GoalRunID     string `json:"goal_run_id"`
	WorkspaceID   string `json:"workspace_id"`
	GoalTitle     string `json:"goal_title"`
	Goal          string `json:"goal"`
	Outcome       string `json:"outcome"` // completed / partial / failed
	SubtaskDigest string `json:"subtask_digest"`
}

// GoalPersistContextType marks a task as a repo-persist (one-click snapshot)
// job: the squad leader (总控) writes the task's content into the bound project
// repo following the dev-roleplay-harness structure (docs/task/{slug}/progress.md
// + plan/step-*.md dual contracts + memory/). This is the "持久化到工程" entry —
// the platform DB stays the main truth; the repo gets an on-demand snapshot so
// any tool opening the repo can pick the task up. See design-repo-ssot-task-env.md.
const GoalPersistContextType = "goal_persist"

// GoalPersistContext is stored in agent_task_queue.context for a persist task.
// The slug + digest are built server-side so the leader can author the harness
// files without a DB round-trip. The agent runs inside the project's
// local_directory (surfaced via ProjectResources at claim time) and writes there.
type GoalPersistContext struct {
	Type        string `json:"type"`
	GoalRunID   string `json:"goal_run_id"`
	WorkspaceID string `json:"workspace_id"`
	ProjectID   string `json:"project_id"`
	GoalTitle   string `json:"goal_title"`
	Goal        string `json:"goal"`
	// Slug is the docs/task/{slug} directory name ({YYMMDD}-{kebab-title}),
	// computed server-side so re-runs target the SAME directory (snapshot
	// overwrite, not a new dir each click).
	Slug string `json:"slug"`
	// SubtaskDigest is every subtask's title + spec + result + dual-contract
	// material, assembled like buildSubtaskDigest, so the leader can author
	// progress.md + plan/step-*.md from it.
	SubtaskDigest string `json:"subtask_digest"`
	// Outcome is the goal_run's current status snapshot (discussion / planning /
	// executing / completed / partial / failed) at persist time.
	Outcome string `json:"outcome"`
}

// GoalDecisionContextType marks a task as a 总控 "下一步判断" (next-step judgment)
// job. When a subtask reaches a non-trivial terminal state (failure with
// dependents), the engine does NOT blindly cascade-block downstream — it asks
// the coordinator to look at what the failed node produced and decide how to
// proceed. This is the Claude-Code-style judgment edge: the coordinator passes a
// DECISION, not data, and not via a memory blackboard. See
// design-repo-ssot-task-env.md §三④.
const GoalDecisionContextType = "goal_decision"

// GoalDecisionContext is stored in agent_task_queue.context for a decision task.
// It carries the failed node's title/spec/failure + downstream titles so the
// coordinator can judge without DB access, and the CLI write-back contract
// (`multica goal decide <subtask> proceed|reshape|abort`).
type GoalDecisionContext struct {
	Type          string `json:"type"`
	GoalRunID     string `json:"goal_run_id"`
	GoalSubtaskID string `json:"goal_subtask_id"` // the node being judged
	WorkspaceID   string `json:"workspace_id"`
	GoalTitle     string `json:"goal_title"`
	SubtaskTitle  string `json:"subtask_title"`
	SubtaskSpec   string `json:"subtask_spec"`
	FailureReason string `json:"failure_reason"`
	// Downstream is a human-readable list of the dependents that are blocked
	// behind this node, so the coordinator understands the blast radius.
	Downstream string `json:"downstream"`
	// Trigger distinguishes WHY this judgment is needed: "failure" (an execute
	// node failed) or "reject" (a verify node rejected the work it reviewed).
	// The prompt steers the coordinator differently for each.
	Trigger string `json:"trigger,omitempty"`
	// RejectReason carries the verifier's stated reason when Trigger=="reject".
	RejectReason string `json:"reject_reason,omitempty"`
	// Attempts is how many times the judged node has already been attempted, so
	// the coordinator can weigh "try once more" against "this is fundamentally
	// stuck" — instead of a hardcoded attempt-budget rule doing it blindly.
	Attempts int32 `json:"attempts,omitempty"`
	// DagSnapshot is a human-readable global view of EVERY node's current state
	// (title / kind / status). This is the key to LLM-driven orchestration: the
	// coordinator can see what is still running, what already completed, and what
	// is waiting — so it never decides as if the failed node were the whole world.
	DagSnapshot string `json:"dag_snapshot,omitempty"`
}

// ---------------------------------------------------------------------------
// Task mode (design-task-mode.md): PMO planning layer + dynamic squad.
//
// Flow: CreateTask (discussion phase) — resolves the PMO (workspace default
// planner agent, or fallback), dynamically creates an "XXX 目标小队" squad
// (leader=PMO, members=selected agents), creates the goal_run on it in
// 'discussion' status, and opens a discussion chat session bound to the PMO.
// The user converses with the PMO to form the goal, then ConfirmTask dispatches
// planning (existing StartPlanning logic). Members can be added during
// discussion (PMO suggests missing roles → AddTaskMember).
// ---------------------------------------------------------------------------

// resolvePlannerAgent returns the workspace's PMO: its configured default
// planner agent, or — when unset/invalid — the first non-archived agent in the
// workspace. Returns an error only when the workspace has no usable agent.
func (s *GoalService) resolvePlannerAgent(ctx context.Context, workspaceID pgtype.UUID) (db.Agent, error) {
	ws, err := s.Queries.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return db.Agent{}, fmt.Errorf("load workspace: %w", err)
	}
	if ws.DefaultPlannerAgentID.Valid {
		if agent, err := s.Queries.GetAgent(ctx, ws.DefaultPlannerAgentID); err == nil &&
			agent.WorkspaceID == workspaceID && !agent.ArchivedAt.Valid {
			return agent, nil
		}
		// Configured planner is gone/archived/foreign → fall through to fallback.
	}
	agents, err := s.Queries.ListAgents(ctx, workspaceID)
	if err != nil {
		return db.Agent{}, fmt.Errorf("list agents: %w", err)
	}
	for _, a := range agents {
		if !a.ArchivedAt.Valid && a.RuntimeID.Valid {
			return a, nil
		}
	}
	return db.Agent{}, fmt.Errorf("workspace has no usable agent to act as planner")
}

// CreateTask starts a task-mode goal in the discussion phase. It resolves the
// PMO, builds a dynamic squad (leader=PMO + selected member agents), creates
// the goal_run in 'discussion', and opens a discussion chat with the PMO.
// memberAgentIDs may be empty (PMO will pick from the workspace during
// planning, or the user adds members during discussion).
func (s *GoalService) CreateTask(
	ctx context.Context,
	workspaceID, creatorID pgtype.UUID,
	title, goal string,
	memberAgentIDs []pgtype.UUID,
	projectID pgtype.UUID,
) (db.GoalRun, db.ChatSession, error) {
	pmo, err := s.resolvePlannerAgent(ctx, workspaceID)
	if err != nil {
		return db.GoalRun{}, db.ChatSession{}, err
	}

	squadName := title
	if squadName == "" {
		squadName = "目标"
	}
	squadName = squadName + " 目标小队"
	squad, err := s.Queries.CreateSquad(ctx, db.CreateSquadParams{
		WorkspaceID: workspaceID,
		Name:        s.uniqueSquadName(ctx, workspaceID, squadName),
		Description: "任务模式动态小队（leader=PMO 规划层）",
		LeaderID:    pmo.ID,
		CreatorID:   creatorID,
	})
	if err != nil {
		return db.GoalRun{}, db.ChatSession{}, fmt.Errorf("create dynamic squad: %w", err)
	}
	// Add selected members (the leader is auto-added by CreateSquad).
	for _, mid := range memberAgentIDs {
		if mid == pmo.ID {
			continue
		}
		_, _ = s.Queries.AddSquadMember(ctx, db.AddSquadMemberParams{
			SquadID:    squad.ID,
			MemberType: "agent",
			MemberID:   mid,
			Role:       "",
		})
	}

	run, err := s.Queries.CreateGoalRun(ctx, db.CreateGoalRunParams{
		WorkspaceID: workspaceID,
		SquadID:     squad.ID,
		CreatorID:   creatorID,
		Title:       title,
		Goal:        goal,
		Status:      pgtype.Text{String: "discussion", Valid: true},
		ProjectID:   projectID,
	})
	if err != nil {
		return db.GoalRun{}, db.ChatSession{}, fmt.Errorf("create goal run: %w", err)
	}

	// Open the discussion chat with the PMO.
	discTitle := "讨论：" + title
	if title == "" {
		discTitle = "任务讨论"
	}
	chat, err := s.Queries.CreateDiscussionChatSession(ctx, db.CreateDiscussionChatSessionParams{
		WorkspaceID: workspaceID,
		AgentID:     pmo.ID,
		CreatorID:   creatorID,
		Title:       discTitle,
		GoalRunID:   run.ID,
	})
	if err != nil {
		return db.GoalRun{}, db.ChatSession{}, fmt.Errorf("create discussion chat: %w", err)
	}
	// Link the goal back to its discussion chat.
	if updated, uerr := s.Queries.SetGoalRunChatSession(ctx, db.SetGoalRunChatSessionParams{
		ID:            run.ID,
		ChatSessionID: chat.ID,
	}); uerr == nil {
		run = updated
	}

	slog.Info("task created (discussion phase)",
		"goal_run_id", util.UUIDToString(run.ID),
		"squad_id", util.UUIDToString(squad.ID),
		"pmo_agent_id", util.UUIDToString(pmo.ID),
		"members", len(memberAgentIDs),
	)
	s.broadcastGoalRun(ctx, run)
	return run, chat, nil
}

// AddTaskMember adds an agent to a task's dynamic squad during discussion
// (e.g. the PMO suggested a missing reviewer/adversary role).
func (s *GoalService) AddTaskMember(ctx context.Context, workspaceID, goalRunID, agentID pgtype.UUID) error {
	run, err := s.Queries.GetGoalRunInWorkspace(ctx, db.GetGoalRunInWorkspaceParams{
		ID:          goalRunID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return fmt.Errorf("load goal: %w", err)
	}
	agent, err := s.Queries.GetAgent(ctx, agentID)
	if err != nil {
		return fmt.Errorf("load agent: %w", err)
	}
	if agent.WorkspaceID != workspaceID {
		return fmt.Errorf("agent does not belong to workspace")
	}
	_, err = s.Queries.AddSquadMember(ctx, db.AddSquadMemberParams{
		SquadID:    run.SquadID,
		MemberType: "agent",
		MemberID:   agentID,
		Role:       "",
	})
	return err
}

// SetTaskProject binds (or clears) a task's working directory = dependency
// project. projectID is the zero UUID to unbind. When set, it is validated to
// belong to the workspace. Returns the updated goal so the UI reflects the new
// binding (which drives role-sync, PMO planning context, and repo-persist).
func (s *GoalService) SetTaskProject(ctx context.Context, workspaceID, goalRunID, projectID pgtype.UUID) (db.GoalRun, error) {
	if _, err := s.Queries.GetGoalRunInWorkspace(ctx, db.GetGoalRunInWorkspaceParams{
		ID:          goalRunID,
		WorkspaceID: workspaceID,
	}); err != nil {
		return db.GoalRun{}, fmt.Errorf("load goal: %w", err)
	}

	if projectID.Valid {
		project, err := s.Queries.GetProject(ctx, projectID)
		if err != nil {
			return db.GoalRun{}, fmt.Errorf("load project: %w", err)
		}
		if project.WorkspaceID != workspaceID {
			return db.GoalRun{}, fmt.Errorf("project does not belong to workspace")
		}
	}

	updated, err := s.Queries.SetGoalRunProject(ctx, db.SetGoalRunProjectParams{
		ID:        goalRunID,
		ProjectID: projectID,
	})
	if err != nil {
		return db.GoalRun{}, fmt.Errorf("set project: %w", err)
	}
	return updated, nil
}

// ConfirmTask passes the discussion → execution gate for a task-mode goal:
// dispatches the PMO planning task (the PMO decomposes the now-agreed goal
// using its dynamic squad). Reuses the StartPlanning dispatch internals.
func (s *GoalService) ConfirmTask(ctx context.Context, workspaceID, goalRunID pgtype.UUID) (db.GoalRun, error) {
	run, err := s.Queries.GetGoalRunInWorkspace(ctx, db.GetGoalRunInWorkspaceParams{
		ID:          goalRunID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return db.GoalRun{}, fmt.Errorf("load goal: %w", err)
	}

	// Conversational-create path: the goal text lives in the discussion chat,
	// not goal_run.goal (there is no create form). Before dispatching planning,
	// backfill goal/title from the user's discussion messages so the PMO has a
	// goal to decompose. Only when goal is still empty — an explicit goal (the
	// thin-slice / form path) is never overwritten.
	if strings.TrimSpace(run.Goal) == "" {
		filled, ferr := s.backfillGoalFromDiscussion(ctx, run)
		if ferr != nil {
			return db.GoalRun{}, ferr
		}
		run = filled
	}

	squad, err := s.Queries.GetSquad(ctx, run.SquadID)
	if err != nil {
		return db.GoalRun{}, fmt.Errorf("load squad: %w", err)
	}

	planning, err := s.Queries.UpdateGoalRunStatus(ctx, db.UpdateGoalRunStatusParams{
		ID:     run.ID,
		Status: "planning",
	})
	if err != nil {
		return db.GoalRun{}, fmt.Errorf("set planning: %w", err)
	}
	s.broadcastGoalRun(ctx, planning)

	if derr := s.dispatchPlanningTask(ctx, planning, squad); derr != nil {
		if _, ferr := s.Queries.CompleteGoalRun(ctx, db.CompleteGoalRunParams{
			ID:            run.ID,
			Status:        "failed",
			FailureReason: pgtype.Text{String: derr.Error(), Valid: true},
		}); ferr == nil {
			if failed, gerr := s.Queries.GetGoalRun(ctx, run.ID); gerr == nil {
				planning = failed
				s.broadcastGoalRun(ctx, planning)
			}
		}
		return planning, fmt.Errorf("dispatch planning: %w", derr)
	}
	return planning, nil
}

// backfillGoalFromDiscussion derives goal_run.goal from the user's messages in
// the linked discussion chat and persists it. This is the conversational-create
// path: the user describes the goal in chat, not a form, so goal_run.goal starts
// empty and must be filled before the PMO can plan.
//
// Only user messages are joined — the PMO's replies are not the goal. If no
// user message exists yet (confirm clicked before saying anything), it returns
// an error instead of dispatching an empty goal, which is the only legitimate
// "no goal" case.
func (s *GoalService) backfillGoalFromDiscussion(ctx context.Context, run db.GoalRun) (db.GoalRun, error) {
	if !run.ChatSessionID.Valid {
		return db.GoalRun{}, fmt.Errorf("task has no goal: describe the goal in the discussion before confirming")
	}

	msgs, err := s.Queries.ListChatMessages(ctx, run.ChatSessionID)
	if err != nil {
		return db.GoalRun{}, fmt.Errorf("load discussion: %w", err)
	}

	var parts []string
	for _, m := range msgs {
		if m.Role != "user" {
			continue
		}
		if text := strings.TrimSpace(m.Content); text != "" {
			parts = append(parts, text)
		}
	}

	goal := strings.TrimSpace(strings.Join(parts, "\n\n"))
	if goal == "" {
		return db.GoalRun{}, fmt.Errorf("task has no goal: describe the goal in the discussion before confirming")
	}

	title := run.Title
	if strings.TrimSpace(title) == "" {
		title = truncateForSummary(goal, 40)
	}

	updated, err := s.Queries.UpdateGoalRunGoal(ctx, db.UpdateGoalRunGoalParams{
		ID:    run.ID,
		Title: title,
		Goal:  goal,
	})
	if err != nil {
		return db.GoalRun{}, fmt.Errorf("persist goal: %w", err)
	}

	slog.Info("backfilled goal from discussion",
		"goal_run_id", util.UUIDToString(run.ID),
		"user_messages", len(parts),
		"goal_runes", len([]rune(goal)),
	)
	return updated, nil
}

// uniqueSquadName appends a numeric suffix if the name collides (squad name is
// unique per workspace). Bounded probe; falls back to the raw name on exhaustion.
func (s *GoalService) uniqueSquadName(ctx context.Context, workspaceID pgtype.UUID, base string) string {
	for i := 0; i < 50; i++ {
		name := base
		if i > 0 {
			name = fmt.Sprintf("%s %d", base, i+1)
		}
		existing, err := s.Queries.ListSquads(ctx, workspaceID)
		if err != nil {
			return name
		}
		clash := false
		for _, sq := range existing {
			if sq.Name == name {
				clash = true
				break
			}
		}
		if !clash {
			return name
		}
	}
	return base
}

// SubtaskSpec is the planner-provided definition of one subtask. The DAG is
// expressed by DependsOn referencing the Seq of upstream subtasks within the
// same decomposition (seq-relative, so the caller doesn't need pre-assigned
// UUIDs). CreateGoal translates seq dependencies into the persisted UUID array.
type SubtaskSpec struct {
	Seq             int32
	Title           string
	Spec            string
	AssigneeAgentID pgtype.UUID
	// DependsOn lists the Seq values of subtasks that must complete first.
	DependsOn []int32
	// Kind is 'execute' (default) or 'verify'. A verify node adversarially
	// reviews the output of the nodes it depends on.
	Kind string
}

// CreateGoal creates a goal_run and its subtask DAG in one transaction. The
// goal starts in 'discussion' unless confirmed=true (the thin-slice path skips
// the multi-round discussion and goes straight to a confirmed, ready-to-run
// plan). When confirmed, root subtasks (no dependencies) are marked 'ready'.
//
// subtasks may be empty (pure discussion goal, decomposed later).
func (s *GoalService) CreateGoal(
	ctx context.Context,
	workspaceID, squadID, creatorID pgtype.UUID,
	chatSessionID pgtype.UUID,
	title, goal string,
	subtasks []SubtaskSpec,
	confirmed bool,
) (db.GoalRun, []db.GoalSubtask, error) {
	// Validate the squad belongs to the workspace and resolve nothing else —
	// the PMO is squad.leader_id, used at dispatch time.
	squad, err := s.Queries.GetSquad(ctx, squadID)
	if err != nil {
		return db.GoalRun{}, nil, fmt.Errorf("load squad: %w", err)
	}
	if squad.WorkspaceID != workspaceID {
		return db.GoalRun{}, nil, fmt.Errorf("squad does not belong to workspace")
	}

	status := "discussion"
	if confirmed {
		status = "executing"
	}

	run, err := s.Queries.CreateGoalRun(ctx, db.CreateGoalRunParams{
		WorkspaceID:   workspaceID,
		SquadID:       squadID,
		ChatSessionID: chatSessionID,
		CreatorID:     creatorID,
		Title:         title,
		Goal:          goal,
		Status:        pgtype.Text{String: status, Valid: true},
	})
	if err != nil {
		return db.GoalRun{}, nil, fmt.Errorf("create goal run: %w", err)
	}

	created, err := s.persistSubtasks(ctx, run.ID, subtasks, confirmed)
	if err != nil {
		return db.GoalRun{}, nil, err
	}

	if confirmed {
		// Stamp confirmed_at so the gate is recorded even on the thin-slice
		// straight-to-executing path. ConfirmGoalRun forces status='confirmed';
		// restore 'executing' right after.
		if _, cerr := s.Queries.ConfirmGoalRun(ctx, run.ID); cerr == nil {
			if updated, uerr := s.Queries.UpdateGoalRunStatus(ctx, db.UpdateGoalRunStatusParams{
				ID:     run.ID,
				Status: "executing",
			}); uerr == nil {
				run = updated
			}
		}
	}

	slog.Info("goal created",
		"goal_run_id", util.UUIDToString(run.ID),
		"squad_id", util.UUIDToString(squadID),
		"subtasks", len(created),
		"confirmed", confirmed,
	)
	s.broadcastGoalRun(ctx, run)

	if confirmed {
		// Kick the scheduler: dispatch every root (dependency-free) subtask.
		if derr := s.dispatchReadySubtasks(ctx, run); derr != nil {
			// Dispatch failures are logged but don't roll back the goal — the
			// goal exists and can be retried/inspected. Return the goal so the
			// caller still sees it.
			slog.Error("initial goal dispatch failed",
				"goal_run_id", util.UUIDToString(run.ID), "error", derr)
		}
		// Re-read post-dispatch state (root subtasks become 'running').
		if fresh, ferr := s.Queries.ListGoalSubtasks(ctx, run.ID); ferr == nil {
			created = fresh
		}
	}

	return run, created, nil
}

// StartPlanning creates a goal in 'planning' status and dispatches a planning
// task to the squad leader (PMO). The leader decomposes the goal into subtasks,
// assigns roles, declares dependencies, and submits the plan via the CLI
// (`multica goal plan`), which lands in SubmitPlan. This is the LLM-driven
// decomposition path: the "LLM" is the leader agent reached through the task
// queue — the server never calls a model directly.
func (s *GoalService) StartPlanning(
	ctx context.Context,
	workspaceID, squadID, creatorID pgtype.UUID,
	chatSessionID pgtype.UUID,
	title, goal string,
) (db.GoalRun, error) {
	squad, err := s.Queries.GetSquad(ctx, squadID)
	if err != nil {
		return db.GoalRun{}, fmt.Errorf("load squad: %w", err)
	}
	if squad.WorkspaceID != workspaceID {
		return db.GoalRun{}, fmt.Errorf("squad does not belong to workspace")
	}

	run, err := s.Queries.CreateGoalRun(ctx, db.CreateGoalRunParams{
		WorkspaceID:   workspaceID,
		SquadID:       squadID,
		ChatSessionID: chatSessionID,
		CreatorID:     creatorID,
		Title:         title,
		Goal:          goal,
		Status:        pgtype.Text{String: "planning", Valid: true},
	})
	if err != nil {
		return db.GoalRun{}, fmt.Errorf("create goal run: %w", err)
	}
	s.broadcastGoalRun(ctx, run)

	if err := s.dispatchPlanningTask(ctx, run, squad); err != nil {
		// Planning dispatch failed (leader offline/archived): mark the goal
		// failed so the user sees why instead of a goal stuck in 'planning'.
		if _, ferr := s.Queries.CompleteGoalRun(ctx, db.CompleteGoalRunParams{
			ID:            run.ID,
			Status:        "failed",
			FailureReason: pgtype.Text{String: err.Error(), Valid: true},
		}); ferr == nil {
			if failed, gerr := s.Queries.GetGoalRun(ctx, run.ID); gerr == nil {
				run = failed
				s.broadcastGoalRun(ctx, run)
			}
		}
		return run, fmt.Errorf("dispatch planning task: %w", err)
	}

	slog.Info("goal planning started",
		"goal_run_id", util.UUIDToString(run.ID),
		"squad_id", util.UUIDToString(squadID),
		"leader_id", util.UUIDToString(squad.LeaderID),
	)
	return run, nil
}

// dispatchPlanningTask enqueues a planning task for the squad leader. Reuses the
// free-prompt task path (no issue); the daemon's buildGoalPlanningPrompt + the
// injected squad roster tell the leader how to decompose and submit.
func (s *GoalService) dispatchPlanningTask(ctx context.Context, run db.GoalRun, squad db.Squad) error {
	return s.dispatchPlanningTaskWithMode(ctx, run, squad, false)
}

// dispatchPlanningTaskWithMode is dispatchPlanningTask plus the autofix flag.
// When autofix is true the planning context carries Autofix=true so the daemon
// surfaces it to buildGoalPlanningPrompt, which then injects the fixed 4-node
// auto-fix guidance instead of the free-form decomposition guidance.
func (s *GoalService) dispatchPlanningTaskWithMode(ctx context.Context, run db.GoalRun, squad db.Squad, autofix bool) error {
	leader, err := s.Queries.GetAgent(ctx, squad.LeaderID)
	if err != nil {
		return fmt.Errorf("load squad leader: %w", err)
	}
	if leader.ArchivedAt.Valid {
		return fmt.Errorf("squad leader is archived")
	}
	if !leader.RuntimeID.Valid {
		return fmt.Errorf("squad leader has no runtime")
	}

	payload := GoalPlanningContext{
		Type:        GoalPlanningContextType,
		GoalRunID:   util.UUIDToString(run.ID),
		WorkspaceID: util.UUIDToString(run.WorkspaceID),
		SquadID:     util.UUIDToString(squad.ID),
		GoalTitle:   run.Title,
		Goal:        run.Goal,
	}
	if run.ProjectID.Valid {
		payload.ProjectID = util.UUIDToString(run.ProjectID)
	}
	payload.Autofix = autofix
	contextJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal goal planning context: %w", err)
	}

	// Planning tasks carry no goal_subtask_id (they precede subtasks). The
	// goal_run_id lives in the context JSONB, resolved at claim time — same
	// shape as quick-create, which carries its workspace there.
	task, err := s.Queries.CreateQuickCreateTask(ctx, db.CreateQuickCreateTaskParams{
		AgentID:   leader.ID,
		RuntimeID: leader.RuntimeID,
		Priority:  priorityToInt("high"),
		Context:   contextJSON,
	})
	if err != nil {
		return fmt.Errorf("create goal planning task: %w", err)
	}

	s.TaskSvc.NotifyTaskEnqueued(ctx, task)
	return nil
}

// SubmitPlan lands a leader-produced decomposition: it persists the subtasks +
// DAG, flips the goal to executing, and dispatches root subtasks. This is the
// CLI write-back target (`multica goal plan`). It is the planning counterpart
// of the thin-slice CreateGoal(confirmed=true) path — same persist + dispatch,
// but applied to an existing goal in 'planning' status.
func (s *GoalService) SubmitPlan(
	ctx context.Context,
	goalRunID pgtype.UUID,
	subtasks []SubtaskSpec,
) (db.GoalRun, []db.GoalSubtask, error) {
	run, err := s.Queries.GetGoalRun(ctx, goalRunID)
	if err != nil {
		return db.GoalRun{}, nil, fmt.Errorf("load goal run: %w", err)
	}
	if len(subtasks) == 0 {
		return db.GoalRun{}, nil, fmt.Errorf("plan must contain at least one subtask")
	}

	// Persist with confirmed=true so dependency-free subtasks come up 'ready'.
	created, err := s.persistSubtasks(ctx, run.ID, subtasks, true)
	if err != nil {
		return db.GoalRun{}, nil, err
	}

	// Stamp the confirm gate, then move to executing.
	if _, cerr := s.Queries.ConfirmGoalRun(ctx, run.ID); cerr == nil {
		if updated, uerr := s.Queries.UpdateGoalRunStatus(ctx, db.UpdateGoalRunStatusParams{
			ID:     run.ID,
			Status: "executing",
		}); uerr == nil {
			run = updated
		}
	}
	s.broadcastGoalRun(ctx, run)

	slog.Info("goal plan submitted",
		"goal_run_id", util.UUIDToString(run.ID),
		"subtasks", len(created),
	)

	if derr := s.dispatchReadySubtasks(ctx, run); derr != nil {
		slog.Error("plan dispatch failed",
			"goal_run_id", util.UUIDToString(run.ID), "error", derr)
	}

	// Re-read so the caller sees post-dispatch state (root subtasks 'running',
	// not the pre-dispatch 'ready' captured during persist).
	if fresh, ferr := s.Queries.ListGoalSubtasks(ctx, run.ID); ferr == nil {
		created = fresh
	}

	return run, created, nil
}

// persistSubtasks inserts subtask rows, translating seq-relative DependsOn into
// the persisted UUID array. Two passes: first insert (collecting seq→id), then
// patch depends_on. Root subtasks become 'ready' when the goal is confirmed.
func (s *GoalService) persistSubtasks(
	ctx context.Context,
	goalRunID pgtype.UUID,
	subtasks []SubtaskSpec,
	confirmed bool,
) ([]db.GoalSubtask, error) {
	seqToID := make(map[int32]pgtype.UUID, len(subtasks))
	created := make([]db.GoalSubtask, 0, len(subtasks))

	for _, st := range subtasks {
		initialStatus := "pending"
		if confirmed && len(st.DependsOn) == 0 {
			initialStatus = "ready"
		}
		kind := st.Kind
		if kind == "" {
			kind = "execute"
		}
		row, err := s.Queries.CreateGoalSubtask(ctx, db.CreateGoalSubtaskParams{
			GoalRunID:       goalRunID,
			Seq:             st.Seq,
			Title:           st.Title,
			Spec:            st.Spec,
			AssigneeAgentID: st.AssigneeAgentID,
			DependsOn:       nil, // patched below once all ids are known
			Status:          pgtype.Text{String: initialStatus, Valid: true},
			Kind:            pgtype.Text{String: kind, Valid: true},
		})
		if err != nil {
			return nil, fmt.Errorf("create subtask seq=%d: %w", st.Seq, err)
		}
		seqToID[st.Seq] = row.ID
		created = append(created, row)
	}

	// Second pass: resolve depends_on seq refs to UUIDs and persist them.
	for i, st := range subtasks {
		if len(st.DependsOn) == 0 {
			continue
		}
		deps := make([]pgtype.UUID, 0, len(st.DependsOn))
		for _, depSeq := range st.DependsOn {
			depID, ok := seqToID[depSeq]
			if !ok {
				return nil, fmt.Errorf("subtask seq=%d depends on unknown seq=%d", st.Seq, depSeq)
			}
			deps = append(deps, depID)
		}
		updated, err := s.Queries.SetGoalSubtaskDependsOn(ctx, db.SetGoalSubtaskDependsOnParams{
			ID:        created[i].ID,
			DependsOn: deps,
		})
		if err != nil {
			return nil, fmt.Errorf("set depends_on seq=%d: %w", st.Seq, err)
		}
		created[i] = updated
	}

	return created, nil
}

// ConfirmGoal passes the confirm gate: discussion/confirmed → executing, then
// dispatches root subtasks. Idempotent enough for the thin slice — re-confirming
// an executing goal just re-runs ready dispatch (no-op when nothing is ready).
func (s *GoalService) ConfirmGoal(ctx context.Context, goalRunID pgtype.UUID) (db.GoalRun, error) {
	if _, err := s.Queries.ConfirmGoalRun(ctx, goalRunID); err != nil {
		return db.GoalRun{}, fmt.Errorf("confirm goal run: %w", err)
	}
	// Move past the gate into executing; mark dependency-free subtasks ready.
	if err := s.markRootSubtasksReady(ctx, goalRunID); err != nil {
		return db.GoalRun{}, err
	}
	executing, err := s.Queries.UpdateGoalRunStatus(ctx, db.UpdateGoalRunStatusParams{
		ID:     goalRunID,
		Status: "executing",
	})
	if err != nil {
		return db.GoalRun{}, fmt.Errorf("set executing: %w", err)
	}
	s.broadcastGoalRun(ctx, executing)

	if err := s.dispatchReadySubtasks(ctx, executing); err != nil {
		slog.Error("confirm dispatch failed",
			"goal_run_id", util.UUIDToString(goalRunID), "error", err)
	}
	return executing, nil
}

// markRootSubtasksReady flips dependency-free 'pending' subtasks to 'ready'.
func (s *GoalService) markRootSubtasksReady(ctx context.Context, goalRunID pgtype.UUID) error {
	subtasks, err := s.Queries.ListGoalSubtasks(ctx, goalRunID)
	if err != nil {
		return fmt.Errorf("list subtasks: %w", err)
	}
	for _, st := range subtasks {
		if st.Status == "pending" && len(st.DependsOn) == 0 {
			if _, err := s.Queries.UpdateGoalSubtaskStatus(ctx, db.UpdateGoalSubtaskStatusParams{
				ID:     st.ID,
				Status: "ready",
			}); err != nil {
				return fmt.Errorf("ready subtask: %w", err)
			}
		}
	}
	return nil
}

// dispatchReadySubtasks enqueues a task for every 'ready' subtask of the goal.
// Each dispatch creates an agent_task_queue row linked via goal_subtask_id, and
// flips the subtask to 'running'. Idempotent: only 'ready' rows are dispatched.
func (s *GoalService) dispatchReadySubtasks(ctx context.Context, run db.GoalRun) error {
	subtasks, err := s.Queries.ListGoalSubtasks(ctx, run.ID)
	if err != nil {
		return fmt.Errorf("list subtasks: %w", err)
	}
	for _, st := range subtasks {
		if st.Status != "ready" {
			continue
		}
		if err := s.dispatchSubtask(ctx, run, st); err != nil {
			// Mark this subtask failed but continue with the rest (fail-isolate,
			// matching the "继续其余 + 阻塞下游" failure policy).
			slog.Error("dispatch subtask failed",
				"goal_subtask_id", util.UUIDToString(st.ID), "error", err)
			if _, ferr := s.Queries.FailGoalSubtask(ctx, db.FailGoalSubtaskParams{
				ID:            st.ID,
				FailureReason: pgtype.Text{String: err.Error(), Valid: true},
			}); ferr == nil {
				s.handleSubtaskTerminal(ctx, run.ID, st.ID, false)
			}
		}
	}
	return nil
}

// depsSatisfied reports whether every dependency of st has reached a
// success-terminal state (completed or skipped). It is the single dependency
// gate: a node — especially a verify node — must never run while any node it
// depends on is still pending/ready/running. Returns true for a dependency-free
// (root) node.
//
// This structurally prevents the "verify抢跑" class of bug: even if a caller
// re-arms and tries to redispatch a verify node while the nodes it reviews are
// being re-run, the gate holds it back until those nodes finish. When the last
// dependency completes, unblockDownstream re-triggers the dispatch.
func (s *GoalService) depsSatisfied(ctx context.Context, st db.GoalSubtask) bool {
	for _, depID := range st.DependsOn {
		dep, err := s.Queries.GetGoalSubtask(ctx, depID)
		if err != nil {
			return false
		}
		if dep.Status != "completed" && dep.Status != "skipped" {
			return false
		}
	}
	return true
}

// dispatchSubtask enqueues one subtask's execution task and marks it running.
func (s *GoalService) dispatchSubtask(ctx context.Context, run db.GoalRun, st db.GoalSubtask) error {
	return s.dispatchSubtaskWithFeedback(ctx, run, st, "")
}

// dispatchSubtaskWithFeedback is dispatchSubtask plus an incremental rerun
// instruction (the reviewer's reject reason) injected into the prompt, used by
// the retry/reshape decision path so a session-resumed agent fixes the specific
// points rather than starting over.
func (s *GoalService) dispatchSubtaskWithFeedback(ctx context.Context, run db.GoalRun, st db.GoalSubtask, rerunFeedback string) error {
	if !st.AssigneeAgentID.Valid {
		return fmt.Errorf("subtask has no assignee agent")
	}
	// Dependency gate (the single choke point): never dispatch a node whose
	// dependencies have not all reached completed/skipped. If a caller tries to
	// dispatch too early, hold the node at 'pending' and bail without error —
	// unblockDownstream will redispatch it once the last dependency completes.
	if !s.depsSatisfied(ctx, st) {
		if st.Status != "pending" {
			if held, err := s.Queries.UpdateGoalSubtaskStatus(ctx, db.UpdateGoalSubtaskStatusParams{
				ID:     st.ID,
				Status: "pending",
			}); err == nil {
				s.broadcastGoalSubtask(ctx, run.WorkspaceID, held)
			}
		}
		slog.Info("dispatch held: dependencies not satisfied",
			"goal_subtask_id", util.UUIDToString(st.ID),
			"kind", st.Kind)
		return nil
	}
	agent, err := s.Queries.GetAgent(ctx, st.AssigneeAgentID)
	if err != nil {
		return fmt.Errorf("load assignee agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		return fmt.Errorf("assignee agent is archived")
	}
	if !agent.RuntimeID.Valid {
		return fmt.Errorf("assignee agent has no runtime")
	}

	payload := GoalSubtaskContext{
		Type:          GoalSubtaskContextType,
		GoalRunID:     util.UUIDToString(run.ID),
		GoalSubtaskID: util.UUIDToString(st.ID),
		WorkspaceID:   util.UUIDToString(run.WorkspaceID),
		GoalTitle:     run.Title,
		SubtaskTitle:  st.Title,
		Spec:          st.Spec,
		Kind:          st.Kind,
		RerunFeedback: rerunFeedback,
	}
	if run.ProjectID.Valid {
		payload.ProjectID = util.UUIDToString(run.ProjectID)
	}
	// Mark autofix subtasks so the execute prompt surfaces the `multica goal
	// report` artifact channel. The linked-issue lookup is the same discriminator
	// the Report* helpers use — one source of truth for "is this an autofix run".
	if _, ok := s.resolveAutofixIssue(ctx, run.WorkspaceID, run.ID); ok {
		payload.Autofix = true
	}
	// A verify node reviews the output of the node(s) it depends on. Gather
	// those nodes' titles + results so the verifier has the work product to
	// judge without re-deriving it.
	if st.Kind == "verify" {
		payload.ReviewTarget = s.buildReviewTarget(ctx, st)
	} else if len(st.DependsOn) > 0 {
		// Execute node with dependencies: pass source material directly so it
		// builds on what was produced instead of re-deriving it. If a dependency
		// is a verifier, include the verifier's reviewed work products too; a
		// synthesis task should not have to understand the DAG just to find the
		// content the verifier approved.
		payload.UpstreamOutput = s.buildExecutionInput(ctx, st)
		if payload.UpstreamOutput != "" {
			payload.HandoffBrief = buildGoalHandoffBrief(st)
		}
	}
	contextJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal goal subtask context: %w", err)
	}

	task, err := s.Queries.CreateGoalSubtaskTask(ctx, db.CreateGoalSubtaskTaskParams{
		AgentID:       agent.ID,
		RuntimeID:     agent.RuntimeID,
		Priority:      priorityToInt("high"),
		Context:       contextJSON,
		GoalSubtaskID: st.ID,
	})
	if err != nil {
		return fmt.Errorf("create goal subtask task: %w", err)
	}

	started, err := s.Queries.StartGoalSubtask(ctx, st.ID)
	if err != nil {
		return fmt.Errorf("mark subtask running: %w", err)
	}
	s.broadcastGoalSubtask(ctx, run.WorkspaceID, started)

	slog.Info("goal subtask dispatched",
		"goal_run_id", util.UUIDToString(run.ID),
		"goal_subtask_id", util.UUIDToString(st.ID),
		"task_id", util.UUIDToString(task.ID),
		"agent_id", util.UUIDToString(agent.ID),
	)

	// Wake the daemon so the task gets claimed promptly (same as every other
	// Enqueue* path).
	s.TaskSvc.NotifyTaskEnqueued(ctx, task)
	return nil
}

// SubmitVerdict records a verify node's verdict, reported by the verifier agent
// via the CLI (`multica goal verdict`). It only persists the verdict + reason;
// the task-completion hook (handleVerifyCompleted) reads it and drives the
// workflow (pass → unblock, reject → bounce). Validated: the subtask must be a
// verify node in this workspace.
func (s *GoalService) SubmitVerdict(
	ctx context.Context,
	workspaceID, subtaskID pgtype.UUID,
	verdict, reason string,
) (db.GoalSubtask, error) {
	if verdict != "pass" && verdict != "reject" {
		return db.GoalSubtask{}, fmt.Errorf("verdict must be 'pass' or 'reject'")
	}
	st, err := s.Queries.GetGoalSubtask(ctx, subtaskID)
	if err != nil {
		return db.GoalSubtask{}, fmt.Errorf("load subtask: %w", err)
	}
	if st.Kind != "verify" {
		return db.GoalSubtask{}, fmt.Errorf("subtask is not a verify node")
	}
	run, err := s.Queries.GetGoalRun(ctx, st.GoalRunID)
	if err != nil {
		return db.GoalSubtask{}, fmt.Errorf("load goal run: %w", err)
	}
	if run.WorkspaceID != workspaceID {
		return db.GoalSubtask{}, fmt.Errorf("subtask does not belong to workspace")
	}

	// Persist the verdict only. We do NOT advance the workflow here — the
	// verifier's task is still running until it exits; the completion hook
	// (handleVerifyCompleted) is the single place that reads the verdict and
	// drives downstream. This keeps one code path for "verify finished".
	var resultJSON []byte
	if reason != "" {
		resultJSON, _ = json.Marshal(map[string]string{"reason": reason})
	}
	updated, err := s.Queries.SetGoalSubtaskVerdict(ctx, db.SetGoalSubtaskVerdictParams{
		ID:      subtaskID,
		Verdict: pgtype.Text{String: verdict, Valid: true},
		Result:  resultJSON,
	})
	if err != nil {
		return db.GoalSubtask{}, fmt.Errorf("set verdict: %w", err)
	}
	// SetGoalSubtaskVerdict also marks status=completed; revert to 'running' so
	// the completion hook (fired when the agent's task actually ends) is the
	// authority on advancing the workflow. The verdict value persists.
	reverted, err := s.Queries.UpdateGoalSubtaskStatus(ctx, db.UpdateGoalSubtaskStatusParams{
		ID:     subtaskID,
		Status: "running",
	})
	if err == nil {
		updated = reverted
	}
	s.broadcastGoalSubtask(ctx, run.WorkspaceID, updated)
	return updated, nil
}

// ---------------------------------------------------------------------------
// Human intervention on a failed / blocked subtask (the escalation buttons).
// All four share a shape: validate workspace ownership, mutate the node, then
// (for retry/reassign/edit-spec) rearm-fresh + dispatch, or (for skip) mark
// skipped + unblock downstream. Every op recomputes the goal_run status so a
// 'partial' / 'failed' goal flows again. Manual retry resets the attempt budget
// (RearmGoalSubtaskFresh) — a human override is not bounded by the auto-retry
// budget the escalation already exhausted.
// ---------------------------------------------------------------------------

// loadSubtaskForIntervention resolves a subtask + its run, gated on workspace.
func (s *GoalService) loadSubtaskForIntervention(
	ctx context.Context, workspaceID, subtaskID pgtype.UUID,
) (db.GoalSubtask, db.GoalRun, error) {
	st, err := s.Queries.GetGoalSubtask(ctx, subtaskID)
	if err != nil {
		return db.GoalSubtask{}, db.GoalRun{}, fmt.Errorf("load subtask: %w", err)
	}
	run, err := s.Queries.GetGoalRun(ctx, st.GoalRunID)
	if err != nil {
		return db.GoalSubtask{}, db.GoalRun{}, fmt.Errorf("load goal run: %w", err)
	}
	if run.WorkspaceID != workspaceID {
		return db.GoalSubtask{}, db.GoalRun{}, fmt.Errorf("subtask does not belong to workspace")
	}
	return st, run, nil
}

// reviveGoalIfTerminal flips a goal_run that had settled to partial/failed back
// to 'executing' when an intervention re-activates a node. No-op if already
// executing/planning/etc.
func (s *GoalService) reviveGoalIfTerminal(ctx context.Context, run db.GoalRun) db.GoalRun {
	if run.Status != "partial" && run.Status != "failed" {
		return run
	}
	updated, err := s.Queries.UpdateGoalRunStatus(ctx, db.UpdateGoalRunStatusParams{
		ID:     run.ID,
		Status: "executing",
	})
	if err != nil {
		return run
	}
	s.broadcastGoalRun(ctx, updated)
	return updated
}

// RetrySubtask re-runs a failed/blocked node with a fresh attempt budget.
func (s *GoalService) RetrySubtask(ctx context.Context, workspaceID, subtaskID pgtype.UUID) (db.GoalSubtask, error) {
	_, run, err := s.loadSubtaskForIntervention(ctx, workspaceID, subtaskID)
	if err != nil {
		return db.GoalSubtask{}, err
	}
	run = s.reviveGoalIfTerminal(ctx, run)
	return s.rearmAndDispatch(ctx, run, subtaskID)
}

// ReassignSubtask swaps the executing agent then re-runs the node.
func (s *GoalService) ReassignSubtask(
	ctx context.Context, workspaceID, subtaskID, newAgentID pgtype.UUID,
) (db.GoalSubtask, error) {
	_, run, err := s.loadSubtaskForIntervention(ctx, workspaceID, subtaskID)
	if err != nil {
		return db.GoalSubtask{}, err
	}
	// Validate the new agent lives in this workspace and is runnable.
	agent, err := s.Queries.GetAgent(ctx, newAgentID)
	if err != nil {
		return db.GoalSubtask{}, fmt.Errorf("load new agent: %w", err)
	}
	if agent.WorkspaceID != workspaceID {
		return db.GoalSubtask{}, fmt.Errorf("agent does not belong to workspace")
	}
	if _, err := s.Queries.ReassignGoalSubtask(ctx, db.ReassignGoalSubtaskParams{
		ID:              subtaskID,
		AssigneeAgentID: newAgentID,
	}); err != nil {
		return db.GoalSubtask{}, fmt.Errorf("reassign: %w", err)
	}
	run = s.reviveGoalIfTerminal(ctx, run)
	return s.rearmAndDispatch(ctx, run, subtaskID)
}

// EditSubtaskSpec rewrites a node's spec then re-runs it (fix-then-retry).
func (s *GoalService) EditSubtaskSpec(
	ctx context.Context, workspaceID, subtaskID pgtype.UUID, spec string,
) (db.GoalSubtask, error) {
	_, run, err := s.loadSubtaskForIntervention(ctx, workspaceID, subtaskID)
	if err != nil {
		return db.GoalSubtask{}, err
	}
	if _, err := s.Queries.UpdateGoalSubtaskSpec(ctx, db.UpdateGoalSubtaskSpecParams{
		ID:   subtaskID,
		Spec: spec,
	}); err != nil {
		return db.GoalSubtask{}, fmt.Errorf("edit spec: %w", err)
	}
	run = s.reviveGoalIfTerminal(ctx, run)
	return s.rearmAndDispatch(ctx, run, subtaskID)
}

// SkipSubtask abandons a node: mark skipped, unblock downstream, recompute goal.
func (s *GoalService) SkipSubtask(ctx context.Context, workspaceID, subtaskID pgtype.UUID) (db.GoalSubtask, error) {
	_, run, err := s.loadSubtaskForIntervention(ctx, workspaceID, subtaskID)
	if err != nil {
		return db.GoalSubtask{}, err
	}
	skipped, err := s.Queries.SkipGoalSubtask(ctx, subtaskID)
	if err != nil {
		return db.GoalSubtask{}, fmt.Errorf("skip: %w", err)
	}
	run = s.reviveGoalIfTerminal(ctx, run)
	s.broadcastGoalSubtask(ctx, run.WorkspaceID, skipped)
	// Skipped counts as a non-blocking terminal → unblock dependents.
	s.handleSubtaskTerminal(ctx, run.ID, subtaskID, true)
	return skipped, nil
}

// StartTakeover opens a human-takeover chat for a failed/blocked subtask: it
// creates a chat session bound to that subtask's assignee agent + runtime,
// stamped with goal_subtask_id so the daemon's chat prompt injects the subtask
// spec + failure history. The user then guides the agent hands-on in the normal
// chat surface. Takeover only opens the conversation — it does NOT mutate the
// goal/subtask state; the user advances the goal afterward via retry/skip or by
// re-dispatching once the work is sorted.
func (s *GoalService) StartTakeover(
	ctx context.Context, workspaceID, subtaskID, creatorID pgtype.UUID,
) (db.ChatSession, error) {
	st, run, err := s.loadSubtaskForIntervention(ctx, workspaceID, subtaskID)
	if err != nil {
		return db.ChatSession{}, err
	}
	if !st.AssigneeAgentID.Valid {
		return db.ChatSession{}, fmt.Errorf("subtask has no assignee agent to take over")
	}
	agent, err := s.Queries.GetAgent(ctx, st.AssigneeAgentID)
	if err != nil {
		return db.ChatSession{}, fmt.Errorf("load assignee agent: %w", err)
	}

	title := st.Title
	if title == "" {
		title = "Goal subtask"
	}
	// Bind the session to the subtask's agent + runtime so the takeover talks to
	// exactly the role that failed. runtime_id nil → query falls back to the
	// agent default (same as a normal chat).
	session, err := s.Queries.CreateTakeoverChatSession(ctx, db.CreateTakeoverChatSessionParams{
		WorkspaceID:   run.WorkspaceID,
		AgentID:       agent.ID,
		CreatorID:     creatorID,
		Title:         "Takeover: " + title,
		GoalSubtaskID: subtaskID,
	})
	if err != nil {
		return db.ChatSession{}, fmt.Errorf("create takeover chat session: %w", err)
	}

	slog.Info("goal subtask takeover started",
		"goal_run_id", util.UUIDToString(run.ID),
		"goal_subtask_id", util.UUIDToString(subtaskID),
		"chat_session_id", util.UUIDToString(session.ID),
		"agent_id", util.UUIDToString(agent.ID),
	)
	return session, nil
}

// rearmAndDispatch resets a node to a fresh-ready state and dispatches it,
// then recomputes the goal status. Shared by retry/reassign/edit-spec.
func (s *GoalService) rearmAndDispatch(
	ctx context.Context, run db.GoalRun, subtaskID pgtype.UUID,
) (db.GoalSubtask, error) {
	rearmed, err := s.Queries.RearmGoalSubtaskFresh(ctx, subtaskID)
	if err != nil {
		return db.GoalSubtask{}, fmt.Errorf("rearm: %w", err)
	}
	s.broadcastGoalSubtask(ctx, run.WorkspaceID, rearmed)
	if err := s.dispatchSubtask(ctx, run, rearmed); err != nil {
		// Dispatch failed (e.g. agent offline) — mark failed again so the node
		// doesn't sit stuck in 'ready', and surface the reason.
		if failed, ferr := s.Queries.FailGoalSubtask(ctx, db.FailGoalSubtaskParams{
			ID:            subtaskID,
			FailureReason: pgtype.Text{String: err.Error(), Valid: true},
		}); ferr == nil {
			s.broadcastGoalSubtask(ctx, run.WorkspaceID, failed)
			s.recomputeGoalStatus(ctx, run.ID)
			return failed, nil
		}
		return db.GoalSubtask{}, fmt.Errorf("dispatch: %w", err)
	}
	return rearmed, nil
}

// buildReviewTarget assembles the work product a verify node must judge: the
// title + result of each node it depends on. Verify nodes need the spec too (to
// judge against it), so includeSpec=true.
func (s *GoalService) buildReviewTarget(ctx context.Context, verify db.GoalSubtask) string {
	return s.buildUpstreamOutput(ctx, verify, true)
}

// buildExecutionInput assembles the source material an execute node needs. It
// starts with the direct dependencies. When a direct dependency is a verify node,
// it also includes the work products that verifier reviewed, because the
// downstream worker needs semantic inputs, not a graph puzzle.
func (s *GoalService) buildExecutionInput(ctx context.Context, st db.GoalSubtask) string {
	return s.buildWorkProductBundle(ctx, st, workProductBundleOptions{
		ExpandVerifyDeps: true,
	})
}

// buildUpstreamOutput assembles the direct work products a verify node must
// judge. Verify nodes need the reviewed spec too, so includeSpec=true.
func (s *GoalService) buildUpstreamOutput(ctx context.Context, st db.GoalSubtask, includeSpec bool) string {
	return s.buildWorkProductBundle(ctx, st, workProductBundleOptions{
		IncludeSpec: includeSpec,
	})
}

type workProductBundleOptions struct {
	IncludeSpec      bool
	ExpandVerifyDeps bool
}

func (s *GoalService) buildWorkProductBundle(ctx context.Context, st db.GoalSubtask, opts workProductBundleOptions) string {
	var b strings.Builder
	seen := make(map[string]bool, len(st.DependsOn))

	appendDep := func(dep db.GoalSubtask, includeSpec bool) {
		key := util.UUIDToString(dep.ID)
		if seen[key] {
			return
		}
		seen[key] = true

		fmt.Fprintf(&b, "### %s\n", dep.Title)
		if includeSpec && dep.Spec != "" {
			fmt.Fprintf(&b, "Spec: %s\n", dep.Spec)
		}
		if dep.Kind == "verify" && dep.Verdict.Valid {
			fmt.Fprintf(&b, "Verdict: %s\n", dep.Verdict.String)
		}
		if len(dep.Result) > 0 {
			fmt.Fprintf(&b, "Result: %s\n", string(dep.Result))
		} else {
			b.WriteString("Result: (no structured result reported)\n")
		}
		b.WriteString("\n")
	}

	for _, depID := range st.DependsOn {
		dep, err := s.Queries.GetGoalSubtask(ctx, depID)
		if err != nil {
			continue
		}

		if opts.ExpandVerifyDeps && dep.Kind == "verify" {
			for _, reviewedID := range dep.DependsOn {
				reviewed, err := s.Queries.GetGoalSubtask(ctx, reviewedID)
				if err != nil {
					continue
				}
				appendDep(reviewed, false)
			}
		}

		appendDep(dep, opts.IncludeSpec)
	}
	return strings.TrimSpace(b.String())
}

func buildGoalHandoffBrief(st db.GoalSubtask) string {
	target := strings.TrimSpace(st.Title)
	if target == "" {
		target = "assigned task"
	}
	return fmt.Sprintf("Task objective: %s\nThe coordinator has embedded the source material this task needs. Use it directly; do not read or create an intermediate handoff file, and do not redo prior discovery.", target)
}

// handleVerifyCompleted processes a finished verify node. The verifier reports
// its verdict via the CLI (which sets goal_subtask.verdict), so by the time the
// task completes the verdict is already persisted. We read it and act:
//   - pass: the verify node counts as completed → unblock downstream.
//   - reject: do NOT auto-bounce with a hardcoded attempt budget (that抢跑'd the
//     reviewed nodes and mis-killed running work). Instead ask the coordinator
//     (主链路 LLM) what to do next via a goal_decision task — it sees the reject
//     reason + the global DAG snapshot and decides retry / reshape / proceed /
//     abort. The verify node is parked at 'pending' so the dependency gate keeps
//     it from re-running until the reviewed nodes are re-done and it is
//     explicitly re-armed.
//   - missing verdict (agent forgot to report): fail-open to pass + warn. A
//     broken verifier must not deadlock delivery.
func (s *GoalService) handleVerifyCompleted(ctx context.Context, verify db.GoalSubtask, task db.AgentTaskQueue) {
	// Re-read to get the verdict the CLI may have just written.
	fresh, err := s.Queries.GetGoalSubtask(ctx, verify.ID)
	if err != nil {
		slog.Error("verify sync: reload", "error", err)
		return
	}
	wsID := s.resolveWorkspace(ctx, fresh.GoalRunID)

	verdict := ""
	if fresh.Verdict.Valid {
		verdict = fresh.Verdict.String
	}

	if verdict == "reject" {
		run, rerr := s.Queries.GetGoalRun(ctx, fresh.GoalRunID)
		if rerr != nil {
			slog.Error("verify reject: load run", "error", rerr)
			return
		}
		// The verifier's reason lives in the verdict result JSON ({"reason": ...}),
		// written by SubmitVerdict. Fall back to failure_reason for older rows.
		rejectReason := ""
		if len(fresh.Result) > 0 {
			var r struct {
				Reason string `json:"reason"`
			}
			if json.Unmarshal(fresh.Result, &r) == nil {
				rejectReason = strings.TrimSpace(r.Reason)
			}
		}
		if rejectReason == "" && fresh.FailureReason.Valid {
			rejectReason = fresh.FailureReason.String
		}
		// Park the verify node at 'pending'. It must not run again until the
		// reviewed nodes are re-done and the coordinator re-arms it; the
		// dependency gate in dispatchSubtask enforces that structurally.
		if parked, perr := s.Queries.UpdateGoalSubtaskStatus(ctx, db.UpdateGoalSubtaskStatusParams{
			ID:     fresh.ID,
			Status: "pending",
		}); perr == nil {
			s.broadcastGoalSubtask(ctx, wsID, parked)
			fresh = parked
		}
		// The reviewed nodes (verify's dependencies) are the work to re-judge.
		reviewed := make([]db.GoalSubtask, 0, len(fresh.DependsOn))
		for _, depID := range fresh.DependsOn {
			if dep, derr := s.Queries.GetGoalSubtask(ctx, depID); derr == nil {
				reviewed = append(reviewed, dep)
			}
		}
		// Ask the coordinator how to proceed. The judged node is the verifier
		// itself; the coordinator decides whether to retry the reviewed work,
		// reshape it, accept it (proceed), or abort.
		if s.dispatchDecisionTask(ctx, run, fresh.ID, reviewed, "reject", rejectReason) {
			s.recomputeGoalStatus(ctx, fresh.GoalRunID)
			return
		}
		// No coordinator available → fail-open: accept the work so delivery is not
		// deadlocked by a reject we cannot adjudicate (mirrors the missing-verdict
		// fail-open below). Mark the verify completed with its reject verdict and
		// unblock downstream.
		slog.Warn("verify reject: no coordinator to adjudicate — failing open to accept",
			"goal_subtask_id", util.UUIDToString(fresh.ID))
		if done, derr := s.Queries.SetGoalSubtaskVerdict(ctx, db.SetGoalSubtaskVerdictParams{
			ID:      fresh.ID,
			Verdict: pgtype.Text{String: "reject", Valid: true},
			Result:  task.Result,
		}); derr == nil {
			s.broadcastGoalSubtask(ctx, wsID, done)
		}
		s.handleSubtaskTerminal(ctx, fresh.GoalRunID, fresh.ID, true)
		return
	}

	// pass (or missing verdict → fail-open to pass). Finalize the verify node
	// to completed with a pass verdict so its downstream can unlock (deps must
	// be 'completed'). SubmitVerdict left it 'running'; this is where it lands.
	if verdict == "" {
		slog.Warn("verify node completed without a verdict — defaulting to pass",
			"goal_subtask_id", util.UUIDToString(fresh.ID))
	}
	if finalized, err := s.Queries.SetGoalSubtaskVerdict(ctx, db.SetGoalSubtaskVerdictParams{
		ID:      fresh.ID,
		Verdict: pgtype.Text{String: "pass", Valid: true},
		Result:  task.Result,
	}); err == nil {
		fresh = finalized
	}
	s.broadcastGoalSubtask(ctx, wsID, fresh)
	// A passing verify node is terminal-success → unblock its downstream.
	s.handleSubtaskTerminal(ctx, fresh.GoalRunID, fresh.ID, true)
}

// SyncPlanningFromTask handles a goal-planning task reaching a terminal state.
// The planning task (squad leader decomposing the goal) carries no
// goal_subtask_id — it lives only in context JSONB — so SyncSubtaskFromTask
// never sees it. Without this hook a failed planning task (agent error, e.g.
// no AI credential) or a leader that finished without calling `multica goal
// plan` would leave the goal stuck in 'planning' forever.
//
// Rule: if the planning task ends and the goal is still in 'planning' (no plan
// landed via SubmitPlan, which would have flipped it to 'executing'), fail the
// goal with a reason. If a plan DID land, the goal already moved on — no-op.
func (s *GoalService) SyncPlanningFromTask(ctx context.Context, task db.AgentTaskQueue) {
	gc, ok := s.parseGoalPlanningContext(task)
	if !ok {
		return
	}
	goalRunID, err := util.ParseUUID(gc.GoalRunID)
	if err != nil {
		return
	}
	run, err := s.Queries.GetGoalRun(ctx, goalRunID)
	if err != nil {
		return
	}
	// If the plan already landed, the goal left 'planning' — nothing to do.
	if run.Status != "planning" {
		return
	}

	reason := "planning did not produce a plan"
	switch task.Status {
	case "failed", "cancelled":
		reason = "planning task failed"
		if task.FailureReason.Valid {
			reason = "planning failed: " + task.FailureReason.String
		} else if task.Error.Valid {
			reason = "planning failed: " + task.Error.String
		}
	case "completed":
		// Completed but goal still 'planning' → the leader finished without
		// submitting a plan via `multica goal plan`.
		reason = "planning completed without submitting a plan"
	}

	updated, err := s.Queries.CompleteGoalRun(ctx, db.CompleteGoalRunParams{
		ID:            goalRunID,
		Status:        "failed",
		FailureReason: pgtype.Text{String: reason, Valid: true},
	})
	if err != nil {
		slog.Error("goal planning sync: fail goal", "error", err)
		return
	}
	slog.Info("goal planning ended without a plan, marking failed",
		"goal_run_id", gc.GoalRunID, "task_status", task.Status, "reason", reason)
	s.broadcastGoalRun(ctx, updated)
}

// SyncSummaryFromTask lands the goal's terminal status once the PMO summary
// task finishes. The summary's content lives in its task_messages (surfaced as
// the tail of the main session in ④); here we only flip the goal_run to its
// final status, which maybeDispatchSummary deferred. A failed/cancelled summary
// still finalizes — the subtask work is done; a missing wrap-up must not strand
// the goal in 'executing'.
func (s *GoalService) SyncSummaryFromTask(ctx context.Context, task db.AgentTaskQueue) {
	gc, ok := s.parseGoalSummaryContext(task)
	if !ok {
		return
	}
	goalRunID, err := util.ParseUUID(gc.GoalRunID)
	if err != nil {
		return
	}
	run, err := s.Queries.GetGoalRun(ctx, goalRunID)
	if err != nil {
		return
	}
	// Only finalize from the executing state the summary was dispatched in.
	if run.Status != "executing" {
		return
	}

	failureReason := ""
	if gc.Outcome == "partial" {
		failureReason = "some subtasks failed or were blocked"
	} else if gc.Outcome == "failed" {
		failureReason = "all subtasks failed or blocked"
	}
	s.finalizeGoalRun(ctx, goalRunID, gc.Outcome, failureReason)
	slog.Info("goal finalized after PMO summary",
		"goal_run_id", gc.GoalRunID, "outcome", gc.Outcome, "summary_task_status", task.Status)
}

// SyncDecisionFromTask is the fail-safe for a 下一步判断 task that ends without
// the coordinator reporting a decision (agent error, or the leader finished but
// never called `multica goal decide`). If the judged node is still 'failed'
// (DecideSubtask would have moved it to skipped/ready), we fall back to the
// original behavior — block the downstream — so the goal never strands in
// 'executing' waiting on a verdict that will never come.
func (s *GoalService) SyncDecisionFromTask(ctx context.Context, task db.AgentTaskQueue) {
	gc, ok := s.parseGoalDecisionContext(task)
	if !ok {
		return
	}
	// Only act on a terminal decision task.
	switch task.Status {
	case "completed", "failed", "cancelled":
	default:
		return
	}
	subtaskID, err := util.ParseUUID(gc.GoalSubtaskID)
	if err != nil {
		return
	}
	st, err := s.Queries.GetGoalSubtask(ctx, subtaskID)
	if err != nil {
		return
	}
	// If the coordinator already decided, the node left 'failed' — nothing to do.
	if st.Status != "failed" {
		return
	}
	goalRunID, err := util.ParseUUID(gc.GoalRunID)
	if err != nil {
		return
	}
	slog.Info("goal decision task ended without a verdict, blocking downstream",
		"goal_run_id", gc.GoalRunID, "failed_subtask_id", gc.GoalSubtaskID, "task_status", task.Status)
	s.blockDownstream(ctx, goalRunID, subtaskID)
}

// parseGoalDecisionContext extracts a goal-decision context from a task, or
// false when the task is not a goal-decision task. FK-less, like planning.
func (s *GoalService) parseGoalDecisionContext(task db.AgentTaskQueue) (GoalDecisionContext, bool) {
	if task.GoalSubtaskID.Valid || task.IssueID.Valid || task.ChatSessionID.Valid || task.AutopilotRunID.Valid {
		return GoalDecisionContext{}, false
	}
	if len(task.Context) == 0 {
		return GoalDecisionContext{}, false
	}
	var gc GoalDecisionContext
	if err := json.Unmarshal(task.Context, &gc); err != nil {
		return GoalDecisionContext{}, false
	}
	if gc.Type != GoalDecisionContextType {
		return GoalDecisionContext{}, false
	}
	return gc, true
}

// parseGoalSummaryContext extracts a goal-summary context from a task, or false
// when the task is not a goal-summary task. Like planning, summary tasks carry
// no FK link — the context JSONB is the only signal.
func (s *GoalService) parseGoalSummaryContext(task db.AgentTaskQueue) (GoalSummaryContext, bool) {
	if task.GoalSubtaskID.Valid || task.IssueID.Valid || task.ChatSessionID.Valid || task.AutopilotRunID.Valid {
		return GoalSummaryContext{}, false
	}
	if len(task.Context) == 0 {
		return GoalSummaryContext{}, false
	}
	var gc GoalSummaryContext
	if err := json.Unmarshal(task.Context, &gc); err != nil {
		return GoalSummaryContext{}, false
	}
	if gc.Type != GoalSummaryContextType {
		return GoalSummaryContext{}, false
	}
	return gc, true
}

// parseGoalPlanningContext extracts a goal-planning context from a task, or
// false when the task is not a goal-planning task.
func (s *GoalService) parseGoalPlanningContext(task db.AgentTaskQueue) (GoalPlanningContext, bool) {
	if task.GoalSubtaskID.Valid || task.IssueID.Valid || task.ChatSessionID.Valid || task.AutopilotRunID.Valid {
		return GoalPlanningContext{}, false
	}
	if len(task.Context) == 0 {
		return GoalPlanningContext{}, false
	}
	var gc GoalPlanningContext
	if err := json.Unmarshal(task.Context, &gc); err != nil {
		return GoalPlanningContext{}, false
	}
	if gc.Type != GoalPlanningContextType {
		return GoalPlanningContext{}, false
	}
	return gc, true
}

// SyncSubtaskFromTask is the completion hook: when a task carrying a
// goal_subtask_id reaches a terminal state, update the subtask, then unlock
// downstream (on success) or block it (on failure). Called from the
// EventTaskCompleted / EventTaskFailed listener.
func (s *GoalService) SyncSubtaskFromTask(ctx context.Context, task db.AgentTaskQueue) {
	if !task.GoalSubtaskID.Valid {
		return
	}
	subtaskID := task.GoalSubtaskID
	st, err := s.Queries.GetGoalSubtask(ctx, subtaskID)
	if err != nil {
		slog.Error("goal subtask sync: load subtask", "error", err)
		return
	}

	switch task.Status {
	case "completed":
		if st.Kind == "verify" {
			s.handleVerifyCompleted(ctx, st, task)
			return
		}
		updated, err := s.Queries.CompleteGoalSubtask(ctx, db.CompleteGoalSubtaskParams{
			ID:     subtaskID,
			Result: task.Result,
		})
		if err != nil {
			slog.Error("goal subtask complete", "error", err)
			return
		}
		s.broadcastGoalSubtask(ctx, s.resolveWorkspace(ctx, st.GoalRunID), updated)
		s.handleSubtaskTerminal(ctx, st.GoalRunID, subtaskID, true)

	case "failed":
		// No hardcoded auto-retry. A failed node — for ANY reason, including a
		// transient infra/socket error — goes straight to failed, and the
		// next-step decision (retry / reshape / proceed / abort) is the
		// coordinator's to make via a goal_decision task (handleSubtaskFailure).
		// The attempt count is carried into that decision as information, not
		// used as a blind budget gate. This is the "LLM主链路驱动" policy: the
		// backend never silently decides to re-run; it asks.
		failReason := pgtype.Text{String: "task failed", Valid: true}
		if task.FailureReason.Valid {
			failReason = task.FailureReason
		} else if task.Error.Valid {
			failReason = task.Error
		}
		updated, err := s.Queries.FailGoalSubtask(ctx, db.FailGoalSubtaskParams{
			ID:            subtaskID,
			FailureReason: failReason,
		})
		if err != nil {
			slog.Error("goal subtask fail", "error", err)
			return
		}
		s.broadcastGoalSubtask(ctx, s.resolveWorkspace(ctx, st.GoalRunID), updated)
		s.handleSubtaskTerminal(ctx, st.GoalRunID, subtaskID, false)
	}
}

// handleSubtaskTerminal reacts to a subtask reaching completed/failed:
//   - on success: any dependent whose deps are now ALL satisfied → ready → dispatch.
//   - on failure: ask the coordinator for a 下一步判断 (next-step judgment) when
//     the node has dependents and a coordinator is available; otherwise fall
//     back to the original behavior — block every dependent (transitively).
//
// Then it recomputes the goal_run aggregate status.
func (s *GoalService) handleSubtaskTerminal(ctx context.Context, goalRunID, subtaskID pgtype.UUID, success bool) {
	if success {
		s.unblockDownstream(ctx, goalRunID, subtaskID)
		s.recomputeGoalStatus(ctx, goalRunID)
		return
	}
	s.handleSubtaskFailure(ctx, goalRunID, subtaskID)
}

// unblockDownstream readies + dispatches any dependent of subtaskID whose
// dependencies are now ALL satisfied (completed or skipped). Pure success-edge
// logic; callers recompute the goal afterwards.
func (s *GoalService) unblockDownstream(ctx context.Context, goalRunID, subtaskID pgtype.UUID) {
	dependents, run, statusByID, ok := s.loadDependents(ctx, goalRunID, subtaskID)
	if !ok {
		return
	}
	for _, dep := range dependents {
		// Ready the dependent iff every dependency is satisfied. A dep is
		// satisfied when completed, or skipped (proceed-past via skip / a
		// coordinator "proceed" judgment).
		ready := true
		for _, depID := range dep.DependsOn {
			st := statusByID[util.UUIDToString(depID)]
			if st != "completed" && st != "skipped" {
				ready = false
				break
			}
		}
		// A dependent that is still pending, OR was blocked by this now-
		// satisfied upstream, becomes ready. 'blocked' is included so
		// skip/retry/proceed interventions re-activate downstream that an
		// earlier failure had halted.
		if ready && (dep.Status == "pending" || dep.Status == "blocked") {
			if readied, err := s.Queries.UpdateGoalSubtaskStatus(ctx, db.UpdateGoalSubtaskStatusParams{
				ID:     dep.ID,
				Status: "ready",
			}); err == nil {
				s.broadcastGoalSubtask(ctx, run.WorkspaceID, readied)
				if derr := s.dispatchSubtask(ctx, run, readied); derr != nil {
					slog.Error("dispatch unlocked subtask", "error", derr)
				}
			}
		}
	}
}

// blockDownstream blocks every pending/ready dependent of subtaskID (cascading
// transitively) — the "abort" enactment and the no-coordinator fallback. Then
// recomputes the goal so a fully-terminal DAG rolls up to partial/failed.
func (s *GoalService) blockDownstream(ctx context.Context, goalRunID, subtaskID pgtype.UUID) {
	dependents, run, _, ok := s.loadDependents(ctx, goalRunID, subtaskID)
	if ok {
		for _, dep := range dependents {
			if dep.Status == "pending" || dep.Status == "ready" {
				if blocked, err := s.Queries.UpdateGoalSubtaskStatus(ctx, db.UpdateGoalSubtaskStatusParams{
					ID:     dep.ID,
					Status: "blocked",
				}); err == nil {
					s.broadcastGoalSubtask(ctx, run.WorkspaceID, blocked)
					// Cascade: blocking a node blocks ITS dependents too. The
					// cascade never asks for a judgment — that decision was
					// already made for the head of the chain.
					s.blockDownstream(ctx, goalRunID, dep.ID)
				}
			}
		}
	}
	s.recomputeGoalStatus(ctx, goalRunID)
}

// handleSubtaskFailure is the 下一步判断 fork. A failed node with downstream work
// is NOT blindly cascade-blocked: the coordinator is asked to judge what the
// failure means and how to proceed (proceed / reshape / abort). The downstream
// stays pending in the meantime, so the goal remains 'executing' (not finalized)
// until the coordinator decides.
//
// It degrades to the original block-downstream behavior whenever a judgment
// cannot be obtained — no dependents (nothing to judge), no usable coordinator,
// or a decision task is already in flight for this node. Fail-safe by design:
// the worst case is exactly today's behavior, never a stuck goal.
func (s *GoalService) handleSubtaskFailure(ctx context.Context, goalRunID, subtaskID pgtype.UUID) {
	dependents, run, _, ok := s.loadDependents(ctx, goalRunID, subtaskID)
	if !ok || len(dependents) == 0 {
		// A failed leaf: nothing downstream to judge. Just recompute the rollup.
		s.recomputeGoalStatus(ctx, goalRunID)
		return
	}
	if s.dispatchDecisionTask(ctx, run, subtaskID, dependents, "failure", "") {
		// Judgment dispatched; leave downstream pending. The goal stays
		// 'executing' (dependents are still non-terminal) until the coordinator
		// reports via `multica goal decide`.
		s.recomputeGoalStatus(ctx, goalRunID)
		return
	}
	// No judgment possible → original behavior: block the whole downstream.
	s.blockDownstream(ctx, goalRunID, subtaskID)
}

// loadDependents is the shared preamble of the (un)block helpers: the direct
// dependents of subtaskID, the goal run, and a subtask-id → status map. ok is
// false on any load error (the caller bails).
func (s *GoalService) loadDependents(
	ctx context.Context, goalRunID, subtaskID pgtype.UUID,
) ([]db.GoalSubtask, db.GoalRun, map[string]string, bool) {
	dependents, err := s.Queries.ListGoalSubtaskDependents(ctx, db.ListGoalSubtaskDependentsParams{
		GoalRunID:    goalRunID,
		DependencyID: subtaskID,
	})
	if err != nil {
		slog.Error("list dependents", "error", err)
		return nil, db.GoalRun{}, nil, false
	}
	run, err := s.Queries.GetGoalRun(ctx, goalRunID)
	if err != nil {
		slog.Error("load goal run for terminal", "error", err)
		return nil, db.GoalRun{}, nil, false
	}
	allSubtasks, err := s.Queries.ListGoalSubtasks(ctx, goalRunID)
	if err != nil {
		slog.Error("list subtasks for terminal", "error", err)
		return nil, db.GoalRun{}, nil, false
	}
	statusByID := make(map[string]string, len(allSubtasks))
	for _, st := range allSubtasks {
		statusByID[util.UUIDToString(st.ID)] = st.Status
	}
	return dependents, run, statusByID, true
}

// buildDagSnapshot renders EVERY node of the goal as a compact line
// "[status] (kind) title" so a coordinator judging one node sees the whole
// board: what is still running, what completed, what is waiting. This is what
// lets the LLM say "node B is still running, don't decide yet" instead of
// treating the judged node as the entire world.
func (s *GoalService) buildDagSnapshot(ctx context.Context, goalRunID, focusID pgtype.UUID) string {
	subtasks, err := s.Queries.ListGoalSubtasks(ctx, goalRunID)
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, st := range subtasks {
		marker := "  "
		if st.ID == focusID {
			marker = "▶ " // the node being judged
		}
		kind := st.Kind
		if kind == "" {
			kind = "execute"
		}
		fmt.Fprintf(&b, "%s[%s] (%s) %s\n", marker, st.Status, kind, st.Title)
	}
	return strings.TrimRight(b.String(), "\n")
}

// dispatchDecisionTask enqueues one 下一步判断 task for the coordinator (squad
// leader) about a node that needs a next-step judgment — either an execute node
// that FAILED (trigger="failure") or a verify node that REJECTED its review
// (trigger="reject"). Returns true when a decision task was dispatched (caller
// leaves the DAG paused); false when none could be — no coordinator, or a
// decision is already in flight — so the caller falls back to its safe default.
//
// The payload carries a global DAG snapshot + attempt count + reject reason so
// the coordinator decides with full context, not blind to what else is running.
func (s *GoalService) dispatchDecisionTask(
	ctx context.Context, run db.GoalRun, subtaskID pgtype.UUID, dependents []db.GoalSubtask,
	trigger, rejectReason string,
) bool {
	// Idempotency: one in-flight decision per node.
	if _, err := s.Queries.GetActiveDecisionTaskForSubtask(ctx, util.UUIDToString(subtaskID)); err == nil {
		return false
	}
	st, err := s.Queries.GetGoalSubtask(ctx, subtaskID)
	if err != nil {
		return false
	}
	squad, err := s.Queries.GetSquad(ctx, run.SquadID)
	if err != nil {
		return false
	}
	leader, err := s.Queries.GetAgent(ctx, squad.LeaderID)
	if err != nil || leader.ArchivedAt.Valid || !leader.RuntimeID.Valid {
		return false // no usable coordinator → fall back to default behavior
	}

	var down strings.Builder
	for _, d := range dependents {
		fmt.Fprintf(&down, "- %s\n", d.Title)
	}

	failureReason := ""
	if st.FailureReason.Valid {
		failureReason = st.FailureReason.String
	}
	payload := GoalDecisionContext{
		Type:          GoalDecisionContextType,
		GoalRunID:     util.UUIDToString(run.ID),
		GoalSubtaskID: util.UUIDToString(subtaskID),
		WorkspaceID:   util.UUIDToString(run.WorkspaceID),
		GoalTitle:     run.Title,
		SubtaskTitle:  st.Title,
		SubtaskSpec:   st.Spec,
		FailureReason: failureReason,
		Downstream:    strings.TrimSpace(down.String()),
		Trigger:       trigger,
		RejectReason:  rejectReason,
		Attempts:      st.Attempt,
		DagSnapshot:   s.buildDagSnapshot(ctx, run.ID, subtaskID),
	}
	contextJSON, err := json.Marshal(payload)
	if err != nil {
		return false
	}

	task, err := s.Queries.CreateQuickCreateTask(ctx, db.CreateQuickCreateTaskParams{
		AgentID:   leader.ID,
		RuntimeID: leader.RuntimeID,
		Priority:  priorityToInt("high"),
		Context:   contextJSON,
	})
	if err != nil {
		slog.Error("decision: create task", "error", err)
		return false
	}

	slog.Info("goal next-step judgment dispatched",
		"goal_run_id", util.UUIDToString(run.ID),
		"judged_subtask_id", util.UUIDToString(subtaskID),
		"trigger", trigger,
		"task_id", util.UUIDToString(task.ID),
		"dependents", len(dependents),
	)
	s.TaskSvc.NotifyTaskEnqueued(ctx, task)
	return true
}

// DecideSubtask enacts the coordinator's 下一步判断, reported via the CLI
// (`multica goal decide <subtask> retry|reshape|proceed|abort`). The judged node
// is either a FAILED execute node or a verify node that REJECTED its review
// (parked at 'pending' by handleVerifyCompleted). The four decisions:
//   - retry:   re-run the node as-is, fresh attempt budget (no spec change). For
//     a verify-reject, re-run the REVIEWED nodes, then re-arm the verify.
//   - reshape: rewrite the spec (--spec), then re-run. For a verify-reject the
//     spec applies to the single reviewed node (when there is exactly
//     one); with multiple reviewed nodes, reshape is rejected — use
//     `multica goal edit-spec` on the specific node first, then retry.
//   - proceed: accept/skip. A failed node is skipped (non-blocking terminal); a
//     rejected verify is accepted as pass → downstream unblocks.
//   - abort:   block the whole downstream.
//
// Gated on workspace.
func (s *GoalService) DecideSubtask(
	ctx context.Context, workspaceID, subtaskID pgtype.UUID, decision, reshapeSpec string,
) (db.GoalSubtask, error) {
	st, run, err := s.loadSubtaskForIntervention(ctx, workspaceID, subtaskID)
	if err != nil {
		return db.GoalSubtask{}, err
	}

	// Idempotency guard: a node is "awaiting a decision" only when it FAILED (an
	// execute failure) or is a verify node parked at 'pending' with a 'reject'
	// verdict. Anything else means the decision was already enacted (the agent
	// can call `goal decide` more than once in a single task) — re-enacting would
	// re-dispatch / re-skip a node that has already moved on. No-op those.
	isVerifyReject := st.Kind == "verify" && st.Status == "pending" &&
		st.Verdict.Valid && st.Verdict.String == "reject"
	awaitingDecision := st.Status == "failed" || isVerifyReject
	if !awaitingDecision {
		slog.Info("goal decide ignored: node not awaiting a decision",
			"goal_subtask_id", util.UUIDToString(subtaskID),
			"status", st.Status, "kind", st.Kind, "decision", decision)
		return st, nil
	}

	switch decision {
	case "retry":
		run = s.reviveGoalIfTerminal(ctx, run)
		if isVerifyReject {
			return s.rerunReviewedThenVerify(ctx, run, st, "")
		}
		return s.rearmAndDispatch(ctx, run, subtaskID)

	case "reshape":
		if isVerifyReject {
			run = s.reviveGoalIfTerminal(ctx, run)
			return s.rerunReviewedThenVerify(ctx, run, st, reshapeSpec)
		}
		if strings.TrimSpace(reshapeSpec) != "" {
			if _, uerr := s.Queries.UpdateGoalSubtaskSpec(ctx, db.UpdateGoalSubtaskSpecParams{
				ID:   subtaskID,
				Spec: reshapeSpec,
			}); uerr != nil {
				return db.GoalSubtask{}, fmt.Errorf("reshape (edit spec): %w", uerr)
			}
		}
		run = s.reviveGoalIfTerminal(ctx, run)
		return s.rearmAndDispatch(ctx, run, subtaskID)

	case "proceed":
		if isVerifyReject {
			// Accept the reviewed work despite the reject: complete the verify as
			// a pass-equivalent terminal so its downstream unblocks.
			run = s.reviveGoalIfTerminal(ctx, run)
			accepted, aerr := s.Queries.SetGoalSubtaskVerdict(ctx, db.SetGoalSubtaskVerdictParams{
				ID:      subtaskID,
				Verdict: pgtype.Text{String: "pass", Valid: true},
				Result:  st.Result,
			})
			if aerr != nil {
				return db.GoalSubtask{}, fmt.Errorf("proceed (accept review): %w", aerr)
			}
			s.broadcastGoalSubtask(ctx, run.WorkspaceID, accepted)
			s.handleSubtaskTerminal(ctx, run.ID, subtaskID, true)
			return accepted, nil
		}
		skipped, serr := s.Queries.SkipGoalSubtask(ctx, subtaskID)
		if serr != nil {
			return db.GoalSubtask{}, fmt.Errorf("proceed (skip): %w", serr)
		}
		run = s.reviveGoalIfTerminal(ctx, run)
		s.broadcastGoalSubtask(ctx, run.WorkspaceID, skipped)
		s.handleSubtaskTerminal(ctx, run.ID, subtaskID, true)
		return skipped, nil

	case "abort":
		s.blockDownstream(ctx, run.ID, subtaskID)
		updated, gerr := s.Queries.GetGoalSubtask(ctx, subtaskID)
		if gerr != nil {
			return st, nil
		}
		return updated, nil

	default:
		return db.GoalSubtask{}, fmt.Errorf("decision must be 'retry', 'reshape', 'proceed', or 'abort'")
	}
}

// rerunReviewedThenVerify re-runs the nodes a rejected verify node reviewed, then
// leaves the verify parked at 'pending'. The dependency gate re-arms and
// redispatches the verify automatically once every reviewed node completes again
// (via unblockDownstream) — so the verifier always judges the FRESH output and
// never抢跑s while the reviewed nodes are still running.
//
// When reshapeSpec is non-empty it applies to the reviewed node, but only when
// there is exactly one — with several reviewed nodes the target is ambiguous.
func (s *GoalService) rerunReviewedThenVerify(
	ctx context.Context, run db.GoalRun, verify db.GoalSubtask, reshapeSpec string,
) (db.GoalSubtask, error) {
	reviewedIDs := verify.DependsOn
	if len(reviewedIDs) == 0 {
		return db.GoalSubtask{}, fmt.Errorf("verify node has no reviewed dependencies to re-run")
	}
	if strings.TrimSpace(reshapeSpec) != "" {
		if len(reviewedIDs) != 1 {
			return db.GoalSubtask{}, fmt.Errorf("reshape needs a single target: this review covers %d nodes — edit the specific node's spec, then retry", len(reviewedIDs))
		}
		if _, uerr := s.Queries.UpdateGoalSubtaskSpec(ctx, db.UpdateGoalSubtaskSpecParams{
			ID:   reviewedIDs[0],
			Spec: reshapeSpec,
		}); uerr != nil {
			return db.GoalSubtask{}, fmt.Errorf("reshape reviewed node: %w", uerr)
		}
	}
	// Ensure the verify node is parked at pending (the gate keeps it there until
	// the reviewed nodes complete again).
	if verify.Status != "pending" {
		if parked, perr := s.Queries.UpdateGoalSubtaskStatus(ctx, db.UpdateGoalSubtaskStatusParams{
			ID:     verify.ID,
			Status: "pending",
		}); perr == nil {
			s.broadcastGoalSubtask(ctx, run.WorkspaceID, parked)
			verify = parked
		}
	}
	// The reviewer's reason becomes the incremental instruction for the re-run,
	// so each reviewed node's (session-resumed) agent addresses the specific
	// points instead of starting over. It lives in the verdict result JSON.
	feedback := ""
	if len(verify.Result) > 0 {
		var r struct {
			Reason string `json:"reason"`
		}
		if json.Unmarshal(verify.Result, &r) == nil {
			feedback = strings.TrimSpace(r.Reason)
		}
	}
	if feedback == "" && verify.FailureReason.Valid {
		feedback = verify.FailureReason.String
	}
	// Re-run each reviewed node with a fresh budget + the reviewer's feedback.
	for _, depID := range reviewedIDs {
		rearmed, rerr := s.Queries.RearmGoalSubtaskFresh(ctx, depID)
		if rerr != nil {
			slog.Error("verify reject retry: rearm reviewed", "error", rerr,
				"reviewed_subtask_id", util.UUIDToString(depID))
			continue
		}
		s.broadcastGoalSubtask(ctx, run.WorkspaceID, rearmed)
		if derr := s.dispatchSubtaskWithFeedback(ctx, run, rearmed, feedback); derr != nil {
			slog.Error("verify reject retry: dispatch reviewed", "error", derr,
				"reviewed_subtask_id", util.UUIDToString(depID))
		}
	}
	return verify, nil
}

// recomputeGoalStatus rolls subtask states up to the goal_run:
//   - all completed                          → completed
//   - any failed/blocked but some completed  → partial
//   - all failed/blocked, none completed     → failed
//   - otherwise (work still pending/running) → leave executing.
func (s *GoalService) recomputeGoalStatus(ctx context.Context, goalRunID pgtype.UUID) {
	subtasks, err := s.Queries.ListGoalSubtasks(ctx, goalRunID)
	if err != nil || len(subtasks) == 0 {
		return
	}
	var completed, failed, blocked, terminal int
	for _, st := range subtasks {
		switch st.Status {
		case "completed":
			completed++
			terminal++
		case "failed":
			failed++
			terminal++
		case "blocked", "skipped":
			blocked++
			terminal++
		}
	}
	if terminal < len(subtasks) {
		return // still work in flight
	}

	var newStatus, failureReason string
	switch {
	case completed == len(subtasks):
		newStatus = "completed"
	case completed > 0:
		newStatus = "partial"
		failureReason = fmt.Sprintf("%d failed, %d blocked", failed, blocked)
	default:
		newStatus = "failed"
		failureReason = "all subtasks failed or blocked"
	}

	// PMO 收口: before completing, dispatch one summary task so the leader reads
	// the subtask outputs and writes the final deliverable. The summary streams
	// into its own task; on its completion finalizeGoalAfterSummary lands the
	// terminal status. Skipped when no work succeeded (nothing to synthesize) or
	// when a summary already ran (idempotent — guards re-entry from retries).
	if completed > 0 {
		if dispatched := s.maybeDispatchSummary(ctx, goalRunID, newStatus, failureReason, subtasks); dispatched {
			return // stay 'executing'; finalize lands when the summary completes
		}
	}

	s.finalizeGoalRun(ctx, goalRunID, newStatus, failureReason)
}

// finalizeGoalRun lands the terminal status + broadcasts. Split out so both the
// no-summary path (recomputeGoalStatus) and the post-summary path
// (finalizeGoalAfterSummary) share one completion point.
func (s *GoalService) finalizeGoalRun(ctx context.Context, goalRunID pgtype.UUID, status, failureReason string) {
	var reason pgtype.Text
	if failureReason != "" {
		reason = pgtype.Text{String: failureReason, Valid: true}
	}
	updated, err := s.Queries.CompleteGoalRun(ctx, db.CompleteGoalRunParams{
		ID:            goalRunID,
		Status:        status,
		FailureReason: reason,
	})
	if err != nil {
		slog.Error("complete goal run", "error", err)
		return
	}
	s.broadcastGoalRun(ctx, updated)

	// Auto-fix report-back: a `partial` terminal status is the "needs more info"
	// product state (design decision B) — the verify node could not reproduce or
	// the fix is incomplete. Record the reason on the linked issue's metadata.
	// resolveAutofixIssue is itself the autofix discriminator: a no-op when this
	// run is an ordinary task-mode goal (no issue carries its latest_goal_run_id).
	if status == "partial" {
		s.ReportAutofixNeedsInfo(ctx, updated.WorkspaceID, goalRunID, failureReason)
	}
}

// maybeDispatchSummary enqueues a single PMO summary task when all subtasks are
// terminal and no summary has run yet. Returns true when a summary was
// dispatched (caller must leave the goal 'executing'); false when it should
// finalize now (summary already exists / no leader / dispatch failed — never
// strand the goal).
func (s *GoalService) maybeDispatchSummary(
	ctx context.Context,
	goalRunID pgtype.UUID,
	outcome, failureReason string,
	subtasks []db.GoalSubtask,
) bool {
	// Idempotency: if a summary task already exists, the goal is either mid-
	// summary or done — don't dispatch a second one.
	if _, err := s.Queries.GetSummaryTaskForGoal(ctx, util.UUIDToString(goalRunID)); err == nil {
		return false
	}

	run, err := s.Queries.GetGoalRun(ctx, goalRunID)
	if err != nil {
		slog.Error("summary: load goal run", "error", err)
		return false
	}
	squad, err := s.Queries.GetSquad(ctx, run.SquadID)
	if err != nil {
		slog.Error("summary: load squad", "error", err)
		return false
	}
	leader, err := s.Queries.GetAgent(ctx, squad.LeaderID)
	if err != nil || leader.ArchivedAt.Valid || !leader.RuntimeID.Valid {
		// No usable PMO to summarize — finalize without a summary rather than
		// stranding the goal in 'executing' forever.
		return false
	}

	digest := buildSubtaskDigest(subtasks)

	payload := GoalSummaryContext{
		Type:          GoalSummaryContextType,
		GoalRunID:     util.UUIDToString(run.ID),
		WorkspaceID:   util.UUIDToString(run.WorkspaceID),
		GoalTitle:     run.Title,
		Goal:          run.Goal,
		Outcome:       outcome,
		SubtaskDigest: digest,
	}
	contextJSON, err := json.Marshal(payload)
	if err != nil {
		slog.Error("summary: marshal context", "error", err)
		return false
	}

	task, err := s.Queries.CreateQuickCreateTask(ctx, db.CreateQuickCreateTaskParams{
		AgentID:   leader.ID,
		RuntimeID: leader.RuntimeID,
		Priority:  priorityToInt("high"),
		Context:   contextJSON,
	})
	if err != nil {
		slog.Error("summary: create task", "error", err)
		return false
	}

	slog.Info("goal summary dispatched",
		"goal_run_id", util.UUIDToString(run.ID),
		"task_id", util.UUIDToString(task.ID),
		"outcome", outcome,
	)
	s.TaskSvc.NotifyTaskEnqueued(ctx, task)
	return true
}

// buildSubtaskDigest assembles every subtask's title + spec + result into a
// single block the PMO summary prompt can synthesize. Mirrors buildReviewTarget
// (used by verify nodes) but spans all subtasks, ordered by seq.
func buildSubtaskDigest(subtasks []db.GoalSubtask) string {
	var b strings.Builder
	for _, st := range subtasks {
		fmt.Fprintf(&b, "### [%s] %s\n", st.Status, st.Title)
		if len(st.Result) > 0 {
			fmt.Fprintf(&b, "Result: %s\n", string(st.Result))
		} else if st.FailureReason.Valid && st.FailureReason.String != "" {
			fmt.Fprintf(&b, "Failure: %s\n", st.FailureReason.String)
		} else {
			b.WriteString("Result: (no structured result reported)\n")
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

// PersistGoal dispatches a one-click snapshot of the task's content into its
// bound project repo. It is the "持久化到工程" entry: the platform DB stays the
// main truth; this writes a harness-structured export (docs/task/{slug}/) so any
// tool opening the repo can pick the task up.
//
// Iron laws honored: the SERVER never touches the repo — it only enqueues a
// task; the leader AGENT (running on the daemon machine, inside the project's
// local_directory) authors the files. The backend never calls an LLM.
//
// Gating: the goal must be bound to a project that carries a local_directory
// resource (a repo on a daemon). Without one there is nowhere to write — return
// an error so the caller (and the disabled button) can explain why.
//
// Repeatable: persist is a snapshot, so re-running overwrites the same slug
// directory. No idempotency guard — each click re-exports current DB state.
func (s *GoalService) PersistGoal(ctx context.Context, workspaceID, goalRunID pgtype.UUID) (db.AgentTaskQueue, error) {
	run, err := s.Queries.GetGoalRunInWorkspace(ctx, db.GetGoalRunInWorkspaceParams{
		ID:          goalRunID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("load goal: %w", err)
	}
	if !run.ProjectID.Valid {
		return db.AgentTaskQueue{}, fmt.Errorf("task is not bound to a project — bind a project with a local repo before persisting")
	}
	// Gate on the project actually having a local_directory (a repo on a daemon).
	if !s.projectHasLocalDir(ctx, run.ProjectID) {
		return db.AgentTaskQueue{}, fmt.Errorf("project has no local repo (local_directory) to persist into")
	}

	squad, err := s.Queries.GetSquad(ctx, run.SquadID)
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("load squad: %w", err)
	}
	leader, err := s.Queries.GetAgent(ctx, squad.LeaderID)
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("load squad leader: %w", err)
	}
	if leader.ArchivedAt.Valid {
		return db.AgentTaskQueue{}, fmt.Errorf("squad leader is archived")
	}
	if !leader.RuntimeID.Valid {
		return db.AgentTaskQueue{}, fmt.Errorf("squad leader has no runtime")
	}

	subtasks, err := s.Queries.ListGoalSubtasks(ctx, run.ID)
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("list subtasks: %w", err)
	}

	payload := GoalPersistContext{
		Type:          GoalPersistContextType,
		GoalRunID:     util.UUIDToString(run.ID),
		WorkspaceID:   util.UUIDToString(run.WorkspaceID),
		ProjectID:     util.UUIDToString(run.ProjectID),
		GoalTitle:     run.Title,
		Goal:          run.Goal,
		Slug:          goalTaskSlug(run),
		SubtaskDigest: buildSubtaskDigest(subtasks),
		Outcome:       run.Status,
	}
	contextJSON, err := json.Marshal(payload)
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("marshal persist context: %w", err)
	}

	task, err := s.Queries.CreateQuickCreateTask(ctx, db.CreateQuickCreateTaskParams{
		AgentID:   leader.ID,
		RuntimeID: leader.RuntimeID,
		Priority:  priorityToInt("high"),
		Context:   contextJSON,
	})
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("create persist task: %w", err)
	}

	slog.Info("goal persist dispatched",
		"goal_run_id", util.UUIDToString(run.ID),
		"project_id", util.UUIDToString(run.ProjectID),
		"slug", payload.Slug,
		"task_id", util.UUIDToString(task.ID),
	)
	s.TaskSvc.NotifyTaskEnqueued(ctx, task)
	return task, nil
}

// projectHasLocalDir reports whether the project carries a local_directory
// resource (a repo bound to a daemon) — the precondition for repo persistence.
func (s *GoalService) projectHasLocalDir(ctx context.Context, projectID pgtype.UUID) bool {
	resources, err := s.Queries.ListProjectResources(ctx, projectID)
	if err != nil {
		return false
	}
	for _, r := range resources {
		if r.ResourceType == "local_directory" {
			return true
		}
	}
	return false
}

// goalTaskSlug builds the docs/task/{slug} directory name as {YYMMDD}-{kebab},
// matching the dev-roleplay-harness / AI-GAME convention. The date comes from
// the goal_run's creation time (NOT now), so re-persisting the same task always
// targets the SAME directory — a snapshot overwrite, not a new dir per click.
func goalTaskSlug(run db.GoalRun) string {
	date := "000000"
	if run.CreatedAt.Valid {
		date = run.CreatedAt.Time.Format("060102")
	}
	title := strings.TrimSpace(run.Title)
	if title == "" {
		title = strings.TrimSpace(run.Goal)
	}
	kebab := kebabCase(title)
	if kebab == "" {
		// Fall back to the run id's first segment so the dir is still unique.
		kebab = "task-" + strings.SplitN(util.UUIDToString(run.ID), "-", 2)[0]
	}
	return date + "-" + kebab
}

// kebabCase lowercases, replaces runs of non-alphanumeric runes with a single
// hyphen, and trims leading/trailing hyphens. Non-ASCII letters/digits (e.g.
// CJK) are kept so titles like "贪吃蛇" still produce a usable slug. Bounded to
// 50 runes so the directory name stays sane.
func kebabCase(s string) string {
	var b strings.Builder
	lastHyphen := true // suppress a leading hyphen
	runes := 0
	for _, r := range strings.ToLower(s) {
		if runes >= 50 {
			break
		}
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen {
				b.WriteRune('-')
				lastHyphen = true
			}
		}
		runes++
	}
	return strings.Trim(b.String(), "-")
}

func (s *GoalService) resolveWorkspace(ctx context.Context, goalRunID pgtype.UUID) pgtype.UUID {
	run, err := s.Queries.GetGoalRun(ctx, goalRunID)
	if err != nil {
		return pgtype.UUID{}
	}
	return run.WorkspaceID
}

func (s *GoalService) broadcastGoalRun(ctx context.Context, run db.GoalRun) {
	if s.Bus == nil || !run.WorkspaceID.Valid {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventGoalRunUpdated,
		WorkspaceID: util.UUIDToString(run.WorkspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"goal_run_id": util.UUIDToString(run.ID),
			"squad_id":    util.UUIDToString(run.SquadID),
			"status":      run.Status,
		},
	})
}

func (s *GoalService) broadcastGoalSubtask(ctx context.Context, workspaceID pgtype.UUID, st db.GoalSubtask) {
	if s.Bus == nil || !workspaceID.Valid {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventGoalSubtaskUpdated,
		WorkspaceID: util.UUIDToString(workspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"goal_run_id":     util.UUIDToString(st.GoalRunID),
			"goal_subtask_id": util.UUIDToString(st.ID),
			"status":          st.Status,
		},
	})
}
