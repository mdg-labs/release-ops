"use client";

import { TriangleAlertIcon, GithubIcon, UsersIcon } from "lucide-react";
import { useFormatter, useTranslations } from "next-intl";
import { SettingsSection } from "@/components/common/settings-section";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api/client";
import { formatAppDateTime } from "@/lib/format/datetime";
import { useGitHubApp } from "@/lib/hooks/use-github-app";
import type { GitHubAppResponse, Installation } from "@/lib/query/types";

type Row = { label: string; value: React.ReactNode; testId?: string };

function Rows({ rows }: { rows: Row[] }): React.ReactElement {
  return (
    <dl className="grid gap-x-4 gap-y-2 text-sm sm:grid-cols-[12rem_1fr]">
      {rows.map((row) => (
        <div className="contents" key={row.label}>
          <dt className="font-medium">{row.label}</dt>
          <dd className="min-w-0 break-words" data-testid={row.testId}>
            {row.value}
          </dd>
        </div>
      ))}
    </dl>
  );
}

function AppRows({
  app,
}: {
  app: GitHubAppResponse["app"];
}): React.ReactElement {
  const t = useTranslations("settings.app");
  const flag = (stored: boolean): React.ReactElement => (
    <Badge size="sm" variant={stored ? "success" : "error"}>
      {stored ? t("stored") : t("missing")}
    </Badge>
  );

  return (
    <Rows
      rows={[
        { label: t("appId"), value: app.appId, testId: "app-id" },
        {
          label: t("slug"),
          value: (
            <a
              className="underline"
              href={app.htmlUrl}
              rel="noopener noreferrer"
              target="_blank"
            >
              {app.slug}
            </a>
          ),
          testId: "app-slug",
        },
        {
          label: t("owner"),
          value: t("ownerValue", {
            login: app.ownerLogin,
            type:
              app.ownerType === "Organization"
                ? t("ownerTypeOrganization")
                : t("ownerTypeUser"),
          }),
          testId: "app-owner",
        },
        {
          label: t("webhook"),
          value: (
            <Badge
              size="sm"
              variant={app.webhookActive ? "success" : "warning"}
            >
              {app.webhookActive ? t("webhookActive") : t("webhookInactive")}
            </Badge>
          ),
          testId: "app-webhook",
        },
        { label: t("privateKey"), value: flag(app.hasPrivateKey) },
        { label: t("clientSecret"), value: flag(app.hasClientSecret) },
        { label: t("webhookSecret"), value: flag(app.hasWebhookSecret) },
      ]}
    />
  );
}

function InstallationRows({
  installation,
}: {
  installation: Installation;
}): React.ReactElement {
  const t = useTranslations("settings.installation");
  const format = useFormatter();
  const granted = Object.entries(installation.permissions).sort(([a], [b]) =>
    a.localeCompare(b),
  );
  const statusKey = {
    active: "statusActive",
    suspended: "statusSuspended",
    removed: "statusRemoved",
  } as const;
  const rate = installation.rateLimit;

  return (
    <Rows
      rows={[
        {
          label: t("account"),
          value: t("accountValue", {
            login: installation.accountLogin,
            type: installation.accountType,
          }),
          testId: "installation-account",
        },
        {
          label: t("status"),
          value: (
            <Badge
              size="sm"
              variant={installation.status === "active" ? "success" : "warning"}
            >
              {t(statusKey[installation.status])}
            </Badge>
          ),
          testId: "installation-status",
        },
        {
          label: t("repositories"),
          value: t("repositoriesValue", {
            count: installation.repositoryCount,
            selection:
              installation.repositorySelection === "all"
                ? t("selectionAll")
                : t("selectionSelected"),
          }),
          testId: "installation-repositories",
        },
        {
          label: t("permissions"),
          testId: "installation-permissions",
          value: (
            <div className="flex flex-col gap-2">
              {granted.length === 0 ? (
                <span>{t("permissionsNone")}</span>
              ) : (
                <ul className="flex flex-wrap gap-1">
                  {granted.map(([name, level]) => (
                    <li key={name}>
                      <Badge size="sm" variant="outline">
                        {name}: {level}
                      </Badge>
                    </li>
                  ))}
                </ul>
              )}
              {installation.missingPermissions.length > 0 ? (
                <Alert data-testid="missing-permissions" variant="warning">
                  <TriangleAlertIcon />
                  <AlertDescription>
                    {t("permissionsMissing", {
                      names: installation.missingPermissions.join(", "),
                    })}
                  </AlertDescription>
                </Alert>
              ) : (
                <span className="text-muted-foreground">
                  {t("permissionsComplete")}
                </span>
              )}
            </div>
          ),
        },
        {
          label: t("rateLimit"),
          testId: "installation-rate-limit",
          value:
            rate && rate.remaining !== null && rate.limit !== null
              ? t("rateLimitValue", {
                  remaining: rate.remaining,
                  limit: rate.limit,
                  resetAt: formatAppDateTime(format, rate.resetAt, "-"),
                })
              : t("rateLimitUnknown"),
        },
      ]}
    />
  );
}

