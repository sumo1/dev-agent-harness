"use client";

import { WorkingDirsPage } from "@multica/views/working-dirs/components";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";

export default function Page() {
  return (
    <ErrorBoundary>
      <WorkingDirsPage />
    </ErrorBoundary>
  );
}
