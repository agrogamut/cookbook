"use client";

import { useEffect, useRef, useState } from "react";
import { Check, LockKeyhole } from "lucide-react";
import { AppointmentWizard, type BookingChoice } from "./appointment-wizard";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  cancelPublicAppointment,
  createPublicAppointment,
  createCheckoutOrder,
  getPublicSettings,
  getRegistrationStatus,
  listPublicAvailability,
  registerConsultation,
  verifyCheckout,
} from "@/lib/api";
import {
  ageFromBirth,
  calendarDate,
  errorMessage,
  money,
  newRegistrationToken,
  paymentLabels,
} from "@/lib/portal-utils";
import { useHoldClock } from "@/lib/use-hold-clock";
import { loadCheckout } from "@/lib/checkout";
import type {
  PublicRegistrationStatus,
  PublicSettings,
} from "@/lib/portal-types";

type Receipt = { id: string; token: string } & PublicRegistrationStatus;
const receiptKey = "madamgy.consultation.receipt";
const emptyForm = {
  guardian_name: "",
  child_name: "",
  date_of_birth: "",
  phone: "",
  email: "",
};

function forgetReceipt() {
  try {
    sessionStorage.removeItem(receiptKey);
  } catch {
    /* Browser storage can be disabled. */
  }
}

function displayInstant(value: string) {
  return new Intl.DateTimeFormat("en-IN", {
    timeZone: "Asia/Kolkata",
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}

export function ConsultationForm() {
  const today = calendarDate();
  const [form, setForm] = useState(emptyForm);
  const [settings, setSettings] = useState<PublicSettings | null>(null);
  const [receipt, setReceipt] = useState<Receipt | null>(null);
  const hold = useHoldClock(receipt?.hold_expires_at);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const formToken = useRef("");
  const age = ageFromBirth(form.date_of_birth, today);

  useEffect(() => {
    getPublicSettings()
      .then(setSettings)
      .catch(() => setSettings(null));
    try {
      const saved = JSON.parse(
        sessionStorage.getItem(receiptKey) ?? "null",
      ) as Receipt | null;
      if (saved?.id && saved?.token) {
        getRegistrationStatus(saved.id, saved.token)
          .then((status) => setReceipt({ ...saved, ...status }))
          .catch(forgetReceipt);
      }
    } catch {
      forgetReceipt();
    }
  }, []);

  useEffect(() => {
    if (!receipt?.id || !receipt?.token) return;
    const id = receipt.id,
      token = receipt.token;
    const timer = window.setInterval(() => {
      getRegistrationStatus(id, token)
        .then((status) => {
          setReceipt((current) =>
            current?.id === id ? { ...current, ...status } : current,
          );
        })
        .catch(() => {});
    }, 15000);
    return () => window.clearInterval(timer);
  }, [receipt?.id, receipt?.token]);

  function remember(next: Receipt) {
    setReceipt(next);
    try {
      sessionStorage.setItem(receiptKey, JSON.stringify(next));
    } catch {
      /* The current page can still finish checkout when storage is disabled. */
    }
  }

  async function refreshStatus() {
    if (!receipt) return;
    setBusy(true);
    setMessage("");
    try {
      remember({
        ...receipt,
        ...(await getRegistrationStatus(receipt.id, receipt.token)),
      });
    } catch (e) {
      setMessage(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  async function schedule(choice: BookingChoice) {
    if (!receipt || busy) return;
    setBusy(true);
    setMessage("");
    try {
      await createPublicAppointment(receipt.id, receipt.token, choice);
      remember({
        ...receipt,
        ...(await getRegistrationStatus(receipt.id, receipt.token)),
      });
    } catch (e) {
      setMessage(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  async function cancelAppointment() {
    if (!receipt?.appointment_id || busy) return;
    setBusy(true);
    setMessage("");
    try {
      await cancelPublicAppointment(
        receipt.id,
        receipt.appointment_id,
        receipt.token,
      );
      remember({
        ...receipt,
        ...(await getRegistrationStatus(receipt.id, receipt.token)),
      });
    } catch (e) {
      setMessage(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    if (!age) {
      setMessage("Enter a valid date of birth that is not in the future.");
      return;
    }
    setBusy(true);
    setMessage("");
    formToken.current ||= newRegistrationToken();
    try {
      const result = await registerConsultation({
        ...form,
        token: formToken.current,
      });
      remember({
        id: result.id,
        token: formToken.current,
        payment_status: "unpaid",
      });
    } catch (e) {
      setMessage(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  async function pay() {
    if (!receipt || busy) return;
    setBusy(true);
    setMessage("");
    try {
      const Checkout = await loadCheckout();
      const order = await createCheckoutOrder(receipt.id, receipt.token);
      const checkout = new Checkout({
        key: order.key_id,
        order_id: order.order_id,
        amount: order.amount_paise,
        currency: order.currency,
        name: "MadamGY",
        description: "Doctor consultation",
        theme: { color: "#c31352" },
        modal: {
          ondismiss: () => {
            setBusy(false);
            setMessage(
              "Your registration is saved. Complete payment before the hold expires, or choose another time.",
            );
          },
        },
        handler: async (result) => {
          setMessage("Confirming your payment...");
          try {
            remember({
              ...receipt,
              ...(await verifyCheckout(receipt.id, receipt.token, result)),
            });
            setMessage("");
          } catch (e) {
            setMessage(errorMessage(e));
          } finally {
            setBusy(false);
          }
        },
      });
      checkout.on("payment.failed", () => {
        setBusy(false);
        setMessage(
          "Payment was not completed. Your registration is saved; you can try again.",
        );
      });
      checkout.open();
    } catch (e) {
      setMessage(errorMessage(e));
      setBusy(false);
    }
  }

  function startAnother() {
    forgetReceipt();
    setReceipt(null);
    setForm(emptyForm);
    formToken.current = "";
    setMessage("");
  }

  const field = (name: keyof typeof form) => ({
    value: form[name],
    onChange: (event: React.ChangeEvent<HTMLInputElement>) =>
      setForm((current) => ({ ...current, [name]: event.target.value })),
  });

  if (receipt) {
    const paid = receipt.payment_status === "paid";
    const canPay =
      Boolean(receipt.appointment_id) &&
      ["awaiting_payment", "pending_admin"].includes(
        receipt.appointment_status ?? "",
      ) &&
      !hold.expired &&
      ["unpaid", "pending", "failed"].includes(receipt.payment_status);
    const canSchedule =
      !receipt.appointment_id ||
      ["rejected", "cancelled", "expired", "refunded"].includes(
        receipt.appointment_status ?? "",
      );
    return (
      <section
        className="intake-panel family-surface"
        id="consultation"
        aria-labelledby="registered-title"
      >
        <div className="confirmation-symbol">
          <Check aria-hidden="true" />
        </div>
        <h2 id="registered-title">Your request is with us.</h2>
        <p className="intake-intro">
          Choose your consultation time and pay. The team will then confirm your
          appointment.
        </p>
        <div className="receipt-row">
          <span>Payment</span>
          <strong>{paymentLabels[receipt.payment_status]}</strong>
        </div>
        <p className="receipt-reference">Reference: {receipt.id}</p>
        <p className="field-hint">
          Save this private token to open your child’s portal with their full
          name and date of birth:{" "}
          <code className="block break-all rounded-md border p-2 mt-2">
            {receipt.token}
          </code>
        </p>
        {receipt.appointment_id && (
          <div className="receipt-row">
            <span>Appointment</span>
            <strong>{receipt.appointment_status?.replaceAll("_", " ")}</strong>
          </div>
        )}
        {receipt.hold_expires_at &&
          receipt.appointment_status === "awaiting_payment" && (
            <p className="field-hint">
              {hold.expired
                ? "This hold has expired. Refresh to choose another time."
                : `Time remaining to pay: ${hold.label}. Held until ${displayInstant(receipt.hold_expires_at)} IST.`}
            </p>
          )}
        {receipt.doctor_name && (
          <p className="text-sm">
            {receipt.doctor_name}
            {receipt.appointment_starts_at &&
              `, ${displayInstant(receipt.appointment_starts_at)} IST`}
          </p>
        )}
        {settings?.amount_paise != null && (
          <p className="text-sm font-medium">
            Consultation fee: {money(settings.amount_paise, settings.currency)}
          </p>
        )}
        {canSchedule && (
          <div className="schedule-panel">
            <h3>Choose a consultation time</h3>
            <AppointmentWizard
              loadAvailability={listPublicAvailability}
              onBook={schedule}
              busy={busy}
            />
          </div>
        )}
        {receipt.appointment_id &&
        !paid &&
        canPay &&
        settings?.checkout_available ? (
          <>
            <p className="payment-intro">
              Your time is held for 15 minutes. Payment is required before the
              team can confirm this appointment.
            </p>
            <Button className="landing-button" disabled={busy} onClick={pay}>
              {busy
                ? "Opening checkout..."
                : `Pay ${money(settings.amount_paise, settings.currency)} for consultation`}
            </Button>
            <p className="field-hint">
              Secure checkout with Razorpay. Your booking is not confirmed until
              payment is captured and the team approves the time.
            </p>
          </>
        ) : receipt.appointment_status === "refund_required" ? (
          <p className="confirmation-note">
            This time is no longer booked. Your payment needs a refund. Contact
            the team with your reference.
          </p>
        ) : receipt.appointment_status === "confirmed" ? (
          <p className="confirmation-note">Your appointment is confirmed.</p>
        ) : paid ? (
          <p className="confirmation-note">
            Payment received. Your appointment is waiting for the team’s
            confirmation.
          </p>
        ) : receipt.appointment_status === "paid_pending_admin" ? (
          <p className="confirmation-note">
            Payment received. Your appointment is waiting for the team’s
            confirmation.
          </p>
        ) : receipt.appointment_id &&
          receipt.appointment_status === "awaiting_payment" ? (
          <p className="payment-intro">
            Complete payment before the hold expires to keep this time.
          </p>
        ) : (
          <p className="payment-intro">
            {receipt.payment_status === "authorized"
              ? "Your payment is awaiting confirmation. Check its status shortly."
              : receipt.payment_status === "refunded"
                ? "This consultation payment has been refunded. Contact the team about any further payment."
                : "Online payment is not available right now. The team can discuss payment with you."}
          </p>
        )}
        {receipt.appointment_id &&
          ["awaiting_payment", "pending_admin"].includes(
            receipt.appointment_status ?? "",
          ) && (
            <Button
              type="button"
              variant="outline"
              onClick={cancelAppointment}
              disabled={busy}
            >
              Release this time
            </Button>
          )}
        {message && (
          <p role="status" className="form-message">
            {message}
          </p>
        )}
        <div className="receipt-actions">
          <Button variant="outline" onClick={refreshStatus} disabled={busy}>
            Check payment status
          </Button>
          <Button variant="ghost" onClick={startAnother} disabled={busy}>
            Register another child
          </Button>
        </div>
      </section>
    );
  }

  return (
    <section
      className="intake-panel family-surface"
      id="consultation"
      aria-labelledby="intake-title"
    >
      <div className="intake-step">
        <span>1. Your details</span>
        <span>2. Schedule and payment</span>
      </div>
      <h2 id="intake-title">Let’s start with your child.</h2>
      <p className="intake-intro">
        Register your child, choose an available doctor and time, then pay
        securely.
      </p>
      <form onSubmit={submit}>
        <fieldset disabled={busy} className="intake-fields">
          <div>
            <Label htmlFor="guardian-name">Guardian’s name</Label>
            <Input
              id="guardian-name"
              autoComplete="name"
              required
              maxLength={100}
              placeholder="Your full name"
              {...field("guardian_name")}
            />
          </div>
          <div>
            <Label htmlFor="child-name">Child’s name</Label>
            <Input
              id="child-name"
              autoComplete="off"
              required
              maxLength={100}
              placeholder="Child’s full name"
              {...field("child_name")}
            />
          </div>
          <div>
            <Label htmlFor="child-dob">Child’s date of birth</Label>
            <Input
              id="child-dob"
              type="date"
              required
              min="1900-01-01"
              max={today}
              aria-describedby="child-age"
              {...field("date_of_birth")}
            />
            <p id="child-age" className="field-hint" aria-live="polite">
              {age
                ? `Age: ${age}`
                : "Your child’s age is calculated from this date."}
            </p>
          </div>
          <div>
            <Label htmlFor="guardian-phone">Phone number</Label>
            <Input
              id="guardian-phone"
              type="tel"
              autoComplete="tel"
              required
              maxLength={25}
              placeholder="+91"
              aria-describedby="phone-hint"
              {...field("phone")}
            />
            <p id="phone-hint" className="field-hint">
              Include your country code if outside India.
            </p>
          </div>
          <div>
            <Label htmlFor="guardian-email">
              Email <span className="optional-label">(optional)</span>
            </Label>
            <Input
              id="guardian-email"
              type="email"
              autoComplete="email"
              maxLength={254}
              placeholder="Your email address"
              {...field("email")}
            />
          </div>
        </fieldset>
        {message && (
          <p role="alert" className="form-message">
            {message}
          </p>
        )}
        <Button type="submit" className="landing-button" disabled={busy}>
          {busy ? "Saving your request..." : "Request a consultation"}
        </Button>
        <p className="intake-privacy">
          <LockKeyhole size={14} aria-hidden="true" />
          <span>
            MadamGY staff use these details to contact you about this
            consultation. No account is needed.
          </span>
        </p>
      </form>
    </section>
  );
}
