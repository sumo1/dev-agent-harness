import { z } from "zod";
import { parseWithFallback } from "../api/schema";
import type { GoalRun, Issue } from "../types";

// ---------------------------------------------------------------------------
// issue.metadata.autofix — the structured blob that links an issue to the
// goal_run(s) auto-fixing it, the GitHub issue filed, and any "needs more
// info" reason a verify agent reported. Mirrors the Go shape in
// server/internal/service/issue_autofix.go (AutofixMetadata).
//
// This is read from `issue.metadata.autofix`, which crosses the network as
// untyped JSON. Per the API Response Compatibility rule we PARSE, never cast:
// `parseAutofixMetadata` runs the value through a LENIENT zod schema and falls
// back to the empty blob on any drift, never throwing into the UI.
// ---------------------------------------------------------------------------

// Lenient by the same rules as the schemas in api/schemas.ts: optional fields
// stay optional, the array defaults to [], and `.loose()` lets unknown
// server-side fields pass through unchanged.
const autofixGithubSchema = z.object({
  issue_number: z.number().optional(),
  issue_url: z.string().optional(),
}).loose();

export const autofixMetadataSchema = z.object({
  goal_run_ids: z.array(z.string()).default([]),
  latest_goal_run_id: z.string().optional(),
  github: autofixGithubSchema.optional(),
  pr_url: z.string().optional(),
  needs_info_reason: z.string().optional(),
}).loose();

export interface AutofixGithub {
  issue_number?: number;
  issue_url?: string;
}

export interface AutofixMetadata {
  goal_run_ids: string[];
  latest_goal_run_id?: string;
  github?: AutofixGithub;
  pr_url?: string;
  needs_info_reason?: string;
}

const EMPTY_AUTOFIX_METADATA: AutofixMetadata = { goal_run_ids: [] };

/**
 * Read `issue.metadata.autofix` defensively. Returns the parsed blob on
 * success, or `{ goal_run_ids: [] }` on any drift (missing key, null array,
 * wrong type). Never throws.
 */
export function parseAutofixMetadata(issue: Pick<Issue, "metadata">): AutofixMetadata {
  // `metadata` is typed as a flat KV map (Record<string, primitive>), but the
  // autofix blob is a nested object written by the backend service helper
  // outside the primitive-only handler path. Read it as `unknown` and let the
  // schema validate the shape.
  const raw = (issue.metadata as Record<string, unknown> | undefined)?.autofix;
  if (raw === undefined || raw === null) {
    return EMPTY_AUTOFIX_METADATA;
  }
  return parseWithFallback<AutofixMetadata>(
    raw,
    autofixMetadataSchema,
    EMPTY_AUTOFIX_METADATA,
    { endpoint: "issue.metadata.autofix" },
  );
}

// ---------------------------------------------------------------------------
// Five-state derivation (design §1 decision B + design-quick-actions §1).
//
// The product surfaces five states, derived from goal_run.status + the autofix
// metadata blob — no new goal_run enum value:
//
//   not_started : no autofix metadata, or no goal_run yet / goalRun missing
//   running     : a live status (planning / executing / a future unknown)
//   completed   : goalRun.status === "completed" (pr_url if the N4 node reported it)
//   needs_info  : goalRun.status === "partial" + needs_info_reason set
//   failed      : goalRun.status === "failed" / "cancelled" (执行错误)
//
// `failed` was the gap: a failed run used to fall through to `running`, so it
// looked like it was still in flight. The reason is pulled from the first failed
// subtask (run-level failure_reason isn't on the GoalRun response; subtasks are).
// ---------------------------------------------------------------------------

export type AutofixStatus =
  | { state: "not_started" }
  | { state: "running" }
  | { state: "completed"; prUrl?: string }
  | { state: "needs_info"; reason: string }
  | { state: "failed"; reason: string };

/** The reason a run failed = the first failed subtask's failure_reason (the
 *  GoalRun response carries subtasks, not a run-level reason). Empty when none. */
function firstFailedSubtaskReason(
  goalRun: Partial<Pick<GoalRun, "subtasks">> | null | undefined,
): string {
  const failed = goalRun?.subtasks?.find((s) => s.status === "failed");
  return failed?.failure_reason ?? "";
}

/**
 * Pure derivation of the product-facing autofix status from an issue and its
 * latest goal_run. `goalRun` is optional: when the metadata references a
 * goal_run that hasn't loaded (or doesn't exist), we treat it as not_started.
 *
 * `goalRun.status` is a server-driven string; the trailing `running` branch is
 * the default, so an unknown future status downgrades to "running" rather than
 * crashing (enum-drift rule).
 */
export function deriveAutofixStatus(
  issue: Pick<Issue, "metadata">,
  goalRun?: (Pick<GoalRun, "status"> & Partial<Pick<GoalRun, "subtasks">>) | null,
): AutofixStatus {
  const autofix = parseAutofixMetadata(issue);

  // No autofix metadata at all, or no goal_run recorded → not started.
  const hasGoalRun =
    autofix.goal_run_ids.length > 0 || (autofix.latest_goal_run_id ?? "") !== "";
  if (!hasGoalRun) {
    return { state: "not_started" };
  }

  // Metadata says a run exists but the run object isn't available → treat as
  // not started rather than guessing a live state.
  if (!goalRun) {
    return { state: "not_started" };
  }

  if (goalRun.status === "completed") {
    return { state: "completed", prUrl: autofix.pr_url };
  }

  if (goalRun.status === "partial" && (autofix.needs_info_reason ?? "") !== "") {
    return { state: "needs_info", reason: autofix.needs_info_reason as string };
  }

  // Hard failure / cancellation — the gap that used to read as "running".
  if (goalRun.status === "failed" || goalRun.status === "cancelled") {
    return { state: "failed", reason: firstFailedSubtaskReason(goalRun) };
  }

  // Everything else (planning / executing / confirmed / a future unknown
  // status) is an in-progress run from the user's point of view.
  return { state: "running" };
}
