import { describe, it, expect } from "vitest";
import { toWorkingDir, daemonChoicesFromRuntimes } from "./model";
import type { Project, ProjectResource, AgentRuntime } from "../types";

function project(over: Partial<Project> = {}): Project {
  return {
    id: "p1",
    workspace_id: "w1",
    title: "My App",
    description: null,
    icon: null,
    status: "planned",
    priority: "none",
    lead_type: null,
    lead_id: null,
    created_at: "2026-06-15T00:00:00Z",
    updated_at: "2026-06-15T00:00:00Z",
    issue_count: 0,
    done_count: 0,
    resource_count: 0,
    ...over,
  };
}

function resource(over: Partial<ProjectResource>): ProjectResource {
  return {
    id: "r1",
    project_id: "p1",
    workspace_id: "w1",
    resource_type: "local_directory",
    resource_ref: {},
    label: null,
    position: 0,
    created_at: "2026-06-15T00:00:00Z",
    created_by: null,
    ...over,
  };
}

function runtime(over: Partial<AgentRuntime>): AgentRuntime {
  return {
    id: "rt1",
    workspace_id: "w1",
    daemon_id: "d1",
    name: "Mac Studio",
    runtime_mode: "local" as AgentRuntime["runtime_mode"],
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "",
    metadata: {},
    owner_id: null,
    visibility: "private",
    last_seen_at: null,
    created_at: "2026-06-15T00:00:00Z",
    updated_at: "2026-06-15T00:00:00Z",
    ...over,
  };
}

describe("toWorkingDir", () => {
  it("maps title + local dir + git into the 1:1 view", () => {
    const wd = toWorkingDir(project({ title: "Snake" }), [
      resource({
        id: "loc-1",
        resource_type: "local_directory",
        resource_ref: { local_path: "/repos/snake", daemon_id: "d1" },
        position: 0,
      }),
      resource({
        id: "git-1",
        resource_type: "github_repo",
        resource_ref: { url: "https://github.com/o/snake" },
        position: 1,
      }),
    ]);
    expect(wd).toEqual({
      projectId: "p1",
      name: "Snake",
      localPath: "/repos/snake",
      daemonId: "d1",
      gitRepoUrl: "https://github.com/o/snake",
      localResourceId: "loc-1",
      gitResourceId: "git-1",
    });
  });

  it("git is optional — absent leaves gitRepoUrl empty and gitResourceId null", () => {
    const wd = toWorkingDir(project(), [
      resource({
        id: "loc-1",
        resource_ref: { local_path: "/x", daemon_id: "d1" },
      }),
    ]);
    expect(wd.gitRepoUrl).toBe("");
    expect(wd.gitResourceId).toBeNull();
    expect(wd.localResourceId).toBe("loc-1");
  });

  it("no resources yet → empty path/daemon, null resource ids (no crash)", () => {
    const wd = toWorkingDir(project({ title: "Fresh" }), []);
    expect(wd).toMatchObject({
      name: "Fresh",
      localPath: "",
      daemonId: "",
      gitRepoUrl: "",
      localResourceId: null,
      gitResourceId: null,
    });
  });

  it("picks the PRIMARY (lowest position) when multiple local dirs exist", () => {
    const wd = toWorkingDir(project(), [
      resource({
        id: "loc-late",
        resource_ref: { local_path: "/secondary", daemon_id: "d2" },
        position: 5,
      }),
      resource({
        id: "loc-primary",
        resource_ref: { local_path: "/primary", daemon_id: "d1" },
        position: 1,
      }),
    ]);
    expect(wd.localPath).toBe("/primary");
    expect(wd.localResourceId).toBe("loc-primary");
  });

  it("tolerates a malformed resource_ref (defaults to empty, never throws)", () => {
    const wd = toWorkingDir(project(), [
      resource({ id: "loc-1", resource_ref: { wrong: "shape" } }),
    ]);
    expect(wd.localPath).toBe("");
    // The row is still tracked so an edit can repair it.
    expect(wd.localResourceId).toBe("loc-1");
  });
});

describe("daemonChoicesFromRuntimes", () => {
  it("collapses multiple runtimes on one daemon into a single choice", () => {
    const choices = daemonChoicesFromRuntimes([
      runtime({ id: "a", daemon_id: "d1", name: "Mac", provider: "claude" }),
      runtime({ id: "b", daemon_id: "d1", name: "Mac", provider: "codex" }),
    ]);
    expect(choices).toHaveLength(1);
    expect(choices[0]).toMatchObject({ daemonId: "d1", label: "Mac" });
  });

  it("a daemon is online if ANY of its runtimes is online", () => {
    const choices = daemonChoicesFromRuntimes([
      runtime({ id: "a", daemon_id: "d1", status: "offline" }),
      runtime({ id: "b", daemon_id: "d1", status: "online" }),
    ]);
    expect(choices[0]?.online).toBe(true);
  });

  it("skips cloud runtimes with no daemon_id", () => {
    const choices = daemonChoicesFromRuntimes([
      runtime({ id: "a", daemon_id: null, name: "Cloud" }),
      runtime({ id: "b", daemon_id: "d1", name: "Local" }),
    ]);
    expect(choices.map((c) => c.daemonId)).toEqual(["d1"]);
  });

  it("sorts choices by label", () => {
    const choices = daemonChoicesFromRuntimes([
      runtime({ id: "a", daemon_id: "d2", name: "Zeta" }),
      runtime({ id: "b", daemon_id: "d1", name: "Alpha" }),
    ]);
    expect(choices.map((c) => c.label)).toEqual(["Alpha", "Zeta"]);
  });
});
