import { PageHeader } from "@/components/page-header";
import { RegistrationWorkspace } from "@/components/registration-workspace";
import { requireStaff } from "@/lib/server-api";
import { redirect } from "next/navigation";
import { AvailabilityEditor } from "@/components/availability-editor";
import { DoctorBookings } from "@/components/doctor-bookings";
export default async function DoctorPage() {
  if ((await requireStaff()).role === "admin") redirect("/admin");
  return (
    <div className="space-y-6">
      <PageHeader
        title="Assigned children"
        description="Consultation requests assigned to you. Open a record or continue to the book generator."
      />
      <RegistrationWorkspace />
      <AvailabilityEditor />
      <DoctorBookings />
    </div>
  );
}
