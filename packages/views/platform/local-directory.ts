// Desktop-only helpers for the project_resource local_directory flow.
//
// These wrap the preload `desktopAPI` surface so view components can
// SSR-render on web (where `window.desktopAPI` is undefined) and degrade
// gracefully to no-op promises instead of crashing.

export type PickDirectoryResult = {
  ok: boolean;
  path?: string;
  basename?: string;
  reason?: "cancelled" | "no_window" | "error" | "unsupported";
  error?: string;
};

export type ValidateLocalDirectoryResult = {
  ok: boolean;
  reason?:
    | "not_absolute"
    | "not_found"
    | "not_a_directory"
    | "not_readable"
    | "not_writable"
    | "error"
    | "unsupported";
  error?: string;
};

export type GitRemoteResult = {
  ok: boolean;
  url?: string;
  reason?: "not_absolute" | "not_git" | "no_remote" | "error" | "unsupported";
  error?: string;
};

interface DesktopLocalDirectoryAPI {
  pickDirectory?: (defaultPath?: string) => Promise<PickDirectoryResult>;
  validateLocalDirectory?: (
    path: string,
  ) => Promise<ValidateLocalDirectoryResult>;
  detectGitRemote?: (path: string) => Promise<GitRemoteResult>;
}

function readDesktopAPI(): DesktopLocalDirectoryAPI | undefined {
  if (typeof window === "undefined") return undefined;
  const api = (window as unknown as { desktopAPI?: DesktopLocalDirectoryAPI })
    .desktopAPI;
  return api;
}

/** True when the renderer is running inside the Electron desktop shell, as
 *  evidenced by the preload-exposed pickDirectory bridge. Avoids hard-coding
 *  navigator/process checks — those vary across electron-vite + jsdom tests. */
export function isDesktopShell(): boolean {
  const api = readDesktopAPI();
  return typeof api?.pickDirectory === "function";
}

export async function pickDirectory(
  defaultPath?: string,
): Promise<PickDirectoryResult> {
  const api = readDesktopAPI();
  if (!api?.pickDirectory) return { ok: false, reason: "unsupported" };
  return api.pickDirectory(defaultPath);
}

export async function validateLocalDirectory(
  path: string,
): Promise<ValidateLocalDirectoryResult> {
  const api = readDesktopAPI();
  if (!api?.validateLocalDirectory) return { ok: false, reason: "unsupported" };
  return api.validateLocalDirectory(path);
}

/** Read the picked folder's git origin URL so the working-dir form can auto-fill
 *  the git binding. Non-git folders / web shell resolve to ok=false (no url). */
export async function detectGitRemote(path: string): Promise<GitRemoteResult> {
  const api = readDesktopAPI();
  if (!api?.detectGitRemote) return { ok: false, reason: "unsupported" };
  return api.detectGitRemote(path);
}
