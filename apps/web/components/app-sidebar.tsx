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
  const tNav = useTranslations("nav");
  const tAuth = useTranslations("auth");
  const tCommon = useTranslations("common");

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
        <span className="truncate font-semibold text-sm group-data-[collapsible=icon]:hidden">
          {tCommon("appTitle")}
        </span>
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
