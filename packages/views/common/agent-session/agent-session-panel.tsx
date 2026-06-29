"use client";

import { Badge } from "@multica/ui/components/ui/badge";
import { cn } from "@multica/ui/lib/utils";

export interface AgentSessionMessage {
  id: string;
  role: string;
  content: string;
  created_at?: string | null;
}

export function AgentSessionPanel({
  messages,
  empty,
}: {
  messages: AgentSessionMessage[];
  empty: string;
}) {
  if (messages.length === 0) {
    return (
      <div className="flex min-h-36 items-center justify-center px-4 text-center text-sm text-muted-foreground">
        {empty}
      </div>
    );
  }

  return (
    <div className="space-y-3">
      {messages.map((message) => (
        <div
          key={message.id}
          className={cn(
            "max-w-[760px] rounded-md border px-3 py-2 text-sm",
            message.role === "user" ? "ml-auto bg-primary text-primary-foreground" : "bg-background",
          )}
        >
          <div className="mb-1 flex items-center gap-2">
            <Badge variant="secondary" className="h-5 rounded-sm px-1.5 text-[11px] uppercase">
              {message.role}
            </Badge>
            {message.created_at && (
              <span className="text-[11px] text-muted-foreground">{message.created_at}</span>
            )}
          </div>
          <div className="whitespace-pre-wrap break-words leading-6">{message.content}</div>
        </div>
      ))}
    </div>
  );
}
