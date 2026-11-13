"use client";
import { useEffect, useState } from "react";
import { decideRefund, getRefundReviews, syncRefundReview } from "@/lib/api";
import type { RefundCase, RefundReview } from "@/lib/portal-types";
import { errorMessage, money } from "@/lib/portal-utils";
import { parseRupees } from "@/lib/wallet";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Label } from "./ui/label";

const instant = (value: string) => new Date(value).toLocaleString("en-IN", {timeZone: "Asia/Kolkata"}) + " IST";

function RefundDecision({item, review, refresh}: {item: RefundCase; review: RefundReview; refresh: () => void}) {
  const [amount, setAmount] = useState("");
  const [explanation, setExplanation] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function submit(event: React.FormEvent) {
    event.preventDefault();
    if (busy) return;
    setError("");
    const adjustment = parseRupees(amount);
    if (amount.trim() === "" || adjustment === null || adjustment > review.remaining_earning_paise || !explanation.trim() || !confirmed) {
      setError("Choose a deduction from zero to the remaining earning, explain the decision and confirm it.");
      return;
    }
    setBusy(true);
    try {
      await decideRefund(item.id, {adjustment_paise: adjustment, expected_remaining_paise: review.remaining_earning_paise, explanation: explanation.trim(), confirmed});
      refresh();
    } catch (e) { setError(errorMessage(e)); }
    finally { setBusy(false); }
  }
  return <section className="space-y-3 rounded-md border p-3" aria-label={`Refund ${item.id}`}>
    <h4 className="break-all font-semibold">{item.id}</h4>
    <p className="text-sm">Refund: {money(item.amount_paise, "INR")} · {instant(item.provider_created_at)}</p>
    {item.adjustment_paise !== null ? <div className="space-y-1 text-sm" role="status">
      <p>Recorded deduction: <strong>{money(item.adjustment_paise, "INR")}</strong>. Earning before decision: {money(item.earning_before_paise, "INR")}.</p>
      <p>{item.explanation}</p>
      <p>Decided by {item.actor_name} · {item.decided_at && instant(item.decided_at)}</p>
      <p>This decision cannot be edited.</p>
    </div> : review.status === "refunded" ? <p className="text-sm">Fully refunded payment. Any remaining appointment earning has been reversed; no further deduction is needed.</p> :
    <form onSubmit={submit} className="space-y-3">
      <Label htmlFor={`refund-amount-${item.id}`}>Doctor earning deduction (INR)</Label>
      <Input id={`refund-amount-${item.id}`} inputMode="decimal" required value={amount} onChange={(e) => {setAmount(e.target.value); setConfirmed(false);}} />
      <p className="text-xs text-muted-foreground">Choose ₹0 to retain the earning, or up to {money(review.remaining_earning_paise, "INR")}. This records an earning adjustment only. It does not send a refund or recover money from a bank.</p>
      <Label htmlFor={`refund-reason-${item.id}`}>Reason for this decision</Label>
      <textarea id={`refund-reason-${item.id}`} required maxLength={500} value={explanation} onChange={(e) => {setExplanation(e.target.value); setConfirmed(false);}} className="min-h-20 w-full rounded-md border bg-background p-2 text-sm" />
      <label className="flex items-start gap-2 text-sm">
        <input type="checkbox" className="mt-1" required checked={confirmed} onChange={(e) => setConfirmed(e.target.checked)} />
        I confirm this deduction for this refund. The decision is permanent.
      </label>
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
      <Button disabled={busy || !confirmed || review.known_refunded_paise !== review.refunded_paise}>Record refund decision</Button>
    </form>}
  </section>;
}

function RefundOrder({review, refresh}: {review: RefundReview; refresh: () => void}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function sync() {
    setBusy(true); setError("");
    try { await syncRefundReview(review.order_id); refresh(); }
    catch (e) { setError(errorMessage(e)); }
    finally { setBusy(false); }
  }
  return <article className="space-y-4 rounded-lg border p-4">
    <div>
      <h3 className="font-semibold">{review.doctor_name}</h3>
      <p className="text-sm">{instant(review.starts_at)}</p>
      <p className="break-all text-xs text-muted-foreground">Appointment {review.appointment_id}<br />Payment {review.payment_id}</p>
    </div>
    <dl className="grid gap-2 text-sm sm:grid-cols-2">
      <div><dt>Patient paid</dt><dd>{money(review.payment_amount_paise, "INR")}</dd></div>
      <div><dt>Refunded to patient</dt><dd>{money(review.refunded_paise, "INR")}</dd></div>
      <div><dt>Original doctor earning</dt><dd>{money(review.original_earning_paise, "INR")}</dd></div>
      <div><dt>Previously reduced</dt><dd>{money(review.original_earning_paise - review.remaining_earning_paise, "INR")}</dd></div>
      <div><dt>Remaining appointment earning</dt><dd>{money(review.remaining_earning_paise, "INR")}</dd></div>
    </dl>
    {review.known_refunded_paise !== review.refunded_paise && <p role="status" className="text-sm text-destructive">Refund identities are incomplete. The wallet hold stays until current provider details are available and each partial refund is resolved.</p>}
    <Button variant="outline" disabled={busy} onClick={sync}>Refresh provider refund details</Button>
    {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
    {review.refunds.map((item) => <RefundDecision key={item.id + String(item.adjustment_paise) + review.remaining_earning_paise} item={item} review={review} refresh={refresh} />)}
  </article>;
}

export function RefundReviews({onResolved}: {onResolved: () => void}) {
  const [reviews, setReviews] = useState<RefundReview[]>([]);
  const [revision, setRevision] = useState(0);
  const [error, setError] = useState("");
  const [loaded, setLoaded] = useState(false);
  useEffect(() => {
    let active = true;
    getRefundReviews().then((items) => { if (active) {setReviews(items); setError("");} })
      .catch((e) => active && setError(errorMessage(e)))
      .finally(() => active && setLoaded(true));
    return () => {active = false;};
  }, [revision]);
  function refresh() {setRevision((value) => value + 1); onResolved();}
  return <section className="space-y-4 border-t pt-5" aria-label="Refund reconciliation">
    <h2 className="text-xl font-semibold">Refund reconciliation</h2>
    <p className="max-w-3xl text-sm text-muted-foreground">Review every partial refund separately. Other unresolved refunds, disputes, a negative balance or reservations exceeding the balance continue to block withdrawals and paid marks. Reject a pending withdrawal above to explicitly release its reservation. A negative balance does not recover an earlier bank transfer.</p>
    <Button variant="outline" onClick={refresh}>Refresh refund cases</Button>
    {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
    {!loaded ? <p>Loading refund cases...</p> : reviews.length ? <div className="grid items-start gap-4 xl:grid-cols-2">{reviews.map((review) => <RefundOrder key={review.order_id} review={review} refresh={refresh} />)}</div> : <p>No refunded earnings to review.</p>}
  </section>;
}
