"use client";

import { CircleCheckIcon, ClockIcon } from "lucide-react";
import { useTranslations } from "next-intl";
import { useEffect, useState } from "react";
import { SettingsSection } from "@/components/common/settings-section";
import { PageHeader } from "@/components/common/page-header";
import { SettingsNav } from "@/components/settings/settings-nav";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field";
import {
  NumberField,
  NumberFieldDecrement,
  NumberFieldGroup,
  NumberFieldIncrement,
  NumberFieldInput,
} from "@/components/ui/number-field";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { ApiError } from "@/lib/api/client";
import { useSettings } from "@/lib/hooks/use-settings";

const MIN_POLL_INTERVAL_MINUTES = 5;

type SettingsFormState = {
  pollIntervalMinutes: number | null;
};

export function SettingsView(): React.ReactElement {
  const t = useTranslations("settings");
  const tCommon = useTranslations("common");
  const { data, isLoading, isError, updateSettings } = useSettings();

  const [form, setForm] = useState<SettingsFormState>({
    pollIntervalMinutes: null,
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
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-6">
      <PageHeader title={t("title")} />
      <SettingsNav />

      {isError ? (
        <p className="text-destructive-foreground text-sm" role="alert">
          {t("loadFailed")}
        </p>
      ) : null}

      {isLoading ? (
        <Skeleton className="h-96 w-full" />
      ) : (
        <div className="flex flex-col gap-8">
          <SettingsSection
            description={t("pollIntervalDescription")}
            icon={<ClockIcon aria-hidden="true" />}
            title={t("pollIntervalSection")}
          >
            <Field>
              <FieldLabel>{t("pollInterval")}</FieldLabel>
              <NumberField
                min={MIN_POLL_INTERVAL_MINUTES}
                value={form.pollIntervalMinutes ?? MIN_POLL_INTERVAL_MINUTES}
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
            {validationError ? (
              <FieldError>{validationError}</FieldError>
            ) : null}
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
            <Button
              disabled={updateSettings.isPending}
              onClick={() => {
                void handleSave();
              }}
            >
              {updateSettings.isPending ? <Spinner className="size-4" /> : null}
              {tCommon("save")}
            </Button>
          </SettingsSection>
        </div>
      )}
    </div>
  );
}
