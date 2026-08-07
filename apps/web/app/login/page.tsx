import { Suspense } from "react";
import { LoginForm } from "./login-form";

export default function LoginPage(): React.ReactElement {
  return (
    <div className="flex min-h-svh items-center justify-center p-6">
      <div className="w-full max-w-sm">
        <Suspense fallback={null}>
          <LoginForm />
        </Suspense>
      </div>
    </div>
  );
}
