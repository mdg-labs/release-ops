"use client";

import { useTranslations } from "next-intl";
import {
  ForbiddenState,
  isForbiddenError,
} from "@/components/common/forbidden-state";
import { PageHeader } from "@/components/common/page-header";
import { DefaultTicketProjectSection } from "@/components/settings/default-ticket-project-section";
import { GitHubAppKeySection } from "@/components/settings/github-app-key-section";
import { GitHubAppSection } from "@/components/settings/github-app-section";
import { GlobalConfigSection } from "@/components/settings/global-config-section";
import { InstanceSettingsSection } from "@/components/settings/instance-settings-section";
import { SettingsNav } from "@/components/settings/settings-nav";
import { Skeleton } from "@/components/ui/skeleton";
import { useSettings } from "@/lib/hooks/use-settings";

export function SettingsView(): React.ReactElement {
  const t = useTranslations("settings");
  const { data, isLoading, isError, error } = useSettings();

  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-6">
      <PageHeader title={t("title")} />
      <SettingsNav />

      {isError && isForbiddenError(error) ? <ForbiddenState /> : null}
      {isError && !isForbiddenError(error) ? (
        <p className="text-destructive-foreground text-sm" role="alert">
          {t("loadFailed")}
        </p>
      ) : null}

      {isLoading ? <Skeleton className="h-96 w-full" /> : null}
      {!isLoading && !isError && data ? (
        <div className="flex flex-col gap-8">
          <InstanceSettingsSection settings={data} />
          <DefaultTicketProjectSection />
          <GlobalConfigSection />
          <GitHubAppSection />
          <GitHubAppKeySection />
        </div>
      ) : null}
    </div>
  );
}
