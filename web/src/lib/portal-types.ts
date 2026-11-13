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
  guardian_description?: string;
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
  price?: Price;
  amount_paise: number | null;
  currency: string;
  checkout_available: boolean;
}
export interface ConsultationSettings extends PublicSettings {
  doctor_earning_paise?: number | null;
  wallet_configured?: boolean;
  patient_tax_active?: boolean;
  withdrawal_deduction_active?: boolean;
  payments_enabled: boolean;
  gateway_configured: boolean;
}
export interface CheckoutOrder {
  price?: Price;
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
  guardian_description?: string;
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
  meeting_url?: string;
  guardian_description?: string;
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
  meeting_url?: string;
  price?: Price;
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


export interface Price { base_paise: number; tax_paise: number; total_paise: number; tax_bps: number; policy: string }
export interface PayoutDestination { kind: "bank" | "upi"; holder: string; bank: string; account: string; confirm_account?: string; ifsc: string; upi: string }
export interface WalletBalance { doctor_id: string; doctor_name: string; balance_paise: number; reserved_paise: number; available_paise: number; reconciliation_required: boolean; masked_destination: string }
export interface Withdrawal { policy_pending: boolean; id: string; doctor_id: string; doctor_name: string; requested_paise: number; deduction_paise: number; net_paise: number; deduction_bps: number; masked_destination: string; status: "requested" | "processing" | "paid" | "rejected" | "cancelled"; created_at: string; transferred_paise: number | null; transfer_reference: string | null; transferred_at: string | null; decision_note: string }
export interface LedgerEntry { id: number; kind: string; amount_paise: number; appointment_id: string | null; withdrawal_id: string | null; reason: string; created_at: string }
export interface DoctorWalletData { balance: WalletBalance; withdrawals: Withdrawal[]; ledger: LedgerEntry[]; configured: boolean; deduction_bps: number; deduction_active: boolean }

export interface RefundCase { id: string; amount_paise: number; provider_created_at: string; adjustment_paise: number | null; earning_before_paise: number | null; actor_id: string | null; actor_name: string | null; explanation: string | null; decided_at: string | null }
export interface RefundReview { order_id: string; payment_id: string; appointment_id: string; starts_at: string; doctor_id: string; doctor_name: string; payment_amount_paise: number; refunded_paise: number; known_refunded_paise: number; original_earning_paise: number; remaining_earning_paise: number; status: string; refunds: RefundCase[] }
