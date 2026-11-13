import type { Price } from "@/lib/portal-types";
import { money } from "@/lib/portal-utils";
export function PriceBreakdown({ price }: { price?: Price }) {
  if (!price) return null;
  return (
    <dl className="space-y-1 rounded-md border p-3 text-sm">
      <div className="flex justify-between gap-3">
        <dt>Consultation fee</dt>
        <dd>{money(price.base_paise, "INR")}</dd>
      </div>
      <div className="flex justify-between gap-3">
        <dt>{price.tax_bps ? `GST (${price.tax_bps / 100}%)` : "GST (inactive)"}</dt>
        <dd>{money(price.tax_paise, "INR")}</dd>
      </div>
      <div className="flex justify-between gap-3 border-t pt-2 font-semibold">
        <dt>Total</dt>
        <dd>{money(price.total_paise, "INR")}</dd>
      </div>
    </dl>
  );
}
