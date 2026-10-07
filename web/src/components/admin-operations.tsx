"use client";

import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  refundAppointment,
  decideAppointment,
  decideBookRelease,
  downloadStaffBookRelease,
  listAppointments,
  listBookReleases,
  listStaff,
} from "@/lib/api";
import { errorMessage, indiaInstant, money } from "@/lib/portal-utils";
import type {
  Appointment,
  BookRelease,
  StaffAccount,
} from "@/lib/portal-types";

function localInput(value: string) {
  const date = new Date(value);
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone: "Asia/Kolkata",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).formatToParts(date);
  const get = (type: string) =>
    parts.find((part) => part.type === type)?.value ?? "";
  return `${get("year")}-${get("month")}-${get("day")}T${get("hour")}:${get("minute")}`;
}
function show(value: string) {
  return new Date(value).toLocaleString("en-IN", {
    timeZone: "Asia/Kolkata",
    dateStyle: "medium",
    timeStyle: "short",
  });
}

export function AdminOperations() {
  const [filter, setFilter] = useState("all");
  const [appointments, setAppointments] = useState<Appointment[]>([]);
  const [releases, setReleases] = useState<BookRelease[]>([]);
  const [doctors, setDoctors] = useState<StaffAccount[]>([]);
  const [changes, setChanges] = useState<
    Record<string, { doctor: string; starts: string; ends: string }>
  >({});
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [revision, setRevision] = useState(0);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    let active = true;
    Promise.all([listAppointments(), listBookReleases(), listStaff()])
      .then(([a, b, staff]) => {
        if (active) {
          setAppointments(a);
          setReleases(b);
          setDoctors(staff.filter((item) => item.role === "doctor"));
        }
      })
      .catch((e) => active && setError(errorMessage(e)));
    return () => {
      active = false;
    };
  }, [revision]);
  function changeFor(item: Appointment) {
    return (
      changes[item.id] ?? {
        doctor: item.doctor_id,
        starts: localInput(item.starts_at),
        ends: localInput(item.ends_at),
      }
    );
  }
  function updateChange(
    item: Appointment,
    patch: Partial<{ doctor: string; starts: string; ends: string }>,
  ) {
    setChanges((current) => ({
      ...current,
      [item.id]: { ...changeFor(item), ...patch },
    }));
  }
  async function decide(
    item: Appointment,
    action: "confirm" | "reject" | "reschedule" | "cancel",
  ) {
    if (
      busy ||
      (action !== "confirm" &&
        !window.confirm(
          `${action} the appointment for ${item.child_name}? Captured payments will require a refund if cancelled or rejected.`,
        ))
    )
      return;
    setBusy(true);
    setError("");
    try {
      const change = changeFor(item);
      await decideAppointment(
        item.id,
        action === "confirm" || action === "reschedule"
          ? {
              action,
              doctor_id: change.doctor,
              starts_at: indiaInstant(
                change.starts.slice(0, 10),
                change.starts.slice(11, 16),
              ),
              ends_at: indiaInstant(
                change.ends.slice(0, 10),
                change.ends.slice(11, 16),
              ),
            }
          : { action },
      );
      setMessage(action);
      setRevision((v) => v + 1);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  async function release(id: string, action: "approve" | "reject") {
    setBusy(true);
    setError("");
    try {
      await decideBookRelease(id, action);
      setMessage(action);
      setRevision((v) => v + 1);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  async function refund(item: Appointment) {
    if (
      busy ||
      !window.confirm(
        `Refund the remaining payment for ${item.child_name}? This submits a refund to Razorpay.`,
      )
    )
      return;
    setBusy(true);
    setError("");
    try {
      const result = await refundAppointment(item.id);
      setMessage(result.message);
      setRevision((v) => v + 1);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  async function download(id: string) {
    try {
      const blob = await downloadStaffBookRelease(id);
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = "draft-book.pdf";
      a.click();
      URL.revokeObjectURL(url);
    } catch (e) {
      setError(errorMessage(e));
    }
  }
  return (
    <div className="space-y-8">
      {error && (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      {message && (
        <Alert>
          <AlertDescription>{message}</AlertDescription>
        </Alert>
      )}
      <section className="space-y-3">
        <div>
          <h2 className="text-base font-semibold">Booking requests</h2>
          <p className="text-xs text-muted-foreground">
            Payment must be captured before a request can be confirmed. All
            times are IST.
          </p>
        </div>
        <label className="block text-sm">
          Filter requests
          <select
            aria-label="Filter requests"
            className="ml-3 rounded-md border bg-background px-3 py-2"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          >
            {[
              "all",
              "awaiting_payment",
              "paid_pending_admin",
              "confirmed",
              "failed",
              "expired",
              "cancelled",
              "refund_required",
              "refunded",
            ].map((value) => (
              <option key={value} value={value}>
                {value.replaceAll("_", " ")}
              </option>
            ))}
          </select>
        </label>
        {appointments.length === 0 ? (
          <p className="text-sm text-muted-foreground">not available</p>
        ) : (
          appointments
            .filter(
              (item) =>
                filter === "all" ||
                item.status === filter ||
                item.payment_status === filter,
            )
            .map((item) => {
              const change = changeFor(item);
              const editable = [
                "awaiting_payment",
                "pending_admin",
                "paid_pending_admin",
                "confirmed",
              ].includes(item.status);
              const paid = item.payment_status === "paid";
              return (
                <div className="space-y-3 rounded-md border p-3" key={item.id}>
                  <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
                    <span className="font-medium">
                      {item.child_name}
                      <span className="ml-3 font-normal text-muted-foreground">
                        {item.guardian_name}
                        {item.phone ? `, ${item.phone}` : ""}
                      </span>
                    </span>
                    <div className="flex items-center gap-2">
                      <Badge variant="outline">{item.status}</Badge>
                      <Badge variant="secondary">
                        payment: {item.payment_status}
                      </Badge>
                    </div>
                  </div>
                  <div className="grid gap-2 md:grid-cols-3">
                    <select
                      aria-label={`Doctor for ${item.child_name}`}
                      className="h-9 rounded-md border bg-background px-3 text-sm"
                      value={change.doctor}
                      disabled={!editable || busy}
                      onChange={(e) =>
                        updateChange(item, { doctor: e.target.value })
                      }
                    >
                      {doctors
                        .filter(
                          (doctor) =>
                            doctor.active || doctor.id === change.doctor,
                        )
                        .map((doctor) => (
                          <option value={doctor.id} key={doctor.id}>
                            {doctor.name}
                          </option>
                        ))}
                    </select>
                    <Input
                      aria-label={`Start for ${item.child_name}`}
                      type="datetime-local"
                      value={change.starts}
                      disabled={!editable || busy}
                      onChange={(e) =>
                        updateChange(item, { starts: e.target.value })
                      }
                    />
                    <Input
                      aria-label={`End for ${item.child_name}`}
                      type="datetime-local"
                      value={change.ends}
                      disabled={!editable || busy}
                      onChange={(e) =>
                        updateChange(item, { ends: e.target.value })
                      }
                    />
                  </div>
                  <p className="text-xs text-muted-foreground">
                    Requested interval: {show(item.starts_at)} to{" "}
                    {show(item.ends_at)}. Mode: {item.mode}.
                  </p>
                  {["pending_admin", "paid_pending_admin"].includes(
                    item.status,
                  ) && (
                    <div className="flex gap-2">
                      <Button
                        size="sm"
                        disabled={!paid || busy}
                        className="bg-[#c31352] hover:bg-[#a90f46]"
                        onClick={() => decide(item, "confirm")}
                      >
                        Confirm
                      </Button>
                      <Button
                        size="sm"
                        disabled={busy}
                        variant="outline"
                        onClick={() => decide(item, "reject")}
                      >
                        Reject
                      </Button>
                    </div>
                  )}
                  <details className="text-xs">
                    <summary className="cursor-pointer">
                      Payment references
                    </summary>
                    <dl className="mt-2 grid gap-1 break-all">
                      <div>Registration: {item.registration_id}</div>
                      <div>Order: {item.order_id || "Not created"}</div>
                      <div>Payment: {item.payment_id || "Not captured"}</div>
                      <div>
                        Amount: {money(item.amount_paise ?? null, "INR")},
                        refunded: {money(item.refunded_paise ?? 0, "INR")}
                      </div>
                      <div>Refund: {item.refund_id || "Not submitted"}</div>
                    </dl>
                  </details>
                  {item.status === "refund_required" && (
                    <Button
                      size="sm"
                      disabled={busy}
                      variant="outline"
                      onClick={() => refund(item)}
                    >
                      {item.refund_id
                        ? "Check refund status"
                        : "Refund payment"}
                    </Button>
                  )}
                  {item.status === "awaiting_payment" && (
                    <p className="text-xs text-muted-foreground">
                      Waiting for payment before admin confirmation.
                    </p>
                  )}
                  {item.status === "confirmed" && (
                    <div className="flex gap-2">
                      <Button
                        size="sm"
                        disabled={busy}
                        className="bg-[#c31352] hover:bg-[#a90f46]"
                        onClick={() => decide(item, "reschedule")}
                      >
                        Move or reassign
                      </Button>
                      <Button
                        size="sm"
                        disabled={busy}
                        variant="outline"
                        onClick={() => decide(item, "cancel")}
                      >
                        Cancel booking
                      </Button>
                    </div>
                  )}
                </div>
              );
            })
        )}
      </section>
      <section className="space-y-3">
        <div>
          <h2 className="text-base font-semibold">Book releases</h2>
          <p className="text-xs text-muted-foreground">
            Approval applies to the stored PDF bytes shown here.
          </p>
        </div>
        {releases.length === 0 ? (
          <p className="text-sm text-muted-foreground">not available</p>
        ) : (
          releases.map((item) => (
            <div
              className="flex flex-wrap items-center justify-between gap-3 rounded-md border p-3 text-sm"
              key={item.id}
            >
              <div>
                <p className="font-medium">
                  {item.child_name}, {item.book}
                </p>
                <p className="text-xs text-muted-foreground">
                  {item.status}, {Math.round(item.size_bytes / 1024)} KB,
                  generated {show(item.generated_at)}
                </p>
              </div>
              <div className="flex gap-2">
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => download(item.id)}
                >
                  Open PDF
                </Button>
                {item.status === "pending_admin" && (
                  <>
                    <Button
                      size="sm"
                      className="bg-[#c31352] hover:bg-[#a90f46]"
                      onClick={() => release(item.id, "approve")}
                    >
                      Approve
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => release(item.id, "reject")}
                    >
                      Reject
                    </Button>
                  </>
                )}
              </div>
            </div>
          ))
        )}
      </section>
    </div>
  );
}
