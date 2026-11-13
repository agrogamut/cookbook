import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { RefundReviews } from "./refund-review";
import { decideRefund, getRefundReviews } from "@/lib/api";
import type { RefundReview } from "@/lib/portal-types";
vi.mock("@/lib/api",()=>({getRefundReviews:vi.fn(),decideRefund:vi.fn(),syncRefundReview:vi.fn()}));
const review:RefundReview={order_id:"order_local",payment_id:"pay_local",appointment_id:"appointment",starts_at:"2026-01-01T10:00:00Z",doctor_id:"doctor",doctor_name:"Synthetic doctor",payment_amount_paise:236000,refunded_paise:100,known_refunded_paise:100,original_earning_paise:500000,remaining_earning_paise:500000,status:"partially_refunded",refunds:[{id:"rfnd_local",amount_paise:100,provider_created_at:"2026-01-02T10:00:00Z",adjustment_paise:null,earning_before_paise:null,actor_id:null,actor_name:null,explanation:null,decided_at:null}]};
beforeEach(()=>{vi.clearAllMocks();vi.mocked(getRefundReviews).mockResolvedValue([structuredClone(review)]);});
it("requires an explicit zero, explanation and confirmation and sends current earnings",async()=>{
 const resolved=vi.fn();render(<RefundReviews onResolved={resolved}/>);
 const region=within(await screen.findByRole("region",{name:"Refund rfnd_local"}));
 expect(region.getByRole("button",{name:"Record refund decision"})).toBeDisabled();
 await userEvent.type(region.getByLabelText("Doctor earning deduction (INR)"),"0");
 await userEvent.type(region.getByLabelText("Reason for this decision"),"Keep the earning for this refund");
 await userEvent.click(region.getByRole("checkbox"));
 vi.mocked(decideRefund).mockResolvedValue({ok:true});
 await userEvent.click(region.getByRole("button",{name:"Record refund decision"}));
 expect(decideRefund).toHaveBeenCalledWith("rfnd_local",{adjustment_paise:0,expected_remaining_paise:500000,explanation:"Keep the earning for this refund",confirmed:true});
 expect(resolved).toHaveBeenCalledOnce();
});
it("keeps incomplete cases disabled and explains the hold",async()=>{
 vi.mocked(getRefundReviews).mockResolvedValue([{...review,known_refunded_paise:50}]);
 render(<RefundReviews onResolved={vi.fn()}/>);
 await screen.findByText(/Refund identities are incomplete/);
 await userEvent.click(screen.getByRole("checkbox"));
 expect(screen.getByRole("button",{name:"Record refund decision"})).toBeDisabled();
 expect(decideRefund).not.toHaveBeenCalled();
});
it("shows a permanent decision with actor and no editable controls",async()=>{
 vi.mocked(getRefundReviews).mockResolvedValue([{...review,remaining_earning_paise:499900,refunds:[{...review.refunds[0],adjustment_paise:100,earning_before_paise:500000,actor_name:"Test administrator",actor_id:"admin",explanation:"Explicit reviewed amount",decided_at:"2026-01-02T11:00:00Z"}]}]);
 render(<RefundReviews onResolved={vi.fn()}/>);
 await screen.findByText("This decision cannot be edited.");
 expect(screen.getByText(/Decided by Test administrator/)).toBeVisible();
 expect(screen.queryByRole("button",{name:"Record refund decision"})).toBeNull();
});
it("shows stale earnings conflicts without claiming the case is resolved",async()=>{
 vi.mocked(decideRefund).mockRejectedValue(new Error("Earnings changed. Refresh and choose again."));
 const refresh=vi.fn();render(<RefundReviews onResolved={refresh}/>);
 await screen.findByText("rfnd_local");
 await userEvent.type(screen.getByLabelText("Doctor earning deduction (INR)"),"10");
 await userEvent.type(screen.getByLabelText("Reason for this decision"),"Explicit adjustment");
 await userEvent.click(screen.getByRole("checkbox"));
 await userEvent.click(screen.getByRole("button",{name:"Record refund decision"}));
 expect(await screen.findByRole("alert")).toHaveTextContent("Earnings changed");
 expect(refresh).not.toHaveBeenCalled();
});
