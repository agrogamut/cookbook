"use client";

import { useEffect, useRef, useState } from "react";
import { Check, LockKeyhole } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  createCheckoutOrder,
  getPublicSettings,
  getRegistrationStatus,
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
import { loadCheckout } from "@/lib/checkout";
import type { PaymentStatus, PublicSettings } from "@/lib/portal-types";

type Receipt = { id: string; token: string; payment_status: PaymentStatus };
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

export function ConsultationForm() {
  const [form, setForm] = useState(emptyForm);
  const [settings, setSettings] = useState<PublicSettings | null>(null);
  const [receipt, setReceipt] = useState<Receipt | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const formToken = useRef("");
  const today = calendarDate();
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
              "Your registration is saved. You can complete payment whenever you are ready.",
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
    const paid =
      receipt.payment_status === "paid" ||
      receipt.payment_status === "partially_refunded";
    const canPay = ["unpaid", "pending", "failed"].includes(
      receipt.payment_status,
    );
    return (
      <section
        className="intake-panel"
        id="consultation"
        aria-labelledby="registered-title"
      >
        <div className="confirmation-symbol">
          <Check aria-hidden="true" />
        </div>
        <h2 id="registered-title">Your request is with us.</h2>
        <p className="intake-intro">
          MadamGY staff can now arrange your doctor consultation using the
          contact details you shared.
        </p>
        <div className="receipt-row">
          <span>Payment</span>
          <strong>{paymentLabels[receipt.payment_status]}</strong>
        </div>
        <p className="receipt-reference">Reference: {receipt.id}</p>
        {paid ? (
          <p className="confirmation-note">
            Payment received. Your consultation time will be arranged
            separately.
          </p>
        ) : canPay && settings?.checkout_available ? (
          <>
            <p className="payment-intro">
              You can pay for the consultation now, or discuss payment with the
              team.
            </p>
            <Button className="landing-button" disabled={busy} onClick={pay}>
              {busy
                ? "Opening checkout..."
                : `Pay ${money(settings.amount_paise, settings.currency)} for consultation`}
            </Button>
            <p className="field-hint">
              Secure checkout with Razorpay. Payment is optional.
            </p>
          </>
        ) : (
          <p className="payment-intro">
            {receipt.payment_status === "authorized"
              ? "Your payment is awaiting confirmation. Check its status shortly."
              : receipt.payment_status === "refunded"
                ? "This consultation payment has been refunded. Contact the team about any further payment."
                : "Online payment is not available right now. The team can discuss payment with you."}
          </p>
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
      className="intake-panel"
      id="consultation"
      aria-labelledby="intake-title"
    >
      <div className="intake-step">
        <span>1. Your details</span>
        <span>2. Optional payment</span>
      </div>
      <h2 id="intake-title">Let’s start with your child.</h2>
      <p className="intake-intro">
        Request a consultation. Our team will contact you to arrange the next
        step.
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
