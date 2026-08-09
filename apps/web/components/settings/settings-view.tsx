"use client";

import { useTranslations } from "next-intl";
import { useEffect, useState } from "react";
import { CircleCheckIcon } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardDescription,
  CardFooter,
  CardHeader,
  CardPanel,
  CardTitle,
} from "@/components/ui/card";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field";
import { Frame, FrameFooter } from "@/components/ui/frame";
import {
  NumberField,
  NumberFieldDecrement,
  NumberFieldGroup,
  NumberFieldIncrement,
  NumberFieldInput,
} from "@/components/ui/number-field";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { SettingsNav } from "@/components/settings/settings-nav";
import { ApiError } from "@/lib/api/client";
import { useSettings } from "@/lib/hooks/use-settings";

const MIN_POLL_INTERVAL_MINUTES = 5;
const MIN_INVITE_TOKEN_EXPIRY_HOURS = 1;
const MAX_INVITE_TOKEN_EXPIRY_HOURS = 720;
const MIN_PASSWORD_RESET_TOKEN_EXPIRY_MINUTES = 5;
const MAX_PASSWORD_RESET_TOKEN_EXPIRY_MINUTES = 1440;

type SettingsFormState = {
  pollIntervalMinutes: number | null;
  inviteTokenExpiryHours: number | null;
  passwordResetTokenExpiryMinutes: number | null;
};

