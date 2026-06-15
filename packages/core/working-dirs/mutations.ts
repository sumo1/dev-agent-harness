import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { projectKeys, projectResourceKeys } from "../projects";
import { useWorkspaceId } from "../hooks";
import type {
  CreateProjectResourceRequest,
  LocalDirectoryResourceRef,
  GithubRepoResourceRef,
} from "../types";
import type { WorkingDir, WorkingDirForm } from "./model";

// ---------------------------------------------------------------------------
// Working-dir mutations — orchestrate the existing project + project_resource
// endpoints so the UI does ONE call per user action. No new backend.
//
// Create: POST /api/projects with bundled resources (one local_directory +
//         optional github_repo).
// Update: PUT project title; then create / update / delete each resource so the
//         project ends up with exactly the form's local dir + optional git.
// Delete: DELETE project (cascades resources).
// ---------------------------------------------------------------------------

function trim(s: string): string {
  return s.trim();
}

function localDirResource(form: WorkingDirForm): CreateProjectResourceRequest {
  const ref: LocalDirectoryResourceRef = {
    local_path: trim(form.localPath),
    daemon_id: trim(form.daemonId),
  };
  return { resource_type: "local_directory", resource_ref: ref, position: 0 };
}

function gitResource(form: WorkingDirForm): CreateProjectResourceRequest {
  const ref: GithubRepoResourceRef = { url: trim(form.gitRepoUrl) };
  return { resource_type: "github_repo", resource_ref: ref, position: 1 };
}

/**
 * Create a working directory = create a project (title = name) bundling a
 * local_directory resource and, when a git URL is given, a github_repo resource.
 */
export function useCreateWorkingDir() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: async (form: WorkingDirForm) => {
      const resources: CreateProjectResourceRequest[] = [localDirResource(form)];
      if (trim(form.gitRepoUrl) !== "") resources.push(gitResource(form));
      return api.createProject({ title: trim(form.name), resources });
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: projectKeys.list(wsId) });
    },
  });
}

/**
 * Update a working directory: rename the project if needed, then reconcile its
 * local_directory and github_repo resources to match the form (create the row
 * if absent, update if present, delete a git row when the URL is cleared).
 */
export function useUpdateWorkingDir() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: async ({
      current,
      form,
    }: {
      current: WorkingDir;
      form: WorkingDirForm;
    }) => {
      const projectId = current.projectId;

      if (trim(form.name) !== current.name) {
        await api.updateProject(projectId, { title: trim(form.name) });
      }

      // Local directory — always present on a working dir; create or update.
      const localRef: LocalDirectoryResourceRef = {
        local_path: trim(form.localPath),
        daemon_id: trim(form.daemonId),
      };
      if (current.localResourceId) {
        await api.updateProjectResource(projectId, current.localResourceId, {
          resource_ref: localRef,
        });
      } else {
        await api.createProjectResource(projectId, {
          resource_type: "local_directory",
          resource_ref: localRef,
          position: 0,
        });
      }

      // Git repo — optional; reconcile presence against the form.
      const wantGit = trim(form.gitRepoUrl) !== "";
      if (wantGit) {
        const gitRef: GithubRepoResourceRef = { url: trim(form.gitRepoUrl) };
        if (current.gitResourceId) {
          await api.updateProjectResource(projectId, current.gitResourceId, {
            resource_ref: gitRef,
          });
        } else {
          await api.createProjectResource(projectId, {
            resource_type: "github_repo",
            resource_ref: gitRef,
            position: 1,
          });
        }
      } else if (current.gitResourceId) {
        await api.deleteProjectResource(projectId, current.gitResourceId);
      }
    },
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: projectKeys.list(wsId) });
      qc.invalidateQueries({
        queryKey: projectResourceKeys.list(wsId, vars.current.projectId),
      });
    },
  });
}

/** Delete a working directory = delete its backing project (cascades resources). */
export function useDeleteWorkingDir() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (projectId: string) => api.deleteProject(projectId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: projectKeys.list(wsId) });
    },
  });
}
