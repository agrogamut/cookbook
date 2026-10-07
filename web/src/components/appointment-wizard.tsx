"use client";

import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { DateInput } from "@/components/date-input";
import { Label } from "@/components/ui/label";
import { listPublicDoctors } from "@/lib/api";
import { consultationDurationError, consultationEnd } from "@/lib/consultation";
import {
  calendarDayValue,
  errorMessage,
  indiaCalendarDate,
  indiaInstant,
  indiaTimeValue,
} from "@/lib/portal-utils";
import type { FreeInterval } from "@/lib/portal-types";

export type BookingChoice = {
  doctor_id?: string;
  starts_at: string;
  ends_at: string;
};

export function AppointmentWizard({
  loadAvailability,
  onBook,
  busy,
}: {
  loadAvailability: (date: string, doctor?: string) => Promise<FreeInterval[]>;
  onBook: (choice: BookingChoice) => Promise<void>;
  busy: boolean;
}) {
  const today = calendarDayValue(indiaCalendarDate());
  const [date, setDate] = useState(today);
  const [doctor, setDoctor] = useState("");
  const [doctors, setDoctors] = useState<{ id: string; name: string }[]>([]);
  const [intervals, setIntervals] = useState<FreeInterval[]>([]);
  const [selected, setSelected] = useState<FreeInterval | null>(null);
  const [starts, setStarts] = useState("");
  const [ends, setEnds] = useState("");
  const [loading, setLoading] = useState(false);
  const [searched, setSearched] = useState(false);
  const [error, setError] = useState("");

  function endForStart(start: string, interval = selected) {
    if (!start || !interval) return "";
    const end = consultationEnd(indiaInstant(date, start), interval.ends_at);
    return end ? indiaTimeValue(end) : "";
  }

  useEffect(() => {
    let active = true;
    listPublicDoctors()
      .then((items) => {
        if (active) setDoctors(items);
      })
      .catch((e) => {
        if (active) setError(errorMessage(e));
      });
    return () => {
      active = false;
    };
  }, []);

  function reset() {
    setIntervals([]);
    setSelected(null);
    setSearched(false);
    setStarts("");
    setEnds("");
  }
  async function findTimes() {
    setLoading(true);
    setError("");
    setSelected(null);
    try {
      setIntervals(await loadAvailability(date, doctor));
      setSearched(true);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setLoading(false);
    }
  }
  async function submit() {
    if (!selected || !starts || !ends) return;
    const start = indiaInstant(date, starts),
      end = indiaInstant(date, ends);
    const durationError = consultationDurationError(start, end);
    if (durationError) {
      setError(durationError);
      return;
    }
    if (
      new Date(start) < new Date(selected.starts_at) ||
      new Date(end) > new Date(selected.ends_at) ||
      end <= start
    ) {
      setError(
        "Choose a start and end within the selected available interval.",
      );
      return;
    }
    await onBook({
      doctor_id: doctor || undefined,
      starts_at: start,
      ends_at: end,
    });
  }

  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">
        Choose a doctor or let us assign an available doctor. All times are in
        India Standard Time (IST).
      </p>
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="space-y-2">
          <Label htmlFor="booking-doctor">Doctor</Label>
          <select
            id="booking-doctor"
            className="h-10 w-full rounded-md border bg-background px-3 text-sm"
            value={doctor}
            disabled={busy || loading}
            onChange={(e) => {
              setDoctor(e.target.value);
              reset();
            }}
          >
            <option value="">Any available doctor</option>
            {doctors.map((item) => (
              <option key={item.id} value={item.id}>
                {item.name}
              </option>
            ))}
          </select>
        </div>
        <div className="space-y-2">
          <Label htmlFor="booking-date">Date</Label>
          <DateInput
            id="booking-date"
            min={today}
            value={date}
            disabled={busy || loading}
            onValueChange={(value) => {
              setDate(value);
              reset();
            }}
          />
        </div>
      </div>
      <Button
        type="button"
        variant="outline"
        disabled={busy || loading || !date}
        onClick={findTimes}
      >
        {loading ? "Finding times..." : "Find available times"}
      </Button>
      {searched && intervals.length === 0 && (
        <p role="status" className="text-sm">
          No available times for this selection. Choose another date or doctor.
        </p>
      )}
      <div className="grid gap-2" aria-label="Available times">
        {intervals.map((item) => (
          <button
            type="button"
            disabled={busy}
            aria-pressed={selected === item}
            className={`rounded-lg border p-3 text-left text-sm focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[#c31352] ${selected === item ? "border-[#c31352] bg-[#fff0f4]" : "hover:bg-muted"}`}
            key={`${item.doctor_id}-${item.starts_at}`}
            onClick={() => {
              setSelected(item);
              setStarts(indiaTimeValue(item.starts_at));
              setEnds(endForStart(indiaTimeValue(item.starts_at), item));
              setError("");
            }}
          >
            {doctor ? item.doctor_name : "Available consultation"}:{" "}
            {indiaTimeValue(item.starts_at)} to {indiaTimeValue(item.ends_at)}{" "}
            IST
          </button>
        ))}
      </div>
      {selected && (
        <div className="space-y-3 rounded-lg bg-muted p-3">
          <p className="text-sm">
            Each consultation is limited to 30 minutes. Choose a time within
            these available hours.
          </p>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <Label htmlFor="booking-start">Start time</Label>
              <Input
                id="booking-start"
                type="time"
                value={starts}
                min={indiaTimeValue(selected.starts_at)}
                max={indiaTimeValue(selected.ends_at)}
                onChange={(e) => {
                  setStarts(e.target.value);
                  setEnds(endForStart(e.target.value));
                  setError("");
                }}
              />
            </div>
            <div>
              <Label htmlFor="booking-end">End time</Label>
              <Input
                id="booking-end"
                type="time"
                value={ends}
                min={starts}
                max={endForStart(starts)}
                onChange={(e) => setEnds(e.target.value)}
              />
            </div>
          </div>
        </div>
      )}
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <Button
        type="button"
        className="w-full bg-[#c31352] text-white hover:bg-[#a90f46]"
        disabled={busy || loading || !selected || !starts || !ends}
        onClick={submit}
      >
        {busy ? "Holding your time..." : "Continue to payment"}
      </Button>
      <p className="text-xs text-muted-foreground">
        An unpaid time is held for up to 15 minutes. The team confirms your
        appointment after payment.
      </p>
    </div>
  );
}