export function SettingsView(): React.ReactElement {
  const t = useTranslations("settings");
  const tCommon = useTranslations("common");
  const { data, isLoading, isError, updateSettings } = useSettings();

  const [form, setForm] = useState<SettingsFormState>({
    pollIntervalMinutes: null,
    inviteTokenExpiryHours: null,
    passwordResetTokenExpiryMinutes: null,
  });
  const [validationError, setValidationError] = useState<string | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [showSuccess, setShowSuccess] = useState(false);

  useEffect(() => {
    if (!data) {
      return;
    }
    setForm({
      pollIntervalMinutes: data.pollIntervalMinutes,
      inviteTokenExpiryHours: data.inviteTokenExpiryHours,
      passwordResetTokenExpiryMinutes: data.passwordResetTokenExpiryMinutes,
    });
    setShowSuccess(false);
  }, [data]);

  function validateForm(): string | null {
    if (
      form.pollIntervalMinutes === null ||
      form.pollIntervalMinutes < MIN_POLL_INTERVAL_MINUTES
    ) {
      return t("validation.minInterval");
    }
    if (
      form.inviteTokenExpiryHours === null ||
      form.inviteTokenExpiryHours < MIN_INVITE_TOKEN_EXPIRY_HOURS ||
      form.inviteTokenExpiryHours > MAX_INVITE_TOKEN_EXPIRY_HOURS
    ) {
      return t("validation.inviteExpiryRange");
    }
    if (
      form.passwordResetTokenExpiryMinutes === null ||
      form.passwordResetTokenExpiryMinutes <
        MIN_PASSWORD_RESET_TOKEN_EXPIRY_MINUTES ||
      form.passwordResetTokenExpiryMinutes >
        MAX_PASSWORD_RESET_TOKEN_EXPIRY_MINUTES
    ) {
      return t("validation.resetExpiryRange");
    }
    return null;
  }

  async function handleSave(): Promise<void> {
    setSaveError(null);
    setShowSuccess(false);

    const error = validateForm();
    if (error) {
      setValidationError(error);
      return;
    }

    setValidationError(null);

    try {
      await updateSettings.mutateAsync({
        pollIntervalMinutes: form.pollIntervalMinutes ?? undefined,
        inviteTokenExpiryHours: form.inviteTokenExpiryHours ?? undefined,
        passwordResetTokenExpiryMinutes:
          form.passwordResetTokenExpiryMinutes ?? undefined,
      });
      setShowSuccess(true);
    } catch (error) {
      if (error instanceof ApiError) {
        setSaveError(error.message);
      } else {
        setSaveError(t("saveFailed"));
      }
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <h1 className="font-semibold text-2xl">{t("title")}</h1>
      <SettingsNav />

      {isError ? (
        <p className="text-destructive-foreground text-sm" role="alert">
          {t("loadFailed")}
        </p>
      ) : null}

      {isLoading ? (
        <Skeleton className="h-96 w-full max-w-lg" />
      ) : (
        <div className="flex max-w-lg flex-col gap-6">
          <Frame>
            <Card>
              <CardHeader>
                <CardTitle>{t("pollIntervalSection")}</CardTitle>
                <CardDescription>
                  {t("pollIntervalDescription")}
                </CardDescription>
              </CardHeader>
              <CardPanel>
                <Field>
                  <FieldLabel>{t("pollInterval")}</FieldLabel>
                  <NumberField
                    min={MIN_POLL_INTERVAL_MINUTES}
                    value={
                      form.pollIntervalMinutes ?? MIN_POLL_INTERVAL_MINUTES
                    }
                    onValueChange={(value) => {
                      if (value !== null) {
                        setForm((current) => ({
                          ...current,
                          pollIntervalMinutes: value,
                        }));
                      }
                      setValidationError(null);
                      setShowSuccess(false);
                    }}
                  >
                    <NumberFieldGroup>
                      <NumberFieldDecrement />
                      <NumberFieldInput />
                      <NumberFieldIncrement />
                    </NumberFieldGroup>
                  </NumberField>
                  <FieldDescription>{t("pollIntervalHint")}</FieldDescription>
                </Field>
              </CardPanel>
            </Card>
          </Frame>

          <Frame>
            <Card>
              <CardHeader>
                <CardTitle>{t("tokenExpirySection")}</CardTitle>
                <CardDescription>{t("tokenExpiryDescription")}</CardDescription>
              </CardHeader>
              <CardPanel className="flex flex-col gap-4">
                <Field>
                  <FieldLabel>{t("inviteTokenExpiry")}</FieldLabel>
                  <NumberField
                    min={MIN_INVITE_TOKEN_EXPIRY_HOURS}
                    max={MAX_INVITE_TOKEN_EXPIRY_HOURS}
                    value={
                      form.inviteTokenExpiryHours ??
                      MIN_INVITE_TOKEN_EXPIRY_HOURS
                    }
                    onValueChange={(value) => {
                      if (value !== null) {
                        setForm((current) => ({
                          ...current,
                          inviteTokenExpiryHours: value,
                        }));
                      }
                      setValidationError(null);
                      setShowSuccess(false);
                    }}
                  >
                    <NumberFieldGroup>
                      <NumberFieldDecrement />
                      <NumberFieldInput />
                      <NumberFieldIncrement />
                    </NumberFieldGroup>
                  </NumberField>
                  <FieldDescription>
                    {t("inviteTokenExpiryHint")}
                  </FieldDescription>
                </Field>
                <Field>
                  <FieldLabel>{t("resetTokenExpiry")}</FieldLabel>
                  <NumberField
                    min={MIN_PASSWORD_RESET_TOKEN_EXPIRY_MINUTES}
                    max={MAX_PASSWORD_RESET_TOKEN_EXPIRY_MINUTES}
                    value={
                      form.passwordResetTokenExpiryMinutes ??
                      MIN_PASSWORD_RESET_TOKEN_EXPIRY_MINUTES
                    }
                    onValueChange={(value) => {
                      if (value !== null) {
                        setForm((current) => ({
                          ...current,
                          passwordResetTokenExpiryMinutes: value,
                        }));
                      }
                      setValidationError(null);
                      setShowSuccess(false);
                    }}
                  >
                    <NumberFieldGroup>
                      <NumberFieldDecrement />
                      <NumberFieldInput />
                      <NumberFieldIncrement />
                    </NumberFieldGroup>
                  </NumberField>
                  <FieldDescription>
                    {t("resetTokenExpiryHint")}
                  </FieldDescription>
                </Field>
                {validationError ? (
                  <FieldError>{validationError}</FieldError>
                ) : null}
              </CardPanel>
              <CardFooter>
                <Button
                  disabled={updateSettings.isPending}
                  onClick={() => {
                    void handleSave();
                  }}
                >
                  {updateSettings.isPending ? (
                    <Spinner className="size-4" />
                  ) : null}
                  {tCommon("save")}
                </Button>
              </CardFooter>
            </Card>
            {showSuccess || saveError ? (
              <FrameFooter>
                {showSuccess ? (
                  <Alert variant="success">
                    <CircleCheckIcon />
                    <AlertTitle>{t("saved")}</AlertTitle>
                    <AlertDescription>{t("savedDescription")}</AlertDescription>
                  </Alert>
                ) : null}
                {saveError ? (
                  <Alert variant="error">
                    <AlertTitle>{t("saveFailed")}</AlertTitle>
                    <AlertDescription>{saveError}</AlertDescription>
                  </Alert>
                ) : null}
              </FrameFooter>
            ) : null}
          </Frame>
        </div>
      )}
    </div>
  );
}
