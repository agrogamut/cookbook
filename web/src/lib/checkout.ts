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
