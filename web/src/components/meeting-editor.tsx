"use client";
import { useState } from "react";
import { saveMeeting } from "@/lib/api";
import { errorMessage } from "@/lib/portal-utils";
import type { Appointment } from "@/lib/portal-types";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Label } from "./ui/label";

export function MeetingEditor({
  appointment,
  onSaved,
}: {
  appointment: Appointment;
  onSaved: () => void;
}) {
  const [value, setValue] = useState(appointment.meeting_url ?? "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  if (
    ![
      "awaiting_payment",
      "pending_admin",
      "paid_pending_admin",
      "confirmed",
    ].includes(appointment.status)
  )
    return null;
  async function save(e: React.FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    setMessage("");
    try {
      await saveMeeting(appointment.id, value);
      setMessage(
        value.trim() ? "Meeting link saved." : "Meeting link removed.",
      );
      onSaved();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <form onSubmit={save} className="space-y-2 border-t pt-3">
      <Label htmlFor={`meeting-${appointment.id}`}>Meeting link</Label>
      <div className="flex flex-col gap-2 sm:flex-row">
        <Input
          id={`meeting-${appointment.id}`}
          type="url"
          placeholder="https://meet.google.com/..."
          maxLength={2048}
          value={value}
          onChange={(e) => setValue(e.target.value)}
          className="min-w-0 flex-1"
        />
        <Button type="submit" variant="outline" disabled={busy}>
          {busy ? "Saving..." : "Save link"}
        </Button>
        {appointment.meeting_url && (
          <Button asChild>
            <a
              href={appointment.meeting_url}
              target="_blank"
              rel="noopener noreferrer"
            >
              Join call
            </a>
          </Button>
        )}
      </div>
      <p className="text-xs text-muted-foreground">
        Add the HTTPS link for this appointment. The signed-in family can see
        it. Clear the field and save to remove it.
      </p>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      {message && (
        <p role="status" className="text-sm">
          {message}
        </p>
      )}
    </form>
  );
}
