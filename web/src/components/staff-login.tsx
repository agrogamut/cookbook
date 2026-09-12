"use client";
import { useState } from "react";
import { useRouter } from "next/navigation";
import { signIn } from "@/lib/api";
import { errorMessage } from "@/lib/portal-utils";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Label } from "./ui/label";

export function StaffLogin() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const router = useRouter();
  async function submit(event: React.FormEvent) {
    event.preventDefault();
    if (busy) return;
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
    <form onSubmit={submit} className="space-y-5">
      <div className="space-y-2">
        <Label htmlFor="staff-email">Email</Label>
        <Input
          className="bg-white text-[#58293b]"
          id="staff-email"
          type="email"
          autoComplete="username"
          required
          maxLength={254}
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          disabled={busy}
        />
      </div>
      <div className="space-y-2">
        <Label htmlFor="staff-password">Password</Label>
        <Input
          className="bg-white text-[#58293b]"
          id="staff-password"
          type="password"
          autoComplete="current-password"
          required
          maxLength={256}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
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
