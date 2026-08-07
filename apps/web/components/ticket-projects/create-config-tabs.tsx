"use client";

import { useTranslations } from "next-intl";
import { useId } from "react";
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Tabs, TabsList, TabsPanel, TabsTab } from "@/components/ui/tabs";
import type { TicketIntegrationKind } from "@/lib/integrations/kinds";
import { TICKET_INTEGRATION_KINDS } from "@/lib/integrations/kinds";

type CreateConfigTabsProps = {
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
  kind,
  values,
  onChange,
}: CreateConfigTabsProps): React.ReactElement | null {
  const t = useTranslations("ticket-projects");
  const statusId = useId();
  const priorityId = useId();
  const issueTypeId = useId();
  const initialStatusId = useId();
  const stateIdFieldId = useId();

  if (!kind) {
    return null;
  }

  const activeTab = kind;

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
          <Field name="phasicalStatus">
            <FieldLabel htmlFor={statusId}>{t("phasical.status")}</FieldLabel>
            <Input
              id={statusId}
              onChange={(event) =>
                onChange(
                  updateStringField(values, "status", event.target.value),
                )
              }
              value={String(values.status ?? "")}
            />
          </Field>
          <Field name="phasicalPriority">
            <FieldLabel htmlFor={priorityId}>
              {t("phasical.priority")}
            </FieldLabel>
            <Input
              id={priorityId}
              onChange={(event) =>
                onChange(
                  updateStringField(values, "priority", event.target.value),
                )
              }
              value={String(values.priority ?? "")}
            />
          </Field>
        </TabsPanel>

        <TabsPanel className="flex flex-col gap-4 pt-2" value="jira">
          <Field name="jiraIssueType">
            <FieldLabel htmlFor={issueTypeId}>{t("jira.issueType")}</FieldLabel>
            <Input
              id={issueTypeId}
              onChange={(event) =>
                onChange(
                  updateStringField(values, "issueType", event.target.value),
                )
              }
              value={String(values.issueType ?? "")}
            />
          </Field>
          <Field name="jiraPriority">
            <FieldLabel htmlFor={priorityId}>{t("jira.priority")}</FieldLabel>
            <Input
              id={priorityId}
              onChange={(event) =>
                onChange(
                  updateStringField(values, "priority", event.target.value),
                )
              }
              value={String(values.priority ?? "")}
            />
          </Field>
          <Field name="jiraInitialStatus">
            <FieldLabel htmlFor={initialStatusId}>
              {t("jira.initialStatus")}
            </FieldLabel>
            <Input
              id={initialStatusId}
              onChange={(event) =>
                onChange(
                  updateStringField(
                    values,
                    "initialStatus",
                    event.target.value,
                  ),
                )
              }
              value={String(values.initialStatus ?? "")}
            />
          </Field>
        </TabsPanel>

        <TabsPanel className="flex flex-col gap-4 pt-2" value="linear">
          <Field name="linearPriority">
            <FieldLabel htmlFor={priorityId}>{t("linear.priority")}</FieldLabel>
            <Input
              id={priorityId}
              inputMode="numeric"
              onChange={(event) =>
                onChange(
                  updateStringField(values, "priority", event.target.value),
                )
              }
              value={String(values.priority ?? "")}
            />
          </Field>
          <Field name="linearStateId">
            <FieldLabel htmlFor={stateIdFieldId}>
              {t("linear.stateId")}
            </FieldLabel>
            <Input
              id={stateIdFieldId}
              onChange={(event) =>
                onChange(
                  updateStringField(values, "stateId", event.target.value),
                )
              }
              value={String(values.stateId ?? "")}
            />
          </Field>
        </TabsPanel>
      </Tabs>
    </div>
  );
}
