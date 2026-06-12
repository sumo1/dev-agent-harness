package service

import (
	"reflect"
	"testing"
)

func TestReadAutofixMetadata_EmptyAndMalformed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   map[string]any
	}{
		{"nil map", nil},
		{"no autofix key", map[string]any{"pr_number": float64(7)}},
		{"autofix is not an object", map[string]any{"autofix": "garbage"}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := readAutofixMetadata(tc.in)
			if got.GoalRunIDs == nil {
				t.Fatalf("GoalRunIDs must be non-nil empty slice, got nil")
			}
			if len(got.GoalRunIDs) != 0 || got.LatestGoalRunID != "" ||
				got.Github != nil || got.NeedsInfoReason != "" {
				t.Fatalf("expected empty autofix, got %+v", got)
			}
		})
	}
}

func TestReadAutofixMetadata_NullGoalRunIDs(t *testing.T) {
	t.Parallel()

	// A persisted blob with an explicit null array must decode to an empty
	// (non-nil) slice so append paths don't panic.
	in := map[string]any{"autofix": map[string]any{"goal_run_ids": nil}}
	got := readAutofixMetadata(in)
	if got.GoalRunIDs == nil || len(got.GoalRunIDs) != 0 {
		t.Fatalf("expected empty non-nil slice, got %#v", got.GoalRunIDs)
	}
}

func TestAppendAutofixGoalRun_OnEmpty(t *testing.T) {
	t.Parallel()

	got := appendAutofixGoalRun(nil, "run-1")
	if !reflect.DeepEqual(got.GoalRunIDs, []string{"run-1"}) {
		t.Fatalf("GoalRunIDs = %v, want [run-1]", got.GoalRunIDs)
	}
	if got.LatestGoalRunID != "run-1" {
		t.Fatalf("LatestGoalRunID = %q, want run-1", got.LatestGoalRunID)
	}
}

func TestAppendAutofixGoalRun_OnExisting(t *testing.T) {
	t.Parallel()

	metadata := map[string]any{
		"autofix": map[string]any{
			"goal_run_ids":       []any{"run-1"},
			"latest_goal_run_id": "run-1",
		},
	}
	got := appendAutofixGoalRun(metadata, "run-2")
	if !reflect.DeepEqual(got.GoalRunIDs, []string{"run-1", "run-2"}) {
		t.Fatalf("GoalRunIDs = %v, want [run-1 run-2]", got.GoalRunIDs)
	}
	if got.LatestGoalRunID != "run-2" {
		t.Fatalf("LatestGoalRunID = %q, want run-2", got.LatestGoalRunID)
	}
}

func TestAppendAutofixGoalRun_Idempotent(t *testing.T) {
	t.Parallel()

	metadata := map[string]any{
		"autofix": map[string]any{
			"goal_run_ids":       []any{"run-1", "run-2"},
			"latest_goal_run_id": "run-1",
		},
	}
	// Re-appending an existing id must not duplicate it, but must promote it
	// to latest.
	got := appendAutofixGoalRun(metadata, "run-1")
	if !reflect.DeepEqual(got.GoalRunIDs, []string{"run-1", "run-2"}) {
		t.Fatalf("GoalRunIDs = %v, want [run-1 run-2] (no dup)", got.GoalRunIDs)
	}
	if got.LatestGoalRunID != "run-1" {
		t.Fatalf("LatestGoalRunID = %q, want run-1", got.LatestGoalRunID)
	}
}

func TestSetAutofixGithub(t *testing.T) {
	t.Parallel()

	// On empty metadata.
	got := setAutofixGithub(nil, 1234, "https://github.com/o/r/issues/1234")
	if got.Github == nil || got.Github.IssueNumber != 1234 ||
		got.Github.IssueURL != "https://github.com/o/r/issues/1234" {
		t.Fatalf("github not written correctly: %+v", got.Github)
	}

	// On existing metadata: preserves goal run history.
	metadata := map[string]any{
		"autofix": map[string]any{
			"goal_run_ids":       []any{"run-1"},
			"latest_goal_run_id": "run-1",
		},
	}
	got = setAutofixGithub(metadata, 7, "https://github.com/o/r/issues/7")
	if !reflect.DeepEqual(got.GoalRunIDs, []string{"run-1"}) {
		t.Fatalf("github write clobbered goal_run_ids: %v", got.GoalRunIDs)
	}
	if got.Github == nil || got.Github.IssueNumber != 7 {
		t.Fatalf("github not written correctly on existing metadata: %+v", got.Github)
	}
}

func TestSetAutofixNeedsInfo(t *testing.T) {
	t.Parallel()

	// On empty metadata.
	got := setAutofixNeedsInfo(nil, "could not reproduce")
	if got.NeedsInfoReason != "could not reproduce" {
		t.Fatalf("NeedsInfoReason = %q, want 'could not reproduce'", got.NeedsInfoReason)
	}

	// On existing metadata: preserves github + history.
	metadata := map[string]any{
		"autofix": map[string]any{
			"goal_run_ids": []any{"run-1"},
			"github": map[string]any{
				"issue_number": float64(7),
				"issue_url":    "https://github.com/o/r/issues/7",
			},
		},
	}
	got = setAutofixNeedsInfo(metadata, "need repro steps")
	if got.NeedsInfoReason != "need repro steps" {
		t.Fatalf("NeedsInfoReason = %q", got.NeedsInfoReason)
	}
	if !reflect.DeepEqual(got.GoalRunIDs, []string{"run-1"}) {
		t.Fatalf("needs_info write clobbered goal_run_ids: %v", got.GoalRunIDs)
	}
	if got.Github == nil || got.Github.IssueNumber != 7 {
		t.Fatalf("needs_info write clobbered github: %+v", got.Github)
	}
}
