"use client";

import { AutofixIssuesPage } from "@multica/views/issues/components";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";

export default function Page() {
  return (
    <ErrorBoundary>
      <AutofixIssuesPage />
    </ErrorBoundary>
  );
}
