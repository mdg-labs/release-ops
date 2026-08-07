import { Suspense } from "react";
import { AcceptInvitationForm } from "./accept-invitation-form";

export default function AcceptInvitationPage(): React.ReactElement {
  return (
    <div className="flex min-h-svh items-center justify-center p-6">
      <div className="w-full max-w-sm">
        <Suspense fallback={null}>
          <AcceptInvitationForm />
        </Suspense>
      </div>
    </div>
  );
}
