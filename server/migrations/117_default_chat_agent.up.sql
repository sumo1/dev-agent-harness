-- Default chat agent: the workspace's "just talk to a runtime" agent.
--
-- The assistant's new-session dialog lets a user pick ONLY a runtime without
-- choosing an agent. Because chat_session.agent_id is NOT NULL (the whole
-- prompt/dispatch/claim/permission pipeline needs an agent), "agentless chat"
-- isn't representable. Instead a workspace has ONE managed default chat agent —
-- empty instructions, a plain passthrough to whatever runtime the session binds
-- — created on demand the first time someone starts a session without picking
-- an agent. This column points at it (resolve-or-create caches the id here).
--
-- Nullable: unset until the first agentless session is created. Mirrors
-- default_planner_agent_id (migration 115). ON DELETE SET NULL so deleting the
-- agent just clears the pointer; the next agentless session recreates it.
ALTER TABLE workspace
    ADD COLUMN default_chat_agent_id UUID REFERENCES agent(id) ON DELETE SET NULL;
