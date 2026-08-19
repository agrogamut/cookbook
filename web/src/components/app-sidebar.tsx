"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useTheme } from "next-themes";
import {
  Moon, Search, Wheat, ClipboardCheck, AlertTriangle, History, BookOpen, Sun,
} from "lucide-react";
import {
  Sidebar, SidebarContent, SidebarFooter, SidebarGroup, SidebarGroupContent, SidebarGroupLabel,
  SidebarHeader, SidebarMenu, SidebarMenuButton, SidebarMenuItem,
} from "@/components/ui/sidebar";

// The 7 routes from CLAUDE.md's "Screens" table, in the order an operator actually
// works through them: search first, then per-recipe detail, then the audit/reference
// screens a specific query rarely needs but a reviewer does.
const routes = [
  { href: "/", label: "Engine console", icon: Search },
  { href: "/ingredients", label: "Ingredients", icon: Wheat },
  { href: "/audit/nutrition", label: "Nutrition audit", icon: ClipboardCheck },
  { href: "/audit/gaps", label: "Gap register", icon: AlertTriangle },
  { href: "/runs", label: "Import runs", icon: History },
  { href: "/reference", label: "Reference", icon: BookOpen },
];

export function AppSidebar() {
  const pathname = usePathname();
  const { resolvedTheme, setTheme } = useTheme();
  return (
    <Sidebar collapsible="icon">
      <SidebarHeader className="border-b px-3 py-3 group-data-[collapsible=icon]:px-2">
        <div className="flex items-baseline gap-1.5 group-data-[collapsible=icon]:hidden">
          <span className="font-mono text-sm font-semibold tracking-tight">MadamGY</span>
          <span className="text-[10px] uppercase tracking-wide text-muted-foreground">console</span>
        </div>
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel className="font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
            Routes
          </SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              {routes.map((r) => (
                <SidebarMenuItem key={r.href}>
                  <SidebarMenuButton asChild isActive={pathname === r.href} tooltip={r.label}>
                    <Link href={r.href}>
                      <r.icon />
                      <span>{r.label}</span>
                    </Link>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              ))}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
      <SidebarFooter>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              onClick={() => setTheme(resolvedTheme === "dark" ? "light" : "dark")}
            >
              {resolvedTheme === "dark" ? <Sun /> : <Moon />}
              <span>{resolvedTheme === "dark" ? "Light mode" : "Dark mode"}</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarFooter>
    </Sidebar>
  );
}
