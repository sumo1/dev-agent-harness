"use client";

import { useCallback, useState } from "react";
import { FolderGit, FolderOpen, Plus, Pencil, Trash2 } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  useWorkingDirs,
  useDaemonChoices,
  useCreateWorkingDir,
  useUpdateWorkingDir,
  useDeleteWorkingDir,
} from "@multica/core/working-dirs";
import type { WorkingDir, WorkingDirForm } from "@multica/core/working-dirs";
import { Button } from "@multica/ui/components/ui/button";
import { Label } from "@multica/ui/components/ui/label";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { toast } from "sonner";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";
import {
  isDesktopShell,
  pickDirectory,
  detectGitRemote,
} from "../../platform/local-directory";
import { useLocalDaemonStatus } from "../../platform/use-local-daemon-status";

export function WorkingDirsPage() {
  const { t } = useT("working-dirs");
  const wsId = useWorkspaceId();
  const { workingDirs, isLoading } = useWorkingDirs(wsId);
  const { daemons } = useDaemonChoices(wsId);

  // null = closed; { dir: null } = create; { dir } = edit.
  const [editing, setEditing] = useState<{ dir: WorkingDir | null } | null>(null);
  const [deleting, setDeleting] = useState<WorkingDir | null>(null);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <PageHeader className="justify-between px-5">
        <div className="flex items-center gap-2">
          <FolderGit className="h-4 w-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">{t(($) => $.page.title)}</h1>
          {workingDirs.length > 0 && (
            <span className="font-mono text-xs tabular-nums text-muted-foreground/70">
              {workingDirs.length}
            </span>
          )}
        </div>
        <Button type="button" size="sm" onClick={() => setEditing({ dir: null })}>
          <Plus className="h-3 w-3" />
          {t(($) => $.page.new)}
        </Button>
      </PageHeader>

      <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
        <div className="mx-auto max-w-3xl">
        <p className="mb-4 text-xs text-muted-foreground">
          {t(($) => $.page.description)}
        </p>
        {isLoading ? (
          <div className="space-y-2">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-14 w-full rounded-md" />
            ))}
          </div>
        ) : workingDirs.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-3 py-16 text-center">
            <FolderOpen className="size-10 text-muted-foreground opacity-30" />
            <p className="text-sm text-muted-foreground">{t(($) => $.page.empty)}</p>
            <Button variant="outline" size="sm" className="gap-1.5" onClick={() => setEditing({ dir: null })}>
              <Plus className="size-3.5" />
              {t(($) => $.page.create_first)}
            </Button>
          </div>
        ) : (
          <ul className="space-y-2">
            {workingDirs.map((wd) => {
              const daemon = daemons.find((d) => d.daemonId === wd.daemonId);
              return (
                <li
                  key={wd.projectId}
                  className="group flex items-center gap-3 rounded-md border bg-card p-3"
                >
                  <FolderOpen className="size-4 shrink-0 text-muted-foreground" />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="truncate text-sm font-medium">{wd.name}</span>
                      {wd.gitRepoUrl ? (
                        <span className="inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
                          <FolderGit className="size-3" />
                          {t(($) => $.list.git_bound)}
                        </span>
                      ) : (
                        <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
                          {t(($) => $.list.no_git)}
                        </span>
                      )}
                    </div>
                    <p className="truncate text-xs text-muted-foreground" title={wd.localPath}>
                      {wd.localPath || "—"}
                      {daemon ? ` · ${daemon.label}${daemon.online ? "" : ` (${t(($) => $.list.offline)})`}` : ""}
                    </p>
                  </div>
                  <div className="flex shrink-0 items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100">
                    <Button variant="ghost" size="icon" className="size-7" title={t(($) => $.form.edit_title)} onClick={() => setEditing({ dir: wd })}>
                      <Pencil className="size-3.5" />
                    </Button>
                    <Button variant="ghost" size="icon" className="size-7 text-destructive" title={t(($) => $.delete.title)} onClick={() => setDeleting(wd)}>
                      <Trash2 className="size-3.5" />
                    </Button>
                  </div>
                </li>
              );
            })}
          </ul>
        )}
        </div>
      </div>

      {editing && (
        <WorkingDirFormDialog
          current={editing.dir}
          onClose={() => setEditing(null)}
        />
      )}

      <DeleteWorkingDirDialog dir={deleting} onClose={() => setDeleting(null)} />
    </div>
  );
}

/** Derive the working-dir display name from an absolute folder path's basename
 *  (the directive: "name = the chosen folder's name"). Tolerant of trailing
 *  slashes and either separator. */
function basenameOf(path: string): string {
  const trimmed = path.replace(/[/\\]+$/, "");
  const idx = Math.max(trimmed.lastIndexOf("/"), trimmed.lastIndexOf("\\"));
  return idx >= 0 ? trimmed.slice(idx + 1) : trimmed;
}

/**
 * Simplified form: a working directory is JUST a local folder on THIS machine.
 * The user picks a folder; the name is taken from the folder basename, the path
 * from the picker, and the machine defaults to the local daemon. Git binding and
 * a free-form name/machine are no longer asked — git is preserved as-is on edit.
 * The underlying WorkingDirForm shape is unchanged so the create/update
 * orchestration keeps working untouched.
 */
