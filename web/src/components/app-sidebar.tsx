"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { signOut } from "@/lib/api";
import { useStaff } from "./staff-context";
import { useTheme } from "next-themes";
import {
  Moon, Search, Wheat, ClipboardCheck, AlertTriangle, History, BookOpen, BookMarked, Sun, Users, LayoutDashboard, LogOut, KeyRound,
} from "lucide-react";
import {
  Sidebar, SidebarContent, SidebarFooter, SidebarGroup, SidebarGroupContent, SidebarGroupLabel,
  SidebarHeader, SidebarMenu, SidebarMenuButton, SidebarMenuItem,
} from "@/components/ui/sidebar";

const routes = [
  { href: "/console", label: "Engine console", icon: Search },
  { href: "/books", label: "Books", icon: BookMarked },
  { href: "/ingredients", label: "Ingredients", icon: Wheat },
  { href: "/audit/nutrition", label: "Nutrition audit", icon: ClipboardCheck },
  { href: "/audit/gaps", label: "Gap register", icon: AlertTriangle },
  { href: "/runs", label: "Import runs", icon: History },
  { href: "/reference", label: "Reference", icon: BookOpen },
];

export function AppSidebar() {
  const pathname = usePathname();
  const actor = useStaff();
  const router = useRouter();
  const [logoutError, setLogoutError] = useState("");
  const [signingOut, setSigningOut] = useState(false);
  const { resolvedTheme, setTheme } = useTheme();
  const visibleRoutes = [
    actor.role === "admin" ? { href: "/admin", label: "Administration", icon: LayoutDashboard } : { href: "/doctor", label: "Assigned children", icon: Users },
    ...routes.filter(route => actor.role === "admin" || !["/audit/nutrition", "/audit/gaps", "/runs"].includes(route.href)),
    { href: "/account", label: "My account", icon: KeyRound },
  ];
  async function logout() {
    setSigningOut(true); setLogoutError("");
    try { await signOut(); router.replace("/login"); router.refresh(); }
    catch { setLogoutError("Sign-out failed. Try again."); setSigningOut(false); }
  }
  return (
    <Sidebar collapsible="icon">
      <SidebarHeader className="border-b px-3 py-3 group-data-[collapsible=icon]:px-2">
        <div className="flex items-baseline gap-1.5 group-data-[collapsible=icon]:hidden">
          <span className="font-mono text-sm font-semibold tracking-tight">MadamGY</span>
          <span className="text-xs text-muted-foreground">{actor.role}</span>
        </div>
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel className="font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
            Routes
          </SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              {visibleRoutes.map((r) => (
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
        <div className="truncate px-2 py-1 text-xs text-muted-foreground group-data-[collapsible=icon]:hidden" title={actor.email}>{actor.name}</div>
        {logoutError && <p role="alert" className="px-2 text-xs text-destructive">{logoutError}</p>}
        <SidebarMenu>
          <SidebarMenuItem><SidebarMenuButton onClick={logout} disabled={signingOut}><LogOut /><span>{signingOut ? "Signing out..." : "Sign out"}</span></SidebarMenuButton></SidebarMenuItem>
          <SidebarMenuItem>
            <SidebarMenuButton
              onClick={() => setTheme(resolvedTheme === "dark" ? "light" : "dark")}
            >
              <Sun className="hidden dark:block" />
              <Moon className="dark:hidden" />
              <span className="hidden dark:inline">Light mode</span>
              <span className="dark:hidden">Dark mode</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarFooter>
    </Sidebar>
  );
}
