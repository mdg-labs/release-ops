"use client";

import { useTranslations } from "next-intl";
import { useEffect, useId, useMemo, useState } from "react";
import { CreateConfigTabs } from "@/components/ticket-projects/create-config-tabs";
import { ContentTemplateForm } from "@/components/ticket-projects/content-template-form";
import { MetadataSelect } from "@/components/ticket-projects/metadata-select";
import {
  ON_OPEN_TICKET_POLICIES,
  PolicySelect,
  type OnOpenTicketPolicy,
} from "@/components/ticket-projects/policy-select";
import { StatusMappingForm } from "@/components/ticket-projects/status-mapping-form";
import { Button } from "@/components/ui/button";
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectItem,
  SelectPopup,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Sheet,
  SheetClose,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetPanel,
  SheetPopup,
  SheetTitle,
} from "@/components/ui/sheet";
import { Tabs, TabsList, TabsPanel, TabsTab } from "@/components/ui/tabs";
import { ApiError } from "@/lib/api/client";
import {
  useTicketMetadataProjects,
  useTicketMetadataWorkspaces,
} from "@/lib/hooks/use-ticket-metadata";
import {
  kindIsTicket,
  type TicketIntegrationKind,
} from "@/lib/integrations/kinds";
import {
  createConfigFromRecord,
  defaultCreateConfig,
} from "@/lib/ticket-projects/create-config";
import {
  contentTemplatesFromRecord,
  defaultContentTemplates,
} from "@/lib/ticket-projects/content-templates";
import {
  defaultStatusMapping,
  statusMappingFromRecord,
  statusMappingToRecord,
  type StatusMappingValues,
} from "@/lib/ticket-projects/status-mapping";
import { cn } from "@/lib/utils";
import type {
  ContentTemplates,
  Integration,
  TicketProject,
} from "@/lib/query/types";

type ProjectDrawerMode = "create" | "edit";

type DrawerTab =
  "general" | "createConfig" | "statusMapping" | "contentTemplates";

const DRAWER_TABS: DrawerTab[] = [
  "general",
  "createConfig",
  "statusMapping",
  "contentTemplates",
];

