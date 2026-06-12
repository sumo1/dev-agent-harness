export {
  paths,
  isGlobalPath,
  ASSISTANT_GOAL_RUN_PARAM,
  ASSISTANT_SESSION_PARAM,
} from "./paths";
export type { WorkspacePaths, AssistantLocator } from "./paths";
export { RESERVED_SLUGS, isReservedSlug } from "./reserved-slugs";
export { resolvePostAuthDestination, useHasOnboarded } from "./resolve";
export {
  WorkspaceSlugProvider,
  useWorkspaceSlug,
  useRequiredWorkspaceSlug,
  useCurrentWorkspace,
  useWorkspacePaths,
} from "./hooks";
