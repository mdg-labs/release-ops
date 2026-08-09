import { Suspense } from "react";
import { ConfirmEmailChangeForm } from "./confirm-email-change-form";

export default function ConfirmEmailChangePage(): React.ReactElement {
  return (
    <Suspense fallback={null}>
      <ConfirmEmailChangeForm />
    </Suspense>
  );
}
