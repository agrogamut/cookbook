import { BookGenerator } from "@/components/book-generator";
import { PageHeader } from "@/components/page-header";
import { getRegistration, ApiError } from "@/lib/server-api";
import { notFound } from "next/navigation";

export default async function BooksPage({
  searchParams,
}: {
  searchParams: Promise<{ registration?: string }>;
}) {
  const { registration: id } = await searchParams;
  let registration;
  if (id) {
    try {
      registration = await getRegistration(id);
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) notFound();
      throw e;
    }
  }
  return (
    <div className="flex h-full flex-col">
      <PageHeader
        title="Books"
        description={
          registration
            ? `Consultation for ${registration.child_name}. Guardian: ${registration.guardian_name}. ${registration.phone}.`
            : "Enter a child's details to generate, preview and download books. Payment is not required."
        }
      />
      <BookGenerator
        key={registration?.id ?? "new"}
        initialChild={
          registration
            ? {
                display_name: registration.child_name,
                date_of_birth: registration.date_of_birth,
              }
            : undefined
        }
      />
    </div>
  );
}
