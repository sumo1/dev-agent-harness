import { useQuery, useQueries } from "@tanstack/react-query";
import { projectListOptions, projectResourcesOptions } from "../projects";
import { runtimeListOptions } from "../runtimes/queries";
import { toWorkingDir, daemonChoicesFromRuntimes } from "./model";
import type { WorkingDir, DaemonChoice } from "./model";

// ---------------------------------------------------------------------------
// Working-dir queries — thin reads layered over the existing project +
// project_resource queries. No new endpoints, no new cache keys for the data
// itself: we reuse projectKeys / projectResourceKeys so a working-dir mutation
// and a project mutation invalidate the same caches.
// ---------------------------------------------------------------------------

export interface UseWorkingDirsResult {
  workingDirs: WorkingDir[];
  isLoading: boolean;
}

/**
 * List the workspace's working directories. Each is a project + its resources
 * reduced to the 1:1 view. Resources are fetched per-project (small N for a
 * config surface); a project whose resources are still loading is surfaced with
 * empty path/git until they arrive (no flash of "missing").
 */
export function useWorkingDirs(wsId: string): UseWorkingDirsResult {
  const { data: projects = [], isLoading: projectsLoading } = useQuery(
    projectListOptions(wsId),
  );

  const resourceQueries = useQueries({
    queries: projects.map((p) => projectResourcesOptions(wsId, p.id)),
  });

  const workingDirs = projects.map((p, i) =>
    toWorkingDir(p, resourceQueries[i]?.data ?? []),
  );

  const resourcesLoading = resourceQueries.some((q) => q.isLoading);

  return { workingDirs, isLoading: projectsLoading || resourcesLoading };
}

/** The daemon machines a local directory can be bound to (from runtimes). */
export function useDaemonChoices(wsId: string): {
  daemons: DaemonChoice[];
  isLoading: boolean;
} {
  const { data: runtimes = [], isLoading } = useQuery(runtimeListOptions(wsId));
  return { daemons: daemonChoicesFromRuntimes(runtimes), isLoading };
}
