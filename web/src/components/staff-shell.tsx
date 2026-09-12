"use client";
import { StaffContext } from "./staff-context";
import { SidebarProvider, SidebarTrigger } from "./ui/sidebar";
import { AppSidebar } from "./app-sidebar";
import { CommandPalette } from "./command-palette";
import type { StaffAccount } from "@/lib/portal-types";

export function StaffShell({
  actor,
  children,
}: {
  actor: StaffAccount;
  children: React.ReactNode;
}) {
  return (
    <StaffContext value={actor}>
      <SidebarProvider>
        <AppSidebar />
        <main className="h-svh min-w-0 flex-1 overflow-y-auto p-4 md:p-6">
          <div className="mb-4 flex items-center gap-2 text-xs text-muted-foreground md:hidden">
            <SidebarTrigger />
            MadamGY staff workspace
          </div>
          {children}
        </main>
        <CommandPalette />
      </SidebarProvider>
    </StaffContext>
  );
}
