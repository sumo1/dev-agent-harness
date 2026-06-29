"use client";

import type { RuntimeContext } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { cn } from "@multica/ui/lib/utils";
import { FolderGit2, RadioTower, Workflow } from "lucide-react";
import { ProviderLogo } from "../../runtimes/components/provider-logo";

function ContextValue({ label, value }: { label: string; value?: string }) {
  if (!value) return null;

  return (
    <span className="inline-flex min-w-0 items-center gap-1.5">
      <span className="text-muted-foreground">{label}</span>
      <span className="min-w-0 truncate font-medium text-foreground">{value}</span>
    </span>
  );
}

export function ContextBar({
  context,
  className,
}: {
  context: RuntimeContext;
  className?: string;
}) {
  const provider = context.channel?.provider ?? context.runtime?.provider;

  return (
    <div className={cn("border-b bg-muted/20 px-5 py-2 text-xs text-muted-foreground", className)}>
      <div className="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1">
        <span className="inline-flex min-w-0 items-center gap-1.5">
          <Workflow className="size-3.5 shrink-0" />
          <span className="text-muted-foreground">Work item</span>
          <Badge variant="secondary" className="h-5 rounded-sm px-1.5 text-[11px]">
            {context.work_item.kind}
          </Badge>
          <span className="min-w-0 truncate font-medium text-foreground">{context.work_item.title}</span>
        </span>

        {provider && (
          <span className="inline-flex items-center gap-1.5">
            <ProviderLogo provider={provider} className="size-3.5" />
            <span className="font-medium text-foreground">{provider}</span>
          </span>
        )}

        <ContextValue label="Runtime" value={context.runtime?.name ?? context.runtime?.id} />
        <ContextValue label="Workspace" value={context.workspace.name || context.workspace.id} />

        {context.project && (
          <span className="inline-flex min-w-0 items-center gap-1.5">
            <FolderGit2 className="size-3.5 shrink-0" />
            <span className="min-w-0 truncate font-medium text-foreground">{context.project.name}</span>
          </span>
        )}

        {context.channel && (
          <span className="inline-flex min-w-0 items-center gap-1.5">
            <RadioTower className="size-3.5 shrink-0" />
            <span className="text-muted-foreground">Channel</span>
            <span className="min-w-0 truncate font-medium text-foreground">{context.channel.channel}</span>
            {context.channel.external_conversation_id && (
              <span className="max-w-40 truncate font-mono text-[11px]">
                {context.channel.external_conversation_id}
              </span>
            )}
          </span>
        )}
      </div>
    </div>
  );
}
