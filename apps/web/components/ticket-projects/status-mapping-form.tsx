"use client";

import { InfoIcon } from "lucide-react";
import { useTranslations } from "next-intl";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import type { StatusMappingValues } from "@/lib/ticket-projects/status-mapping";
import {
  formatStatusList,
  parseStatusList,
} from "@/lib/ticket-projects/status-mapping";

type StatusMappingFormProps = {
  values: StatusMappingValues;
  onChange: (values: StatusMappingValues) => void;
};

export function StatusMappingForm({
  values,
  onChange,
}: StatusMappingFormProps): React.ReactElement {
  const t = useTranslations("ticket-projects");

  return (
    <div className="flex flex-col gap-4">
      <Alert variant="warning">
        <InfoIcon />
        <AlertTitle>{t("mappingNoteTitle")}</AlertTitle>
        <AlertDescription>{t("mappingNoteDescription")}</AlertDescription>
      </Alert>

      <Field name="statusOpen">
        <FieldLabel>
          {t("statusOpen")} <span aria-hidden="true">*</span>
        </FieldLabel>
        <Input
          name="statusOpen"
          onChange={(event) =>
            onChange({
              ...values,
              open: parseStatusList(event.target.value),
            })
          }
          placeholder={t("statusListPlaceholder")}
          value={formatStatusList(values.open)}
        />
        <FieldDescription>{t("statusListHint")}</FieldDescription>
      </Field>

      <Field name="statusDone">
        <FieldLabel>
          {t("statusDone")} <span aria-hidden="true">*</span>
        </FieldLabel>
        <Input
          name="statusDone"
          onChange={(event) =>
            onChange({
              ...values,
              done: parseStatusList(event.target.value),
            })
          }
          placeholder={t("statusListPlaceholder")}
          value={formatStatusList(values.done)}
        />
        <FieldDescription>{t("statusListHint")}</FieldDescription>
      </Field>

      <Field name="statusCancelled">
        <FieldLabel>
          {t("statusCancelled")} <span aria-hidden="true">*</span>
        </FieldLabel>
        <Input
          name="statusCancelled"
          onChange={(event) =>
            onChange({
              ...values,
              cancelled: parseStatusList(event.target.value),
            })
          }
          placeholder={t("statusListPlaceholder")}
          value={formatStatusList(values.cancelled)}
        />
        <FieldDescription>{t("statusListHint")}</FieldDescription>
      </Field>

      <Field name="statusSuperseded">
        <FieldLabel>
          {t("statusSuperseded")} <span aria-hidden="true">*</span>
        </FieldLabel>
        <Input
          name="statusSuperseded"
          onChange={(event) =>
            onChange({
              ...values,
              superseded: event.target.value,
            })
          }
          placeholder={t("statusSupersededPlaceholder")}
          value={values.superseded}
        />
        <FieldDescription>{t("statusSupersededHint")}</FieldDescription>
      </Field>
    </div>
  );
}
