import type { CheckoutConfirmation } from "./portal-types";

interface CheckoutOptions {
  key: string;
  order_id: string;
  amount: number;
  currency: string;
  name: string;
  description: string;
  handler: (response: CheckoutConfirmation) => void;
  modal: { ondismiss: () => void };
  theme: { color: string };
}
interface CheckoutInstance {
  open(): void;
  on(event: "payment.failed", callback: () => void): void;
}
type CheckoutConstructor = new (options: CheckoutOptions) => CheckoutInstance;
declare global {
  interface Window {
    Razorpay?: CheckoutConstructor;
  }
}
let loading: Promise<CheckoutConstructor> | null = null;

export function loadCheckout(): Promise<CheckoutConstructor> {
  if (window.Razorpay) return Promise.resolve(window.Razorpay);
  if (loading) return loading;
  loading = new Promise<CheckoutConstructor>((resolve, reject) => {
    const script = document.createElement("script");
    const fail = () => {
      clearTimeout(timeout);
      script.remove();
      loading = null;
      reject(
        new Error(
          "Checkout could not load. Your registration is saved. Please try again.",
        ),
      );
    };
    script.src = "https://checkout.razorpay.com/v1/checkout.js";
    script.async = true;
    script.onerror = fail;
    script.onload = () => {
      clearTimeout(timeout);
      if (window.Razorpay) resolve(window.Razorpay);
      else fail();
    };
    const timeout = setTimeout(fail, 20000);
    document.head.appendChild(script);
  });
  return loading;
}



export function confirmCheckoutAmount(order: import("./portal-types").CheckoutOrder): boolean {
  const price = order.price;
  if (!price || order.currency !== "INR" ||
      ![order.amount_paise, price.base_paise, price.tax_paise, price.total_paise, price.tax_bps].every(Number.isSafeInteger) ||
      price.base_paise <= 0 || price.tax_paise < 0 ||
      ![0, 1800].includes(price.tax_bps) ||
      price.tax_paise !== Math.floor((price.base_paise * price.tax_bps + 5000) / 10000) ||
      price.base_paise + price.tax_paise !== order.amount_paise ||
      price.total_paise !== order.amount_paise) {
    throw new Error("Itemized payment details are unavailable or inconsistent. Refresh and try again shortly. Checkout has not opened.");
  }
  const money = (value: number) => new Intl.NumberFormat("en-IN", { style: "currency", currency: "INR" }).format(value / 100);
  return window.confirm(`Consultation fee: ${money(price.base_paise)}\nGST (${price.tax_bps / 100}%): ${money(price.tax_paise)}\nTotal: ${money(order.amount_paise)}\nContinue to secure payment?`);
}
