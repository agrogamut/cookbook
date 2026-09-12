"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useStaff } from "./staff-context";
import {
  CommandDialog, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList,
} from "@/components/ui/command";

const destinations = [
  { label: "Engine console", href: "/console" },
  { label: "Generate books", href: "/books" },
  { label: "Ingredients", href: "/ingredients" },
  { label: "Nutrition audit", href: "/audit/nutrition" },
  { label: "Gap register", href: "/audit/gaps" },
  { label: "Import runs", href: "/runs" },
  { label: "Reference", href: "/reference" },
];

export function CommandPalette() {
  const [open, setOpen] = useState(false);
  const router = useRouter();
  const actor = useStaff();
  const visible = [
    actor.role === "admin" ? { label: "Administration", href: "/admin" } : { label: "Assigned children", href: "/doctor" },
    ...destinations.filter(d => actor.role === "admin" || !["/audit/nutrition", "/audit/gaps", "/runs"].includes(d.href)),
  ];

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === "k" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        setOpen((v) => !v);
      }
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, []);

  return (
    <CommandDialog open={open} onOpenChange={setOpen}>
      <CommandInput placeholder="Jump to a screen..." />
      <CommandList>
        <CommandEmpty>No match.</CommandEmpty>
        <CommandGroup heading="Screens">
          {visible.map((d) => (
            <CommandItem key={d.href} onSelect={() => { setOpen(false); router.push(d.href); }}>
              {d.label}
            </CommandItem>
          ))}
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  );
}
