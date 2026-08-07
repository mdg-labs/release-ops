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

const PAGE_LABEL_KEYS: Record<string, string> = {
  "/": "dashboard",
  "/repos": "repos",
  "/integrations": "integrations",
  "/ticket-projects": "ticketProjects",
  "/notifications": "notifications",
  "/settings": "settings",
  "/settings/users": "settingsUsers",
};

export function AppBreadcrumb(): React.ReactElement {
  const pathname = usePathname();
  const t = useTranslations("nav");
  const labelKey =
    PAGE_LABEL_KEYS[pathname] ??
    (pathname.startsWith("/settings") ? "settings" : "dashboard");
  const pageLabel = t(labelKey);
  const showPageCrumb = pathname !== "/";

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
        {showPageCrumb ? (
          <>
            <BreadcrumbSeparator />
            <BreadcrumbItem>
              <BreadcrumbPage>{pageLabel}</BreadcrumbPage>
            </BreadcrumbItem>
          </>
        ) : null}
      </BreadcrumbList>
    </Breadcrumb>
  );
}
