"use client";
import { useEffect, useState } from "react";
import {
  getConsultationSettings,
  saveDoctorEarning,
  getWallets,
  getWithdrawals,
  revealWithdrawalDestination,
  decideWithdrawal,
} from "@/lib/api";
import type {
  ConsultationSettings,
  PayoutDestination,
  WalletBalance,
  Withdrawal,
} from "@/lib/portal-types";
import { errorMessage, indiaInstant, money } from "@/lib/portal-utils";
import { parseRupees } from "@/lib/wallet";
import { RefundReviews } from "./refund-review";
import { WithdrawalSummary } from "./doctor-wallet";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Label } from "./ui/label";

export function EarningSettings() {
  const [settings, setSettings] = useState<ConsultationSettings | null>(null);
  const [amount, setAmount] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  useEffect(() => {
    let active = true;
    getConsultationSettings()
      .then((v) => {
        if (active) {
          setSettings(v);
          setAmount(
            v.doctor_earning_paise == null
              ? ""
              : (v.doctor_earning_paise / 100).toFixed(2),
          );
        }
      })
      .catch((e) => active && setError(errorMessage(e)));
    return () => {
      active = false;
    };
  }, []);
  async function save(e: React.FormEvent) {
    e.preventDefault();
    if (busy) return;
    const value = amount.trim() === "" ? null : parseRupees(amount);
    if (value === null && amount.trim() !== "") {
      setError("Enter an amount with at most two decimal places.");
      return;
    }
    setBusy(true);
    setError("");
    setMessage("");
    try {
      setSettings(await saveDoctorEarning(value));
      setMessage("Saved. This applies to newly created appointments only.");
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="mt-6 max-w-xl space-y-3 rounded-md border p-4">
      <h2 className="text-lg font-semibold">
        Doctor earnings per consultation
      </h2>
      <p className="text-sm text-muted-foreground">
        A fixed amount is recorded on each new appointment and credited once
        when payment is verified as captured. Changing it does not rewrite
        existing appointments or add historical earnings.
      </p>
      <form onSubmit={save} className="space-y-3">
        <Label htmlFor="doctor-earning">Fixed doctor amount (INR)</Label>
        <Input
          id="doctor-earning"
          inputMode="decimal"
          value={amount}
          onChange={(e) => setAmount(e.target.value)}
          placeholder="Leave blank to disable new earnings"
        />
        <Button disabled={busy || !settings}>Save doctor amount</Button>
      </form>
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
      {settings && (
        <div className="space-y-2 border-t pt-3 text-sm">
          <p>
            Secure payout storage:{" "}
            {settings.wallet_configured ? "configured" : "not configured"}
          </p>
          <p>
            Patient tax:{" "}
            {settings.patient_tax_active
              ? "owner-selected 18% active"
              : "inactive"}
            . Withdrawal deduction:{" "}
            {settings.withdrawal_deduction_active
              ? "owner-selected 18% active"
              : "inactive"}
            .
          </p>
          <p className="text-xs text-muted-foreground">
            Active-policy example: ₹2,000 requested, ₹360 deduction,
            final withdrawable amount ₹1,640. This is the owner-selected
            calculation. Legal tax treatment requires separate verification.
            Existing requests keep their recorded policy. This screen cannot
            change the deployment policy.
          </p>
        </div>
      )}
    </section>
  );
}
function WithdrawalReview({
  item,
  refresh,
}: {
  item: Withdrawal;
  refresh: () => void;
}) {
  const [destination, setDestination] = useState<PayoutDestination | null>(
    null,
  );
  const [amount, setAmount] = useState("");
  const [reference, setReference] = useState("");
  const [instant, setInstant] = useState("");
  const [note, setNote] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const pending = item.status === "requested" || item.status === "processing";
  async function reveal() {
    if (
      busy ||
      !window.confirm(
        "Reveal private payout details to review this manual transfer? This access is recorded.",
      )
    )
      return;
    setBusy(true);
    setError("");
    try {
      setDestination(await revealWithdrawalDestination(item.id));
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  async function decide(action: "paid" | "processing" | "rejected") {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      if (action === "paid") {
        const paise = parseRupees(amount);
        if (
          paise !== item.net_paise ||
          !confirmed ||
          !instant ||
          !reference.trim()
        )
          throw new Error(
            "Confirm the transfer is already complete. Enter the actual final amount, reference and transfer time.",
          );
        await decideWithdrawal(item.id, {
          action,
          transferred_paise: paise,
          transfer_reference: reference,
          transferred_at: indiaInstant(
            instant.slice(0, 10),
            instant.slice(11, 16),
          ),
          transfer_completed: confirmed,
          note,
        });
      } else {
        await decideWithdrawal(item.id, { action, note });
      }
      setDestination(null);
      setReference("");
      setAmount("");
      setConfirmed(false);
      refresh();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <article className="space-y-3 rounded-lg border p-4">
      <h3 className="font-semibold">{item.doctor_name}</h3>
      <WithdrawalSummary item={item} />
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      {pending && (
        <>
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" disabled={busy} onClick={reveal}>
              Review payout destination
            </Button>
            {destination && (
              <Button variant="outline" onClick={() => setDestination(null)}>
                Hide private details
              </Button>
            )}
          </div>
          {destination && (
            <dl className="space-y-1 rounded-md border p-3 text-sm">
              <div>
                <dt className="font-semibold">Account holder</dt>
                <dd>{destination.holder}</dd>
              </div>
              {destination.kind === "bank" ? (
                <>
                  <div>
                    <dt className="font-semibold">Bank</dt>
                    <dd>{destination.bank}</dd>
                  </div>
                  <div>
                    <dt className="font-semibold">Account number</dt>
                    <dd className="break-all font-mono">
                      {destination.account}
                    </dd>
                  </div>
                  <div>
                    <dt className="font-semibold">IFSC</dt>
                    <dd>{destination.ifsc}</dd>
                  </div>
                </>
              ) : (
                <div>
                  <dt className="font-semibold">UPI ID</dt>
                  <dd className="break-all">{destination.upi}</dd>
                </div>
              )}
            </dl>
          )}
          <Label htmlFor={`note-${item.id}`}>Review note</Label>
          <Input
            id={`note-${item.id}`}
            maxLength={500}
            value={note}
            onChange={(e) => setNote(e.target.value)}
          />
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              disabled={busy || item.status === "processing"}
              onClick={() => decide("processing")}
            >
              Mark processing
            </Button>
            <Button
              variant="outline"
              disabled={busy}
              onClick={() => decide("rejected")}
            >
              Reject and release reservation
            </Button>
          </div>
          {item.policy_pending ? (
            <p role="status" className="rounded-md border p-3 text-sm">
              Final payout pending policy review. Do not transfer or mark paid.
              Reject this reservation and request a fresh withdrawal once the
              selected policy is active.
            </p>
          ) : (
            <details className="border-t pt-3">
              <summary className="cursor-pointer font-semibold">
                Record completed manual transfer
              </summary>
              <form
                className="mt-3 space-y-3"
                onSubmit={(e) => {
                  e.preventDefault();
                  void decide("paid");
                }}
              >
                <p className="text-sm text-muted-foreground">
                  Transfer the final amount outside this app first. This check
                  mark only records a transfer already completed. Estimated
                  processing: 2 to 4 business days.
                </p>
                <Label htmlFor={`paid-amount-${item.id}`}>
                  Actual transferred amount (INR)
                </Label>
                <Input
                  id={`paid-amount-${item.id}`}
                  inputMode="decimal"
                  required
                  value={amount}
                  onChange={(e) => setAmount(e.target.value)}
                />
                <Label htmlFor={`paid-reference-${item.id}`}>
                  Transfer reference
                </Label>
                <Input
                  id={`paid-reference-${item.id}`}
                  required
                  maxLength={120}
                  value={reference}
                  onChange={(e) => setReference(e.target.value)}
                />
                <Label htmlFor={`paid-at-${item.id}`}>
                  Actual transfer time (IST)
                </Label>
                <Input
                  id={`paid-at-${item.id}`}
                  type="datetime-local"
                  required
                  value={instant}
                  onChange={(e) => setInstant(e.target.value)}
                />
                <label className="flex items-start gap-2 text-sm">
                  <input
                    type="checkbox"
                    required
                    checked={confirmed}
                    onChange={(e) => setConfirmed(e.target.checked)}
                    className="mt-1"
                  />
                  I confirm the transfer is complete and these details match the
                  bank or UPI receipt.
                </label>
                <Button type="submit" disabled={busy || !confirmed}>
                  Mark paid
                </Button>
              </form>
            </details>
          )}
        </>
      )}
    </article>
  );
}
export function AdminWallet() {
  const [wallets, setWallets] = useState<WalletBalance[]>([]);
  const [withdrawals, setWithdrawals] = useState<Withdrawal[]>([]);
  const [revision, setRevision] = useState(0);
  const [error, setError] = useState("");
  const [loaded, setLoaded] = useState(false);
  useEffect(() => {
    let active = true;
    Promise.all([getWallets(), getWithdrawals()])
      .then(([w, r]) => {
        if (active) {
          setWallets(w);
          setWithdrawals(r);
          setError("");
        }
      })
      .catch((e) => active && setError(errorMessage(e)))
      .finally(() => active && setLoaded(true));
    return () => {
      active = false;
    };
  }, [revision]);
  return (
    <section className="space-y-5">
      <div className="flex flex-wrap justify-between gap-3">
        <div>
          <h2 className="text-xl font-semibold">Doctor withdrawals</h2>
          <p className="mt-1 text-sm text-muted-foreground">
            Review requests, transfer manually, then record the completed
            payment. No automatic transfers.
          </p>
        </div>
        <Button variant="outline" onClick={() => setRevision((v) => v + 1)}>
          Refresh withdrawals
        </Button>
      </div>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {wallets.map((w) => (
          <div
            className="space-y-1 rounded-md border p-3 text-sm"
            key={w.doctor_id}
          >
            <p className="font-semibold">{w.doctor_name}</p>
            <p>Available: {money(w.available_paise, "INR")}</p>
            <p>Reserved: {money(w.reserved_paise, "INR")}</p>
            {w.reconciliation_required && (
              <p className="font-medium text-destructive">
                Reconciliation required. Pause transfers and review refunds or
                disputes.
              </p>
            )}
          </div>
        ))}
      </div>
      {!loaded ? (
        <p>Loading withdrawals...</p>
      ) : withdrawals.length ? (
        <div className="grid items-start gap-4 lg:grid-cols-2">
          {withdrawals.map((item) => (
            <WithdrawalReview
              key={item.id + item.status}
              item={item}
              refresh={() => setRevision((v) => v + 1)}
            />
          ))}
        </div>
      ) : (
        <p>No withdrawal requests yet.</p>
      )}
      <RefundReviews onResolved={() => setRevision((v) => v + 1)} />
    </section>
  );
}
