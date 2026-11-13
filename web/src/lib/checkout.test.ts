import { afterEach, describe, expect, it, vi } from "vitest";
import { confirmCheckoutAmount } from "./checkout";
import type { CheckoutOrder } from "./portal-types";
afterEach(() => vi.restoreAllMocks());
const active: CheckoutOrder = {order_id:"order_local", amount_paise:236000, currency:"INR", key_id:"local", price:{base_paise:200000, tax_paise:36000, tax_bps:1800, total_paise:236000, policy:"owner-18-v1"}};
describe("server order price confirmation", () => {
 it("shows the actual itemized active order before allowing checkout", () => {
  const confirm=vi.spyOn(window,"confirm").mockReturnValue(false);
  expect(confirmCheckoutAmount(active)).toBe(false);
  const text=confirm.mock.calls[0][0];
  expect(text).toContain("Consultation fee: ₹2,000.00");
  expect(text).toContain("GST (18%): ₹360.00");
  expect(text).toContain("Total: ₹2,360.00");
 });
 it("uses an existing tax-off order snapshot even after the default changes", () => {
  const confirm=vi.spyOn(window,"confirm").mockReturnValue(true);
  expect(confirmCheckoutAmount({...active, amount_paise:200000,price:{base_paise:200000,tax_paise:0,tax_bps:0,total_paise:200000,policy:"inactive"}})).toBe(true);
  expect(confirm.mock.calls[0][0]).toContain("GST (0%): ₹0.00");
  expect(confirm.mock.calls[0][0]).toContain("Total: ₹2,000.00");
 });
});
