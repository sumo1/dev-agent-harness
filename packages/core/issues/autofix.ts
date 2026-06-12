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
// Three-state derivation (design §1 decision B).
//
// The product surfaces three states — not_started / completed / needs_info —
// plus an interstitial running state. None of them is a new goal_run enum
// value: we derive them from the existing goal_run.status string + the autofix
// metadata blob.
//
//   not_started : no autofix metadata, or no goal_run yet / goalRun missing
//   completed   : goalRun.status === "completed" (PR url if recorded)
//   needs_info  : goalRun.status === "partial" + needs_info_reason set
//   running     : any other live status (planning / executing / etc.)
// ---------------------------------------------------------------------------

export type AutofixStatus =
  | { state: "not_started" }
  | { state: "running" }
  | { state: "completed"; prUrl?: string }
  | { state: "needs_info"; reason: string };

/**
 * Pure derivation of the product-facing autofix status from an issue and its
 * latest goal_run. `goalRun` is optional: when the metadata references a
 * goal_run that hasn't loaded (or doesn't exist), we treat it as not_started.
 *
 * `goalRun.status` is a server-driven string; the `switch`-equivalent here has
 * an explicit default branch (the trailing `running`) so an unknown future
 * status downgrades to "running" rather than crashing (enum-drift rule).
 */
export function deriveAutofixStatus(
  issue: Pick<Issue, "metadata">,
  goalRun?: Pick<GoalRun, "status"> | null,
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
    return { state: "completed", prUrl: autofix.github?.issue_url };
  }

  if (goalRun.status === "partial" && (autofix.needs_info_reason ?? "") !== "") {
    return { state: "needs_info", reason: autofix.needs_info_reason as string };
  }

  // Everything else (planning / executing / confirmed / a future unknown
  // status) is an in-progress run from the user's point of view.
  return { state: "running" };
}
