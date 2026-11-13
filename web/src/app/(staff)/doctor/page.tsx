import { PageHeader } from "@/components/page-header";
import { RegistrationWorkspace } from "@/components/registration-workspace";
import { requireStaff } from "@/lib/server-api";
import { redirect } from "next/navigation";
import { AvailabilityEditor } from "@/components/availability-editor";
import { DoctorWallet } from "@/components/doctor-wallet";
import { DoctorBookings } from "@/components/doctor-bookings";
export default async function DoctorPage() {
  if ((await requireStaff()).role === "admin") redirect("/admin");
  return (
    <div className="space-y-6">
      <PageHeader
        title="Doctor workspace"
        description="Your consultation requests, upcoming calls, books and earnings."
      />
      <DoctorBookings />
      <section className="space-y-3 rounded-lg border-2 border-[#c31352] p-4 dark:border-pink-400"><h2 className="text-lg font-semibold">Child records and book generation</h2><p className="text-sm text-muted-foreground">Open an assigned record to review details or generate both books.</p><RegistrationWorkspace /></section>
      <DoctorWallet />
      <details className="rounded-md border p-4"><summary className="cursor-pointer font-medium">Manage my available times</summary><div className="mt-4"><AvailabilityEditor /></div></details>
    </div>
  );
}
