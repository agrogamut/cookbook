"use client";

import { useEffect, useState } from "react";
import { createAdminAppointment, listAllRegistrations, listStaff } from "@/lib/api";
import { errorMessage, indiaInstant } from "@/lib/portal-utils";
import { consultationDurationError } from "@/lib/consultation";
import type { Registration, StaffAccount } from "@/lib/portal-types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Alert, AlertDescription } from "@/components/ui/alert";

export function AdminDirectBooking() {
  const [registrations, setRegistrations] = useState<Registration[]>([]);
  const [doctors, setDoctors] = useState<StaffAccount[]>([]);
  const [registrationID, setRegistrationID] = useState("");
  const [doctorID, setDoctorID] = useState("");
  const [starts, setStarts] = useState("");
  const [ends, setEnds] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  useEffect(() => { let active = true; Promise.all([listAllRegistrations(), listStaff()]).then(([items, staff]) => { if (active) { setRegistrations(items); setDoctors(staff.filter((item) => item.role === "doctor")); } }).catch((e) => active && setError(errorMessage(e))); return () => { active = false; }; }, []);
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setMessage("");
    const startsAt = indiaInstant(starts.slice(0, 10), starts.slice(11, 16));
    const endsAt = indiaInstant(ends.slice(0, 10), ends.slice(11, 16));
    const durationError = consultationDurationError(startsAt, endsAt);
    if (durationError) {
      setError(durationError);
      return;
    }
    setBusy(true);
    try {
      await createAdminAppointment({ registration_id: registrationID, doctor_id: doctorID, starts_at: startsAt, ends_at: endsAt });
      setMessage("pending_admin");
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  return <section className="space-y-3 rounded-md border p-4"><div><h2 className="text-base font-semibold">Book for a registration</h2><p className="text-xs text-muted-foreground">Choose a free interval of up to 30 minutes. Payment is required before confirmation.</p></div>{error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}{message && <Alert><AlertDescription>{message}</AlertDescription></Alert>}<form className="grid gap-2 md:grid-cols-4" onSubmit={submit}><select aria-label="Registration" className="h-9 rounded-md border bg-background px-3 text-sm" required value={registrationID} onChange={(e) => setRegistrationID(e.target.value)}><option value="">Registration</option>{registrations.map((item) => <option value={item.id} key={item.id}>{item.child_name}</option>)}</select><select aria-label="Doctor" className="h-9 rounded-md border bg-background px-3 text-sm" required value={doctorID} onChange={(e) => setDoctorID(e.target.value)}><option value="">Doctor</option>{doctors.filter((item) => item.active).map((item) => <option value={item.id} key={item.id}>{item.name}</option>)}</select><Input aria-label="Start" type="datetime-local" required value={starts} onChange={(e) => setStarts(e.target.value)} /><Input aria-label="End" type="datetime-local" required value={ends} onChange={(e) => setEnds(e.target.value)} /><Button className="md:col-span-4 md:justify-self-start" disabled={busy}>Create pending booking</Button></form></section>;
}
