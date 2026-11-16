"use client";

import { FileTextIcon } from "lucide-react";
import { useFormatter, useTranslations } from "next-intl";
import { SettingsSection } from "@/components/common/settings-section";
import { Skeleton } from "@/components/ui/skeleton";
import { formatAppDateTime } from "@/lib/format/datetime";
import { useGlobalConfig } from "@/lib/hooks/use-global-config";
import type { GlobalConfig } from "@/lib/query/types";

function SourceLine({ config }: { config: GlobalConfig }): React.ReactElement {
  const t = useTranslations("settings.globalConfig");

  if (config.source === "repository" && config.repository) {
    return config.ref ? (
      <>
        {t("sourceRepository", {
          repository: config.repository,
          ref: config.ref,
        })}
      </>
    ) : (
      <>{t("sourceRepositoryDefaultRef", { repository: config.repository })}</>
    );
  }
  if (config.source === "mounted") {
    return <>{t("sourceMounted")}</>;
  }
  return <>{t("sourceUnknown")}</>;
}

/**
 * The global Renovate config as the raw text the worker last used. Read-only: no edit control,
 * and the text is shown as it is, never parsed, so allowedCommands is only part of it.
 */
export function GlobalConfigSection(): React.ReactElement {
  const t = useTranslations("settings.globalConfig");
  const format = useFormatter();
  const { data, isLoading, isError } = useGlobalConfig();

  let body: React.ReactElement;
  if (isError) {
    body = (
      <p className="text-destructive-foreground text-sm" role="alert">
        {t("loadFailed")}
      </p>
    );
  } else if (isLoading || !data) {
    body = (
      <Skeleton className="h-40 w-full" data-testid="global-config-loading" />
    );
  } else {
    body = (
      <>
        <dl className="grid gap-x-4 gap-y-1 text-sm sm:grid-cols-[auto_1fr]">
          <dt className="font-medium">{t("source")}</dt>
          <dd data-testid="global-config-source">
            <SourceLine config={data} />
          </dd>
          {data.capturedAt ? (
            <>
              <dt className="font-medium">{t("capturedAt")}</dt>
              <dd>{formatAppDateTime(format, data.capturedAt, "")}</dd>
            </>
          ) : null}
        </dl>
        {data.content === null ? (
          <p className="text-muted-foreground text-sm">{t("notCaptured")}</p>
        ) : (
          <pre
            aria-label={t("contentLabel")}
            className="max-h-96 overflow-auto whitespace-pre rounded-md border bg-muted/40 p-3 font-mono text-xs"
            data-testid="global-config-content"
            tabIndex={0}
          >
            {data.content}
          </pre>
        )}
      </>
    );
  }

  return (
    <SettingsSection
      description={t("description")}
      icon={<FileTextIcon aria-hidden="true" />}
      title={t("title")}
    >
      {body}
    </SettingsSection>
  );
}
