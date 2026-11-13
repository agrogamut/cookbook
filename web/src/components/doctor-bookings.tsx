"use client";
import { useEffect, useState } from "react";
import Link from "next/link";
import { listAppointments } from "@/lib/api";
import { errorMessage } from "@/lib/portal-utils";
import type { Appointment } from "@/lib/portal-types";
import { Badge } from "./ui/badge";
import { Button } from "./ui/button";
import { MeetingEditor } from "./meeting-editor";

export function appointmentTime(value: string) {
  return new Intl.DateTimeFormat("en-IN", {
    timeZone: "Asia/Kolkata",
    dateStyle: "full",
    timeStyle: "short",
  }).format(new Date(value));
}
export function DoctorBookings() {
  const [items, setItems] = useState<Appointment[]>([]);
  const [error, setError] = useState("");
  const [revision, setRevision] = useState(0);
  const [loading, setLoading] = useState(true);
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 60000);
    return () => clearInterval(timer);
  }, []);
  useEffect(() => {
    let active = true;
    listAppointments()
      .then((value) => {
        if (active) {
          setItems(value);
          setError("");
        }
      })
      .catch((e) => active && setError(errorMessage(e)))
      .finally(() => active && setLoading(false));
    return () => {
      active = false;
    };
  }, [revision]);
  const ordered = [...items].sort(
    (a, b) => Date.parse(a.starts_at) - Date.parse(b.starts_at),
  );
  const calls = ordered.filter(
    (a) => a.status === "confirmed" && Date.parse(a.ends_at) > now,
  );
  const requests = ordered.filter((a) =>
    ["awaiting_payment", "pending_admin", "paid_pending_admin"].includes(
      a.status,
    ),
  );
  function card(item: Appointment) {
    return (
      <article
        className="space-y-3 rounded-lg border bg-card p-4"
        key={item.id}
      >
        <div className="flex flex-wrap items-start justify-between gap-2">
          <h3 className="font-semibold">{item.child_name}</h3>
          <Badge variant="outline">{item.status.replaceAll("_", " ")}</Badge>
        </div>
        <p className="text-base font-medium">
          {appointmentTime(item.starts_at)} IST
        </p>
        <p className="text-sm text-muted-foreground">
          Ends{" "}
          {new Date(item.ends_at).toLocaleTimeString("en-IN", {
            timeZone: "Asia/Kolkata",
            hour: "numeric",
            minute: "2-digit",
          })}{" "}
          IST.{" "}
          {item.status === "confirmed"
            ? "Join at the scheduled time."
            : "The administrator confirms the appointment after payment is captured."}
        </p>
        {item.guardian_description && (
          <div className="rounded-md bg-muted/40 p-3">
            <p className="mb-1 text-xs font-semibold">
              Additional details for the doctor, guardian-reported
            </p>
            <p className="whitespace-pre-wrap break-words text-sm">
              {item.guardian_description}
            </p>
          </div>
        )}
        <MeetingEditor
          appointment={item}
          onSaved={() => setRevision((v) => v + 1)}
        />
        {item.status === "confirmed" && (
          <Button
            asChild
            variant="outline"
            className="border-2 border-[#c31352] dark:border-pink-400"
          >
            <Link href={`/books?registration=${item.registration_id}`}>
              Open book generator
            </Link>
          </Button>
        )}
      </article>
    );
  }
  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between gap-3">
        <p className="text-sm text-muted-foreground">
          Review requests, check the meeting link, then join at the scheduled
          time. All times are IST.
        </p>
        <Button variant="outline" onClick={() => setRevision((v) => v + 1)}>
          Refresh calls
        </Button>
      </div>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <section aria-labelledby="upcoming-calls" className="space-y-3">
        <h2 id="upcoming-calls" className="text-xl font-semibold">
          Upcoming calls
        </h2>
        {loading ? (
          <p>Loading calls...</p>
        ) : calls.length ? (
          <div className="grid gap-3 lg:grid-cols-2">{calls.map(card)}</div>
        ) : (
          <p className="rounded-md border p-4 text-sm text-muted-foreground">
            No confirmed calls are scheduled. Confirmed appointments will appear
            here in time order.
          </p>
        )}
      </section>
      <section aria-labelledby="consultation-requests" className="space-y-3">
        <h2 id="consultation-requests" className="text-xl font-semibold">
          Consultation requests
        </h2>
        <p className="text-sm text-muted-foreground">
          These appointments are assigned to you and waiting for payment or
          administrator confirmation.
        </p>
        {loading ? (
          <p>Loading requests...</p>
        ) : requests.length ? (
          <div className="grid gap-3 lg:grid-cols-2">{requests.map(card)}</div>
        ) : (
          <p className="text-sm text-muted-foreground">
            No requests are waiting for confirmation.
          </p>
        )}
      </section>
    </div>
  );
}
