"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useTheme } from "next-themes";
import { Moon, Sun } from "lucide-react";
import {
  Sidebar, SidebarContent, SidebarFooter, SidebarGroup, SidebarGroupContent, SidebarGroupLabel,
  SidebarMenu, SidebarMenuButton, SidebarMenuItem,
} from "@/components/ui/sidebar";

// The 7 routes from CLAUDE.md's "Screens" table, in the order an operator actually
// works through them: search first, then per-recipe detail, then the audit/reference
// screens a specific query rarely needs but a reviewer does.
const routes = [
  { href: "/", label: "Engine console" },
  { href: "/ingredients", label: "Ingredients" },
  { href: "/audit/nutrition", label: "Nutrition audit" },
  { href: "/audit/gaps", label: "Gap register" },
  { href: "/runs", label: "Import runs" },
  { href: "/reference", label: "Reference" },
];

export function AppSidebar() {
  const pathname = usePathname();
  const { resolvedTheme, setTheme } = useTheme();
  return (
    <Sidebar collapsible="icon">
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel className="font-mono text-xs">MadamGY</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              {routes.map((r) => (
                <SidebarMenuItem key={r.href}>
                  <SidebarMenuButton asChild isActive={pathname === r.href}>
                    <Link href={r.href}>{r.label}</Link>
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
