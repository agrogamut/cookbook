"use client";

import { useEffect, useState } from "react";
import { attachRegistrationToGuardian, listAllRegistrations, listGuardians } from "@/lib/api";
import { errorMessage } from "@/lib/portal-utils";
import type { GuardianAccount, Registration } from "@/lib/portal-types";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription } from "@/components/ui/alert";

export function FamilyAccountLinker() {
  const [registrations, setRegistrations] = useState<Registration[]>([]);
  const [guardians, setGuardians] = useState<GuardianAccount[]>([]);
  const [registrationID, setRegistrationID] = useState("");
  const [guardianID, setGuardianID] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  useEffect(() => { let active = true; Promise.all([listAllRegistrations(), listGuardians()]).then(([items, accounts]) => { if (active) { setRegistrations(items); setGuardians(accounts.filter((item) => item.active)); } }).catch((e) => active && setError(errorMessage(e))); return () => { active = false; }; }, []);
  async function save() { if (!registrationID) return; setBusy(true); setError(""); setMessage(""); try { await attachRegistrationToGuardian(registrationID, guardianID); setMessage("Family account link saved"); } catch (e) { setError(errorMessage(e)); } finally { setBusy(false); } }
  return <section className="space-y-3 rounded-md border p-4"><div><h2 className="text-base font-semibold">Family account links</h2><p className="text-xs text-muted-foreground">Attach or detach a registration explicitly. Email is never used to merge rows.</p></div>{error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}{message && <Alert><AlertDescription>{message}</AlertDescription></Alert>}<div className="grid gap-2 md:grid-cols-[1fr_1fr_auto]"><select aria-label="Registration to attach" className="h-9 rounded-md border bg-background px-3 text-sm" value={registrationID} onChange={(e) => setRegistrationID(e.target.value)}><option value="">Choose a registration</option>{registrations.map((item) => <option key={item.id} value={item.id}>{item.child_name}, {item.guardian_name}</option>)}</select><select aria-label="Family account" className="h-9 rounded-md border bg-background px-3 text-sm" value={guardianID} onChange={(e) => setGuardianID(e.target.value)}><option value="">No family account</option>{guardians.map((item) => <option key={item.id} value={item.id}>{item.name}, {item.email}</option>)}</select><Button disabled={busy || !registrationID} onClick={save}>Save link</Button></div></section>;
}
