"use client";

import { CircleCheckIcon } from "lucide-react";
import { useTranslations } from "next-intl";
import { useEffect, useState } from "react";
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
import { ApiError } from "@/lib/api/client";
import { useSettings } from "@/lib/hooks/use-settings";

const MIN_POLL_INTERVAL_MINUTES = 5;

export function SettingsView(): React.ReactElement {
  const t = useTranslations("settings");
  const tCommon = useTranslations("common");
  const { data, isLoading, isError, updateSettings } = useSettings();

  const [pollIntervalMinutes, setPollIntervalMinutes] = useState<number | null>(
    null,
  );
  const [validationError, setValidationError] = useState<string | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [showSuccess, setShowSuccess] = useState(false);

  useEffect(() => {
    if (data?.pollIntervalMinutes !== undefined) {
      setPollIntervalMinutes(data.pollIntervalMinutes);
      setShowSuccess(false);
    }
  }, [data?.pollIntervalMinutes]);

  async function handleSave(): Promise<void> {
    setSaveError(null);
    setShowSuccess(false);

    if (
      pollIntervalMinutes === null ||
      pollIntervalMinutes < MIN_POLL_INTERVAL_MINUTES
    ) {
      setValidationError(t("validation.minInterval"));
      return;
    }

    setValidationError(null);

    try {
      await updateSettings.mutateAsync({
        pollIntervalMinutes,
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

      {isError ? (
        <p className="text-destructive-foreground text-sm" role="alert">
          {t("loadFailed")}
        </p>
      ) : null}

      {isLoading ? (
        <Skeleton className="h-48 w-full max-w-lg" />
      ) : (
        <Frame className="max-w-lg">
          <Card>
            <CardHeader>
              <CardTitle>{t("pollIntervalSection")}</CardTitle>
              <CardDescription>{t("pollIntervalDescription")}</CardDescription>
            </CardHeader>
            <CardPanel>
              <Field>
                <FieldLabel>{t("pollInterval")}</FieldLabel>
                <NumberField
                  min={MIN_POLL_INTERVAL_MINUTES}
                  value={pollIntervalMinutes ?? MIN_POLL_INTERVAL_MINUTES}
                  onValueChange={(value) => {
                    if (value !== null) {
                      setPollIntervalMinutes(value);
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
                {validationError ? (
                  <FieldError>{validationError}</FieldError>
                ) : null}
              </Field>
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
      )}
    </div>
  );
}
