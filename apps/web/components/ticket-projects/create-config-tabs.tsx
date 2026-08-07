"use client";

import { useTranslations } from "next-intl";
import { useId } from "react";
import { MetadataSelect } from "@/components/ticket-projects/metadata-select";
import { Field, FieldLabel } from "@/components/ui/field";
import { Tabs, TabsList, TabsPanel, TabsTab } from "@/components/ui/tabs";
import {
  useTicketMetadataIssueTypes,
  useTicketMetadataPriorities,
  useTicketMetadataStatuses,
} from "@/lib/hooks/use-ticket-metadata";
import type { TicketIntegrationKind } from "@/lib/integrations/kinds";
import { TICKET_INTEGRATION_KINDS } from "@/lib/integrations/kinds";

type CreateConfigTabsProps = {
  integrationId: string | null;
  externalProjectId: string;
  kind: TicketIntegrationKind | null;
  values: Record<string, unknown>;
  onChange: (values: Record<string, unknown>) => void;
};

function updateStringField(
  values: Record<string, unknown>,
  key: string,
  next: string,
): Record<string, unknown> {
  return { ...values, [key]: next };
}

export function CreateConfigTabs({
  integrationId,
  externalProjectId,
  kind,
  values,
  onChange,
}: CreateConfigTabsProps): React.ReactElement | null {
  const t = useTranslations("ticket-projects");
  const statusId = useId();
  const priorityId = useId();
  const issueTypeId = useId();
  const stateIdFieldId = useId();

  const metadataEnabled = Boolean(integrationId && externalProjectId && kind);
  const statusesQuery = useTicketMetadataStatuses(
    integrationId,
    externalProjectId,
    metadataEnabled,
  );
  const prioritiesQuery = useTicketMetadataPriorities(
    integrationId,
    externalProjectId,
    metadataEnabled,
  );
  const issueTypesQuery = useTicketMetadataIssueTypes(
    integrationId,
    externalProjectId,
    metadataEnabled && kind === "jira",
  );

  if (!kind) {
    return null;
  }

  const activeTab = kind;
  const statuses = statusesQuery.data?.items ?? [];
  const priorities = prioritiesQuery.data?.items ?? [];
  const issueTypes = issueTypesQuery.data?.items ?? [];

  return (
    <div className="flex flex-col gap-2">
      <p className="font-medium text-sm">{t("createConfigTitle")}</p>
      <Tabs value={activeTab}>
        <TabsList>
          {TICKET_INTEGRATION_KINDS.map((ticketKind) => (
            <TabsTab
              disabled={ticketKind !== kind}
              key={ticketKind}
              value={ticketKind}
            >
              {t(`kinds.${ticketKind}`)}
            </TabsTab>
          ))}
        </TabsList>

        <TabsPanel className="flex flex-col gap-4 pt-2" value="phasical">
          <MetadataSelect
            disabled={!metadataEnabled}
            errorMessage={statusesQuery.data?.message}
            id={statusId}
            includeMissingValue
            isError={statusesQuery.isError}
            isLoading={statusesQuery.isLoading}
            items={statuses}
            label={t("phasical.status")}
            name="phasicalStatus"
            onValueChange={(next) =>
              onChange(updateStringField(values, "status", next))
            }
            placeholder={t("metadata.statusPlaceholder")}
            required
            value={String(values.status ?? "")}
          />
          <MetadataSelect
            disabled={!metadataEnabled}
            errorMessage={prioritiesQuery.data?.message}
            id={priorityId}
            includeMissingValue
            isError={prioritiesQuery.isError}
            isLoading={prioritiesQuery.isLoading}
            items={priorities}
            label={t("phasical.priority")}
            name="phasicalPriority"
            onValueChange={(next) =>
              onChange(updateStringField(values, "priority", next))
            }
            placeholder={t("metadata.priorityPlaceholder")}
            required
            value={String(values.priority ?? "")}
          />
        </TabsPanel>

        <TabsPanel className="flex flex-col gap-4 pt-2" value="jira">
          <MetadataSelect
            disabled={!metadataEnabled}
            errorMessage={issueTypesQuery.data?.message}
            id={issueTypeId}
            includeMissingValue
            isError={issueTypesQuery.isError}
            isLoading={issueTypesQuery.isLoading}
            items={issueTypes}
            label={t("jira.issueType")}
            name="jiraIssueType"
            onValueChange={(next) =>
              onChange(updateStringField(values, "issueType", next))
            }
            placeholder={t("metadata.issueTypePlaceholder")}
            required
            value={String(values.issueType ?? "")}
          />
          <MetadataSelect
            disabled={!metadataEnabled}
            errorMessage={prioritiesQuery.data?.message}
            id={priorityId}
            includeMissingValue
            isError={prioritiesQuery.isError}
            isLoading={prioritiesQuery.isLoading}
            items={priorities}
            label={t("jira.priority")}
            name="jiraPriority"
            onValueChange={(next) =>
              onChange(updateStringField(values, "priority", next))
            }
            placeholder={t("metadata.priorityPlaceholder")}
            value={String(values.priority ?? "")}
          />
          <MetadataSelect
            disabled={!metadataEnabled}
            errorMessage={statusesQuery.data?.message}
            id={statusId}
            includeMissingValue
            isError={statusesQuery.isError}
            isLoading={statusesQuery.isLoading}
            items={statuses}
            label={t("jira.initialStatus")}
            name="jiraInitialStatus"
            onValueChange={(next) =>
              onChange(updateStringField(values, "initialStatus", next))
            }
            placeholder={t("metadata.statusPlaceholder")}
            value={String(values.initialStatus ?? "")}
          />
        </TabsPanel>

        <TabsPanel className="flex flex-col gap-4 pt-2" value="linear">
          <MetadataSelect
            disabled={!metadataEnabled}
            errorMessage={prioritiesQuery.data?.message}
            id={priorityId}
            includeMissingValue
            isError={prioritiesQuery.isError}
            isLoading={prioritiesQuery.isLoading}
            items={priorities}
            label={t("linear.priority")}
            name="linearPriority"
            onValueChange={(next) =>
              onChange(updateStringField(values, "priority", next))
            }
            placeholder={t("metadata.priorityPlaceholder")}
            value={String(values.priority ?? "")}
          />
          <MetadataSelect
            disabled={!metadataEnabled}
            errorMessage={statusesQuery.data?.message}
            id={stateIdFieldId}
            includeMissingValue
            isError={statusesQuery.isError}
            isLoading={statusesQuery.isLoading}
            items={statuses}
            label={t("linear.stateId")}
            name="linearStateId"
            onValueChange={(next) =>
              onChange(updateStringField(values, "stateId", next))
            }
            placeholder={t("metadata.statePlaceholder")}
            required
            value={String(values.stateId ?? "")}
          />
        </TabsPanel>
      </Tabs>
      {!metadataEnabled ? (
        <Field name="createConfigHint">
          <FieldLabel className="sr-only">
            {t("metadata.projectRequired")}
          </FieldLabel>
          <p className="text-muted-foreground text-sm">
            {t("metadata.projectRequired")}
          </p>
        </Field>
      ) : null}
    </div>
  );
}
