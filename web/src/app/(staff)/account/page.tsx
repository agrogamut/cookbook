import { AccountForm } from "@/components/account-form";
import { PageHeader } from "@/components/page-header";
export default function AccountPage() {
  return (
    <div className="max-w-lg">
      <PageHeader
        title="My account"
        description="Change your password. This signs you out of all current sessions."
      />
      <AccountForm />
    </div>
  );
}
