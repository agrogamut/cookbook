import Link from "next/link";
import Image from "next/image";
import { StaffLogin } from "@/components/staff-login";
import type { Metadata } from "next";
export const metadata: Metadata = {
  title: "Staff sign in",
  robots: { index: false, follow: false },
};
export default function LoginPage() {
  return (
    <main className="flex min-h-svh items-center justify-center bg-[#fff0f4] p-6 text-[#58293b]">
      <section className="w-full max-w-md rounded-xl border border-[#edc6d3] bg-[#fff9fb] p-8 shadow-sm">
        <Link href="/">
          <Image
            src="/madamgy-logo.png"
            alt="MadamGY"
            width={185}
            height={37}
            priority
          />
        </Link>
        <h1 className="mb-2 mt-9 text-2xl font-semibold tracking-tight">
          Staff sign in
        </h1>
        <p className="mb-7 text-sm leading-6 text-[#815b69]">
          For doctors and administrators. Use the account provided by your
          administrator.
        </p>
        <StaffLogin />
        <Link
          href="/"
          className="mt-7 block text-center text-xs text-[#815b69] underline underline-offset-4"
        >
          Back to consultation requests
        </Link>
      </section>
    </main>
  );
}
