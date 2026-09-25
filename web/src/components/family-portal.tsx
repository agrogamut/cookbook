"use client";

import { useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { Calendar } from "@/components/ui/calendar";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  attachFamilyRegistration,
  cancelFamilyAppointment,
  createFamilyAppointment,
  createFamilyOrder,
  createFamilyRegistration,
  downloadFamilyBook,
  familyMe,
  familySignIn,
  familySignUp,
  familySignOut,
  listFamilyAvailability,
  listFamilyRegistrations,
  verifyFamilyCheckout,
} from "@/lib/api";
import { loadCheckout } from "@/lib/checkout";
import { calendarDayValue, errorMessage, indiaCalendarDate, indiaInstant, indiaTimeValue, money } from "@/lib/portal-utils";
import type { FamilyRegistration, FreeInterval } from "@/lib/portal-types";

function displayInstant(value: string | null) {
  if (!value) return "not available";
  return new Intl.DateTimeFormat("en-IN", {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: "Asia/Kolkata",
  }).format(new Date(value));
}

function statusText(value: string | null | undefined) {
  return value || "not available";
}

export function FamilyRegistrationCard({
  registration,
  onPay,
  onBook,
  onDownload,
  onCancel,
}: {
  registration: FamilyRegistration;
  onPay: (registration: FamilyRegistration) => void;
  onBook: (registration: FamilyRegistration) => void;
  onDownload: (releaseID: string) => void;
  onCancel: (appointmentID: string) => void;
}) {
  const approvedBooks = [
    ["Book 1", registration.book1_status, registration.book1_release_id],
    ["Book 2", registration.book2_status, registration.book2_release_id],
  ] as const;
  return (
    <Card data-testid={`family-registration-${registration.id}`}>
      <CardHeader>
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <CardTitle className="text-lg text-[#58293b]">{registration.child_name}</CardTitle>
            <CardDescription>Date of birth: {registration.date_of_birth}</CardDescription>
          </div>
          <Badge variant="outline">{registration.registration_status}</Badge>
        </div>
      </CardHeader>
      <CardContent className="space-y-5">
        <dl className="grid gap-3 text-sm sm:grid-cols-3">
          <div><dt className="text-[#815b69]">Appointment</dt><dd className="mt-1 font-medium">{statusText(registration.appointment_status)}</dd></div>
          <div><dt className="text-[#815b69]">Payment</dt><dd className="mt-1 font-medium">{registration.payment_status}</dd></div>
          <div><dt className="text-[#815b69]">Doctor</dt><dd className="mt-1 font-medium">{registration.doctor_name || "not available"}</dd></div>
        </dl>
        {registration.appointment_status && (
          <div className="rounded-md bg-[#fff0f4] px-3 py-2 text-sm text-[#58293b]">
            {registration.appointment_status}: {displayInstant(registration.appointment_starts_at)} to {displayInstant(registration.appointment_ends_at)}
            {registration.appointment_status === "pending_admin" && registration.appointment_id && (
              <Button className="ml-3 h-7" variant="outline" size="sm" onClick={() => onCancel(registration.appointment_id!)}>Cancel request</Button>
            )}
          </div>
        )}
        <div className="flex flex-wrap gap-2">
          {["unpaid", "pending", "failed"].includes(registration.payment_status) && registration.amount_paise !== null && (
            <Button className="bg-[#c31352] hover:bg-[#a90f46]" onClick={() => onPay(registration)}>
              Pay {money(registration.amount_paise, registration.currency)}
            </Button>
          )}
          {(!registration.appointment_status || ["rejected", "cancelled"].includes(registration.appointment_status)) && <Button variant="outline" onClick={() => onBook(registration)}>Book a consultation</Button>}
        </div>
        <div className="border-t pt-4">
          <p className="mb-2 text-xs font-medium uppercase tracking-wide text-[#815b69]">Released books</p>
          <div className="grid gap-2 sm:grid-cols-2">
            {approvedBooks.map(([label, status, releaseID]) => (
              <div key={label} className="flex items-center justify-between rounded-md border px-3 py-2 text-sm">
                <span>{label}: {statusText(status)}</span>
                {status === "approved" && releaseID && (
                  <Button size="sm" variant="outline" onClick={() => onDownload(releaseID)}>Download</Button>
                )}
              </div>
            ))}
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

export function FamilyPortal() {
  const router = useRouter();
  const [registrations, setRegistrations] = useState<FamilyRegistration[]>([]);
  const [selected, setSelected] = useState<FamilyRegistration | null>(null);
  const [intervals, setIntervals] = useState<FreeInterval[]>([]);
  const [date, setDate] = useState(indiaCalendarDate);
  const [doctorID, setDoctorID] = useState("");
  const [starts, setStarts] = useState("");
  const [ends, setEnds] = useState("");
  const [childName, setChildName] = useState("");
  const [dob, setDob] = useState("");
  const [phone, setPhone] = useState("");
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");

  async function refresh() {
    try {
      await familyMe();
      setRegistrations(await listFamilyRegistrations());
    } catch (e) {
      if (e instanceof Error && "status" in e && (e as { status?: number }).status === 401) {
        router.replace("/family/login");
        return;
      }
      setError(errorMessage(e));
    }
  }
  useEffect(() => {
    let active = true;
    void (async () => {
      try {
        await familyMe();
        const value = await listFamilyRegistrations();
        if (active) setRegistrations(value);
      } catch (e) {
        if (!active) return;
        if (e instanceof Error && "status" in e && (e as { status?: number }).status === 401) {
          router.replace("/family/login");
        } else {
          setError(errorMessage(e));
        }
      }
    })();
    return () => { active = false; };
  }, [router]);
  useEffect(() => {
    let active = true;
    listFamilyAvailability(calendarDayValue(date), doctorID).then((value) => {
      if (active) setIntervals(value);
    }).catch((e) => {
      if (active) setError(errorMessage(e));
    });
    return () => { active = false; };
  }, [date, doctorID]);

  const doctors = useMemo(() => {
    const seen = new Map<string, string>();
    intervals.forEach((item) => seen.set(item.doctor_id, item.doctor_name));
    return [...seen.entries()];
  }, [intervals]);

  function chooseInterval(item: FreeInterval) {
    setDoctorID(item.doctor_id);
    setStarts(indiaTimeValue(item.starts_at));
    setEnds(indiaTimeValue(item.ends_at));
  }
  function openBooking(registration: FamilyRegistration) {
    setSelected(registration);
    setMessage("");
    setError("");
  }
  async function submitBooking(e: React.FormEvent) {
    e.preventDefault();
    if (!selected || !starts || !ends) return;
    setBusy(true); setError(""); setMessage("");
    try {
      await createFamilyAppointment({ registration_id: selected.id, doctor_id: doctorID || undefined, starts_at: indiaInstant(calendarDayValue(date), starts), ends_at: indiaInstant(calendarDayValue(date), ends) });
      setMessage("pending_admin");
      setSelected(null);
      await refresh();
    } catch (e) { setError(errorMessage(e)); }
    finally { setBusy(false); }
  }
  async function pay(registration: FamilyRegistration) {
    setBusy(true); setError("");
    try {
      const order = await createFamilyOrder(registration.id);
      const Razorpay = await loadCheckout();
      const checkout = new Razorpay({
        key: order.key_id, order_id: order.order_id, amount: order.amount_paise, currency: order.currency,
        name: "MadamGY", description: `Consultation for ${registration.child_name}`,
        handler: async (result) => {
          try { await verifyFamilyCheckout(registration.id, result); await refresh(); setMessage("paid"); }
          catch (e) { setError(errorMessage(e)); }
          finally { setBusy(false); }
        }, modal: { ondismiss: () => setBusy(false) }, theme: { color: "#c31352" },
      });
      checkout.on("payment.failed", () => { setError("Payment failed. The registration remains saved."); setBusy(false); });
      checkout.open();
    } catch (e) { setError(errorMessage(e)); setBusy(false); }
  }
  async function download(releaseID: string) {
    setBusy(true); setError("");
    try {
      const blob = await downloadFamilyBook(releaseID);
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a"); link.href = url; link.download = "madamgy-book.pdf"; link.click(); URL.revokeObjectURL(url);
    } catch (e) { setError(errorMessage(e)); }
    finally { setBusy(false); }
  }
  async function createRegistration(e: React.FormEvent) {
    e.preventDefault(); setBusy(true); setError("");
    try { await createFamilyRegistration({ child_name: childName, date_of_birth: dob, phone }); setChildName(""); setDob(""); setPhone(""); await refresh(); }
    catch (e) { setError(errorMessage(e)); }
    finally { setBusy(false); }
  }
  async function attach(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true); setError("");
    try { await attachFamilyRegistration(token); setToken(""); await refresh(); }
    catch (e) { setError(errorMessage(e)); }
    finally { setBusy(false); }
  }
  async function cancel(id: string) {
    setBusy(true); setError("");
    try { await cancelFamilyAppointment(id); await refresh(); }
    catch (e) { setError(errorMessage(e)); }
    finally { setBusy(false); }
  }

  return (
    <main className="min-h-screen bg-[#fff0f4] px-4 py-8 text-[#58293b] sm:px-8">
      <div className="mx-auto max-w-5xl space-y-6">
        <header className="flex flex-wrap items-center justify-between gap-3">
          <div><p className="text-sm text-[#815b69]">MadamGY family portal</p><h1 className="text-3xl font-semibold">Your consultations</h1></div>
          <div className="flex gap-2"><Button variant="outline" onClick={() => router.push("/")}>Back to MadamGY</Button><Button variant="ghost" onClick={async () => { await familySignOut(); router.replace("/family/login"); }}>Sign out</Button></div>
        </header>
        {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
        {message && <Alert><AlertDescription>{message}</AlertDescription></Alert>}
        <section className="grid gap-4 md:grid-cols-2">
          <Card><CardHeader><CardTitle>Add a registration</CardTitle><CardDescription>Create a registration owned by this account.</CardDescription></CardHeader><CardContent><form className="space-y-3" onSubmit={createRegistration}>
            <Label htmlFor="family-child">Child name</Label><Input id="family-child" required value={childName} onChange={(e) => setChildName(e.target.value)} />
            <Label htmlFor="family-dob">Date of birth</Label><Input id="family-dob" type="date" required value={dob} onChange={(e) => setDob(e.target.value)} />
            <Label htmlFor="family-phone">Phone</Label><Input id="family-phone" required value={phone} onChange={(e) => setPhone(e.target.value)} />
            <Button disabled={busy} className="bg-[#c31352] hover:bg-[#a90f46]">Save registration</Button>
          </form></CardContent></Card>
          <Card><CardHeader><CardTitle>Attach an existing request</CardTitle><CardDescription>Use the private token given with an earlier intake. Email is not used to merge accounts.</CardDescription></CardHeader><CardContent><form className="space-y-3" onSubmit={attach}>
            <Label htmlFor="family-token">Private registration token</Label><Input id="family-token" required value={token} onChange={(e) => setToken(e.target.value)} /><Button disabled={busy} variant="outline">Attach request</Button>
          </form></CardContent></Card>
        </section>
        <section className="space-y-4"><h2 className="text-xl font-semibold">Your registrations</h2>{registrations.length === 0 ? <Card><CardContent className="py-8 text-sm text-[#815b69]">not available</CardContent></Card> : registrations.map((item) => <FamilyRegistrationCard key={item.id} registration={item} onPay={pay} onBook={openBooking} onDownload={download} onCancel={cancel} />)}</section>
      </div>
      {selected && <div className="fixed inset-0 z-50 grid place-items-center bg-[#58293b]/30 p-4" role="dialog" aria-modal="true" aria-labelledby="booking-title"><Card className="max-h-[90vh] w-full max-w-3xl overflow-y-auto"><CardHeader><CardTitle id="booking-title">Book a consultation</CardTitle><CardDescription>Times are shown in Asia/Kolkata. The request stays pending until an admin confirms it.</CardDescription></CardHeader><CardContent><form className="grid gap-5 md:grid-cols-[auto_1fr]" onSubmit={submitBooking}><Calendar mode="single" selected={date} onSelect={(value) => { if (value) { setDate(value); setStarts(""); setEnds(""); } }} disabled={{ before: indiaCalendarDate() }} />
        <div className="space-y-4"><div><Label htmlFor="family-doctor">Doctor option</Label><select id="family-doctor" className="mt-2 h-9 w-full rounded-md border bg-background px-3 text-sm" value={doctorID} onChange={(e) => setDoctorID(e.target.value)}><option value="">Any available doctor</option>{doctors.map(([id, name]) => <option value={id} key={id}>{name}</option>)}</select></div><div className="grid grid-cols-2 gap-3"><div><Label htmlFor="family-start">Start</Label><Input id="family-start" type="time" required value={starts} onChange={(e) => setStarts(e.target.value)} /></div><div><Label htmlFor="family-end">End</Label><Input id="family-end" type="time" required value={ends} onChange={(e) => setEnds(e.target.value)} /></div></div><div className="space-y-2"><p className="text-sm font-medium">Free intervals</p>{intervals.length === 0 ? <p className="text-sm text-[#815b69]">not available</p> : intervals.map((item) => <button type="button" key={`${item.doctor_id}-${item.starts_at}`} className="block w-full rounded-md border p-2 text-left text-sm hover:bg-[#fff0f4]" onClick={() => chooseInterval(item)}>{item.doctor_name}, {new Date(item.starts_at).toLocaleTimeString("en-IN", { timeZone: "Asia/Kolkata", hour: "2-digit", minute: "2-digit" })} to {new Date(item.ends_at).toLocaleTimeString("en-IN", { timeZone: "Asia/Kolkata", hour: "2-digit", minute: "2-digit" })}</button>)}</div><div className="flex gap-2"><Button disabled={busy} className="bg-[#c31352] hover:bg-[#a90f46]">Request booking</Button><Button type="button" variant="outline" onClick={() => setSelected(null)}>Close</Button></div></div>
      </form></CardContent></Card></div>}
    </main>
  );
}

export function FamilyAuth({ mode }: { mode: "login" | "register" }) {
  const router = useRouter();
  const [name, setName] = useState(""); const [email, setEmail] = useState(""); const [password, setPassword] = useState(""); const [error, setError] = useState(""); const [busy, setBusy] = useState(false);
  async function submit(e: React.FormEvent) {
    e.preventDefault(); setBusy(true); setError("");
    try {
      if (mode === "login") await familySignIn(email, password);
      else await familySignUp(name, email, password);
      router.replace("/family");
    } catch (e) { setError(errorMessage(e)); setBusy(false); }
  }
  return <main className="min-h-screen bg-[#fff0f4] px-4 py-10"><Card className="mx-auto w-full max-w-md"><CardHeader><CardTitle className="text-[#58293b]">{mode === "login" ? "Family sign in" : "Create a family account"}</CardTitle><CardDescription>{mode === "login" ? "Open your consultation status and released books." : "Email is required to open a private family login."}</CardDescription></CardHeader><CardContent><form className="space-y-4" onSubmit={submit}>{mode === "register" && <div><Label htmlFor="family-name">Your name</Label><Input id="family-name" required value={name} onChange={(e) => setName(e.target.value)} /></div>}<div><Label htmlFor="family-email">Email</Label><Input id="family-email" type="email" required value={email} onChange={(e) => setEmail(e.target.value)} /></div><div><Label htmlFor="family-password">Password</Label><Input id="family-password" type="password" minLength={12} maxLength={256} required value={password} onChange={(e) => setPassword(e.target.value)} /></div>{error && <p role="alert" className="text-sm text-destructive">{error}</p>}<Button disabled={busy} className="w-full bg-[#c31352] hover:bg-[#a90f46]">{busy ? "Working..." : mode === "login" ? "Sign in" : "Create account"}</Button></form><p className="mt-5 text-center text-sm text-[#815b69]">{mode === "login" ? <>Need an account? <a className="underline" href="/family/register">Create one</a></> : <>Already registered? <a className="underline" href="/family/login">Sign in</a></>}</p></CardContent></Card></main>;
}
