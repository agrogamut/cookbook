"use client";

import { useEffect, useState } from "react";
import { issueFamilyAccess, listAllRegistrations } from "@/lib/api";
import { errorMessage } from "@/lib/portal-utils";
import type { Registration } from "@/lib/portal-types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

export function FamilyAccessRecovery() {
  const [registrations, setRegistrations] = useState<Registration[]>([]);
  const [registrationID, setRegistrationID] = useState("");
  const [verified, setVerified] = useState(false);
  const [link, setLink] = useState("");
  const [expires, setExpires] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  useEffect(() => {
    let active = true;
    listAllRegistrations().then((items) => { if (active) setRegistrations(items); }).catch((e) => { if (active) setError(errorMessage(e)); });
    return () => { active = false; };
  }, []);
  async function issue() {
    if (!registrationID || !verified || busy) return;
    setBusy(true); setError(""); setLink(""); setMessage("");
    try {
      const grant = await issueFamilyAccess(registrationID);
      setLink(`${window.location.origin}/family/login#access=${grant.token}`);
      setExpires(new Date(grant.expires_at).toLocaleTimeString("en-IN", { timeZone: "Asia/Kolkata", hour: "2-digit", minute: "2-digit" }));
    } catch (e) { setError(errorMessage(e)); }
    finally { setBusy(false); }
  }
  async function copy() {
    try { await navigator.clipboard.writeText(link); setMessage("Link copied. Share it privately with the verified guardian."); }
    catch { setError("Clipboard unavailable. Select and copy the private link from the field."); }
  }
  return <section className="space-y-3 rounded-md border p-4">
    <h2 className="text-base font-semibold">Child access recovery</h2>
    <p className="text-sm text-muted-foreground">Verify the guardian using the clinic’s identity checks before sharing a link. A child’s name and birth date alone are not enough. Nothing is sent automatically.</p>
    <Label htmlFor="recovery-registration">Registration for access recovery</Label>
    <select id="recovery-registration" disabled={busy} className="h-9 w-full rounded-md border bg-background px-3 text-sm" value={registrationID} onChange={(e) => { setRegistrationID(e.target.value); setVerified(false); setLink(""); setMessage(""); setError(""); }}>
      <option value="">Choose a registration</option>
      {registrations.map((item) => <option key={item.id} value={item.id}>{item.child_name}, {item.guardian_name}</option>)}
    </select>
    <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={verified} disabled={busy} onChange={(e) => setVerified(e.target.checked)} />I verified this guardian’s identity</label>
    <Button disabled={busy || !registrationID || !verified} onClick={issue}>Create private access link</Button>
    {link && <div className="space-y-2">
      <p className="text-sm">One use, expires at {expires} IST. Creating another link replaces this link. Using it replaces the child’s previous saved access and child sessions.</p>
      <Label htmlFor="private-family-link">Private access link</Label>
      <Input id="private-family-link" type="password" readOnly value={link} onFocus={(e) => e.target.select()} />
      <Button variant="outline" onClick={copy}>Copy private link</Button>
    </div>}
    {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
    {message && <p role="status" className="text-sm">{message}</p>}
  </section>;
}
