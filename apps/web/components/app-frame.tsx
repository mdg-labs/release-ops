"use client";

import type React from "react";
import { AppBreadcrumb } from "@/components/app-breadcrumb";
import { Frame, FramePanel } from "@/components/ui/frame";
import { SidebarTrigger } from "@/components/ui/sidebar";

export function AppFrame({
  children,
}: {
  children: React.ReactNode;
}): React.ReactElement {
  return (
    <Frame className="flex min-h-0 flex-1 flex-col">
      <FramePanel className="flex shrink-0 items-center gap-2 border-b px-4 py-2.5">
        <SidebarTrigger />
        <AppBreadcrumb />
      </FramePanel>
      <FramePanel className="flex min-h-0 flex-1 flex-col overflow-auto">
        {children}
      </FramePanel>
    </Frame>
  );
}
