"use client";

import { useEffect, useState } from "react";
import {
  createDoctor,
  getConsultationSettings,
  listStaff,
  resetDoctorPassword,
  saveConsultationSettings,
  updateDoctor,
} from "@/lib/api";
import { errorMessage, money } from "@/lib/portal-utils";
import type { ConsultationSettings, StaffAccount } from "@/lib/portal-types";
import { RegistrationWorkspace } from "./registration-workspace";
import { PageHeader } from "./page-header";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Label } from "./ui/label";
import { Badge } from "./ui/badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "./ui/tabs";
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
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "./ui/dialog";

export function AdminWorkspace() {
  return (
    <div>
      <PageHeader
        title="Administration"
        description="Manage consultation requests, doctor access and payment settings."
      />
      <Tabs defaultValue="registrations">
        <TabsList className="mb-5">
          <TabsTrigger value="registrations">Registrations</TabsTrigger>
          <TabsTrigger value="doctors">Doctors</TabsTrigger>
          <TabsTrigger value="fee">Consultation fee</TabsTrigger>
        </TabsList>
        <TabsContent value="registrations">
          <RegistrationWorkspace />
        </TabsContent>
        <TabsContent value="doctors">
          <DoctorManagement />
        </TabsContent>
        <TabsContent value="fee">
          <FeeSettings />
        </TabsContent>
      </Tabs>
    </div>
  );
}

function DoctorManagement() {
  const [staff, setStaff] = useState<StaffAccount[]>([]);
  const [loadedRevision, setLoadedRevision] = useState(-1);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [revision, setRevision] = useState(0);
  const [creating, setCreating] = useState(false);
  const [selected, setSelected] = useState<StaffAccount | null>(null);
  const loading = loadedRevision !== revision;
  useEffect(() => {
    let active = true;
    listStaff()
      .then((rows) => {
        if (active) {
          setStaff(rows);
          setError("");
        }
      })
      .catch((e) => {
        if (active) setError(errorMessage(e));
      })
      .finally(() => {
        if (active) setLoadedRevision(revision);
      });
    return () => {
      active = false;
    };
  }, [revision]);
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-base font-semibold">Doctor access</h2>
          <p className="mt-1 text-xs text-muted-foreground">
            Create accounts, reset passwords and deactivate access. Doctors only
            see assigned children.
          </p>
        </div>
        <div className="flex gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={loading}
            onClick={() => setRevision((v) => v + 1)}
          >
            Refresh
          </Button>
          <Button size="sm" onClick={() => setCreating(true)}>
            Add doctor
          </Button>
        </div>
      </div>
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
      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Email</TableHead>
              <TableHead>Role</TableHead>
              <TableHead>Access</TableHead>
              <TableHead>
                <span className="sr-only">Actions</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {loading ? (
              <TableRow>
                <TableCell
                  colSpan={5}
                  className="py-8 text-center text-muted-foreground"
                >
                  Loading staff...
                </TableCell>
              </TableRow>
            ) : (
              staff.map((account) => (
                <TableRow key={account.id}>
                  <TableCell className="text-xs font-medium">
                    {account.name}
                  </TableCell>
                  <TableCell className="text-xs">{account.email}</TableCell>
                  <TableCell className="text-xs capitalize">
                    {account.role}
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline">
                      {account.active ? "Active" : "Inactive"}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    {account.role === "doctor" && (
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => setSelected(account)}
                      >
                        Manage
                      </Button>
                    )}
                  </TableCell>
                </TableRow>
              ))
            )}
            {!loading && !error && staff.length === 0 && (
              <TableRow>
                <TableCell
                  colSpan={5}
                  className="py-8 text-center text-muted-foreground"
                >
                  No staff accounts found.
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
      <Dialog open={creating} onOpenChange={setCreating}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Add doctor</DialogTitle>
            <DialogDescription>
              Create a doctor login. Share its initial password with the doctor
              through your usual private channel.
            </DialogDescription>
          </DialogHeader>
          <NewDoctorForm
            onSaved={() => {
              setCreating(false);
              setRevision((v) => v + 1);
              setMessage(
                "Doctor account created. Assign consultation requests from Registrations.",
              );
            }}
          />
        </DialogContent>
      </Dialog>
      <Dialog
        open={selected !== null}
        onOpenChange={(open) => {
          if (!open) setSelected(null);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Manage doctor</DialogTitle>
            <DialogDescription>{selected?.email}</DialogDescription>
          </DialogHeader>
          {selected && (
            <DoctorEditor
              key={selected.id}
              doctor={selected}
              onSaved={(message) => {
                setSelected(null);
                setRevision((v) => v + 1);
                setMessage(message);
              }}
            />
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}

function NewDoctorForm({ onSaved }: { onSaved: () => void }) {
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await createDoctor(name, email, password);
      setPassword("");
      onSaved();
    } catch (e) {
      setError(errorMessage(e));
      setBusy(false);
    }
  }
  return (
    <form className="space-y-4" onSubmit={submit}>
      <fieldset disabled={busy} className="space-y-4">
        <div className="space-y-2">
          <Label htmlFor="doctor-name">Doctor’s name</Label>
          <Input
            id="doctor-name"
            required
            maxLength={100}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor="doctor-email">Email</Label>
          <Input
            id="doctor-email"
            type="email"
            required
            maxLength={254}
            autoComplete="off"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor="doctor-password">Initial password</Label>
          <Input
            id="doctor-password"
            type="password"
            required
            minLength={12}
            maxLength={256}
            autoComplete="new-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
          <p className="text-xs text-muted-foreground">
            At least 12 characters. The doctor can change it after signing in.
          </p>
        </div>
      </fieldset>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <Button type="submit" disabled={busy}>
        {busy ? "Creating..." : "Create doctor account"}
      </Button>
    </form>
  );
}

function DoctorEditor({
  doctor,
  onSaved,
}: {
  doctor: StaffAccount;
  onSaved: (message: string) => void;
}) {
  const [name, setName] = useState(doctor.name);
  const [active, setActive] = useState(doctor.active);
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await updateDoctor(doctor.id, name, active);
      onSaved(
        active
          ? "Doctor account updated."
          : "Doctor deactivated. Their sessions were revoked and children returned to the unassigned queue.",
      );
    } catch (e) {
      setError(errorMessage(e));
      setBusy(false);
    }
  }
  async function reset(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await resetDoctorPassword(doctor.id, password);
      setPassword("");
      onSaved("Doctor password reset. Existing sessions were revoked.");
    } catch (e) {
      setError(errorMessage(e));
      setBusy(false);
    }
  }
  return (
    <div className="space-y-6">
      <form className="space-y-4" onSubmit={save}>
        <div className="space-y-2">
          <Label htmlFor="edit-doctor-name">Doctor’s name</Label>
          <Input
            id="edit-doctor-name"
            required
            maxLength={100}
            value={name}
            onChange={(e) => setName(e.target.value)}
            disabled={busy}
          />
        </div>
        <Label className="flex items-center gap-2">
          <input
            type="checkbox"
            checked={active}
            onChange={(e) => setActive(e.target.checked)}
            disabled={busy}
          />
          Allow this doctor to sign in
        </Label>
        {!active && (
          <p className="text-xs leading-5 text-muted-foreground">
            Saving will end existing sessions and return assigned children to
            the unassigned queue.
          </p>
        )}
        <Button type="submit" disabled={busy}>
          Save access
        </Button>
      </form>
      <form className="space-y-4 border-t pt-5" onSubmit={reset}>
        <div className="space-y-2">
          <Label htmlFor="reset-doctor-password">Reset password</Label>
          <Input
            id="reset-doctor-password"
            type="password"
            autoComplete="new-password"
            minLength={12}
            maxLength={256}
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            disabled={busy}
          />
          <p className="text-xs text-muted-foreground">
            At least 12 characters. Resetting signs the doctor out of all
            sessions.
          </p>
        </div>
        <Button type="submit" variant="outline" disabled={busy}>
          Reset password
        </Button>
      </form>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
    </div>
  );
}

