import { BookGenerator } from "@/components/book-generator";
import { PageHeader } from "@/components/page-header";

export default function BooksPage() {
  return (
    <div>
      <PageHeader
        title="Books"
        description="Generate, preview and download a child's two books. Every page carries the provider's Draft status; nothing here is approved."
      />
      <BookGenerator />
    </div>
  );
}
