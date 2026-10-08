"use client";
import { useState } from "react";
import { useRouter } from "next/navigation";
import { signIn } from "@/lib/api";
import { errorMessage } from "@/lib/portal-utils";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Label } from "./ui/label";

export function StaffLogin() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const router = useRouter();
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    // Read autofilled controls before disabling them for the pending request.
    const data = new FormData(event.currentTarget);
    const email = String(data.get("email") ?? "");
    const password = String(data.get("password") ?? "");
    setBusy(true);
    setError("");
    try {
      const actor = await signIn(email, password);
      router.replace(actor.role === "admin" ? "/admin" : "/doctor");
      router.refresh();
    } catch (e) {
      setError(errorMessage(e));
      setBusy(false);
    }
  }
  return (
    <form onSubmit={submit} noValidate className="space-y-5">
      <div className="space-y-2">
        <Label htmlFor="staff-email">Email</Label>
        <Input
          className="bg-white text-[#58293b]"
          id="staff-email"
          name="email"
          type="email"
          autoComplete="username"
          required
          maxLength={254}
          disabled={busy}
        />
      </div>
      <div className="space-y-2">
        <Label htmlFor="staff-password">Password</Label>
        <Input
          className="bg-white text-[#58293b]"
          id="staff-password"
          name="password"
          type="password"
          autoComplete="current-password"
          required
          maxLength={256}
          disabled={busy}
        />
      </div>
      {error && (
        <p role="alert" className="text-sm text-red-700">
          {error}
        </p>
      )}
      <Button
        type="submit"
        className="h-11 w-full bg-[#c31352] text-white hover:bg-[#a70e45]"
        disabled={busy}
      >
        {busy ? "Signing in..." : "Sign in"}
      </Button>
      <p className="text-xs leading-5 text-[#815b69]">
        Forgot your password? Ask your administrator to reset it.
      </p>
    </form>
  );
}
