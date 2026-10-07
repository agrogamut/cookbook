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
export interface PublicRegistrationStatus {
  id: string;
  payment_status: PaymentStatus;
  appointment_id?: string | null;
  appointment_status?: AppointmentStatus | null;
  doctor_name?: string | null;
  appointment_starts_at?: string | null;
  appointment_ends_at?: string | null;
  hold_expires_at?: string | null;
}
export interface IntakeInput {
  guardian_name: string;
  child_name: string;
  date_of_birth: string;
  phone: string;
  email: string;
  token: string;
}

export interface GuardianAccount {
  registration_id?: string;
  id: string;
  name: string;
  email: string;
  active: boolean;
}
export type AppointmentStatus =
  | "awaiting_payment"
  | "pending_admin"
  | "paid_pending_admin"
  | "confirmed"
  | "rejected"
  | "expired"
  | "cancelled"
  | "refund_required"
  | "refunded";
export type AppointmentMode = "time_range" | "specific_doctor";
export interface AvailabilityBlock {
  id: string;
  doctor_id: string;
  doctor_name: string;
  starts_at: string;
  ends_at: string;
  active: boolean;
  created_at: string;
}
export interface FreeInterval {
  doctor_id: string;
  doctor_name: string;
  starts_at: string;
  ends_at: string;
}
export interface Appointment {
  guardian_name?: string;
  phone?: string;
  order_id?: string;
  payment_id?: string;
  refund_id?: string;
  amount_paise?: number | null;
  refunded_paise?: number;
  id: string;
  registration_id: string;
  child_name: string;
  doctor_id: string;
  doctor_name: string;
  starts_at: string;
  ends_at: string;
  mode: AppointmentMode;
  status: AppointmentStatus;
  payment_status: PaymentStatus;
  requested_by: string;
  decided_by: string;
  created_at: string;
}
export type BookReleaseStatus = "pending_admin" | "approved" | "rejected";
export interface BookRelease {
  id: string;
  registration_id: string;
  child_name: string;
  book: "book1" | "book2";
  status: BookReleaseStatus;
  generated_by: string;
  approved_by: string;
  generated_at: string;
  decided_at: string | null;
  size_bytes: number;
}
export interface FamilyRegistration {
  id: string;
  child_name: string;
  date_of_birth: string;
  registration_status: ConsultationStatus;
  payment_status: PaymentStatus;
  amount_paise: number | null;
  currency: string;
  doctor_name: string;
  appointment_id: string | null;
  appointment_status: AppointmentStatus | null;
  appointment_starts_at: string | null;
  appointment_ends_at: string | null;
  appointment_hold_expires_at: string | null;
  book1_release_id: string | null;
  book1_status: BookReleaseStatus | null;
  book2_release_id: string | null;
  book2_status: BookReleaseStatus | null;
  created_at: string;
}
