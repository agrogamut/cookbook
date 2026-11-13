"use client";
import { useEffect, useRef, useState } from "react";
import {
  cancelWithdrawal,
  getDoctorWallet,
  requestWithdrawal,
  savePayoutDestination,
} from "@/lib/api";
import type {
  DoctorWalletData,
  PayoutDestination,
  Withdrawal,
} from "@/lib/portal-types";
import { errorMessage, money } from "@/lib/portal-utils";
import { parseRupees, withdrawalAmounts } from "@/lib/wallet";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Label } from "./ui/label";

const emptyDestination: PayoutDestination = {
  kind: "bank",
  holder: "",
  bank: "",
  account: "",
  confirm_account: "",
  ifsc: "",
  upi: "",
};
export function WithdrawalAmounts({
  amount,
  bps,
  pending = false,
}: {
  amount: number;
  bps: number;
  pending?: boolean;
}) {
  const v = withdrawalAmounts(amount, bps);
  return (
    <dl className="space-y-2 rounded-md bg-muted/50 p-3 text-sm">
      <div className="flex justify-between gap-4">
        <dt>Requested amount</dt>
        <dd>{money(v.requested, "INR")}</dd>
      </div>
      <div className="flex justify-between gap-4">
        <dt>
          {pending
            ? "Requested deduction estimate (18%)"
            : bps
              ? `Approved deduction (${bps / 100}%)`
              : "Deduction (inactive)"}
        </dt>
        <dd>{money(v.deduction, "INR")}</dd>
      </div>
      <div className="flex justify-between gap-4 border-t pt-2 font-semibold">
        <dt>Final withdrawable amount</dt>
        <dd>{pending ? "Pending policy review" : money(v.net, "INR")}</dd>
      </div>
      {pending && (
        <div>
          <dt>Requested policy estimate after deduction</dt>
          <dd>{money(v.net, "INR")}, not yet approved for payment.</dd>
        </div>
      )}
    </dl>
  );
}
export function WithdrawalSummary({ item }: { item: Withdrawal }) {
  return (
    <div className="space-y-2 text-sm">
      <div className="flex flex-wrap justify-between gap-2">
        <span className="font-semibold">
          {money(item.requested_paise, "INR")} requested
        </span>
        <span>{item.status}</span>
      </div>
      <p className="text-muted-foreground">
        {new Date(item.created_at).toLocaleString("en-IN", {
          timeZone: "Asia/Kolkata",
        })}{" "}
        IST
      </p>
      <p>{item.masked_destination}</p>
      <p>
        {item.policy_pending ? "Requested deduction estimate" : "Deduction"}:{" "}
        {money(item.deduction_paise, "INR")}. Final withdrawable amount:{" "}
        <strong>
          {item.policy_pending
            ? "Pending policy review"
            : money(item.net_paise, "INR")}
        </strong>
        .
        {item.policy_pending &&
          ` Requested policy estimate: ${money(item.net_paise, "INR")}. No payment is approved.`}
      </p>
      {item.status === "paid" && (
        <p>
          Manual transfer recorded: {money(item.transferred_paise, "INR")}.
          Reference: {item.transfer_reference}.
        </p>
      )}
      {item.decision_note && <p>{item.decision_note}</p>}
    </div>
  );
}
export function DoctorWallet() {
  const [data, setData] = useState<DoctorWalletData | null>(null);
  const [revision, setRevision] = useState(0);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const [amount, setAmount] = useState("");
  const [destination, setDestination] = useState(emptyDestination);
  const key = useRef("");
  useEffect(() => {
    let active = true;
    getDoctorWallet()
      .then((v) => active && setData(v))
      .catch((e) => active && setError(errorMessage(e)));
    return () => {
      active = false;
    };
  }, [revision]);
  async function save(e: React.FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    setMessage("");
    try {
      await savePayoutDestination(destination);
      setDestination(emptyDestination);
      setMessage(
        "Payout details saved securely. Pending requests keep their original destination.",
      );
      setRevision((v) => v + 1);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  async function withdraw(e: React.FormEvent) {
    e.preventDefault();
    if (busy) return;
    const paise = parseRupees(amount);
    if (paise === null || paise < 100) {
      setError("Enter at least ₹1, with at most two decimal places.");
      return;
    }
    setBusy(true);
    setError("");
    setMessage("");
    if (!key.current) key.current = crypto.randomUUID();
    try {
      await requestWithdrawal(paise, key.current);
      key.current = "";
      setAmount("");
      setMessage(
        "Withdrawal requested. Estimated processing: 2 to 4 business days.",
      );
      setRevision((v) => v + 1);
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
      await cancelWithdrawal(id);
      setMessage("Request cancelled. The reservation has been released.");
      setRevision((v) => v + 1);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  const field =
    (name: keyof PayoutDestination) =>
    (e: React.ChangeEvent<HTMLInputElement>) =>
      setDestination((v) => ({ ...v, [name]: e.target.value }));
  return (
    <section
      className="space-y-4 rounded-lg border p-4"
      aria-labelledby="wallet-title"
    >
      <div className="flex flex-wrap justify-between gap-3">
        <div>
          <h2 id="wallet-title" className="text-xl font-semibold">
            Earnings and withdrawals
          </h2>
          <p className="mt-1 text-sm text-muted-foreground">
            New eligible consultations earn the configured fixed amount after
            payment capture. No historical earnings are added.
          </p>
        </div>
        <Button variant="outline" onClick={() => setRevision((v) => v + 1)}>
          Refresh wallet
        </Button>
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
      {!data ? (
        <p>Loading wallet...</p>
      ) : (
        <>
          <dl className="grid gap-3 sm:grid-cols-3">
            {[
              ["Recorded balance", data.balance.balance_paise],
              ["Reserved for requests", data.balance.reserved_paise],
              ["Available to request", data.balance.available_paise],
            ].map(([label, value]) => (
              <div className="rounded-md bg-muted/50 p-3" key={label}>
                <dt className="text-xs text-muted-foreground">{label}</dt>
                <dd className="mt-1 text-xl font-semibold">
                  {money(Number(value), "INR")}
                </dd>
              </div>
            ))}
          </dl>
          {data.balance.reconciliation_required && (
            <p
              role="alert"
              className="rounded-md border border-destructive p-3 text-sm"
            >
              Withdrawals are paused for a refund, dispute or negative balance.
              The administrator must reconcile the account.
            </p>
          )}
          {!data.configured && (
            <p className="text-sm">
              Payout details and withdrawal requests are not enabled yet.
              Contact the administrator.
            </p>
          )}
          <div className="grid gap-6 lg:grid-cols-2">
            <form onSubmit={withdraw} className="space-y-3">
              <h3 className="font-semibold">Request a partial withdrawal</h3>
              <Label htmlFor="withdraw-amount">Amount to request (INR)</Label>
              <Input
                id="withdraw-amount"
                inputMode="decimal"
                required
                value={amount}
                onChange={(e) => {
                  setAmount(e.target.value);
                  key.current = "";
                }}
                placeholder="2000.00"
              />
              <WithdrawalAmounts
                amount={parseRupees(amount) ?? 0}
                bps={data.deduction_bps}
                pending={!data.deduction_active}
              />
              <p className="text-xs text-muted-foreground">
                The full requested amount is reserved once. The administrator
                transfers the final amount manually. Estimated processing: 2 to
                4 business days.
              </p>
              {!data.deduction_active && (
                <p className="text-sm">
                  Final payout is pending policy review. You may reserve a
                  request, but the administrator cannot mark it paid. After
                  policy approval, this request must be cancelled or rejected
                  and recreated with the confirmed calculation.
                </p>
              )}
              <p className="text-sm">
                {data.balance.masked_destination ||
                  "Save your payout destination before requesting a withdrawal."}
              </p>
              <Button
                disabled={
                  busy ||
                  !data.configured ||
                  data.balance.reconciliation_required ||
                  !data.balance.masked_destination
                }
              >
                Request withdrawal
              </Button>
            </form>
            <details className="rounded-md border p-3">
              <summary className="cursor-pointer font-semibold">
                Save or replace payout details
              </summary>
              <form onSubmit={save} className="mt-4 space-y-3">
                <Label htmlFor="payout-kind">Payout method</Label>
                <select
                  id="payout-kind"
                  value={destination.kind}
                  onChange={(e) =>
                    setDestination({
                      ...emptyDestination,
                      holder: destination.holder,
                      kind: e.target.value as "bank" | "upi",
                    })
                  }
                  className="h-10 w-full rounded-md border bg-background px-3"
                >
                  <option value="bank">Bank account</option>
                  <option value="upi">UPI</option>
                </select>
                <Label htmlFor="payout-holder">Account holder’s name</Label>
                <Input
                  id="payout-holder"
                  required
                  maxLength={100}
                  value={destination.holder}
                  onChange={field("holder")}
                  autoComplete="off"
                />
                {destination.kind === "bank" ? (
                  <>
                    <Label htmlFor="payout-bank">Bank name</Label>
                    <Input
                      id="payout-bank"
                      required
                      maxLength={100}
                      value={destination.bank}
                      onChange={field("bank")}
                      autoComplete="off"
                    />
                    <Label htmlFor="payout-account">Account number</Label>
                    <Input
                      id="payout-account"
                      required
                      inputMode="numeric"
                      autoComplete="off"
                      minLength={6}
                      maxLength={34}
                      value={destination.account}
                      onChange={field("account")}
                    />
                    <Label htmlFor="payout-confirm">
                      Confirm account number
                    </Label>
                    <Input
                      id="payout-confirm"
                      required
                      inputMode="numeric"
                      autoComplete="off"
                      value={destination.confirm_account}
                      onChange={field("confirm_account")}
                    />
                    <Label htmlFor="payout-ifsc">IFSC</Label>
                    <Input
                      id="payout-ifsc"
                      required
                      maxLength={11}
                      autoComplete="off"
                      value={destination.ifsc}
                      onChange={field("ifsc")}
                    />
                  </>
                ) : (
                  <>
                    <Label htmlFor="payout-upi">UPI ID</Label>
                    <Input
                      id="payout-upi"
                      required
                      autoComplete="off"
                      placeholder="name@bank"
                      value={destination.upi}
                      onChange={field("upi")}
                    />
                  </>
                )}
                <p className="text-xs text-muted-foreground">
                  Verify these details carefully. They are encrypted at rest and
                  shown masked. Saving a destination does not verify it with the
                  bank.
                </p>
                <Button
                  type="submit"
                  variant="outline"
                  disabled={busy || !data.configured}
                >
                  Save payout details
                </Button>
              </form>
            </details>
          </div>
          <h3 className="font-semibold">Withdrawal history</h3>
          {data.withdrawals.length ? (
            <div className="grid gap-3 md:grid-cols-2">
              {data.withdrawals.map((item) => (
                <article
                  key={item.id}
                  className="space-y-3 rounded-md border p-3"
                >
                  <WithdrawalSummary item={item} />
                  {item.status === "requested" && (
                    <Button
                      variant="outline"
                      disabled={busy}
                      onClick={() => cancel(item.id)}
                    >
                      Cancel request
                    </Button>
                  )}
                </article>
              ))}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">
              No withdrawal requests yet.
            </p>
          )}
          <details>
            <summary className="cursor-pointer text-sm font-medium">
              Earnings and adjustments
            </summary>
            <ul className="mt-3 space-y-2">
              {data.ledger.map((item) => (
                <li
                  key={item.id}
                  className="flex flex-wrap justify-between gap-2 border-b py-2 text-sm"
                >
                  <span>
                    {item.reason.replaceAll("_", " ")}
                    <span className="ml-2 text-muted-foreground">
                      {new Date(item.created_at).toLocaleDateString("en-IN", {
                        timeZone: "Asia/Kolkata",
                      })}
                    </span>
                  </span>
                  <strong>{money(item.amount_paise, "INR")}</strong>
                </li>
              ))}
            </ul>
            {!data.ledger.length && (
              <p className="mt-2 text-sm">No recorded earnings yet.</p>
            )}
          </details>
        </>
      )}
    </section>
  );
}