type ProjectDrawerProps = {
  mode: ProjectDrawerMode;
  project: TicketProject | null;
  integrations: Integration[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreate: (input: {
    integrationId: string;
    externalProjectId: string;
    name: string;
    createConfig: Record<string, unknown>;
    statusMapping: Record<string, unknown>;
    contentTemplates: ContentTemplates;
    onOpenTicketPolicy: string;
  }) => Promise<TicketProject>;
  onUpdate: (
    id: string,
    input: {
      name: string;
      createConfig: Record<string, unknown>;
      statusMapping: Record<string, unknown>;
      contentTemplates: ContentTemplates;
      onOpenTicketPolicy: string;
    },
  ) => Promise<TicketProject>;
  isSaving: boolean;
};

type IntegrationOption = { label: string; value: string; kind: string };

type TabValidationError = {
  tab: DrawerTab;
  message: string;
};

function serializeCreateConfig(
  kind: TicketIntegrationKind,
  values: Record<string, unknown>,
): Record<string, unknown> {
  if (kind === "linear") {
    const priority = Number(values.priority);
    return {
      priority: Number.isFinite(priority) ? priority : 2,
      stateId: String(values.stateId ?? ""),
    };
  }

  return createConfigFromRecord(kind, values);
}

function validateStatusMapping(values: StatusMappingValues): string | null {
  if (values.open.length === 0) {
    return "statusOpenRequired";
  }
  if (values.done.length === 0) {
    return "statusDoneRequired";
  }
  if (values.cancelled.length === 0) {
    return "statusCancelledRequired";
  }
  if (!values.superseded.trim()) {
    return "statusSupersededRequired";
  }
  return null;
}

function validateCreateConfig(
  kind: TicketIntegrationKind,
  values: Record<string, unknown>,
): boolean {
  switch (kind) {
    case "phasical":
      return (
        String(values.status ?? "").trim() !== "" &&
        String(values.priority ?? "").trim() !== ""
      );
    case "jira":
      return String(values.issueType ?? "").trim() !== "";
    case "linear":
      return String(values.stateId ?? "").trim() !== "";
    default:
      return true;
  }
}

export function ProjectDrawer({
  mode,
  project,
  integrations,
  open,
  onOpenChange,
  onCreate,
  onUpdate,
  isSaving,
}: ProjectDrawerProps): React.ReactElement {
  const t = useTranslations("ticket-projects");
  const tCommon = useTranslations("common");
  const integrationIdField = useId();
  const workspaceIdField = useId();
  const externalProjectIdField = useId();
  const nameField = useId();

  const ticketIntegrations = useMemo(
    () => integrations.filter((integration) => kindIsTicket(integration.kind)),
    [integrations],
  );

  const integrationItems: IntegrationOption[] = ticketIntegrations.map(
    (integration) => ({
      label: integration.name,
      value: integration.id,
      kind: integration.kind,
    }),
  );

  const [integrationId, setIntegrationId] = useState("");
  const [workspaceId, setWorkspaceId] = useState("");
  const [externalProjectId, setExternalProjectId] = useState("");
  const [name, setName] = useState("");
  const [policy, setPolicy] = useState<OnOpenTicketPolicy>("supersede");
  const [createConfig, setCreateConfig] = useState<Record<string, unknown>>({});
  const [statusMapping, setStatusMapping] =
    useState<StatusMappingValues>(defaultStatusMapping);
  const [contentTemplates, setContentTemplates] = useState<ContentTemplates>(
    defaultContentTemplates(),
  );
  const [activeTab, setActiveTab] = useState<DrawerTab>("general");
  const [invalidTabs, setInvalidTabs] = useState<Set<DrawerTab>>(new Set());
  const [formError, setFormError] = useState<string | null>(null);

  const selectedIntegration =
    mode === "edit"
      ? integrations.find((item) => item.id === project?.integrationId)
      : ticketIntegrations.find((item) => item.id === integrationId);

  const activeKind = selectedIntegration?.kind as
    TicketIntegrationKind | undefined;

  const activeIntegrationId =
    mode === "edit" ? (project?.integrationId ?? null) : integrationId || null;

  const activeExternalProjectId =
    mode === "edit" ? (project?.externalProjectId ?? "") : externalProjectId;

  const metadataEnabled = open && Boolean(activeIntegrationId);
  const workspacesQuery = useTicketMetadataWorkspaces(
    activeIntegrationId,
    metadataEnabled && activeKind === "phasical",
  );
  const projectsQuery = useTicketMetadataProjects(
    activeIntegrationId,
    activeKind === "phasical" ? workspaceId : null,
    metadataEnabled && (activeKind !== "phasical" || Boolean(workspaceId)),
  );

  useEffect(() => {
    if (!open) {
      return;
    }

    setFormError(null);
    setInvalidTabs(new Set());
    setActiveTab("general");

    if (mode === "edit" && project) {
      const integration = integrations.find(
        (item) => item.id === project.integrationId,
      );
      const kind = integration?.kind as TicketIntegrationKind | undefined;

      setIntegrationId(project.integrationId);
      setWorkspaceId("");
      setExternalProjectId(project.externalProjectId);
      setName(project.name);
      setPolicy(
        ON_OPEN_TICKET_POLICIES.includes(
          project.onOpenTicketPolicy as OnOpenTicketPolicy,
        )
          ? (project.onOpenTicketPolicy as OnOpenTicketPolicy)
          : "supersede",
      );
      setCreateConfig(
        kind
          ? createConfigFromRecord(kind, project.createConfig)
          : project.createConfig,
      );
      setStatusMapping(statusMappingFromRecord(project.statusMapping));
      setContentTemplates(contentTemplatesFromRecord(project.contentTemplates));
      return;
    }

    const firstIntegration = ticketIntegrations[0];
    const kind = firstIntegration?.kind as TicketIntegrationKind | undefined;

    setIntegrationId(firstIntegration?.id ?? "");
    setWorkspaceId("");
    setExternalProjectId("");
    setName("");
    setPolicy("supersede");
    setCreateConfig(kind ? defaultCreateConfig(kind) : {});
    setStatusMapping(defaultStatusMapping());
    setContentTemplates(defaultContentTemplates());
  }, [open, mode, project, integrations, ticketIntegrations]);

  useEffect(() => {
    if (!open || mode !== "create" || !activeKind) {
      return;
    }

    setWorkspaceId("");
    setExternalProjectId("");
    setCreateConfig(defaultCreateConfig(activeKind));
    setStatusMapping(defaultStatusMapping());
  }, [open, mode, activeKind, integrationId]);

  useEffect(() => {
    if (!open || mode !== "create" || !externalProjectId) {
      return;
    }

    setCreateConfig(activeKind ? defaultCreateConfig(activeKind) : {});
    setStatusMapping(defaultStatusMapping());
  }, [open, mode, externalProjectId, activeKind]);

  function validateAllTabs(): TabValidationError | null {
    const trimmedName = name.trim();
    const trimmedExternalProjectId = externalProjectId.trim();
    const nextInvalidTabs = new Set<DrawerTab>();

    if (!trimmedName) {
      nextInvalidTabs.add("general");
    }

    if (!trimmedExternalProjectId) {
      nextInvalidTabs.add("general");
    }

    if (mode === "create" && !integrationId) {
      nextInvalidTabs.add("general");
    }

    if (mode === "create" && activeKind === "phasical" && !workspaceId.trim()) {
      nextInvalidTabs.add("general");
    }

    if (!activeKind) {
      nextInvalidTabs.add("general");
    }

    if (activeKind && !validateCreateConfig(activeKind, createConfig)) {
      nextInvalidTabs.add("createConfig");
    }

    const mappingError = validateStatusMapping(statusMapping);
    if (mappingError) {
      nextInvalidTabs.add("statusMapping");
    }

    setInvalidTabs(nextInvalidTabs);

    if (!trimmedName) {
      return { tab: "general", message: t("validation.nameRequired") };
    }

    if (!trimmedExternalProjectId) {
      return {
        tab: "general",
        message: t("validation.externalProjectIdRequired"),
      };
    }

    if (mode === "create" && !integrationId) {
      return { tab: "general", message: t("validation.integrationRequired") };
    }

    if (mode === "create" && activeKind === "phasical" && !workspaceId.trim()) {
      return { tab: "general", message: t("validation.workspaceRequired") };
    }

    if (!activeKind) {
      return { tab: "general", message: t("validation.integrationRequired") };
    }

    if (!validateCreateConfig(activeKind, createConfig)) {
      return {
        tab: "createConfig",
        message: t("validation.createConfigRequired"),
      };
    }

    if (mappingError) {
      return {
        tab: "statusMapping",
        message: t(`validation.${mappingError}`),
      };
    }

    return null;
  }

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFormError(null);

    const validationError = validateAllTabs();
    if (validationError) {
      setActiveTab(validationError.tab);
      setFormError(validationError.message);
      return;
    }

    const trimmedName = name.trim();
    const trimmedExternalProjectId = externalProjectId.trim();

    if (!activeKind) {
      return;
    }

    const payload = {
      name: trimmedName,
      createConfig: serializeCreateConfig(activeKind, createConfig),
      statusMapping: statusMappingToRecord(statusMapping),
      contentTemplates,
      onOpenTicketPolicy: policy,
    };

    try {
      if (mode === "create") {
        await onCreate({
          integrationId,
          externalProjectId: trimmedExternalProjectId,
          ...payload,
        });
      } else if (project) {
        await onUpdate(project.id, payload);
      }
      onOpenChange(false);
    } catch (error) {
      if (error instanceof ApiError) {
        setFormError(error.message);
      } else {
        setFormError(t("saveFailed"));
      }
    }
  }

  function handleTabChange(value: string | number | null) {
    if (value && DRAWER_TABS.includes(value as DrawerTab)) {
      setActiveTab(value as DrawerTab);
    }
  }

  const projects = projectsQuery.data?.items ?? [];
  const workspaces = workspacesQuery.data?.items ?? [];

  return (
    <Sheet onOpenChange={onOpenChange} open={open}>
      <SheetPopup className="max-w-4xl" side="right">
        <SheetHeader>
          <SheetTitle>
            {mode === "create" ? t("createTitle") : t("editTitle")}
          </SheetTitle>
          <SheetDescription>
            {mode === "create" ? t("createDescription") : t("editDescription")}
          </SheetDescription>
        </SheetHeader>
        <form className="flex min-h-0 flex-1 flex-col" onSubmit={handleSubmit}>
          <SheetPanel className="flex flex-col gap-4">
            {formError ? (
              <p className="text-destructive-foreground text-sm" role="alert">
                {formError}
              </p>
            ) : null}

            <Tabs onValueChange={handleTabChange} value={activeTab}>
              <TabsList className="w-full max-w-full flex-wrap">
                {DRAWER_TABS.map((tab) => (
                  <TabsTab
                    className={cn(
                      invalidTabs.has(tab) &&
                        "text-destructive-foreground data-active:text-destructive-foreground",
                    )}
                    key={tab}
                    value={tab}
                  >
                    {t(`tabs.${tab}`)}
                  </TabsTab>
                ))}
              </TabsList>

              <TabsPanel className="flex flex-col gap-4 pt-2" value="general">
                {mode === "create" ? (
                  <Field name="integrationId">
                    <FieldLabel htmlFor={integrationIdField}>
                      {t("integration")} <span aria-hidden="true">*</span>
                    </FieldLabel>
                    {ticketIntegrations.length === 0 ? (
                      <p className="text-muted-foreground text-sm">
                        {t("noTicketIntegrations")}
                      </p>
                    ) : (
                      <Select
                        itemToStringValue={(item) => item.value}
                        items={integrationItems}
                        onValueChange={(value) => {
                          if (value) {
                            setIntegrationId(value.value);
                          }
                        }}
                        value={
                          integrationItems.find(
                            (item) => item.value === integrationId,
                          ) ?? null
                        }
                      >
                        <SelectTrigger id={integrationIdField}>
                          <SelectValue
                            placeholder={t("integrationPlaceholder")}
                          />
                        </SelectTrigger>
                        <SelectPopup>
                          {integrationItems.map((item) => (
                            <SelectItem key={item.value} value={item}>
                              {item.label}
                            </SelectItem>
                          ))}
                        </SelectPopup>
                      </Select>
                    )}
                  </Field>
                ) : (
                  <Field name="integration">
                    <FieldLabel>{t("integration")}</FieldLabel>
                    <Input
                      disabled
                      readOnly
                      value={selectedIntegration?.name ?? ""}
                    />
                  </Field>
                )}

                {activeKind === "phasical" && mode === "create" ? (
                  <MetadataSelect
                    disabled={!metadataEnabled}
                    errorMessage={workspacesQuery.data?.message}
                    id={workspaceIdField}
                    isError={workspacesQuery.isError}
                    isLoading={workspacesQuery.isLoading}
                    items={workspaces}
                    label={t("phasical.workspace")}
                    name="workspaceId"
                    onValueChange={setWorkspaceId}
                    placeholder={t("metadata.workspacePlaceholder")}
                    required
                    value={workspaceId}
                  />
                ) : null}

                <MetadataSelect
                  disabled={mode === "edit" || !metadataEnabled}
                  errorMessage={projectsQuery.data?.message}
                  id={externalProjectIdField}
                  includeMissingValue={mode === "edit"}
                  isError={projectsQuery.isError}
                  isLoading={projectsQuery.isLoading}
                  items={projects}
                  label={t("externalProjectId")}
                  name="externalProjectId"
                  onValueChange={setExternalProjectId}
                  placeholder={t("metadata.projectPlaceholder")}
                  required
                  value={activeExternalProjectId}
                />

                <Field name="name">
                  <FieldLabel htmlFor={nameField}>
                    {t("name")} <span aria-hidden="true">*</span>
                  </FieldLabel>
                  <Input
                    id={nameField}
                    onChange={(event) => setName(event.target.value)}
                    required
                    value={name}
                  />
                </Field>

                <PolicySelect onValueChange={setPolicy} value={policy} />
              </TabsPanel>

              <TabsPanel
                className="flex flex-col gap-4 pt-2"
                value="createConfig"
              >
                <CreateConfigTabs
                  externalProjectId={activeExternalProjectId}
                  integrationId={activeIntegrationId}
                  kind={activeKind ?? null}
                  onChange={setCreateConfig}
                  values={createConfig}
                />
              </TabsPanel>

              <TabsPanel
                className="flex flex-col gap-4 pt-2"
                value="statusMapping"
              >
                <StatusMappingForm
                  externalProjectId={activeExternalProjectId}
                  integrationId={activeIntegrationId}
                  onChange={setStatusMapping}
                  values={statusMapping}
                />
              </TabsPanel>

              <TabsPanel
                className="flex flex-col gap-4 pt-2"
                value="contentTemplates"
              >
                <ContentTemplateForm
                  onChange={setContentTemplates}
                  values={contentTemplates}
                />
              </TabsPanel>
            </Tabs>
          </SheetPanel>
          <SheetFooter variant="bare">
            <SheetClose render={<Button variant="ghost" type="button" />}>
              {tCommon("cancel")}
            </SheetClose>
            <Button
              disabled={mode === "create" && ticketIntegrations.length === 0}
              loading={isSaving}
              type="submit"
            >
              {tCommon("save")}
            </Button>
          </SheetFooter>
        </form>
      </SheetPopup>
    </Sheet>
  );
}
