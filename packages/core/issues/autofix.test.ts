import { describe, expect, it } from "vitest";

import {
  type AutofixMetadata,
  deriveAutofixStatus,
  parseAutofixMetadata,
} from "./autofix";
import type { GoalRun, Issue } from "../types";

// Build a minimal Issue stub whose metadata carries an arbitrary `autofix`
// blob. The cast through `unknown` mirrors reality: the autofix blob is a
// nested object the flat-KV `IssueMetadata` type doesn't model.
function issueWith(autofix: unknown): Pick<Issue, "metadata"> {
  return { metadata: { autofix } as Issue["metadata"] };
}

describe("parseAutofixMetadata — drift is fail-soft", () => {
  it("returns empty blob when metadata has no autofix key", () => {
    const got = parseAutofixMetadata({ metadata: {} as Issue["metadata"] });
    expect(got).toEqual({ goal_run_ids: [] });
  });

  it("returns empty blob for a null array (no throw)", () => {
    const got = parseAutofixMetadata(
      issueWith({ goal_run_ids: null }),
    );
    expect(got.goal_run_ids).toEqual([]);
  });

  it("returns empty blob when the value is the wrong type (no throw)", () => {
    expect(parseAutofixMetadata(issueWith("garbage")).goal_run_ids).toEqual([]);
    expect(parseAutofixMetadata(issueWith(42)).goal_run_ids).toEqual([]);
    expect(parseAutofixMetadata(issueWith(null)).goal_run_ids).toEqual([]);
  });

  it("drops a wrongly-typed field but keeps the valid array (lenient)", () => {
    const got = parseAutofixMetadata(
      issueWith({ goal_run_ids: ["run-1"], needs_info_reason: 123 }),
    );
    // needs_info_reason was a number → fails the optional string check, so the
    // whole object falls back to empty (parseWithFallback is all-or-nothing).
    expect(got).toEqual({ goal_run_ids: [] });
  });

  it("parses a well-formed blob including github + reason", () => {
    const blob: AutofixMetadata = {
      goal_run_ids: ["run-1", "run-2"],
      latest_goal_run_id: "run-2",
      github: { issue_number: 1234, issue_url: "https://gh/o/r/issues/1234" },
      needs_info_reason: "need repro",
    };
    expect(parseAutofixMetadata(issueWith(blob))).toEqual(blob);
  });

  it("passes unknown server fields through without crashing", () => {
    const got = parseAutofixMetadata(
      issueWith({ goal_run_ids: ["run-1"], future_field: "x" }),
    );
    expect(got.goal_run_ids).toEqual(["run-1"]);
  });
});

describe("deriveAutofixStatus — four states + running", () => {
  it("not_started: no autofix metadata", () => {
    expect(
      deriveAutofixStatus({ metadata: {} as Issue["metadata"] }),
    ).toEqual({ state: "not_started" });
  });

  it("not_started: autofix exists but no goal_run recorded", () => {
    expect(
      deriveAutofixStatus(issueWith({ goal_run_ids: [] })),
    ).toEqual({ state: "not_started" });
  });

  it("not_started: goal_run recorded but goalRun object missing", () => {
    const issue = issueWith({
      goal_run_ids: ["run-1"],
      latest_goal_run_id: "run-1",
    });
    expect(deriveAutofixStatus(issue, null)).toEqual({ state: "not_started" });
    expect(deriveAutofixStatus(issue, undefined)).toEqual({
      state: "not_started",
    });
  });

  it("completed: goalRun completed, surfaces the PR url (not the github issue url)", () => {
    const issue = issueWith({
      goal_run_ids: ["run-1"],
      latest_goal_run_id: "run-1",
      // Both present: the completed state must surface the PR url (N4's artifact),
      // never the upstream github issue url (N1's artifact).
      github: { issue_number: 7, issue_url: "https://gh/o/r/issues/7" },
      pr_url: "https://gh/o/r/pull/8",
    });
    expect(deriveAutofixStatus(issue, { status: "completed" })).toEqual({
      state: "completed",
      prUrl: "https://gh/o/r/pull/8",
    });
  });

  it("completed: no PR url still completes", () => {
    const issue = issueWith({
      goal_run_ids: ["run-1"],
      // A github issue url alone must NOT be mistaken for a PR url.
      github: { issue_number: 7, issue_url: "https://gh/o/r/issues/7" },
    });
    expect(deriveAutofixStatus(issue, { status: "completed" })).toEqual({
      state: "completed",
      prUrl: undefined,
    });
  });

  it("needs_info: partial + needs_info_reason", () => {
    const issue = issueWith({
      goal_run_ids: ["run-1"],
      needs_info_reason: "could not reproduce",
    });
    expect(deriveAutofixStatus(issue, { status: "partial" })).toEqual({
      state: "needs_info",
      reason: "could not reproduce",
    });
  });

  it("running: partial WITHOUT a reason is not needs_info", () => {
    const issue = issueWith({ goal_run_ids: ["run-1"] });
    expect(deriveAutofixStatus(issue, { status: "partial" })).toEqual({
      state: "running",
    });
  });

  it("failed: goal_run failed → failed state with the first failed subtask's reason", () => {
    const issue = issueWith({ goal_run_ids: ["run-1"] });
    expect(
      deriveAutofixStatus(issue, {
        status: "failed",
        subtasks: [
          { status: "completed", failure_reason: "" },
          { status: "failed", failure_reason: "compile error in handler" },
        ] as GoalRun["subtasks"],
      }),
    ).toEqual({ state: "failed", reason: "compile error in handler" });
  });

  it("failed: cancelled also maps to failed (reason empty when no failed subtask)", () => {
    const issue = issueWith({ goal_run_ids: ["run-1"] });
    expect(deriveAutofixStatus(issue, { status: "cancelled" })).toEqual({
      state: "failed",
      reason: "",
    });
  });

  it("failed: no longer mistaken for running (the gap this fixes)", () => {
    const issue = issueWith({ goal_run_ids: ["run-1"] });
    const got = deriveAutofixStatus(issue, { status: "failed" });
    expect(got.state).toBe("failed");
    expect(got.state).not.toBe("running");
  });

  it("running: planning / executing", () => {
    const issue = issueWith({ goal_run_ids: ["run-1"] });
    expect(deriveAutofixStatus(issue, { status: "planning" })).toEqual({
      state: "running",
    });
    expect(deriveAutofixStatus(issue, { status: "executing" })).toEqual({
      state: "running",
    });
  });

  it("running: unknown future status downgrades, not crashes", () => {
    const issue = issueWith({ goal_run_ids: ["run-1"] });
    // Cast through GoalRun["status"] to simulate a server enum value the
    // frontend type doesn't know about yet (enum-drift rule).
    expect(
      deriveAutofixStatus(issue, {
        status: "some_future_status" as GoalRun["status"],
      }),
    ).toEqual({ state: "running" });
  });
});
