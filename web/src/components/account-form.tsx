"use client";
import { useState } from "react";
import { useRouter } from "next/navigation";
import { changePassword } from "@/lib/api";
import { errorMessage } from "@/lib/portal-utils";
import { useStaff } from "./staff-context";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Label } from "./ui/label";
export function AccountForm() {
  const actor = useStaff();
  const router = useRouter();
  const [current, setCurrent] = useState("");
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (password !== confirmation) {
      setMessage("The new passwords do not match.");
      return;
    }
    setBusy(true);
    setMessage("");
    try {
      await changePassword(current, password);
      router.replace("/login");
      router.refresh();
    } catch (e) {
      setMessage(errorMessage(e));
      setBusy(false);
    }
  }
  return (
    <form onSubmit={submit} className="space-y-5">
      <p className="text-sm">
        {actor.name}{" "}
        <span className="text-muted-foreground">({actor.email})</span>
      </p>
      <div className="space-y-2">
        <Label htmlFor="current-password">Current password</Label>
        <Input
          id="current-password"
          type="password"
          autoComplete="current-password"
          required
          maxLength={256}
          value={current}
          onChange={(e) => setCurrent(e.target.value)}
        />
      </div>
      <div className="space-y-2">
        <Label htmlFor="new-password">New password</Label>
        <Input
          id="new-password"
          type="password"
          autoComplete="new-password"
          required
          minLength={12}
          maxLength={256}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
        <p className="text-xs text-muted-foreground">
          Use at least 12 characters.
        </p>
      </div>
      <div className="space-y-2">
        <Label htmlFor="confirm-password">Confirm new password</Label>
        <Input
          id="confirm-password"
          type="password"
          autoComplete="new-password"
          required
          minLength={12}
          maxLength={256}
          value={confirmation}
          onChange={(e) => setConfirmation(e.target.value)}
        />
      </div>
      {message && (
        <p role="alert" className="text-sm text-destructive">
          {message}
        </p>
      )}
      <Button disabled={busy} type="submit">
        {busy ? "Updating..." : "Change password and sign out"}
      </Button>
    </form>
  );
}
