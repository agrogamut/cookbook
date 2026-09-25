"use client";

import { useEffect, useState } from "react";
import { listAppointments } from "@/lib/api";
import { errorMessage } from "@/lib/portal-utils";
import type { Appointment } from "@/lib/portal-types";
import { Badge } from "@/components/ui/badge";

export function DoctorBookings() {
  const [items, setItems] = useState<Appointment[]>([]);
  const [error, setError] = useState("");
  useEffect(() => { let active = true; listAppointments().then((value) => active && setItems(value)).catch((e) => active && setError(errorMessage(e))); return () => { active = false; }; }, []);
  return <section className="space-y-3"><div><h2 className="text-base font-semibold">Booking requests</h2><p className="text-xs text-muted-foreground">These are visible for your caseload. An admin confirms or rejects them.</p></div>{error && <p role="alert" className="text-sm text-destructive">{error}</p>}{items.length === 0 ? <p className="text-sm text-muted-foreground">not available</p> : <div className="space-y-2">{items.map((item) => <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border p-3 text-sm" key={item.id}><span><span className="font-medium">{item.child_name}</span><span className="ml-2 text-muted-foreground">{new Date(item.starts_at).toLocaleString("en-IN", { timeZone: "Asia/Kolkata", dateStyle: "medium", timeStyle: "short" })}</span></span><Badge variant="outline">{item.status}</Badge></div>)}</div>}</section>;
}
