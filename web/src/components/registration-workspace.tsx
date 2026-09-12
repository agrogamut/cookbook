"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { RefreshCw, Search } from "lucide-react";
import {
  getRegistration,
  listRegistrations,
  listStaff,
  updateRegistration,
} from "@/lib/api";
import {
  ageFromBirth,
  calendarDate,
  errorMessage,
  money,
  paymentLabels,
} from "@/lib/portal-utils";
import type {
  ConsultationStatus,
  Registration,
  RegistrationList,
  StaffAccount,
} from "@/lib/portal-types";
import { useStaff } from "./staff-context";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Label } from "./ui/label";
import { Badge } from "./ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "./ui/table";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "./ui/dialog";

const selectClass =
  "h-9 rounded-md border bg-background px-3 text-xs focus-visible:outline-2 focus-visible:outline-ring";
const statuses: ConsultationStatus[] = [
  "new",
  "contacted",
  "scheduled",
  "completed",
  "cancelled",
];
const title = (value: string) => value.charAt(0).toUpperCase() + value.slice(1);
export function PaymentBadge({
  value,
}: {
  value: Registration["payment_status"];
}) {
  return (
    <Badge
      variant="outline"
      className={
        value === "paid"
          ? "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400"
          : value === "failed"
            ? "text-destructive"
            : "text-muted-foreground"
      }
    >
      {paymentLabels[value]}
    </Badge>
  );
}

