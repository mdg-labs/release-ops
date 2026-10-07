import type React from "react";
import { AppFrame } from "@/components/app-frame";
import { AppSidebar } from "@/components/app-sidebar";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";

export default function AuthenticatedLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>): React.ReactElement {
  return (
    <SidebarProvider>
      <AppSidebar />
      <SidebarInset className="flex flex-col">
        <AppFrame>{children}</AppFrame>
      </SidebarInset>
    </SidebarProvider>
  );
}
