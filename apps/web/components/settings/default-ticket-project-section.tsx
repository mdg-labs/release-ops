"use client";

import { CircleCheckIcon, TicketIcon } from "lucide-react";
import Link from "next/link";
import { useTranslations } from "next-intl";
import { useState } from "react";
import { SettingsSection } from "@/components/common/settings-section";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field, FieldLabel } from "@/components/ui/field";
import {
  Select,
  SelectItem,
  SelectPopup,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { ApiError } from "@/lib/api/client";
import {
  liveInstallation,
  useInstallations,
  useSetDefaultTicketProject,
} from "@/lib/hooks/use-installations";
import { useTicketProjects } from "@/lib/hooks/use-ticket-projects";
import type { Installation, TicketProject } from "@/lib/query/types";

type Option = { value: string; label: string };

const NO_DEFAULT = "none";

function Form({
  installation,
  projects,
}: {
  installation: Installation;
  projects: TicketProject[];
}): React.ReactElement {
  const t = useTranslations("settings.defaultProject");
  const tCommon = useTranslations("common");
  const setDefaultTicketProject = useSetDefaultTicketProject();
  const [value, setValue] = useState(
    installation.defaultTicketProjectId ?? NO_DEFAULT,
  );
  const [saved, setSaved] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  const options: Option[] = [
    { value: NO_DEFAULT, label: t("none") },
    ...projects.map((project) => ({ value: project.id, label: project.name })),
  ];
  const selected = options.find((option) => option.value === value) ?? null;

  async function handleSave(): Promise<void> {
    setSaved(false);
    setSaveError(null);
    try {
      await setDefaultTicketProject.mutateAsync({
        id: installation.id,
        defaultTicketProjectId: value === NO_DEFAULT ? null : value,
      });
      setSaved(true);
    } catch (error) {
      setSaveError(error instanceof ApiError ? error.message : t("saveFailed"));
    }
  }

  return (
    <>
      <Field>
        <FieldLabel>{t("label")}</FieldLabel>
        <Select
          itemToStringValue={(item) => item.value}
          items={options}
          onValueChange={(next) => {
            if (next) {
              setValue(next.value);
              setSaved(false);
            }
          }}
          value={selected}
        >
          <SelectTrigger data-testid="default-ticket-project-select">
            <SelectValue placeholder={t("placeholder")} />
          </SelectTrigger>
          <SelectPopup>
            {options.map((option) => (
              <SelectItem key={option.value} value={option}>
                {option.label}
              </SelectItem>
            ))}
          </SelectPopup>
        </Select>
      </Field>
      {saved ? (
        <Alert variant="success">
          <CircleCheckIcon />
          <AlertTitle>{t("saved")}</AlertTitle>
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
          disabled={setDefaultTicketProject.isPending}
          onClick={() => {
            void handleSave();
          }}
        >
          {setDefaultTicketProject.isPending ? (
            <Spinner className="size-4" />
          ) : null}
          {tCommon("save")}
        </Button>
      </div>
    </>
  );
}

/** The installation's default ticket project (PATCH /installations/{id}). */
export function DefaultTicketProjectSection(): React.ReactElement {
  const t = useTranslations("settings.defaultProject");
  const installations = useInstallations();
  const projects = useTicketProjects();

  const installation = liveInstallation(installations.data);
  const failed = installations.isError || projects.isError;
  const loading = installations.isLoading || projects.isLoading;

  let body: React.ReactElement;
  if (failed) {
    body = (
      <p className="text-destructive-foreground text-sm" role="alert">
        {t("loadFailed")}
      </p>
    );
  } else if (loading) {
    body = (
      <Skeleton className="h-9 w-full" data-testid="default-project-loading" />
    );
  } else if (!installation) {
    body = (
      <p className="text-muted-foreground text-sm">{t("noInstallation")}</p>
    );
  } else if ((projects.data ?? []).length === 0) {
    body = (
      <div className="flex flex-col items-start gap-2">
        <p className="text-muted-foreground text-sm">{t("noProjects")}</p>
        <Link className="text-sm underline" href="/ticket-projects">
          {t("manageProjects")}
        </Link>
      </div>
    );
  } else {
    body = (
      <Form
        installation={installation}
        key={installation.id}
        projects={projects.data ?? []}
      />
    );
  }

  return (
    <SettingsSection
      description={t("description")}
      icon={<TicketIcon aria-hidden="true" />}
      title={t("title")}
    >
      {body}
    </SettingsSection>
  );
}
