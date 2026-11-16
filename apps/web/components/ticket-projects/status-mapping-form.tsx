"use client";

import { InfoIcon } from "lucide-react";
import { useTranslations } from "next-intl";
import { useId } from "react";
import { MetadataSelect } from "@/components/ticket-projects/metadata-select";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Checkbox } from "@/components/ui/checkbox";
import { CheckboxGroup } from "@/components/ui/checkbox-group";
import { Label } from "@/components/ui/label";
import { useTicketMetadataStatuses } from "@/lib/hooks/use-ticket-metadata";
import type { StatusMappingValues } from "@/lib/ticket-projects/status-mapping";

type StatusMappingFormProps = {
  integrationId: string | null;
  externalProjectId: string;
  values: StatusMappingValues;
  onChange: (values: StatusMappingValues) => void;
};

export function StatusMappingForm({
  integrationId,
  externalProjectId,
  values,
  onChange,
}: StatusMappingFormProps): React.ReactElement {
  const t = useTranslations("ticket-projects");
  const supersededId = useId();
  const groupId = useId();

  const metadataEnabled = Boolean(integrationId && externalProjectId);
  const statusesQuery = useTicketMetadataStatuses(
    integrationId,
    externalProjectId,
    metadataEnabled,
  );
  const statuses = statusesQuery.data?.items ?? [];
  const metadataMessage = statusesQuery.data?.message;

  return (
    <div className="flex flex-col gap-4">
      <Alert variant="warning">
        <InfoIcon />
        <AlertTitle>{t("mappingNoteTitle")}</AlertTitle>
        <AlertDescription>{t("mappingNoteDescription")}</AlertDescription>
      </Alert>

      {!metadataEnabled ? (
        <p className="text-muted-foreground text-sm">
          {t("metadata.projectRequired")}
        </p>
      ) : null}

      {metadataEnabled && statusesQuery.isLoading ? (
        <p className="text-muted-foreground text-sm">{t("metadata.loading")}</p>
      ) : null}

      {metadataEnabled && (statusesQuery.isError || metadataMessage) ? (
        <p className="text-destructive-foreground text-sm" role="alert">
          {metadataMessage ?? t("metadata.loadFailed")}
        </p>
      ) : null}

      {metadataEnabled && !statusesQuery.isLoading && statuses.length === 0 ? (
        <p className="text-muted-foreground text-sm">{t("metadata.empty")}</p>
      ) : null}

      {metadataEnabled && statuses.length > 0 ? (
        <>
          <div className="grid gap-4 md:grid-cols-2">
            <div className="flex flex-col items-start gap-2">
              <p
                className="inline-flex items-center gap-2 font-medium text-base/4.5 text-foreground sm:text-sm/4"
                id={`${groupId}-open`}
              >
                {t("statusOpen")} <span aria-hidden="true">*</span>
              </p>
              <CheckboxGroup
                aria-describedby={`${groupId}-open-hint`}
                aria-labelledby={`${groupId}-open`}
                onValueChange={(next) => onChange({ ...values, open: next })}
                value={values.open}
              >
                {statuses.map((status) => (
                  <Label key={status.id}>
                    <Checkbox value={status.id} />
                    {status.label?.trim() || status.name}
                  </Label>
                ))}
              </CheckboxGroup>
              <p
                className="text-muted-foreground text-xs"
                id={`${groupId}-open-hint`}
              >
                {t("statusOpenHint")}
              </p>
            </div>

            <div className="flex flex-col items-start gap-2">
              <p
                className="inline-flex items-center gap-2 font-medium text-base/4.5 text-foreground sm:text-sm/4"
                id={`${groupId}-done`}
              >
                {t("statusDone")} <span aria-hidden="true">*</span>
              </p>
              <CheckboxGroup
                aria-describedby={`${groupId}-done-hint`}
                aria-labelledby={`${groupId}-done`}
                onValueChange={(next) => onChange({ ...values, done: next })}
                value={values.done}
              >
                {statuses.map((status) => (
                  <Label key={status.id}>
                    <Checkbox value={status.id} />
                    {status.label?.trim() || status.name}
                  </Label>
                ))}
              </CheckboxGroup>
              <p
                className="text-muted-foreground text-xs"
                id={`${groupId}-done-hint`}
              >
                {t("statusDoneHint")}
              </p>
            </div>

            <div className="flex flex-col items-start gap-2 md:col-span-2">
              <p
                className="inline-flex items-center gap-2 font-medium text-base/4.5 text-foreground sm:text-sm/4"
                id={`${groupId}-cancelled`}
              >
                {t("statusCancelled")} <span aria-hidden="true">*</span>
              </p>
              <CheckboxGroup
                aria-describedby={`${groupId}-cancelled-hint`}
                aria-labelledby={`${groupId}-cancelled`}
                onValueChange={(next) =>
                  onChange({ ...values, cancelled: next })
                }
                value={values.cancelled}
              >
                {statuses.map((status) => (
                  <Label key={status.id}>
                    <Checkbox value={status.id} />
                    {status.label?.trim() || status.name}
                  </Label>
                ))}
              </CheckboxGroup>
              <p
                className="text-muted-foreground text-xs"
                id={`${groupId}-cancelled-hint`}
              >
                {t("statusCancelledHint")}
              </p>
            </div>
          </div>

          <MetadataSelect
            description={t("statusSupersededHint")}
            errorMessage={metadataMessage}
            id={supersededId}
            includeMissingValue
            isError={statusesQuery.isError}
            isLoading={statusesQuery.isLoading}
            items={statuses}
            label={t("statusSuperseded")}
            name="statusSuperseded"
            onValueChange={(next) => onChange({ ...values, superseded: next })}
            placeholder={t("metadata.statusPlaceholder")}
            required
            value={values.superseded}
          />
        </>
      ) : null}
    </div>
  );
}
