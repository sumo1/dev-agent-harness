import { describe, expect, it } from "vitest";

import { getSessionCommands } from "./registry";

describe("session command registry", () => {
  it("keeps work item commands scoped to existing work item kinds", () => {
    const ids = getSessionCommands({ workItemKind: "issue" }).map((command) => command.id);

    expect(ids).toContain("retry");
    expect(ids).toContain("interrupt");
    expect(ids).not.toContain("dispatch_as_goal");
  });

  it("models OpenClaw as a channel and automation source, not a work item kind", () => {
    const channelIds = getSessionCommands({ channelProvider: "openclaw" }).map((command) => command.id);
    const automationIds = getSessionCommands({ automationProvider: "openclaw" }).map((command) => command.id);

    expect(channelIds).toEqual(expect.arrayContaining([
      "dispatch_as_goal",
      "dispatch_as_issue",
      "continue_in_assistant",
    ]));
    expect(automationIds).toEqual(expect.arrayContaining([
      "sync_openclaw_automations",
      "pause_openclaw_automation",
      "resume_openclaw_automation",
    ]));
  });
});