function WorkingDirFormDialog({
  current,
  onClose,
}: {
  current: WorkingDir | null;
  onClose: () => void;
}) {
  const { t } = useT("working-dirs");
  const create = useCreateWorkingDir();
  const update = useUpdateWorkingDir();
  const daemonStatus = useLocalDaemonStatus();
  const desktop = isDesktopShell();

  // The picked folder (path + derived name). On edit we seed from the current
  // working dir so the user sees what they're changing.
  const [localPath, setLocalPath] = useState(current?.localPath ?? "");
  const [name, setName] = useState(current?.name ?? "");
  // Git binding: seeded from the existing dir on edit; auto-detected from the
  // picked folder's `origin` remote on create (empty for non-git folders).
  const [gitRepoUrl, setGitRepoUrl] = useState(current?.gitRepoUrl ?? "");
  const [error, setError] = useState<string | null>(null);
  const saving = create.isPending || update.isPending;

  // Machine = this machine's daemon. On edit, keep whatever the dir was bound to
  // if the local daemon id isn't known yet (avoid silently re-homing it).
  const localDaemonId = daemonStatus.daemonId ?? "";
  const effectiveDaemonId = localDaemonId || current?.daemonId || "";

  const choose = useCallback(async () => {
    setError(null);
    const result = await pickDirectory(localPath || undefined);
    if (!result.ok) {
      if (result.reason === "cancelled") return;
      setError(result.error ?? t(($) => $.errors.save_failed));
      return;
    }
    const picked = result.path ?? "";
    setLocalPath(picked);
    setName(result.basename ?? basenameOf(picked));
    // Auto-detect the folder's git remote so the user doesn't type it. A
    // non-git folder leaves the binding empty (still a valid working dir).
    const remote = await detectGitRemote(picked);
    setGitRepoUrl(remote.ok && remote.url ? remote.url : "");
  }, [localPath, t]);

  const submit = useCallback(async () => {
    if (localPath.trim() === "") {
      setError(t(($) => $.errors.path_required));
      return;
    }
    if (effectiveDaemonId.trim() === "") {
      setError(t(($) => $.form.no_local_daemon));
      return;
    }
    setError(null);
    // Git binding: auto-detected from the picked folder on create, preserved on
    // edit (re-detected if the folder was changed to a different git repo).
    const form: WorkingDirForm = {
      name: name.trim() || basenameOf(localPath),
      localPath: localPath.trim(),
      daemonId: effectiveDaemonId,
      gitRepoUrl: gitRepoUrl.trim(),
    };
    try {
      if (current) {
        await update.mutateAsync({ current, form });
      } else {
        await create.mutateAsync(form);
      }
      onClose();
    } catch {
      toast.error(t(($) => $.errors.save_failed));
    }
  }, [localPath, effectiveDaemonId, name, current, create, update, onClose, t]);

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            {current ? t(($) => $.form.edit_title) : t(($) => $.form.create_title)}
          </DialogTitle>
          <DialogDescription>{t(($) => $.page.description)}</DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          <div className="space-y-1.5">
            <Label>{t(($) => $.form.folder_label)}</Label>
            {localPath ? (
              <div className="flex items-center gap-2 rounded-md border bg-muted/30 px-2.5 py-2">
                <FolderOpen className="size-4 shrink-0 text-muted-foreground" />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium" title={name}>{name}</p>
                  <p className="truncate text-xs text-muted-foreground" title={localPath}>
                    {localPath}
                  </p>
                </div>
                {desktop && (
                  <Button variant="ghost" size="sm" className="h-7 shrink-0 text-xs" onClick={choose}>
                    {t(($) => $.form.change_folder)}
                  </Button>
                )}
              </div>
            ) : desktop ? (
              <Button variant="outline" size="sm" className="w-full gap-1.5" onClick={choose}>
                <FolderOpen className="size-4" />
                {t(($) => $.form.choose_folder)}
              </Button>
            ) : (
              <p className="text-xs text-muted-foreground">{t(($) => $.form.web_unsupported)}</p>
            )}
            <p className="text-xs text-muted-foreground">{t(($) => $.form.picked_hint)}</p>
          </div>

          {/* Auto-detected git binding (read-only). Shown once a folder is
              picked: the origin remote when it's a git repo, else "no repo". */}
          {localPath && (
            <div className="flex items-center gap-1.5 text-xs">
              <FolderGit className="size-3.5 shrink-0 text-muted-foreground" />
              {gitRepoUrl ? (
                <span className="truncate text-muted-foreground" title={gitRepoUrl}>
                  {gitRepoUrl}
                </span>
              ) : (
                <span className="text-muted-foreground/70">{t(($) => $.form.git_none)}</span>
              )}
            </div>
          )}

          {error && <p className="text-xs text-destructive">{error}</p>}
        </div>

        <DialogFooter>
          <Button variant="outline" size="sm" onClick={onClose} disabled={saving}>
            {t(($) => $.form.cancel)}
          </Button>
          <Button size="sm" onClick={submit} disabled={saving || !localPath}>
            {current ? t(($) => $.form.save) : t(($) => $.form.create)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function DeleteWorkingDirDialog({
  dir,
  onClose,
}: {
  dir: WorkingDir | null;
  onClose: () => void;
}) {
  const { t } = useT("working-dirs");
  const del = useDeleteWorkingDir();

  const confirm = useCallback(async () => {
    if (!dir) return;
    try {
      await del.mutateAsync(dir.projectId);
      onClose();
    } catch {
      toast.error(t(($) => $.errors.delete_failed));
    }
  }, [dir, del, onClose, t]);

  return (
    <AlertDialog open={!!dir} onOpenChange={(o) => !o && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.delete.title)}</AlertDialogTitle>
          <AlertDialogDescription>{t(($) => $.delete.body)}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t(($) => $.delete.cancel)}</AlertDialogCancel>
          <AlertDialogAction onClick={confirm} disabled={del.isPending}>
            {t(($) => $.delete.confirm)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