export function RegistrationWorkspace() {
  const admin = useStaff().role === "admin";
  const [data, setData] = useState<RegistrationList | null>(null);
  const [query, setQuery] = useState("");
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("");
  const [payment, setPayment] = useState("");
  const [assignment, setAssignment] = useState("");
  const [page, setPage] = useState(1);
  const [revision, setRevision] = useState(0);
  const [loadedQuery, setLoadedQuery] = useState("");
  const [error, setError] = useState("");
  const [selected, setSelected] = useState<Registration | null>(null);
  const [doctors, setDoctors] = useState<StaffAccount[]>([]);
  const [staffError, setStaffError] = useState("");
  const queryKey = JSON.stringify([
    search,
    status,
    payment,
    assignment,
    page,
    revision,
  ]);
  const loading = loadedQuery !== queryKey;

  useEffect(() => {
    let active = true;
    const params = new URLSearchParams({
      q: search,
      status,
      payment,
      assignment,
      page: String(page),
    });
    listRegistrations(params)
      .then((result) => {
        if (active) {
          setData(result);
          setError("");
        }
      })
      .catch((e) => {
        if (active) {
          setData(null);
          setError(errorMessage(e));
        }
      })
      .finally(() => {
        if (active) setLoadedQuery(queryKey);
      });
    return () => {
      active = false;
    };
  }, [search, status, payment, assignment, page, revision, queryKey]);
  useEffect(() => {
    if (!admin) return;
    let active = true;
    listStaff()
      .then((result) => {
        if (active) {
          setDoctors(result.filter((d) => d.role === "doctor"));
          setStaffError("");
        }
      })
      .catch((e) => {
        if (active) setStaffError(errorMessage(e));
      });
    return () => {
      active = false;
    };
  }, [admin, revision]);

  const summary = data?.summary;
  return (
    <div className="space-y-5">
      <dl
        className={`grid grid-cols-2 divide-x rounded-md border ${admin ? "md:grid-cols-4" : "md:grid-cols-3"}`}
      >
        {(
          [
            [admin ? "Registrations" : "Assigned children", summary?.total],
            ["New requests", summary?.new],
            ...(admin ? [["Unassigned", summary?.unassigned]] : []),
            ["Consultations paid", summary?.paid],
          ] as [string, number | undefined][]
        ).map(([label, value]) => (
          <div key={label} className="px-4 py-3">
            <dt className="text-xs text-muted-foreground">{label}</dt>
            <dd className="mt-1 font-mono text-2xl tabular-nums">
              {value ?? "..."}
            </dd>
          </div>
        ))}
      </dl>
      <div className="flex flex-wrap items-center gap-2">
        <form
          className="flex min-w-60 flex-1 gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            setSearch(query.trim());
            setPage(1);
          }}
        >
          <Input
            aria-label="Search registrations"
            className="max-w-sm text-xs"
            placeholder="Search child, guardian, phone or email"
            maxLength={200}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
          <Button type="submit" variant="outline" size="sm">
            <Search size={14} />
            Search
          </Button>
        </form>
        <select
          aria-label="Consultation status"
          className={selectClass}
          value={status}
          onChange={(e) => {
            setStatus(e.target.value);
            setPage(1);
          }}
        >
          <option value="">All statuses</option>
          {statuses.map((v) => (
            <option key={v} value={v}>
              {title(v)}
            </option>
          ))}
        </select>
        <select
          aria-label="Payment status"
          className={selectClass}
          value={payment}
          onChange={(e) => {
            setPayment(e.target.value);
            setPage(1);
          }}
        >
          <option value="">All payments</option>
          {Object.entries(paymentLabels).map(([v, label]) => (
            <option key={v} value={v}>
              {label}
            </option>
          ))}
        </select>
        {admin && (
          <select
            aria-label="Assignment"
            className={selectClass}
            value={assignment}
            onChange={(e) => {
              setAssignment(e.target.value);
              setPage(1);
            }}
          >
            <option value="">All assignments</option>
            <option value="unassigned">Unassigned only</option>
          </select>
        )}
        <Button
          variant="outline"
          size="sm"
          disabled={loading}
          onClick={() => setRevision((v) => v + 1)}
        >
          <RefreshCw size={14} />
          Refresh
        </Button>
      </div>
      {error && (
        <p
          role="alert"
          className="rounded-md border border-destructive/30 p-3 text-sm text-destructive"
        >
          {error}
        </p>
      )}
      {staffError && (
        <p role="alert" className="text-sm text-destructive">
          Doctor list unavailable: {staffError}
        </p>
      )}
      <div className="rounded-md border" aria-busy={loading}>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Child</TableHead>
              <TableHead>Guardian / contact</TableHead>
              {admin && <TableHead>Doctor</TableHead>}
              <TableHead>Consultation</TableHead>
              <TableHead>Payment</TableHead>
              <TableHead>Received</TableHead>
              <TableHead>
                <span className="sr-only">Actions</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {loading ? (
              <TableRow>
                <TableCell
                  colSpan={7}
                  className="py-10 text-center text-muted-foreground"
                >
                  Loading registrations...
                </TableCell>
              </TableRow>
            ) : data?.items.length ? (
              data.items.map((row) => (
                <TableRow key={row.id}>
                  <TableCell className="max-w-52">
                    <p
                      className="truncate text-xs font-medium"
                      title={row.child_name}
                    >
                      {row.child_name}
                    </p>
                    <p className="mt-1 font-mono text-[11px] text-muted-foreground">
                      {row.date_of_birth}
                    </p>
                    <p className="mt-0.5 text-[11px] text-muted-foreground">
                      {ageFromBirth(row.date_of_birth)}
                    </p>
                  </TableCell>
                  <TableCell className="max-w-60">
                    <p className="truncate text-xs" title={row.guardian_name}>
                      {row.guardian_name}
                    </p>
                    <a
                      className="mt-1 block font-mono text-[11px] underline-offset-2 hover:underline"
                      href={`tel:${row.phone}`}
                    >
                      {row.phone}
                    </a>
                    <p
                      className="mt-0.5 truncate text-[11px] text-muted-foreground"
                      title={row.email}
                    >
                      {row.email || "Email not provided"}
                    </p>
                  </TableCell>
                  {admin && (
                    <TableCell
                      className="max-w-40 truncate text-xs"
                      title={row.doctor_name}
                    >
                      {row.doctor_name || (
                        <span className="text-muted-foreground">
                          Unassigned
                        </span>
                      )}
                    </TableCell>
                  )}
                  <TableCell>
                    <Badge variant="outline">{title(row.status)}</Badge>
                  </TableCell>
                  <TableCell>
                    <PaymentBadge value={row.payment_status} />
                    {row.amount_paise !== null && (
                      <p className="mt-1 font-mono text-[11px] text-muted-foreground">
                        {money(row.amount_paise, row.currency)}
                      </p>
                    )}
                  </TableCell>
                  <TableCell className="text-[11px] text-muted-foreground">
                    {new Date(row.created_at).toLocaleDateString("en-IN", {
                      timeZone: "Asia/Kolkata",
                      day: "numeric",
                      month: "short",
                      year: "numeric",
                    })}
                  </TableCell>
                  <TableCell>
                    <div className="flex gap-1">
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => setSelected(row)}
                      >
                        Open
                      </Button>
                      <Button size="sm" variant="outline" asChild>
                        <Link href={`/books?registration=${row.id}`}>
                          Books
                        </Link>
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell
                  colSpan={7}
                  className="py-12 text-center text-sm text-muted-foreground"
                >
                  {error
                    ? "Registrations could not be loaded. Try Refresh."
                    : search || status || payment || assignment
                      ? "No registrations match these filters."
                      : admin
                        ? "No consultation requests yet. New landing-page submissions will appear here."
                        : "No children are assigned to you yet. Your administrator can assign consultation requests here."}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
      {data && (
        <div className="flex items-center justify-between text-xs text-muted-foreground">
          <p>
            {data.total} matching{" "}
            {data.total === 1 ? "registration" : "registrations"} · Page{" "}
            {data.page}
          </p>
          <div className="flex gap-2">
            <Button
              size="sm"
              variant="outline"
              disabled={loading || page <= 1}
              onClick={() => setPage((v) => v - 1)}
            >
              Previous
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={loading || page * data.page_size >= data.total}
              onClick={() => setPage((v) => v + 1)}
            >
              Next
            </Button>
          </div>
        </div>
      )}
      <Dialog
        open={selected !== null}
        onOpenChange={(open) => {
          if (!open) setSelected(null);
        }}
      >
        <DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>Consultation record</DialogTitle>
            <DialogDescription>
              {admin
                ? "Manage contact details, assignment and consultation progress."
                : "Review this child's details and record consultation progress."}
            </DialogDescription>
          </DialogHeader>
          {selected && (
            <RegistrationEditor
              key={selected.id}
              registration={selected}
              doctors={doctors}
              assignmentAvailable={!staffError}
              admin={admin}
              onSaved={() => {
                setSelected(null);
                setRevision((v) => v + 1);
              }}
            />
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}

function RegistrationEditor({
  registration,
  doctors,
  assignmentAvailable,
  admin,
  onSaved,
}: {
  registration: Registration;
  doctors: StaffAccount[];
  assignmentAvailable: boolean;
  admin: boolean;
  onSaved: () => void;
}) {
  const [value, setValue] = useState(registration);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [ready, setReady] = useState(false);
  useEffect(() => {
    let active = true;
    getRegistration(registration.id)
      .then((row) => {
        if (active) {
          setValue(row);
          setReady(true);
        }
      })
      .catch((e) => {
        if (active) setError(errorMessage(e));
      });
    return () => {
      active = false;
    };
  }, [registration.id]);
  function field(
    key: "guardian_name" | "child_name" | "date_of_birth" | "phone" | "email",
  ) {
    return {
      value: value[key],
      disabled: !admin || busy || !ready,
      onChange: (e: React.ChangeEvent<HTMLInputElement>) =>
        setValue((v) => ({ ...v, [key]: e.target.value })),
    };
  }
  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await updateRegistration(value.id, {
        status: value.status,
        notes: value.notes,
        ...(admin
          ? {
              guardian_name: value.guardian_name,
              child_name: value.child_name,
              date_of_birth: value.date_of_birth,
              phone: value.phone,
              email: value.email,
              ...(assignmentAvailable
                ? { assigned_doctor_id: value.assigned_doctor_id || null }
                : {}),
            }
          : {}),
      });
      onSaved();
    } catch (e) {
      setError(errorMessage(e));
      setBusy(false);
    }
  }
  return (
    <form onSubmit={save} className="space-y-5">
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="space-y-2">
          <Label htmlFor="record-guardian">Guardian’s name</Label>
          <Input
            id="record-guardian"
            required
            maxLength={100}
            {...field("guardian_name")}
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor="record-child">Child’s name</Label>
          <Input
            id="record-child"
            required
            maxLength={100}
            {...field("child_name")}
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor="record-dob">Date of birth</Label>
          <Input
            id="record-dob"
            type="date"
            required
            min="1900-01-01"
            max={calendarDate()}
            {...field("date_of_birth")}
          />
          <p className="text-xs text-muted-foreground">
            Age: {ageFromBirth(value.date_of_birth) ?? "Invalid date"}
          </p>
        </div>
        <div className="space-y-2">
          <Label htmlFor="record-phone">Phone number</Label>
          <Input
            id="record-phone"
            type="tel"
            required
            maxLength={25}
            {...field("phone")}
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor="record-email">Email (optional)</Label>
          <Input
            id="record-email"
            type="email"
            maxLength={254}
            {...field("email")}
          />
        </div>
        {admin && (
          <div className="space-y-2">
            <Label htmlFor="record-doctor">Assigned doctor</Label>
            <select
              id="record-doctor"
              className={`${selectClass} w-full`}
              disabled={busy || !ready || !assignmentAvailable}
              value={value.assigned_doctor_id}
              onChange={(e) =>
                setValue((v) => ({ ...v, assigned_doctor_id: e.target.value }))
              }
            >
              <option value="">Unassigned</option>
              {doctors
                .filter((d) => d.active || d.id === value.assigned_doctor_id)
                .map((d) => (
                  <option key={d.id} value={d.id} disabled={!d.active}>
                    {d.name}
                    {d.active ? "" : " (inactive)"}
                  </option>
                ))}
            </select>
          </div>
        )}
        <div className="space-y-2">
          <Label htmlFor="record-status">Consultation status</Label>
          <select
            id="record-status"
            className={`${selectClass} w-full`}
            value={value.status}
            disabled={busy || !ready}
            onChange={(e) =>
              setValue((v) => ({
                ...v,
                status: e.target.value as ConsultationStatus,
              }))
            }
          >
            {statuses.map((status) => (
              <option key={status} value={status}>
                {title(status)}
              </option>
            ))}
          </select>
        </div>
      </div>
      <div className="space-y-2">
        <Label htmlFor="record-notes">Internal consultation notes</Label>
        <textarea
          id="record-notes"
          className="min-h-28 w-full rounded-md border bg-background p-3 text-sm"
          maxLength={5000}
          disabled={busy || !ready}
          value={value.notes}
          onChange={(e) => setValue((v) => ({ ...v, notes: e.target.value }))}
        />
      </div>
      <div className="space-y-2 rounded-md border bg-muted/30 p-3 text-xs">
        <div className="flex justify-between">
          <span>Payment</span>
          <PaymentBadge value={value.payment_status} />
        </div>
        <div className="flex justify-between">
          <span>Consultation amount</span>
          <span className="font-mono">
            {money(value.amount_paise, value.currency)}
          </span>
        </div>
        {value.refunded_paise > 0 && (
          <div className="flex justify-between">
            <span>Refunded</span>
            <span>{money(value.refunded_paise, value.currency)}</span>
          </div>
        )}
        {value.order_id && (
          <p className="break-all font-mono text-muted-foreground">
            Order: {value.order_id}
          </p>
        )}
        {value.payment_id && (
          <p className="break-all font-mono text-muted-foreground">
            Payment: {value.payment_id}
          </p>
        )}
        <p className="text-muted-foreground">
          Payment status is verified with Razorpay. Book generation is available
          regardless of payment.
        </p>
      </div>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <div className="flex justify-between gap-3">
        <Button asChild variant="outline">
          <Link href={`/books?registration=${value.id}`}>
            Open book generator
          </Link>
        </Button>
        <Button type="submit" disabled={busy || !ready}>
          {busy ? "Saving..." : "Save changes"}
        </Button>
      </div>
    </form>
  );
}
