import { Suspense } from "react";
import { LoginForm } from "./login-form";

export default function LoginPage(): React.ReactElement {
  return (
    <Suspense fallback={null}>
      <LoginForm />
    </Suspense>
  );
}
