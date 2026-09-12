import { requireStaff } from "@/lib/server-api";
import { StaffShell } from "@/components/staff-shell";
export default async function StaffLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const actor = await requireStaff();
  return <StaffShell actor={actor}>{children}</StaffShell>;
}
