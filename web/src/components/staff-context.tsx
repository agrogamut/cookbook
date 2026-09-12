"use client";
import { createContext, useContext } from "react";
import type { StaffAccount } from "@/lib/portal-types";
export const StaffContext = createContext<StaffAccount | null>(null);
export function useStaff() {
  const actor = useContext(StaffContext);
  if (!actor) throw new Error("Staff context is missing.");
  return actor;
}
