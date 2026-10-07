"use client";

import { HomeIcon } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useTranslations } from "next-intl";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";

type BreadcrumbSegment = {
  href?: string;
  labelKey: string;
};

const ROUTE_BREADCRUMBS: Record<string, BreadcrumbSegment[]> = {
  "/": [],
  "/poll-runs": [{ labelKey: "pollRuns" }],
  "/repos": [{ labelKey: "repos" }],
  "/integrations": [{ labelKey: "integrations" }],
  "/ticket-projects": [{ labelKey: "ticketProjects" }],
  "/notifications": [{ labelKey: "notifications" }],
  "/settings": [{ labelKey: "settings" }],
  "/settings/users": [
    { href: "/settings", labelKey: "settings" },
    { labelKey: "settingsUsers" },
  ],
  "/profile": [{ labelKey: "profile" }],
};

function getBreadcrumbSegments(pathname: string): BreadcrumbSegment[] {
  if (pathname in ROUTE_BREADCRUMBS) {
    return ROUTE_BREADCRUMBS[pathname];
  }

  if (pathname.startsWith("/settings/")) {
    return [
      { href: "/settings", labelKey: "settings" },
      { labelKey: "settings" },
    ];
  }

  return [{ labelKey: "dashboard" }];
}

export function AppBreadcrumb(): React.ReactElement {
  const pathname = usePathname();
  const t = useTranslations("nav");
  const segments = getBreadcrumbSegments(pathname);

  return (
    <Breadcrumb>
      <BreadcrumbList>
        <BreadcrumbItem>
          <BreadcrumbLink
            aria-label={t("homeAriaLabel")}
            render={<Link href="/" />}
          >
            <HomeIcon aria-hidden="true" />
          </BreadcrumbLink>
        </BreadcrumbItem>
        {segments.map((segment, index) => {
          const label = t(segment.labelKey);
          const isLast = index === segments.length - 1;

          return (
            <BreadcrumbItem key={`${segment.labelKey}-${index}`}>
              <BreadcrumbSeparator />
              {segment.href && !isLast ? (
                <BreadcrumbLink render={<Link href={segment.href} />}>
                  {label}
                </BreadcrumbLink>
              ) : (
                <BreadcrumbPage>{label}</BreadcrumbPage>
              )}
            </BreadcrumbItem>
          );
        })}
      </BreadcrumbList>
    </Breadcrumb>
  );
}
