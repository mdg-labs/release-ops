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
import type { TicketMetadataItem } from "@/lib/query/types";
import type { StatusMappingValues } from "@/lib/ticket-projects/status-mapping";
import { cn } from "@/lib/utils";

type StatusMappingFormProps = {
  integrationId: string | null;
  externalProjectId: string;
  values: StatusMappingValues;
  onChange: (values: StatusMappingValues) => void;
};

type StatusGroupProps = {
  kind: "open" | "done" | "cancelled";
  groupId: string;
  label: string;
  hint: string;
  statuses: TicketMetadataItem[];
  value: string[];
  onValueChange: (value: string[]) => void;
  className?: string;
};

function StatusGroup({
  kind,
  groupId,
  label,
  hint,
  statuses,
  value,
  onValueChange,
  className,
}: StatusGroupProps): React.ReactElement {
  const labelId = `${groupId}-${kind}`;
  const hintId = `${labelId}-hint`;

  return (
    <div className={cn("flex flex-col items-start gap-2", className)}>
      <Label render={<p id={labelId} />}>
        {label} <span aria-hidden="true">*</span>
      </Label>
      <CheckboxGroup
        aria-describedby={hintId}
        aria-labelledby={labelId}
        onValueChange={onValueChange}
        value={value}
      >
        {statuses.map((status) => (
          <Label key={status.id}>
            <Checkbox value={status.id} />
            {status.label?.trim() || status.name}
          </Label>
        ))}
      </CheckboxGroup>
      <p className="text-muted-foreground text-xs" id={hintId}>
        {hint}
      </p>
    </div>
  );
}

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
            <StatusGroup
              groupId={groupId}
              hint={t("statusOpenHint")}
              kind="open"
              label={t("statusOpen")}
              onValueChange={(next) => onChange({ ...values, open: next })}
              statuses={statuses}
              value={values.open}
            />
            <StatusGroup
              groupId={groupId}
              hint={t("statusDoneHint")}
              kind="done"
              label={t("statusDone")}
              onValueChange={(next) => onChange({ ...values, done: next })}
              statuses={statuses}
              value={values.done}
            />
            <StatusGroup
              className="md:col-span-2"
              groupId={groupId}
              hint={t("statusCancelledHint")}
              kind="cancelled"
              label={t("statusCancelled")}
              onValueChange={(next) => onChange({ ...values, cancelled: next })}
              statuses={statuses}
              value={values.cancelled}
            />
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
