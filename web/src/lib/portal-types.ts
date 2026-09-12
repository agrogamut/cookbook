export type StaffRole = "admin" | "doctor";
export interface StaffAccount {
  id: string;
  name: string;
  email: string;
  role: StaffRole;
  active: boolean;
}
export type PaymentStatus =
  | "unpaid"
  | "pending"
  | "authorized"
  | "paid"
  | "failed"
  | "partially_refunded"
  | "refunded";
export type ConsultationStatus =
  | "new"
  | "contacted"
  | "scheduled"
  | "completed"
  | "cancelled";
export interface Registration {
  id: string;
  guardian_name: string;
  child_name: string;
  date_of_birth: string;
  phone: string;
  email: string;
  child_id: string;
  assigned_doctor_id: string;
  doctor_name: string;
  status: ConsultationStatus;
  notes: string;
  created_at: string;
  payment_status: PaymentStatus;
  amount_paise: number | null;
  currency: string;
  payment_id: string;
  order_id: string;
  refunded_paise: number;
}
export interface RegistrationList {
  items: Registration[];
  total: number;
  page: number;
  page_size: number;
  summary: { total: number; new: number; unassigned: number; paid: number };
}
export interface PublicSettings {
  amount_paise: number | null;
  currency: string;
  checkout_available: boolean;
}
export interface ConsultationSettings extends PublicSettings {
  payments_enabled: boolean;
  gateway_configured: boolean;
}
export interface CheckoutOrder {
  order_id: string;
  amount_paise: number;
  currency: string;
  key_id: string;
}
export interface CheckoutConfirmation {
  razorpay_order_id: string;
  razorpay_payment_id: string;
  razorpay_signature: string;
}
export interface IntakeInput {
  guardian_name: string;
  child_name: string;
  date_of_birth: string;
  phone: string;
  email: string;
  token: string;
}
