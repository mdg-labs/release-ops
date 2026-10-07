"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  BellIcon,
  FolderGit2Icon,
  HistoryIcon,
  LayoutDashboardIcon,
  LogOutIcon,
  PlugIcon,
  SettingsIcon,
  TicketIcon,
  UserIcon,
} from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import type React from "react";
import { apiClient } from "@/lib/api/client";
import { useSession } from "@/lib/hooks/use-session";
import { useStatus } from "@/lib/hooks/use-status";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
} from "@/components/ui/sidebar";

const NAV_ITEMS = [
  {
    href: "/",
    icon: LayoutDashboardIcon,
    labelKey: "dashboard",
  },
  {
    href: "/poll-runs",
    icon: HistoryIcon,
    labelKey: "pollRuns",
  },
  {
    href: "/repos",
    icon: FolderGit2Icon,
    labelKey: "repos",
  },
  {
    href: "/integrations",
    icon: PlugIcon,
    labelKey: "integrations",
  },
  {
    href: "/ticket-projects",
    icon: TicketIcon,
    labelKey: "ticketProjects",
  },
  {
    href: "/notifications",
    icon: BellIcon,
    labelKey: "notifications",
  },
  {
    href: "/settings",
    icon: SettingsIcon,
    labelKey: "settings",
  },
] as const;

const REPO_URL = "https://github.com/mdg-labs/release-ops";
const RELEASE_VERSION =
  /^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/;
const NIGHTLY_VERSION = /^nightly-([0-9a-f]{7,40})$/;

/** GitHub page for a running version: release tag, nightly commit, or none (dev). */
export function versionHref(version: string): string | null {
  if (RELEASE_VERSION.test(version)) {
    return `${REPO_URL}/releases/tag/${version}`;
  }

  const nightly = NIGHTLY_VERSION.exec(version);
  if (nightly) {
    return `${REPO_URL}/commit/${nightly[1]}`;
  }

  return null;
}

function isNavActive(pathname: string, href: string): boolean {
  if (href === "/") {
    return pathname === "/";
  }

  return pathname === href || pathname.startsWith(`${href}/`);
}

export function AppSidebar(): React.ReactElement {
  const pathname = usePathname();
  const router = useRouter();
  const queryClient = useQueryClient();
  const { data: session } = useSession();
  const { data: status, isError: statusFailed } = useStatus();
  const tNav = useTranslations("nav");
  const tAuth = useTranslations("auth");
  const tCommon = useTranslations("common");

  // A failed refetch keeps the last data; show no badge then (spec §8).
  const version = statusFailed ? null : status?.version || null;
  const versionLink = version ? versionHref(version) : null;
  const profileLabel = session?.user?.email ?? tNav("profile");

  const logout = useMutation({
    mutationFn: () => apiClient.post("/auth/logout"),
    onSuccess: async () => {
      await queryClient.invalidateQueries();
      queryClient.clear();
      router.push("/login");
    },
  });

  return (
    <Sidebar collapsible="icon" variant="inset">
      <SidebarHeader className="border-sidebar-border border-b p-4">
        <div className="flex items-center gap-2 group-data-[collapsible=icon]:hidden">
          <a
            className="truncate font-semibold text-sm hover:underline"
            data-testid="sidebar-wordmark"
            href={REPO_URL}
            rel="noopener noreferrer"
            target="_blank"
          >
            {tCommon("appTitle")}
          </a>
          {version ? (
            <Badge
              data-testid="sidebar-version"
              render={
                versionLink ? (
                  <a
                    href={versionLink}
                    rel="noopener noreferrer"
                    target="_blank"
                  />
                ) : undefined
              }
              size="sm"
              variant="outline"
            >
              {version}
            </Badge>
          ) : null}
        </div>
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupContent>
            <SidebarMenu>
              {NAV_ITEMS.map((item) => (
                <SidebarMenuItem key={item.href}>
                  <SidebarMenuButton
                    isActive={isNavActive(pathname, item.href)}
                    render={<Link href={item.href} />}
                    tooltip={tNav(item.labelKey)}
                  >
                    <item.icon aria-hidden="true" />
                    <span>{tNav(item.labelKey)}</span>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              ))}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
      <SidebarFooter className="border-sidebar-border border-t p-2">
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              isActive={isNavActive(pathname, "/profile")}
              render={<Link href="/profile" />}
              tooltip={profileLabel}
            >
              <UserIcon aria-hidden="true" />
              <span className="truncate">{profileLabel}</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
        <Button
          className="w-full justify-start"
          data-testid="sidebar-logout"
          loading={logout.isPending}
          onClick={() => logout.mutate()}
          type="button"
          variant="ghost"
        >
          <LogOutIcon aria-hidden="true" />
          <span className="group-data-[collapsible=icon]:hidden">
            {tAuth("logout")}
          </span>
        </Button>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}
