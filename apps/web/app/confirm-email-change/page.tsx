import { Suspense } from "react";
import { ConfirmEmailChangeForm } from "./confirm-email-change-form";

export default function ConfirmEmailChangePage(): React.ReactElement {
  return (
    <div className="flex min-h-svh items-center justify-center p-6">
      <div className="w-full max-w-sm">
        <Suspense fallback={null}>
          <ConfirmEmailChangeForm />
        </Suspense>
      </div>
    </div>
  );
}
