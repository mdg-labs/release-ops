import { Suspense } from "react";
import { SetupForm } from "./setup-form";

export default function SetupPage(): React.ReactElement {
  return (
    <Suspense fallback={null}>
      <SetupForm />
    </Suspense>
  );
}
