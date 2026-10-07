import { Suspense } from "react";
import { AcceptInvitationForm } from "./accept-invitation-form";

export default function AcceptInvitationPage(): React.ReactElement {
  return (
    <Suspense fallback={null}>
      <AcceptInvitationForm />
    </Suspense>
  );
}
