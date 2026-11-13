import {afterEach,expect,it,vi} from "vitest";
import {createCheckoutOrder,createFamilyOrder} from "./api";
import {confirmCheckoutAmount} from "./checkout";
import type {CheckoutOrder} from "./portal-types";
afterEach(()=>{vi.unstubAllGlobals();vi.restoreAllMocks()});
it("uses only versioned routes and does not fall back when the API is old",async()=>{
 const fetcher=vi.fn().mockResolvedValue(new Response("",{status:404}));
 vi.stubGlobal("fetch",fetcher);
 await expect(createCheckoutOrder("synthetic-child","synthetic-private-credential")).rejects.toThrow("being updated");
 await expect(createFamilyOrder("synthetic-child")).rejects.toThrow("being updated");
 expect(fetcher.mock.calls.map(call=>call[0])).toEqual([
 "/api/public/registrations/synthetic-child/order/itemized-v1",
 "/api/family/registrations/synthetic-child/order/itemized-v1"
 ]);
});
const order:CheckoutOrder={order_id:"order_synthetic",key_id:"synthetic",currency:"INR",amount_paise:236000,price:{base_paise:200000,tax_paise:36000,tax_bps:1800,total_paise:236000,policy:"owner-18-v1"}};
it.each([
 {...order,price:undefined},
 {...order,currency:"USD"},
 {...order,amount_paise:200000},
 {...order,price:{...order.price!,total_paise:200000}},
 {...order,price:{...order.price!,base_paise:200000.1}},
 {...order,price:{...order.price!,tax_bps:0}},
 {...order,price:{...order.price!,tax_paise:-1}},
])("blocks missing or inconsistent price metadata before confirmation",invalid=>{
 const confirm=vi.spyOn(window,"confirm");
 expect(()=>confirmCheckoutAmount(invalid)).toThrow("Checkout has not opened");
 expect(confirm).not.toHaveBeenCalled();
});
