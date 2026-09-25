"use client";

import { useEffect, useState } from "react";
import { Calendar } from "@/components/ui/calendar";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { listAvailability, listStaff, revokeAvailability, saveAvailability } from "@/lib/api";
import { calendarDayValue, errorMessage, indiaCalendarDate, indiaDateFromInstant, indiaInstant, indiaTimeValue } from "@/lib/portal-utils";
import type { AvailabilityBlock, StaffAccount } from "@/lib/portal-types";

function timeInIndia(value: string) { return new Date(value).toLocaleTimeString("en-IN", { timeZone: "Asia/Kolkata", hour: "2-digit", minute: "2-digit" }); }
function dateInIndia(value: string) { return new Date(value).toLocaleDateString("en-IN", { timeZone: "Asia/Kolkata", dateStyle: "medium" }); }

export function AvailabilityEditor({ admin = false }: { admin?: boolean }) {
  const [date, setDate] = useState(indiaCalendarDate);
  const [starts, setStarts] = useState("09:00");
  const [ends, setEnds] = useState("17:00");
  const [doctorID, setDoctorID] = useState("");
  const [doctors, setDoctors] = useState<StaffAccount[]>([]);
  const [blocks, setBlocks] = useState<AvailabilityBlock[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [revision, setRevision] = useState(0);
  const [editingID, setEditingID] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    Promise.all([listAvailability(), admin ? listStaff() : Promise.resolve([] as StaffAccount[])])
      .then(([available, staff]) => { if (active) { setBlocks(available); setDoctors(staff.filter((item) => item.role === "doctor" && item.active)); if (admin && !doctorID) setDoctorID(staff.find((item) => item.role === "doctor" && item.active)?.id ?? ""); } })
      .catch((e) => { if (active) setError(errorMessage(e)); });
    return () => { active = false; };
  }, [admin, revision, doctorID]);

  async function save(e: React.FormEvent) {
    e.preventDefault(); setBusy(true); setError(""); setMessage("");
    try { await saveAvailability({ id: editingID ?? undefined, doctor_id: admin ? doctorID : undefined, starts_at: indiaInstant(calendarDayValue(date), starts), ends_at: indiaInstant(calendarDayValue(date), ends) }); setEditingID(null); setMessage("Availability saved"); setRevision((v) => v + 1); }
    catch (e) { setError(errorMessage(e)); }
    finally { setBusy(false); }
  }
  async function revoke(id: string) {
    setBusy(true); setError("");
    try { await revokeAvailability(id); setRevision((v) => v + 1); }
    catch (e) { setError(errorMessage(e)); }
    finally { setBusy(false); }
  }
  function edit(block: AvailabilityBlock) {
    setEditingID(block.id);
    setDate(indiaDateFromInstant(block.starts_at));
    setStarts(indiaTimeValue(block.starts_at));
    setEnds(indiaTimeValue(block.ends_at));
    if (admin) setDoctorID(block.doctor_id);
    setError("");
    setMessage("");
  }
  function cancelEdit() {
    setEditingID(null);
    setError("");
    setMessage("");
  }
  return <Card><CardHeader><CardTitle>{admin ? "Doctor availability" : "My availability"}</CardTitle><CardDescription>{admin ? "Create and revoke real bookable blocks for any active doctor." : "Publish half-open time ranges that families can request immediately."}</CardDescription></CardHeader><CardContent className="space-y-5">
    {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}{message && <Alert><AlertDescription>{message}</AlertDescription></Alert>}
    <form className="grid gap-4 md:grid-cols-[auto_1fr]" onSubmit={save}><Calendar mode="single" selected={date} onSelect={(value) => value && setDate(value)} />
      <div className="space-y-3">{admin && <div><Label htmlFor="availability-doctor">Doctor</Label><select id="availability-doctor" className="mt-2 h-9 w-full rounded-md border bg-background px-3 text-sm" required value={doctorID} onChange={(e) => setDoctorID(e.target.value)}><option value="">Choose a doctor</option>{doctors.map((doctor) => <option value={doctor.id} key={doctor.id}>{doctor.name}</option>)}</select></div>}<div className="grid grid-cols-2 gap-3"><div><Label htmlFor="availability-start">Starts</Label><Input id="availability-start" type="time" required value={starts} onChange={(e) => setStarts(e.target.value)} /></div><div><Label htmlFor="availability-end">Ends</Label><Input id="availability-end" type="time" required value={ends} onChange={(e) => setEnds(e.target.value)} /></div></div><div className="flex gap-2"><Button disabled={busy || (admin && !doctorID)} className="bg-[#c31352] hover:bg-[#a90f46]">{editingID ? "Save changes" : "Publish availability"}</Button>{editingID && <Button type="button" variant="outline" disabled={busy} onClick={cancelEdit}>Cancel edit</Button>}</div></div>
    </form>
    <div className="space-y-2"><h3 className="text-sm font-semibold">Published blocks</h3>{blocks.length === 0 ? <p className="text-sm text-muted-foreground">not available</p> : blocks.map((block) => <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border px-3 py-2 text-sm" key={block.id}><span>{block.doctor_name}, {dateInIndia(block.starts_at)}, {timeInIndia(block.starts_at)} to {timeInIndia(block.ends_at)}</span><div className="flex gap-2"><Button variant="outline" size="sm" disabled={busy || !block.active} onClick={() => edit(block)}>Edit</Button><Button variant="outline" size="sm" disabled={busy || !block.active} onClick={() => revoke(block.id)}>{block.active ? "Revoke" : "Revoked"}</Button></div></div>)}</div>
  </CardContent></Card>;
}
