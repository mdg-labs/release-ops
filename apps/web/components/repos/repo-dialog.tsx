"use client";

import { useTranslations } from "next-intl";
import { useEffect, useId, useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { CheckboxGroup } from "@/components/ui/checkbox-group";
import {
  Dialog,
  DialogClose,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogPanel,
  DialogPopup,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectItem,
  SelectPopup,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { ApiError } from "@/lib/api/client";
import {
  SOURCE_KINDS,
  sourceKindRequiresIntegration,
  type SourceKind,
} from "@/lib/repos/source-kinds";
import type {
  CreateRepoInput,
  Integration,
  NotificationTarget,
  Repo,
  TicketProject,
  UpdateRepoInput,
} from "@/lib/query/types";

type RepoDialogMode = "create" | "edit";

type RepoDialogProps = {
  mode: RepoDialogMode;
  repo: Repo | null;
  integrations: Integration[];
  ticketProjects: TicketProject[];
  notificationTargets: NotificationTarget[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreate: (input: CreateRepoInput) => Promise<Repo>;
  onUpdate: (id: string, input: UpdateRepoInput) => Promise<Repo>;
  isSaving: boolean;
};

type SourceKindOption = { label: string; value: SourceKind };
type SelectOption = { label: string; value: string };

export function RepoDialog({
  mode,
  repo,
  integrations,
  ticketProjects,
  notificationTargets,
  open,
  onOpenChange,
  onCreate,
  onUpdate,
  isSaving,
}: RepoDialogProps): React.ReactElement {
  const t = useTranslations("repos");
  const tIntegrations = useTranslations("integrations");
  const tCommon = useTranslations("common");

  const projectPathId = useId();
  const enabledId = useId();
  const includePrereleasesId = useId();

  const [sourceKind, setSourceKind] = useState<SourceKind>("github");
  const [projectPath, setProjectPath] = useState("");
  const [ticketProjectId, setTicketProjectId] = useState("");
  const [sourceIntegrationId, setSourceIntegrationId] = useState("");
  const [notificationTargetIds, setNotificationTargetIds] = useState<string[]>(
    [],
  );
  const [enabled, setEnabled] = useState(true);
  const [includePrereleases, setIncludePrereleases] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  const sourceKindItems: SourceKindOption[] = SOURCE_KINDS.map((value) => ({
    label: tIntegrations(`kinds.${value}`),
    value,
  }));

  const ticketProjectItems: SelectOption[] = useMemo(
    () =>
      ticketProjects.map((project) => ({
        label: project.name,
        value: project.id,
      })),
    [ticketProjects],
  );

  const requiresIntegration = sourceKindRequiresIntegration(sourceKind);

  const sourceIntegrationItems: SelectOption[] = useMemo(() => {
    return integrations
      .filter((integration) => integration.kind === sourceKind)
      .map((integration) => ({
        label: integration.name,
        value: integration.id,
      }));
  }, [integrations, sourceKind]);

  const integrationSelectItems: SelectOption[] = useMemo(() => {
    if (!requiresIntegration) {
      return [
        { label: t("sourceIntegrationNone"), value: "" },
        ...sourceIntegrationItems,
      ];
    }
    return sourceIntegrationItems;
  }, [requiresIntegration, sourceIntegrationItems, t]);

  const enabledNotificationTargets = useMemo(
    () => notificationTargets.filter((target) => target.enabled),
    [notificationTargets],
  );

  useEffect(() => {
    if (!open) {
      return;
    }

    setFormError(null);
    if (mode === "edit" && repo) {
      setSourceKind(repo.sourceKind as SourceKind);
      setProjectPath(repo.projectPath);
      setTicketProjectId(repo.ticketProjectId);
      setSourceIntegrationId(repo.sourceIntegrationId ?? "");
      setNotificationTargetIds(repo.notificationTargetIds);
      setEnabled(repo.enabled);
      setIncludePrereleases(repo.includePrereleases);
      return;
    }

    setSourceKind("github");
    setProjectPath("");
    setTicketProjectId(ticketProjects[0]?.id ?? "");
    setSourceIntegrationId("");
    setNotificationTargetIds([]);
    setEnabled(true);
    setIncludePrereleases(false);
  }, [open, mode, repo, ticketProjects]);

  useEffect(() => {
    if (
      sourceIntegrationId &&
      !sourceIntegrationItems.some((item) => item.value === sourceIntegrationId)
    ) {
      setSourceIntegrationId("");
    }
  }, [sourceKind, sourceIntegrationId, sourceIntegrationItems]);

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFormError(null);

    const trimmedPath = projectPath.trim();
    if (!trimmedPath) {
      setFormError(t("validation.projectPathRequired"));
      return;
    }

    if (!ticketProjectId) {
      setFormError(t("validation.ticketProjectRequired"));
      return;
    }

    if (requiresIntegration && !sourceIntegrationId) {
      setFormError(t("validation.sourceIntegrationRequired"));
      return;
    }

    const payload = {
      sourceKind,
      projectPath: trimmedPath,
      enabled,
      includePrereleases,
      sourceIntegrationId: sourceIntegrationId || null,
      ticketProjectId,
      notificationTargetIds,
    };

    try {
      if (mode === "create") {
        await onCreate(payload);
      } else if (repo) {
        await onUpdate(repo.id, payload);
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

  return (
    <Dialog onOpenChange={onOpenChange} open={open}>
      <DialogPopup>
        <DialogHeader>
          <DialogTitle>
            {mode === "create" ? t("createTitle") : t("editTitle")}
          </DialogTitle>
          <DialogDescription>
            {mode === "create" ? t("createDescription") : t("editDescription")}
          </DialogDescription>
        </DialogHeader>
        <form className="flex min-h-0 flex-1 flex-col" onSubmit={handleSubmit}>
          <DialogPanel className="flex flex-col gap-4">
            {formError ? (
              <p className="text-destructive-foreground text-sm" role="alert">
                {formError}
              </p>
            ) : null}

            {ticketProjects.length === 0 ? (
              <p className="text-muted-foreground text-sm">
                {t("noTicketProjects")}
              </p>
            ) : null}

            <Field name="sourceKind">
              <FieldLabel>{t("sourceKind")}</FieldLabel>
              <Select
                itemToStringValue={(item) => item.value}
                items={sourceKindItems}
                onValueChange={(value) => {
                  if (value) {
                    setSourceKind(value.value);
                  }
                }}
                value={
                  sourceKindItems.find((item) => item.value === sourceKind) ??
                  null
                }
              >
                <SelectTrigger>
                  <SelectValue placeholder={t("sourceKindPlaceholder")} />
                </SelectTrigger>
                <SelectPopup>
                  {sourceKindItems.map((item) => (
                    <SelectItem key={item.value} value={item}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectPopup>
              </Select>
            </Field>

            <Field name="projectPath">
              <FieldLabel htmlFor={projectPathId}>
                {t("projectPath")} <span aria-hidden="true">*</span>
              </FieldLabel>
              <Input
                id={projectPathId}
                name="projectPath"
                onChange={(event) => setProjectPath(event.target.value)}
                placeholder={t("projectPathPlaceholder")}
                required
                value={projectPath}
              />
              <FieldDescription>{t("projectPathHint")}</FieldDescription>
            </Field>

            <Field name="ticketProjectId">
              <FieldLabel>{t("ticketProject")}</FieldLabel>
              <Select
                itemToStringValue={(item) => item.value}
                items={ticketProjectItems}
                onValueChange={(value) => {
                  if (value) {
                    setTicketProjectId(value.value);
                  }
                }}
                value={
                  ticketProjectItems.find(
                    (item) => item.value === ticketProjectId,
                  ) ?? null
                }
              >
                <SelectTrigger>
                  <SelectValue placeholder={t("ticketProjectPlaceholder")} />
                </SelectTrigger>
                <SelectPopup>
                  {ticketProjectItems.map((item) => (
                    <SelectItem key={item.value} value={item}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectPopup>
              </Select>
            </Field>

            <Field name="sourceIntegrationId">
              <FieldLabel>{t("sourceIntegration")}</FieldLabel>
              {!requiresIntegration ? (
                <FieldDescription>
                  {t("sourceIntegrationOptionalHint")}
                </FieldDescription>
              ) : null}
              {requiresIntegration && sourceIntegrationItems.length === 0 ? (
                <p className="text-muted-foreground text-sm">
                  {t("noSourceIntegrations", {
                    kind: tIntegrations(`kinds.${sourceKind}`),
                  })}
                </p>
              ) : sourceIntegrationItems.length === 0 ? null : (
                <Select
                  itemToStringValue={(item) => item.label}
                  items={integrationSelectItems}
                  onValueChange={(value) => {
                    if (value) {
                      setSourceIntegrationId(value.value);
                    }
                  }}
                  value={
                    integrationSelectItems.find(
                      (item) => item.value === sourceIntegrationId,
                    ) ??
                    (requiresIntegration
                      ? null
                      : (integrationSelectItems.find(
                          (item) => item.value === "",
                        ) ?? null))
                  }
                >
                  <SelectTrigger>
                    <SelectValue
                      placeholder={
                        requiresIntegration
                          ? t("sourceIntegrationPlaceholder")
                          : t("sourceIntegrationOptionalPlaceholder")
                      }
                    />
                  </SelectTrigger>
                  <SelectPopup>
                    {integrationSelectItems.map((item) => (
                      <SelectItem key={item.value || "__none__"} value={item}>
                        {item.label}
                      </SelectItem>
                    ))}
                  </SelectPopup>
                </Select>
              )}
            </Field>

            {enabledNotificationTargets.length > 0 ? (
              <Field name="notificationTargets">
                <FieldLabel>{t("notificationTargets")}</FieldLabel>
                <FieldDescription>
                  {t("notificationTargetsHint")}
                </FieldDescription>
                <CheckboxGroup
                  aria-label={t("notificationTargets")}
                  onValueChange={(value) => {
                    setNotificationTargetIds(value);
                  }}
                  value={notificationTargetIds}
                >
                  {enabledNotificationTargets.map((target) => (
                    <Label key={target.id}>
                      <Checkbox aria-label={target.name} value={target.id} />
                      {target.name}
                    </Label>
                  ))}
                </CheckboxGroup>
              </Field>
            ) : null}

            <Field name="enabled">
              <div className="flex items-center justify-between gap-4">
                <div className="flex flex-col gap-1">
                  <FieldLabel htmlFor={enabledId}>{t("enabled")}</FieldLabel>
                  <FieldDescription>{t("enabledHint")}</FieldDescription>
                </div>
                <Switch
                  checked={enabled}
                  id={enabledId}
                  onCheckedChange={setEnabled}
                />
              </div>
            </Field>

            <Field name="includePrereleases">
              <div className="flex items-center justify-between gap-4">
                <div className="flex flex-col gap-1">
                  <FieldLabel htmlFor={includePrereleasesId}>
                    {t("includePrereleases")}
                  </FieldLabel>
                  <FieldDescription>
                    {t("includePrereleasesHint")}
                  </FieldDescription>
                </div>
                <Switch
                  checked={includePrereleases}
                  id={includePrereleasesId}
                  onCheckedChange={setIncludePrereleases}
                />
              </div>
            </Field>
          </DialogPanel>
          <DialogFooter variant="bare">
            <DialogClose render={<Button variant="ghost" type="button" />}>
              {tCommon("cancel")}
            </DialogClose>
            <Button
              disabled={ticketProjects.length === 0}
              loading={isSaving}
              type="submit"
            >
              {tCommon("save")}
            </Button>
          </DialogFooter>
        </form>
      </DialogPopup>
    </Dialog>
  );
}
