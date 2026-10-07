"use client";

import { useEffect, useRef, useState } from "react";
import { CalendarIcon } from "lucide-react";
import { Popover } from "radix-ui";
import { Button } from "@/components/ui/button";
import { Calendar } from "@/components/ui/calendar";
import { Input } from "@/components/ui/input";
import {
  calendarDayValue,
  formatDayFirstDate,
  parseDayFirstDate,
} from "@/lib/portal-utils";

type DateInputProps = {
  id: string;
  value: string;
  onValueChange: (value: string) => void;
  min?: string;
  max?: string;
  required?: boolean;
  disabled?: boolean;
  "aria-describedby"?: string;
};

function localDate(value: string): Date | undefined {
  return value ? new Date(`${value}T12:00:00`) : undefined;
}

export function DateInput({
  id,
  value,
  onValueChange,
  min,
  max,
  required,
  disabled,
  "aria-describedby": describedBy,
}: DateInputProps) {
  const input = useRef<HTMLInputElement>(null);
  const [draft, setDraft] = useState({ value, text: formatDayFirstDate(value) });
  const [showError, setShowError] = useState(false);
  const [open, setOpen] = useState(false);
  const text = draft.value === value ? draft.text : formatDayFirstDate(value);

  function validationMessage(text: string): string {
    if (!text) return "";
    const date = parseDayFirstDate(text);
    if (!date) return "Enter a valid date as DD/MM/YYYY.";
    if (min && date < min) return `Choose ${formatDayFirstDate(min)} or later.`;
    if (max && date > max) return `Choose ${formatDayFirstDate(max)} or earlier.`;
    return "";
  }

  const error = validationMessage(text);
  useEffect(() => {
    input.current?.setCustomValidity(error);
  }, [error]);

  function update(text: string) {
    const error = validationMessage(text);
    const next = error ? "" : (parseDayFirstDate(text) ?? "");
    input.current?.setCustomValidity(error);
    setDraft({ value: next, text });
    onValueChange(next);
  }

  return (
    <div className="space-y-1">
      <div className="relative">
        <Input
          ref={input}
          id={id}
          type="text"
          inputMode="numeric"
          autoComplete="off"
          placeholder="DD/MM/YYYY"
          maxLength={10}
          required={required}
          disabled={disabled}
          className="pr-10"
          value={text}
          aria-invalid={showError && Boolean(error)}
          aria-describedby={[describedBy, `${id}-format`, showError && error ? `${id}-error` : ""].filter(Boolean).join(" ")}
          onChange={(event) => update(event.target.value)}
          onInvalid={() => setShowError(true)}
          onBlur={() => {
            setShowError(true);
            if (value && !error) setDraft({ value, text: formatDayFirstDate(value) });
          }}
        />
        <Popover.Root open={open} onOpenChange={setOpen}>
          <Popover.Trigger asChild>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="absolute right-0 top-0 h-9 w-9"
              aria-label="Open calendar"
              disabled={disabled}
            >
              <CalendarIcon className="size-4" aria-hidden="true" />
            </Button>
          </Popover.Trigger>
          <Popover.Portal>
            <Popover.Content align="end" sideOffset={4} className="family-surface z-50 rounded-md border bg-popover shadow-md">
              <Calendar
                mode="single"
                selected={localDate(value)}
                defaultMonth={localDate(value || max || min || "")}
                captionLayout={max ? "dropdown" : "label"}
                startMonth={localDate(min ?? "1900-01-01")}
                endMonth={localDate(max ?? "")}
                disabled={[
                  ...(min ? [{ before: localDate(min)! }] : []),
                  ...(max ? [{ after: localDate(max)! }] : []),
                ]}
                onSelect={(date) => {
                  if (!date) return;
                  update(formatDayFirstDate(calendarDayValue(date)));
                  setOpen(false);
                }}
              />
            </Popover.Content>
          </Popover.Portal>
        </Popover.Root>
      </div>
      <p id={`${id}-format`} className="text-xs text-muted-foreground">DD/MM/YYYY</p>
      {showError && error && (
        <p id={`${id}-error`} role="alert" className="text-xs text-destructive">{error}</p>
      )}
    </div>
  );
}
