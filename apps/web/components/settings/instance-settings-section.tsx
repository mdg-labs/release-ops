"use client";

import { CircleCheckIcon, LayersIcon, RadioTowerIcon } from "lucide-react";
import { useTranslations } from "next-intl";
import { useState } from "react";
import { SettingsSection } from "@/components/common/settings-section";
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
import { Spinner } from "@/components/ui/spinner";
import { ApiError } from "@/lib/api/client";
import { useUpdateSettings } from "@/lib/hooks/use-settings";
import type { Settings } from "@/lib/query/types";

const MIN_CONCURRENCY_CAP = 1;

function isValidCap(value: number | null): value is number {
  return (
    value !== null && Number.isInteger(value) && value >= MIN_CONCURRENCY_CAP
  );
}

/** The concurrency cap form and the polling-mode notice, for settings that have loaded. */
export function InstanceSettingsSection({
  settings,
}: {
  settings: Settings;
}): React.ReactElement {
  const t = useTranslations("settings.instance");
  const tCommon = useTranslations("common");
  const updateSettings = useUpdateSettings();

  const [draft, setDraft] = useState<number | null>(settings.concurrencyCap);
  const [validationError, setValidationError] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  async function handleSave(): Promise<void> {
    setSaveError(null);
    setSaved(false);
    if (!isValidCap(draft)) {
      setValidationError(true);
      return;
    }
    setValidationError(false);
    try {
      await updateSettings.mutateAsync({ concurrencyCap: draft });
      setSaved(true);
    } catch (error) {
      setSaveError(error instanceof ApiError ? error.message : t("saveFailed"));
    }
  }

  return (
    <>
      {settings.mode === "polling" ? (
        <Alert data-testid="polling-notice" variant="warning">
          <RadioTowerIcon />
          <AlertTitle>{t("pollingTitle")}</AlertTitle>
          <AlertDescription>{t("pollingDescription")}</AlertDescription>
        </Alert>
      ) : null}
      <SettingsSection
        description={t("description")}
        icon={<LayersIcon aria-hidden="true" />}
        title={t("title")}
      >
        <Field invalid={validationError}>
          <FieldLabel>{t("concurrencyCap")}</FieldLabel>
          <NumberField
            disabled={updateSettings.isPending}
            min={MIN_CONCURRENCY_CAP}
            onValueChange={(value) => {
              setDraft(value);
              setValidationError(false);
              setSaved(false);
            }}
            value={draft}
          >
            <NumberFieldGroup>
              <NumberFieldDecrement />
              <NumberFieldInput />
              <NumberFieldIncrement />
            </NumberFieldGroup>
          </NumberField>
          <FieldDescription>{t("concurrencyCapHint")}</FieldDescription>
          {validationError ? (
            <FieldError match>{t("minCap")}</FieldError>
          ) : null}
        </Field>
        {saved ? (
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
        <div>
          <Button
            disabled={updateSettings.isPending}
            onClick={() => {
              void handleSave();
            }}
          >
            {updateSettings.isPending ? <Spinner className="size-4" /> : null}
            {tCommon("save")}
          </Button>
        </div>
      </SettingsSection>
    </>
  );
}
