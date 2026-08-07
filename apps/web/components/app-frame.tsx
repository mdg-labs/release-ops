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
    <Frame className="m-2 flex min-h-[calc(100svh-1rem)] flex-1 flex-col md:m-4">
      <FramePanel className="flex items-center gap-2 px-3 py-2">
        <SidebarTrigger />
        <AppBreadcrumb />
      </FramePanel>
      <FramePanel className="flex flex-1 flex-col">{children}</FramePanel>
    </Frame>
  );
}
