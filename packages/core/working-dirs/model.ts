import type {
  Project,
  ProjectResource,
  LocalDirectoryResourceRef,
  GithubRepoResourceRef,
  AgentRuntime,
} from "../types";

// ---------------------------------------------------------------------------
// Working Directory — the lightweight UI reframing of `project`.
//
// A "working directory" is a thin face over an existing `project` + its
// `project_resource` rows. The product surfaces only: a name, ONE local
// directory (path + the daemon machine it lives on), and an OPTIONAL single git
// repo. The heavyweight project ceremony (status / priority / lead / icon /
// description / kanban) is intentionally NOT modelled here — those columns stay
// in the DB at their defaults and are never shown.
//
// Backend is untouched: daemon worktree / autofix / goal_persist / role_sync all
// keep reading `project` + `project_resource`. This module only reshapes them
// 1:1 for the config UI and back.
// ---------------------------------------------------------------------------

/** 1:1 view model of a project as a single working directory. */
export interface WorkingDir {
  /** The backing project id (the real entity issues/goals still bind to). */
  projectId: string;
  /** Display name = project.title. */
  name: string;
  /** Absolute path on the daemon machine. Empty if no local_directory resource. */
  localPath: string;
  /** Which daemon machine the path lives on. Empty if unset. */
  daemonId: string;
  /** Optional single git repo URL bound to this directory. */
  gitRepoUrl: string;
  /** The resource ids, kept so edit/delete can target the exact rows. */
  localResourceId: string | null;
  gitResourceId: string | null;
}

function isLocalDirRef(ref: unknown): ref is LocalDirectoryResourceRef {
  return (
    typeof ref === "object" &&
    ref !== null &&
    typeof (ref as { local_path?: unknown }).local_path === "string"
  );
}

function isGithubRef(ref: unknown): ref is GithubRepoResourceRef {
  return (
    typeof ref === "object" &&
    ref !== null &&
    typeof (ref as { url?: unknown }).url === "string"
  );
}

/**
 * Reduce a project + its resources to the 1:1 working-dir view. When a project
 * carries MULTIPLE local_directory / github_repo resources (legacy / advanced
 * data), the PRIMARY one (lowest `position`, then earliest) is surfaced; the
 * rest are left untouched in the DB. Defensive against missing/odd ref shapes.
 */
export function toWorkingDir(project: Project, resources: ProjectResource[]): WorkingDir {
  const byPosition = [...resources].sort((a, b) => {
    if (a.position !== b.position) return a.position - b.position;
    return a.created_at.localeCompare(b.created_at);
  });

  const local = byPosition.find((r) => r.resource_type === "local_directory");
  const git = byPosition.find((r) => r.resource_type === "github_repo");

  const localRef = local && isLocalDirRef(local.resource_ref) ? local.resource_ref : null;
  const gitRef = git && isGithubRef(git.resource_ref) ? git.resource_ref : null;

  return {
    projectId: project.id,
    name: project.title,
    localPath: localRef?.local_path ?? "",
    daemonId: localRef?.daemon_id ?? "",
    gitRepoUrl: gitRef?.url ?? "",
    localResourceId: local?.id ?? null,
    gitResourceId: git?.id ?? null,
  };
}

/** The editable form fields for creating / updating a working directory. */
export interface WorkingDirForm {
  name: string;
  localPath: string;
  daemonId: string;
  /** Optional — empty string means "no git repo bound". */
  gitRepoUrl: string;
}

/** A daemon machine the user can pick when binding a local directory. */
export interface DaemonChoice {
  daemonId: string;
  /** Human label — the device/runtime name, falling back to the id. */
  label: string;
  online: boolean;
}

/**
 * Collapse the workspace's runtimes into the distinct daemon machines behind
 * them. A daemon runs one or more runtimes (providers); we want each physical
 * machine once. A daemon is "online" if ANY of its runtimes is online.
 * Runtimes with no daemon_id (cloud) are skipped — a local directory must live
 * on a real daemon machine.
 */
export function daemonChoicesFromRuntimes(runtimes: AgentRuntime[]): DaemonChoice[] {
  const byDaemon = new Map<string, DaemonChoice>();
  for (const rt of runtimes) {
    const id = rt.daemon_id ?? "";
    if (id === "") continue;
    const existing = byDaemon.get(id);
    const online = rt.status === "online";
    if (existing) {
      existing.online = existing.online || online;
    } else {
      byDaemon.set(id, { daemonId: id, label: rt.name || id, online });
    }
  }
  return [...byDaemon.values()].sort((a, b) => a.label.localeCompare(b.label));
}
