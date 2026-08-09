"use client";

import {
  CircleCheckIcon,
  KeyRoundIcon,
  MailIcon,
  UserIcon,
} from "lucide-react";
import { useTranslations } from "next-intl";
import { useId, useState } from "react";
import { SettingsSection } from "@/components/common/settings-section";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { useProfile } from "@/hooks/useProfile";
import { ApiError } from "@/lib/api/client";
import { useSession } from "@/lib/hooks/use-session";

const MIN_PASSWORD_LENGTH = 8;

export function ProfileView(): React.ReactElement {
  const t = useTranslations("profile");
  const { data: session, isLoading, isError } = useSession();
  const { requestEmailChange, changePassword } = useProfile();

  const newEmailId = useId();
  const emailPasswordId = useId();
  const currentPasswordId = useId();
  const newPasswordId = useId();
  const confirmPasswordId = useId();

  const [newEmail, setNewEmail] = useState("");
  const [emailCurrentPassword, setEmailCurrentPassword] = useState("");
  const [emailValidationError, setEmailValidationError] = useState<
    string | null
  >(null);
  const [emailChangeError, setEmailChangeError] = useState<string | null>(null);
  const [emailChangePending, setEmailChangePending] = useState(false);

  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [passwordValidationError, setPasswordValidationError] = useState<
    string | null
  >(null);
  const [passwordChangeError, setPasswordChangeError] = useState<string | null>(
    null,
  );
  const [passwordChangeSuccess, setPasswordChangeSuccess] = useState(false);

  async function handleEmailChange(): Promise<void> {
    setEmailChangeError(null);
    setEmailValidationError(null);
    setEmailChangePending(false);

    if (!newEmail.trim()) {
      setEmailValidationError(t("changeEmail.validation.newEmailRequired"));
      return;
    }
    if (!emailCurrentPassword) {
      setEmailValidationError(t("changeEmail.validation.passwordRequired"));
      return;
    }

    try {
      await requestEmailChange.mutateAsync({
        newEmail: newEmail.trim(),
        currentPassword: emailCurrentPassword,
      });
      setEmailChangePending(true);
      setNewEmail("");
      setEmailCurrentPassword("");
    } catch (error) {
      if (error instanceof ApiError) {
        if (error.status === 503) {
          setEmailChangeError(t("changeEmail.smtpNotConfigured"));
        } else if (error.status === 401) {
          setEmailChangeError(t("changeEmail.invalidPassword"));
        } else {
          setEmailChangeError(error.message);
        }
      } else {
        setEmailChangeError(t("changeEmail.failed"));
      }
    }
  }

  function validatePasswordForm(): string | null {
    if (!currentPassword) {
      return t("changePassword.validation.currentPasswordRequired");
    }
    if (!newPassword) {
      return t("changePassword.validation.newPasswordRequired");
    }
    if (newPassword.length < MIN_PASSWORD_LENGTH) {
      return t("changePassword.validation.newPasswordTooShort");
    }
    if (!confirmPassword) {
      return t("changePassword.validation.confirmPasswordRequired");
    }
    if (newPassword !== confirmPassword) {
      return t("changePassword.validation.passwordMismatch");
    }
    return null;
  }

  async function handlePasswordChange(): Promise<void> {
    setPasswordChangeError(null);
    setPasswordChangeSuccess(false);

    const error = validatePasswordForm();
    if (error) {
      setPasswordValidationError(error);
      return;
    }

    setPasswordValidationError(null);

    try {
      await changePassword.mutateAsync({
        currentPassword,
        newPassword,
      });
      setPasswordChangeSuccess(true);
      setCurrentPassword("");
      setNewPassword("");
      setConfirmPassword("");
    } catch (error) {
      if (error instanceof ApiError) {
        if (error.status === 401) {
          setPasswordChangeError(t("changePassword.invalidPassword"));
        } else if (error.status === 429 || error.code === "RATE_LIMITED") {
          setPasswordChangeError(t("changePassword.rateLimited"));
        } else {
          setPasswordChangeError(error.message);
        }
      } else {
        setPasswordChangeError(t("changePassword.failed"));
      }
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <h1 className="font-semibold text-2xl">{t("title")}</h1>

      {isError ? (
        <p className="text-destructive-foreground text-sm" role="alert">
          {t("loadFailed")}
        </p>
      ) : null}

      {isLoading ? (
        <Skeleton className="h-96 w-full max-w-lg" />
      ) : (
        <div className="flex max-w-lg flex-col gap-8">
          <SettingsSection
            description={t("account.description")}
            icon={<UserIcon aria-hidden="true" />}
            title={t("account.title")}
          >
            <Field>
              <FieldLabel>{t("account.currentEmail")}</FieldLabel>
              <Input readOnly type="email" value={session?.user?.email ?? ""} />
            </Field>
          </SettingsSection>

          <SettingsSection
            description={t("changeEmail.description")}
            icon={<MailIcon aria-hidden="true" />}
            title={t("changeEmail.title")}
          >
            {emailChangePending ? (
              <Alert variant="success">
                <CircleCheckIcon />
                <AlertTitle>{t("changeEmail.pendingTitle")}</AlertTitle>
                <AlertDescription>
                  {t("changeEmail.pendingDescription")}
                </AlertDescription>
              </Alert>
            ) : null}
            <Field>
              <FieldLabel htmlFor={newEmailId}>
                {t("changeEmail.newEmail")}
              </FieldLabel>
              <Input
                autoComplete="email"
                id={newEmailId}
                onChange={(event) => {
                  setNewEmail(event.target.value);
                  setEmailValidationError(null);
                  setEmailChangeError(null);
                }}
                type="email"
                value={newEmail}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor={emailPasswordId}>
                {t("changeEmail.currentPassword")}
              </FieldLabel>
              <Input
                autoComplete="current-password"
                id={emailPasswordId}
                onChange={(event) => {
                  setEmailCurrentPassword(event.target.value);
                  setEmailValidationError(null);
                  setEmailChangeError(null);
                }}
                type="password"
                value={emailCurrentPassword}
              />
            </Field>
            {emailValidationError ? (
              <p className="text-destructive-foreground text-xs" role="alert">
                {emailValidationError}
              </p>
            ) : null}
            {emailChangeError ? (
              <Alert variant="error">
                <AlertTitle>{t("changeEmail.failed")}</AlertTitle>
                <AlertDescription>{emailChangeError}</AlertDescription>
              </Alert>
            ) : null}
            <Button
              disabled={requestEmailChange.isPending}
              onClick={() => {
                void handleEmailChange();
              }}
            >
              {requestEmailChange.isPending ? (
                <Spinner className="size-4" />
              ) : null}
              {t("changeEmail.submit")}
            </Button>
          </SettingsSection>

          <SettingsSection
            description={t("changePassword.description")}
            icon={<KeyRoundIcon aria-hidden="true" />}
            title={t("changePassword.title")}
          >
            {passwordChangeSuccess ? (
              <Alert variant="success">
                <CircleCheckIcon />
                <AlertTitle>{t("changePassword.successTitle")}</AlertTitle>
                <AlertDescription>
                  {t("changePassword.successDescription")}
                </AlertDescription>
              </Alert>
            ) : null}
            <Field>
              <FieldLabel htmlFor={currentPasswordId}>
                {t("changePassword.currentPassword")}
              </FieldLabel>
              <Input
                autoComplete="current-password"
                id={currentPasswordId}
                onChange={(event) => {
                  setCurrentPassword(event.target.value);
                  setPasswordValidationError(null);
                  setPasswordChangeError(null);
                  setPasswordChangeSuccess(false);
                }}
                type="password"
                value={currentPassword}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor={newPasswordId}>
                {t("changePassword.newPassword")}
              </FieldLabel>
              <Input
                autoComplete="new-password"
                id={newPasswordId}
                onChange={(event) => {
                  setNewPassword(event.target.value);
                  setPasswordValidationError(null);
                  setPasswordChangeError(null);
                  setPasswordChangeSuccess(false);
                }}
                type="password"
                value={newPassword}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor={confirmPasswordId}>
                {t("changePassword.confirmPassword")}
              </FieldLabel>
              <Input
                autoComplete="new-password"
                id={confirmPasswordId}
                onChange={(event) => {
                  setConfirmPassword(event.target.value);
                  setPasswordValidationError(null);
                  setPasswordChangeError(null);
                  setPasswordChangeSuccess(false);
                }}
                type="password"
                value={confirmPassword}
              />
            </Field>
            {passwordValidationError ? (
              <p className="text-destructive-foreground text-xs" role="alert">
                {passwordValidationError}
              </p>
            ) : null}
            {passwordChangeError ? (
              <Alert variant="error">
                <AlertTitle>{t("changePassword.failed")}</AlertTitle>
                <AlertDescription>{passwordChangeError}</AlertDescription>
              </Alert>
            ) : null}
            <Button
              disabled={changePassword.isPending}
              onClick={() => {
                void handlePasswordChange();
              }}
            >
              {changePassword.isPending ? <Spinner className="size-4" /> : null}
              {t("changePassword.submit")}
            </Button>
          </SettingsSection>
        </div>
      )}
    </div>
  );
}
