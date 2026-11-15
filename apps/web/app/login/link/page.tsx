import { Suspense } from "react";
import { LoginLinkForm } from "./login-link-form";

export default function LoginLinkPage(): React.ReactElement {
  return (
    <Suspense fallback={null}>
      <LoginLinkForm />
    </Suspense>
  );
}
