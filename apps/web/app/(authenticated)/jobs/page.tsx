import { Suspense } from "react";
import { JobsView } from "@/components/jobs/jobs-view";

export default function JobsPage(): React.ReactElement {
  return (
    <Suspense fallback={null}>
      <JobsView />
    </Suspense>
  );
}
