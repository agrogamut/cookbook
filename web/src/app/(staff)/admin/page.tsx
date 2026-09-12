import { requireAdmin } from "@/lib/server-api";
import { AdminWorkspace } from "@/components/admin-workspace";
export default async function AdminPage() {
  await requireAdmin();
  return <AdminWorkspace />;
}