function RoleRules({ data }: { data: GitHubAppResponse }): React.ReactElement {
  const t = useTranslations("settings.roles");
  const { installation, roleMapping } = data;

  if (!installation) {
    return (
      <p className="text-muted-foreground text-sm">{t("noInstallation")}</p>
    );
  }
  const rules =
    installation.accountType === "Organization"
      ? [
          t("orgAdmin"),
          roleMapping.viewerTeam ? t("orgViewerTeam") : t("orgViewerAll"),
          t("orgNone"),
        ]
      : [t("personalAdmin"), t("personalNone")];

  return (
    <div className="flex flex-col gap-1 text-sm">
      <span className="font-medium">{t("rules")}</span>
      <ul className="list-disc pl-5" data-testid="role-rules">
        {rules.map((rule) => (
          <li key={rule}>{rule}</li>
        ))}
      </ul>
    </div>
  );
}

function RoleMappingSection({
  data,
}: {
  data: GitHubAppResponse;
}): React.ReactElement {
  const t = useTranslations("settings.roles");
  const { roleMapping } = data;

  return (
    <SettingsSection
      description={t("description")}
      icon={<UsersIcon aria-hidden="true" />}
      title={t("title")}
    >
      <Rows
        rows={[
          {
            label: t("adminTeam"),
            value: roleMapping.adminTeam ?? t("notSet"),
            testId: "admin-team",
          },
          {
            label: t("viewerTeam"),
            value: roleMapping.viewerTeam ?? t("notSet"),
            testId: "viewer-team",
          },
        ]}
      />
      <RoleRules data={data} />
      {roleMapping.problems.map((problem) => (
        <Alert key={problem.variable} variant="warning">
          <TriangleAlertIcon />
          <AlertTitle>{t("problemTitle")}</AlertTitle>
          <AlertDescription>
            {t(`problem_${problem.code}`, { variable: problem.variable })}
          </AlertDescription>
        </Alert>
      ))}
    </SettingsSection>
  );
}

/** The App, its installation and the role mapping, read-only. */
export function GitHubAppSection(): React.ReactElement {
  const t = useTranslations("settings.app");
  const tInstallation = useTranslations("settings.installation");
  const { data, isLoading, isError, error } = useGitHubApp();

  let body: React.ReactElement;
  if (isError) {
    const notConfigured =
      error instanceof ApiError && error.code === "NOT_FOUND";
    body = (
      <p
        className={
          notConfigured
            ? "text-muted-foreground text-sm"
            : "text-destructive-foreground text-sm"
        }
        role={notConfigured ? undefined : "alert"}
      >
        {notConfigured ? t("notConfigured") : t("loadFailed")}
      </p>
    );
  } else if (isLoading || !data) {
    body = (
      <Skeleton className="h-48 w-full" data-testid="github-app-loading" />
    );
  } else {
    body = (
      <>
        <AppRows app={data.app} />
        <h3 className="font-medium text-sm">{tInstallation("title")}</h3>
        {data.installation ? (
          <InstallationRows installation={data.installation} />
        ) : (
          <p
            className="text-muted-foreground text-sm"
            data-testid="no-installation"
          >
            {tInstallation("none")}
          </p>
        )}
      </>
    );
  }

  return (
    <>
      <SettingsSection
        description={t("description")}
        icon={<GithubIcon aria-hidden="true" />}
        title={t("title")}
      >
        {body}
      </SettingsSection>
      {data && !isError ? <RoleMappingSection data={data} /> : null}
    </>
  );
}