function FeeSettings() {
  const [settings, setSettings] = useState<ConsultationSettings | null>(null);
  const [amount, setAmount] = useState("");
  const [enabled, setEnabled] = useState(false);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    getConsultationSettings()
      .then((v) => {
        if (active) {
          setSettings(v);
          setAmount(
            v.amount_paise === null ? "" : (v.amount_paise / 100).toFixed(2),
          );
          setEnabled(v.payments_enabled);
        }
      })
      .catch((e) => {
        if (active) setError(errorMessage(e));
      });
    return () => {
      active = false;
    };
  }, []);
  async function save(e: React.FormEvent) {
    e.preventDefault();
    setMessage("");
    setError("");
    if (amount && !/^\d+(\.\d{1,2})?$/.test(amount)) {
      setError("Enter rupees with up to two decimal places.");
      return;
    }
    const [whole, fraction = ""] = amount.split(".");
    const paise = amount
      ? Number(whole) * 100 + Number(fraction.padEnd(2, "0"))
      : null;
    if (enabled && paise === null) {
      setError("Set a consultation fee before enabling payment.");
      return;
    }
    setBusy(true);
    try {
      setSettings(await saveConsultationSettings(paise, enabled));
      setMessage("Consultation settings saved.");
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <form className="max-w-xl space-y-5" onSubmit={save}>
      <div>
        <h2 className="text-base font-semibold">
          Optional consultation payment
        </h2>
        <p className="mt-2 text-sm leading-6 text-muted-foreground">
          Guardians can submit a request without payment. This fee applies to
          the doctor consultation. Doctors can generate books for paid and
          unpaid registrations.
        </p>
      </div>
      {settings && (
        <div className="space-y-2 rounded-md border p-4 text-xs">
          <p>
            Razorpay:{" "}
            <strong>
              {settings.gateway_configured
                ? "Credentials configured"
                : "Credentials not configured"}
            </strong>
          </p>
          <p>
            Online checkout:{" "}
            <strong>
              {settings.checkout_available ? "Available" : "Unavailable"}
            </strong>
          </p>
          <p>
            Current fee: <strong>{money(settings.amount_paise)}</strong>
          </p>
        </div>
      )}
      <div className="space-y-2">
        <Label htmlFor="consultation-fee">Consultation fee (INR)</Label>
        <Input
          id="consultation-fee"
          type="number"
          min={1}
          max={1000000}
          step="0.01"
          inputMode="decimal"
          placeholder="Set a fee"
          className="max-w-60"
          value={amount}
          disabled={!settings || busy}
          onChange={(e) => setAmount(e.target.value)}
        />
        <p className="text-xs text-muted-foreground">
          Leave blank to keep the fee unset. Existing payment orders retain
          their original fee.
        </p>
      </div>
      <Label className="flex items-center gap-2">
        <input
          type="checkbox"
          checked={enabled}
          disabled={!settings || busy}
          onChange={(e) => setEnabled(e.target.checked)}
        />
        Enable optional online payment
      </Label>
      {settings && !settings.gateway_configured && (
        <p className="text-xs leading-5 text-muted-foreground">
          Checkout stays unavailable until the Razorpay key and webhook secret
          are configured on the server.
        </p>
      )}
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
      <Button type="submit" disabled={!settings || busy}>
        {busy ? "Saving..." : "Save consultation settings"}
      </Button>
    </form>
  );
}
