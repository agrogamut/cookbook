"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import {
  CommandDialog, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList,
} from "@/components/ui/command";

const destinations = [
  { label: "Engine console", href: "/" },
  { label: "Ingredients", href: "/ingredients" },
  { label: "Nutrition audit", href: "/audit/nutrition" },
  { label: "Gap register", href: "/audit/gaps" },
  { label: "Import runs", href: "/runs" },
  { label: "Reference", href: "/reference" },
];

export function CommandPalette() {
  const [open, setOpen] = useState(false);
  const router = useRouter();

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
      <CommandInput placeholder="Jump to a screen, or paste a recipe / ingredient ID..." />
      <CommandList>
        <CommandEmpty>No match.</CommandEmpty>
        <CommandGroup heading="Screens">
          {destinations.map((d) => (
            <CommandItem key={d.href} onSelect={() => { setOpen(false); router.push(d.href); }}>
              {d.label}
            </CommandItem>
          ))}
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  );
}
