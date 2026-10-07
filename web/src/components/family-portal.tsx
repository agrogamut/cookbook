"use client";

import { useEffect, useState, useSyncExternalStore } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import Image from "next/image";
import { AppointmentWizard, type BookingChoice } from "./appointment-wizard";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { DateInput } from "@/components/date-input";
import { Label } from "@/components/ui/label";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog";
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
  familyChildAccess,
  familySignIn,
  familySignUp,
  familySignOut,
  listFamilyAvailability,
  listFamilyRegistrations,
  verifyFamilyCheckout,
} from "@/lib/api";
import { useHoldClock } from "@/lib/use-hold-clock";
import { loadCheckout } from "@/lib/checkout";
import {
  calendarDate,
  errorMessage,
  formatDayFirstDate,
  money,
  rememberedRegistrationToken,
  subscribeToRegistrationReceipt,
} from "@/lib/portal-utils";
import type { FamilyRegistration } from "@/lib/portal-types";

function displayInstant(value: string | null) {
  if (!value) return "not available";
  return new Intl.DateTimeFormat("en-IN", {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: "Asia/Kolkata",
  }).format(new Date(value));
}

function statusText(value: string | null | undefined) {
  return value ? value.replaceAll("_", " ") : "not available";
}

function useSavedRegistrationToken() {
  return useSyncExternalStore(subscribeToRegistrationReceipt, rememberedRegistrationToken, () => "");
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
  const hold = useHoldClock(registration.appointment_hold_expires_at);
  const approvedBooks = [
    ["Book 1", registration.book1_status, registration.book1_release_id],
    ["Book 2", registration.book2_status, registration.book2_release_id],
  ] as const;
  return (
    <Card data-testid={`family-registration-${registration.id}`}>
      <CardHeader>
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <CardTitle className="text-lg text-[#58293b]">
              {registration.child_name}
            </CardTitle>
            <CardDescription>
              Date of birth: {formatDayFirstDate(registration.date_of_birth)}
            </CardDescription>
          </div>
          <Badge variant="outline">{registration.registration_status}</Badge>
        </div>
      </CardHeader>
      <CardContent className="space-y-5">
        <dl className="grid gap-3 text-sm sm:grid-cols-3">
          <div>
            <dt className="text-[#815b69]">Appointment</dt>
            <dd className="mt-1 font-medium">
              {statusText(registration.appointment_status)}
            </dd>
          </div>
          <div>
            <dt className="text-[#815b69]">Payment</dt>
            <dd className="mt-1 font-medium">{registration.payment_status}</dd>
          </div>
          <div>
            <dt className="text-[#815b69]">Doctor</dt>
            <dd className="mt-1 font-medium">
              {registration.doctor_name || "not available"}
            </dd>
          </div>
        </dl>
        {registration.appointment_status && (
          <div className="rounded-md bg-[#fff0f4] px-3 py-2 text-sm text-[#58293b]">
            {registration.appointment_status}:{" "}
            {displayInstant(registration.appointment_starts_at)} to{" "}
            {displayInstant(registration.appointment_ends_at)}
            {registration.appointment_status === "awaiting_payment" &&
              registration.appointment_hold_expires_at && (
                <span className="ml-2">
                  {hold.expired
                    ? "Hold expired. Refresh to choose another time."
                    : `Time remaining to pay: ${hold.label}`}
                </span>
              )}
            {[
              "awaiting_payment",
              "pending_admin",
              "paid_pending_admin",
            ].includes(registration.appointment_status) &&
              registration.appointment_id && (
                <Button
                  className="ml-3 h-7"
                  variant="outline"
                  size="sm"
                  onClick={() => onCancel(registration.appointment_id!)}
                >
                  Cancel request
                </Button>
              )}
          </div>
        )}
        <div className="flex flex-wrap gap-2">
          {["unpaid", "pending", "failed"].includes(
            registration.payment_status,
          ) &&
            ["awaiting_payment", "pending_admin"].includes(
              registration.appointment_status ?? "",
            ) &&
            !hold.expired &&
            registration.amount_paise !== null && (
              <Button
                className="bg-[#c31352] hover:bg-[#a90f46]"
                onClick={() => onPay(registration)}
              >
                Pay {money(registration.amount_paise, registration.currency)}
              </Button>
            )}
          {(!registration.appointment_status ||
            ["rejected", "cancelled", "expired", "refunded"].includes(
              registration.appointment_status,
            )) && (
            <Button variant="outline" onClick={() => onBook(registration)}>
              Book a consultation
            </Button>
          )}
        </div>
        {registration.appointment_status === "refund_required" && (
          <p className="text-sm" role="status">
            Your appointment is no longer booked. Contact the team about the
            refund using reference {registration.id}.
          </p>
        )}
        <div className="border-t pt-4">
          <p className="mb-2 text-xs font-medium uppercase tracking-wide text-[#815b69]">
            Released books
          </p>
          <div className="grid gap-2 sm:grid-cols-2">
            {approvedBooks.map(([label, status, releaseID]) => (
              <div
                key={label}
                className="flex items-center justify-between rounded-md border px-3 py-2 text-sm"
              >
                <span>
                  {label}: {statusText(status)}
                </span>
                {status === "approved" && releaseID && (
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => onDownload(releaseID)}
                  >
                    Download
                  </Button>
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
  const [scoped, setScoped] = useState(true);
  const [childName, setChildName] = useState("");
  const [dob, setDob] = useState("");
  const [phone, setPhone] = useState("");
  const savedToken = useSavedRegistrationToken();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");

  async function refresh() {
    try {
      const account = await familyMe();
      setScoped(Boolean(account.registration_id));
      setRegistrations(await listFamilyRegistrations());
    } catch (e) {
      if (
        e instanceof Error &&
        "status" in e &&
        (e as { status?: number }).status === 401
      ) {
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
        const account = await familyMe();
        if (active) setScoped(Boolean(account.registration_id));
        const value = await listFamilyRegistrations();
        if (active) setRegistrations(value);
      } catch (e) {
        if (!active) return;
        if (
          e instanceof Error &&
          "status" in e &&
          (e as { status?: number }).status === 401
        ) {
          router.replace("/family/login");
        } else {
          setError(errorMessage(e));
        }
      }
    })();
    return () => {
      active = false;
    };
  }, [router]);
  useEffect(() => {
    const timer = window.setInterval(() => {
      listFamilyRegistrations()
        .then(setRegistrations)
        .catch(() => {});
    }, 15000);
    return () => window.clearInterval(timer);
  }, []);

  function openBooking(registration: FamilyRegistration) {
    setSelected(registration);
    setMessage("");
    setError("");
  }
  async function submitBooking(choice: BookingChoice) {
    if (!selected || busy) return;
    setBusy(true);
    setError("");
    setMessage("");
    try {
      await createFamilyAppointment({
        registration_id: selected.id,
        ...choice,
      });
      setMessage("Time held. Complete payment so the team can confirm it.");
      setSelected(null);
      await refresh();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  async function pay(registration: FamilyRegistration) {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const order = await createFamilyOrder(registration.id);
      const Razorpay = await loadCheckout();
      const checkout = new Razorpay({
        key: order.key_id,
        order_id: order.order_id,
        amount: order.amount_paise,
        currency: order.currency,
        name: "MadamGY",
        description: `Consultation for ${registration.child_name}`,
        handler: async (result) => {
          try {
            await verifyFamilyCheckout(registration.id, result);
            await refresh();
            setMessage(
              "Payment checked. Your appointment status is shown below.",
            );
          } catch (e) {
            setError(errorMessage(e));
          } finally {
            setBusy(false);
          }
        },
        modal: { ondismiss: () => setBusy(false) },
        theme: { color: "#c31352" },
      });
      checkout.on("payment.failed", () => {
        setError("Payment failed. The registration remains saved.");
        setBusy(false);
      });
      checkout.open();
    } catch (e) {
      setError(errorMessage(e));
      setBusy(false);
    }
  }
  async function download(releaseID: string) {
    setBusy(true);
    setError("");
    try {
      const blob = await downloadFamilyBook(releaseID);
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = "madamgy-book.pdf";
      link.click();
      URL.revokeObjectURL(url);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  async function createRegistration(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await createFamilyRegistration({
        child_name: childName,
        date_of_birth: dob,
        phone,
      });
      setChildName("");
      setDob("");
      setPhone("");
      await refresh();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  async function attach(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await attachFamilyRegistration(savedToken);
      await refresh();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  async function cancel(id: string) {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      await cancelFamilyAppointment(id);
      await refresh();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="family-surface min-h-screen bg-[#fff0f4] px-4 py-8 text-[#58293b] sm:px-8">
      <div className="mx-auto max-w-5xl space-y-6">
        <header className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <p className="text-sm text-[#815b69]">MadamGY family portal</p>
            <h1 className="text-3xl font-semibold">Your consultations</h1>
          </div>
          <div className="flex gap-2">
            <Button variant="outline" onClick={() => router.push("/")}>
              Back to MadamGY
            </Button>
            <Button
              variant="ghost"
              onClick={async () => {
                await familySignOut();
                router.replace("/family/login");
              }}
            >
              Sign out
            </Button>
          </div>
        </header>
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
        {scoped && (
          <p className="mb-4 text-sm">
            <Link href="/family/register" className="underline">Create a family account</Link>{" "}
            to return from another browser or device.
          </p>
        )}
        {!scoped && (
          <section className="grid gap-4 md:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle>Add a registration</CardTitle>
                <CardDescription>
                  Create a registration owned by this account.
                </CardDescription>
              </CardHeader>
              <CardContent>
                <form className="space-y-3" onSubmit={createRegistration}>
                  <Label htmlFor="family-child">Child name</Label>
                  <Input
                    id="family-child"
                    required
                    value={childName}
                    onChange={(e) => setChildName(e.target.value)}
                  />
                  <Label htmlFor="family-dob">Date of birth</Label>
                  <DateInput
                    id="family-dob"
                    required
                    min="1900-01-01"
                    max={calendarDate()}
                    value={dob}
                    onValueChange={setDob}
                  />
                  <Label htmlFor="family-phone">Phone</Label>
                  <Input
                    id="family-phone"
                    required
                    value={phone}
                    onChange={(e) => setPhone(e.target.value)}
                  />
                  <Button
                    disabled={busy}
                    className="bg-[#c31352] hover:bg-[#a90f46]"
                  >
                    Save registration
                  </Button>
                </form>
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle>Attach an existing request</CardTitle>
                <CardDescription>
                  {savedToken
                    ? "Attach the registration saved in this browser to your family account."
                    : "To link an earlier registration from another browser, contact the MadamGY team."}
                </CardDescription>
              </CardHeader>
              <CardContent>
                <form className="space-y-3" onSubmit={attach}>
                  <Button disabled={busy || !savedToken} variant="outline">
                    Attach saved request
                  </Button>
                </form>
              </CardContent>
            </Card>
          </section>
        )}
        <section className="space-y-4">
          <h2 className="text-xl font-semibold">Your registrations</h2>
          {registrations.length === 0 ? (
            <Card>
              <CardContent className="py-8 text-sm text-[#815b69]">
                not available
              </CardContent>
            </Card>
          ) : (
            registrations.map((item) => (
              <FamilyRegistrationCard
                key={item.id}
                registration={item}
                onPay={pay}
                onBook={openBooking}
                onDownload={download}
                onCancel={cancel}
              />
            ))
          )}
        </section>
      </div>
      <Dialog open={Boolean(selected)} onOpenChange={(open) => { if (!open) setSelected(null); }}>
        <DialogContent className="family-surface max-h-[90vh] overflow-y-auto sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>Book a consultation</DialogTitle>
            <DialogDescription>Choose a time for {selected?.child_name}. Payment is required before the team can confirm it.</DialogDescription>
          </DialogHeader>
          {selected && <AppointmentWizard loadAvailability={listFamilyAvailability} onBook={submitBooking} busy={busy} />}
        </DialogContent>
      </Dialog>
    </main>
  );
}

export function FamilyAuth({ mode }: { mode: "login" | "register" }) {
  const router = useRouter();
  const savedToken = useSavedRegistrationToken();
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [childName, setChildName] = useState("");
  const [dob, setDob] = useState("");
  const [accountLogin, setAccountLogin] = useState(false);
  const [accountCreated, setAccountCreated] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const childAccess = mode === "login" && Boolean(savedToken) && !accountLogin;
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      if (childAccess)
        await familyChildAccess(childName, dob, savedToken);
      else if (mode === "login") await familySignIn(email, password);
      else {
        if (!accountCreated) {
          await familySignUp(name, email, password);
          setAccountCreated(true);
        }
        if (savedToken) await attachFamilyRegistration(savedToken);
      }
      router.replace("/family");
    } catch (e) {
      setError(errorMessage(e));
      setBusy(false);
    }
  }
  return (
    <main className="family-surface min-h-screen bg-[#fff7f9] px-4 py-10 text-[#58293b]">
      <div className="mx-auto w-full max-w-md">
        <div className="mb-5 text-center">
          <Link href="/" className="mb-4 inline-block">
            <Image
              src="/madamgy-logo.png"
              width={185}
              height={37}
              alt="MadamGY"
            />
          </Link>
          <p className="text-sm font-medium uppercase tracking-[0.18em] text-[#a33b5f]">
            MadamGY family care
          </p>
        </div>
        <Card className="border-[#ead0d9] bg-white shadow-[0_20px_60px_rgba(88,41,59,0.12)]">
          <CardHeader>
            <CardTitle className="text-[#58293b]">
              {childAccess
                ? "Open your child’s portal"
                : mode === "login"
                  ? "Family account sign in"
                  : "Create a family account"}
            </CardTitle>
            <CardDescription>
              {childAccess
                ? "Use the child’s full name and date of birth. This browser remembers your registration."
                : mode === "login"
                  ? "Sign in with your family account to manage consultations and approved files."
                  : "Save your registration to an account and return from any browser or device."}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <form className="space-y-4" onSubmit={submit}>
              {childAccess ? (
                <>
                  <div>
                    <Label htmlFor="family-child-name">Child’s full name</Label>
                    <Input
                      id="family-child-name"
                      autoComplete="name"
                      required
                      value={childName}
                      onChange={(e) => setChildName(e.target.value)}
                    />
                  </div>
                  <div>
                    <Label htmlFor="family-child-dob">
                      Child’s date of birth
                    </Label>
                    <DateInput
                      id="family-child-dob"
                      required
                      min="1900-01-01"
                      max={calendarDate()}
                      value={dob}
                      onValueChange={setDob}
                    />
                  </div>
                </>
              ) : (
                <>
                  {mode === "register" && (
                    <div>
                      <Label htmlFor="family-name">Your name</Label>
                      <Input
                        id="family-name"
                        required
                        disabled={accountCreated}
                        value={name}
                        onChange={(e) => setName(e.target.value)}
                      />
                    </div>
                  )}
                  <div>
                    <Label htmlFor="family-email">Email</Label>
                    <Input
                      id="family-email"
                      type="email"
                      required
                      disabled={accountCreated}
                      value={email}
                      onChange={(e) => setEmail(e.target.value)}
                    />
                  </div>
                  <div>
                    <Label htmlFor="family-password">Password</Label>
                    <Input
                      id="family-password"
                      type="password"
                      minLength={12}
                      maxLength={256}
                      required
                      disabled={accountCreated}
                      value={password}
                      onChange={(e) => setPassword(e.target.value)}
                    />
                  </div>
                </>
              )}
              {error && (
                <p role="alert" className="text-sm text-destructive">
                  {error}
                </p>
              )}
              <Button
                disabled={busy}
                className="w-full bg-[#c31352] hover:bg-[#a90f46]"
              >
                {busy
                  ? "Checking details..."
                  : childAccess
                    ? "Open family portal"
                    : mode === "login"
                      ? "Sign in"
                      : accountCreated
                        ? "Retry linking registration"
                        : "Create account"}
              </Button>
            </form>
            {accountCreated && error && (
              <p className="mt-4 text-sm">
                Your account was created. Retry linking your saved registration, or{" "}
                <Link href="/family" className="underline">continue to your account</Link>.
              </p>
            )}
            {mode === "login" && savedToken && (
              <button
                type="button"
                className="mt-4 w-full text-sm text-[#a33b5f] underline"
                onClick={() => setAccountLogin((value) => !value)}
              >
                {accountLogin
                  ? "Use child details"
                  : "Use email and password instead"}
              </button>
            )}
            <p className="mt-5 text-center text-sm text-[#815b69]">
              {mode === "login" ? (
                <>
                  Need an account?{" "}
                  <a className="underline" href="/family/register">
                    Create one
                  </a>
                </>
              ) : (
                <>
                  Already registered?{" "}
                  <a className="underline" href="/family/login">
                    Sign in
                  </a>
                </>
              )}
            </p>
          </CardContent>
        </Card>
        <p className="mt-5 text-center text-xs text-[#815b69]">
          <Link className="underline" href="/">
            Return to MadamGY
          </Link>
        </p>
      </div>
    </main>
  );
}
