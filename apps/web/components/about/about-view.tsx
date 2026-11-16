"use client";

import { InfoIcon, PackageIcon } from "lucide-react";
import { useTranslations } from "next-intl";
import { PageHeader } from "@/components/common/page-header";
import { SettingsSection } from "@/components/common/settings-section";
import { Skeleton } from "@/components/ui/skeleton";
import { useAbout } from "@/lib/hooks/use-about";
import type { AboutResponse } from "@/lib/query/types";

const REPO_URL = "https://github.com/mdg-labs/release-ops";
const LICENSE_URL = `${REPO_URL}/blob/main/LICENSE`;
const NOTICE_URL = `${REPO_URL}/blob/main/NOTICE`;

function ExternalLink({
  href,
  children,
}: {
  href: string;
  children: React.ReactNode;
}): React.ReactElement {
  return (
    <a
      className="underline"
      href={href}
      rel="noopener noreferrer"
      target="_blank"
    >
      {children}
    </a>
  );
}

function Content({ about }: { about: AboutResponse }): React.ReactElement {
  const t = useTranslations("about");

  return (
    <div className="flex flex-col gap-8">
      <SettingsSection
        icon={<InfoIcon aria-hidden="true" />}
        title={t("app.title")}
      >
        <dl className="grid gap-x-4 gap-y-2 text-sm sm:grid-cols-[12rem_1fr]">
          <dt className="font-medium">{t("app.version")}</dt>
          <dd data-testid="about-version">{about.version}</dd>
          <dt className="font-medium">{t("app.license")}</dt>
          <dd>{t("app.licenseValue")}</dd>
        </dl>
        <ul className="flex flex-wrap gap-x-4 gap-y-1 text-sm">
          <li>
            <ExternalLink href={LICENSE_URL}>
              {t("app.licenseLink")}
            </ExternalLink>
          </li>
          <li>
            <ExternalLink href={NOTICE_URL}>{t("app.noticeLink")}</ExternalLink>
          </li>
          <li>
            <ExternalLink href={REPO_URL}>{t("app.sourceLink")}</ExternalLink>
          </li>
        </ul>
      </SettingsSection>
      <SettingsSection
        description={t("renovate.description")}
        icon={<PackageIcon aria-hidden="true" />}
        title={t("renovate.title")}
      >
        <dl className="grid gap-x-4 gap-y-2 text-sm sm:grid-cols-[12rem_1fr]">
          <dt className="font-medium">{t("renovate.version")}</dt>
          <dd data-testid="about-renovate-version">{about.renovate.version}</dd>
          <dt className="font-medium">{t("renovate.license")}</dt>
          <dd data-testid="about-renovate-license">{about.renovate.license}</dd>
        </dl>
        <p className="text-sm">{t("renovate.notice")}</p>
        <ul className="flex flex-wrap gap-x-4 gap-y-1 text-sm">
          <li>
            <ExternalLink href={about.renovate.sourceUrl}>
              {t("renovate.sourceLink")}
            </ExternalLink>
          </li>
          <li>
            <ExternalLink href={NOTICE_URL}>{t("app.noticeLink")}</ExternalLink>
          </li>
        </ul>
      </SettingsSection>
    </div>
  );
}

/** The version, licence and the pinned Renovate version; open to a viewer. */
export function AboutView(): React.ReactElement {
  const t = useTranslations("about");
  const { data, isLoading, isError } = useAbout();

  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-6">
      <PageHeader title={t("title")} />
      {isError ? (
        <p className="text-destructive-foreground text-sm" role="alert">
          {t("loadFailed")}
        </p>
      ) : null}
      {isLoading ? <Skeleton className="h-64 w-full" /> : null}
      {!isLoading && !isError && data ? <Content about={data} /> : null}
    </div>
  );
}
