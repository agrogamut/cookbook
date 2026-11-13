import { expect, test } from "@playwright/test";
import path from "node:path";
import fs from "node:fs/promises";

const evidence = path.resolve(
  process.env.PORTAL_EVIDENCE_DIR ?? "../docs/evidence/consultation-flow",
);
const dayFirst = (iso: string) => iso.split("-").reverse().join("/");
test("wallet, meeting links and manual payout", async ({ page, request }) => {
  expect(
    (await request.post("http://127.0.0.1:8807/__test/reset")).ok(),
  ).toBeTruthy();
  const fixture = await (
    await request.get("http://127.0.0.1:8807/__test/fixture")
  ).json();
  await fs.mkdir(evidence, { recursive: true });
  const errors: string[] = [];
  const confirmations: string[] = [];
  page.on("dialog", (dialog) => { confirmations.push(dialog.message()); return dialog.accept(); });
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("/family/login");
  await expect(page.getByLabel("Child’s full name")).toBeVisible();
  await expect(
    page.getByText(/This tab has no saved registration/),
  ).toBeVisible();
  await expect(page.getByLabel("Private registration token")).toHaveCount(0);
  await page.evaluate(
    (fixture) =>
      sessionStorage.setItem(
        "madamgy.consultation.receipt",
        JSON.stringify({ id: fixture.registration_id, token: fixture.token }),
      ),
    fixture,
  );

  for (const width of [360, 768, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    await page.goto("/family/login");
    await expect(page.getByLabel("Child’s full name")).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBeTruthy();
    await page.screenshot({
      path: path.join(evidence, `family-access-${width}.png`),
      fullPage: true,
    });
  }
  await page.evaluate(() =>
    sessionStorage.removeItem("madamgy.consultation.receipt"),
  );
  await page.goto("/");
  await page.getByLabel("Guardian’s name").fill("Browser Guardian");
  await page
    .getByLabel("Child’s name", { exact: true })
    .fill("New Browser Child");
  await page.getByLabel("Child’s date of birth").fill("01/05/2022");
  await page.getByLabel("Phone number").fill("9876543210");
  await page
    .getByLabel("Additional details for the doctor (optional)")
    .fill("Guardian-reported local test note\nবাংলা: ক্ষ");
  await page
    .getByRole("button", { name: "Request a consultation", exact: true })
    .click();
  await expect(page.getByText("Your request is with us.")).toBeVisible();
  await expect(page.getByText(/private token/i)).toHaveCount(0);
  await page.locator("#consultation").scrollIntoViewIfNeeded();
  await page.screenshot({
    path: path.join(evidence, "registration-receipt.png"),
    fullPage: true,
  });
  await expect(
    page.getByRole("button", { name: /Pay .*for consultation/ }),
  ).toHaveCount(0);
  await page
    .getByLabel("Doctor", { exact: true })
    .selectOption(fixture.doctor_id);
  await page.getByLabel("Date", { exact: true }).fill(dayFirst(fixture.date));
  const availability = page.waitForRequest((request) =>
    request.url().includes("/api/public/availability?"),
  );
  await page.getByRole("button", { name: "Find available times" }).click();
  expect(new URL((await availability).url()).searchParams.get("date")).toBe(
    fixture.date,
  );
  await page
    .getByRole("button", { name: /Test Doctor A:/ })
    .first()
    .click();
  await page.getByLabel("Start time").fill("10:00");
  await expect(page.getByLabel("End time")).toHaveValue("10:30");
  await expect(page.getByLabel("End time")).toHaveAttribute("max", "10:30");
  await page.getByLabel("End time").fill("10:31");
  await page.getByRole("button", { name: "Continue to payment" }).click();
  await expect(
    page.getByText("Consultations cannot exceed 30 minutes.", { exact: true }),
  ).toBeVisible();
  await page.getByLabel("End time").fill("10:30");
  await page.getByRole("button", { name: "Continue to payment" }).click();
  await expect(
    page.getByText("awaiting payment", { exact: true }),
  ).toBeVisible();
  await page.locator("#consultation").scrollIntoViewIfNeeded();
  await page.screenshot({
    path: path.join(evidence, "held-appointment.png"),
    fullPage: true,
  });

  // Only the external checkout script is substituted. All application calls use the real API and database.
  await page.route("https://checkout.razorpay.com/v1/checkout.js", (route) =>
    route.fulfill({
      contentType: "application/javascript",
      body: `window.Razorpay = class { constructor(options) { this.options = options; } on() {} open() { window.__checkoutOptions = this.options; } };`,
    }),
  );
  const legacyResponse = await page.evaluate(async () => {
    const receipt = JSON.parse(sessionStorage.getItem("madamgy.consultation.receipt")!);
    const response = await fetch("/api/public/registrations/" + receipt.id + "/order", {
      method: "POST", headers: {"Content-Type":"application/json","X-Madamgy-Request":"1","X-Registration-Token":receipt.token}, body:"{}",
    });
    return {status:response.status,body:await response.json()};
  });
  expect(legacyResponse.status).toBe(409);
  expect(legacyResponse.body.error).toContain("Refresh");
  await page.route("**/order/itemized-v1", route => route.fulfill({status:404,body:""}));
  await page.getByRole("button", { name: /Pay .*for consultation/ }).click();
  await expect(page.getByText(/Checkout is being updated/)).toBeVisible();
  expect(await page.evaluate(() => Boolean((window as unknown as {__checkoutOptions?:unknown}).__checkoutOptions))).toBe(false);
  await page.unroute("**/order/itemized-v1");
  await page.getByRole("button", { name: /Pay .*for consultation/ }).click();
  await expect
    .poll(() =>
      page.evaluate(() =>
        Boolean(
          (window as unknown as { __checkoutOptions?: unknown })
            .__checkoutOptions,
        ),
      ),
    )
    .toBeTruthy();
  const expectedTotal = process.env.PORTAL_BROWSER_INACTIVE_POLICY === "1" ? "₹250.00" : "₹295.00";
  expect(confirmations.some((message) => message.includes("Total: " + expectedTotal))).toBeTruthy();
  const checkoutAmount = await page.evaluate(() => (window as unknown as {__checkoutOptions: {amount: number}}).__checkoutOptions.amount);
  expect(checkoutAmount).toBe(process.env.PORTAL_BROWSER_INACTIVE_POLICY === "1" ? 25000 : 29500);
  const orderID = await page.evaluate(
    () =>
      (window as unknown as { __checkoutOptions: { order_id: string } })
        .__checkoutOptions.order_id,
  );
  const confirmation = await (
    await request.post("http://127.0.0.1:8807/__test/payment", {
      data: { order_id: orderID, status: "captured" },
    })
  ).json();
  await page.evaluate(async (result) => {
    await (
      window as unknown as {
        __checkoutOptions: { handler: (result: unknown) => Promise<void> };
      }
    ).__checkoutOptions.handler(result);
  }, confirmation);
  await expect(
    page.getByText("paid pending admin", { exact: true }),
  ).toBeVisible();

  await page.goto("/login");
  await page.getByLabel("Email", { exact: true }).fill(fixture.admin_email);
  await page.getByLabel("Password", { exact: true }).fill(fixture.password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page).toHaveURL(/\/admin/);
  await page.getByRole("tab", { name: "Availability and bookings" }).click();
  await expect(
    page.getByRole("button", { name: "Confirm", exact: true }),
  ).toBeVisible();
  await page
    .getByLabel("End for New Browser Child", { exact: true })
    .fill(`${fixture.date}T10:31`);
  await page.getByRole("button", { name: "Confirm", exact: true }).click();
  await expect(
    page.getByText("Consultations cannot exceed 30 minutes.", { exact: true }),
  ).toBeVisible();
  await page
    .getByLabel("End for New Browser Child", { exact: true })
    .fill(`${fixture.date}T10:30`);
  await page.getByRole("button", { name: "Confirm", exact: true }).click();
  await expect(
    page.locator('[data-slot="badge"]').filter({ hasText: /^confirmed$/ }),
  ).toBeVisible();
  await page
    .getByText("Booking requests", { exact: true })
    .scrollIntoViewIfNeeded();
  await page.screenshot({
    path: path.join(evidence, "admin-confirmed.png"),
    fullPage: true,
  });

  await page
    .getByLabel("Meeting link", { exact: true })
    .fill("https://meet.example.invalid/local-call");
  await page.getByRole("button", { name: "Save link", exact: true }).click();
  await expect(
    page.getByRole("link", { name: "Join call", exact: true }),
  ).toHaveAttribute("href", "https://meet.example.invalid/local-call");
  await page.goto("/family/login");
  await page.getByLabel("Guardian’s full name").fill("Browser Guardian");
  await page.getByLabel("Child’s full name").fill("New Browser Child");
  await page.getByLabel("Child’s date of birth").fill("01/05/2022");
  await page.getByRole("button", { name: "Open family portal" }).click();
  await expect(page).toHaveURL(/\/family$/);
  await expect(
    page.getByRole("link", { name: "Join call", exact: true }),
  ).toHaveAttribute("href", "https://meet.example.invalid/local-call");
  await page.screenshot({
    path: path.join(evidence, "family-meeting.png"),
    fullPage: true,
  });
  await page.context().clearCookies();
  await page.goto("/login");
  await page.getByLabel("Email", { exact: true }).fill(fixture.doctor_email);
  await page.getByLabel("Password", { exact: true }).fill(fixture.password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page).toHaveURL(/\/doctor$/);
  await expect(
    page.getByRole("heading", { name: "Upcoming calls", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Guardian-reported local test note", { exact: false }),
  ).toBeVisible();
  await page
    .getByLabel("Meeting link", { exact: true })
    .fill("https://meet.example.invalid/doctor-edited");
  await page.getByRole("button", { name: "Save link", exact: true }).click();
  await expect(
    page.getByRole("link", { name: "Join call", exact: true }),
  ).toHaveAttribute("href", "https://meet.example.invalid/doctor-edited");
  await page.getByLabel("Meeting link", {exact:true}).fill("https://meet.example.invalid/unsaved-draft");
  const otherEditor = await page.context().newPage();
  await otherEditor.goto("/doctor");
  await otherEditor.getByLabel("Meeting link", {exact:true}).fill("https://meet.example.invalid/external-update");
  await otherEditor.getByRole("button", {name:"Save link",exact:true}).click();
  await expect(otherEditor.getByRole("link",{name:"Join call",exact:true})).toHaveAttribute("href","https://meet.example.invalid/external-update");
  await page.getByRole("button",{name:"Save link",exact:true}).click();
  await expect(page.getByText(/Your draft is kept/)).toBeVisible();
  await expect(page.getByLabel("Meeting link",{exact:true})).toHaveValue("https://meet.example.invalid/unsaved-draft");
  await expect(page.getByRole("button",{name:"Save link",exact:true})).toBeDisabled();
  await page.screenshot({path:path.join(evidence,"meeting-conflict.png"),fullPage:true});
  await page.setViewportSize({width:390,height:900});
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBeTruthy();
  await page.screenshot({path:path.join(evidence,"meeting-conflict-mobile.png"),fullPage:true});
  await page.setViewportSize({width:1440,height:1000});
  await page.getByRole("button",{name:"Use current link",exact:true}).click();
  await expect(page.getByLabel("Meeting link",{exact:true})).toHaveValue("https://meet.example.invalid/external-update");
  await otherEditor.getByLabel("Meeting link",{exact:true}).fill("https://meet.example.invalid/doctor-edited");
  await otherEditor.getByRole("button",{name:"Save link",exact:true}).click();
  await expect(otherEditor.getByRole("link",{name:"Join call",exact:true})).toHaveAttribute("href","https://meet.example.invalid/doctor-edited");
  await page.getByRole("button",{name:"Refresh calls",exact:true}).click();
  await expect(page.getByLabel("Meeting link",{exact:true})).toHaveValue("https://meet.example.invalid/doctor-edited");
  await otherEditor.close();
  await expect(page.getByText(/^Loading (calls|requests|registrations|wallet)\.\.\.$/)).toHaveCount(0);
  await page.screenshot({
    path: path.join(evidence, "doctor-calls-light.png"),
    fullPage: true,
  });
  await page.evaluate(() => {
    localStorage.setItem("theme", "dark");
  });
  await page.reload();
  await expect(page.locator("html")).toHaveClass(/dark/);
  await expect(
    page.getByRole("heading", { name: "Upcoming calls", exact: true }),
  ).toBeVisible();
  await expect(page.getByText(/^Loading (calls|requests|registrations|wallet)\.\.\.$/)).toHaveCount(0);
  await page.screenshot({
    path: path.join(evidence, "doctor-calls-dark.png"),
    fullPage: true,
  });
  await page.evaluate(() => {
    localStorage.setItem("theme", "light");
  });
  await page.reload();
  await page.setViewportSize({ width: 390, height: 900 });
  await expect(
    page.getByRole("heading", { name: "Upcoming calls", exact: true }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBeTruthy();
  await expect(page.getByText(/^Loading (calls|requests|registrations|wallet)\.\.\.$/)).toHaveCount(0);
  await page.screenshot({
    path: path.join(evidence, "doctor-mobile.png"),
    fullPage: true,
  });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page
    .getByText("Save or replace payout details", { exact: true })
    .click();
  await page
    .getByLabel("Account holder’s name", { exact: true })
    .fill("Synthetic Doctor");
  await page.getByLabel("Bank name", { exact: true }).fill("Example Bank");
  await page.getByLabel("Account number", { exact: true }).fill("001234567890");
  await page
    .getByLabel("Confirm account number", { exact: true })
    .fill("001234567890");
  await page.getByLabel("IFSC", { exact: true }).fill("TEST0123456");
  await page
    .getByRole("button", { name: "Save payout details", exact: true })
    .click();
  await expect(page.getByText(/Payout details saved securely/)).toBeVisible();
  await page
    .getByLabel("Amount to request (INR)", { exact: true })
    .fill("2000");
  await page
    .getByRole("button", { name: "Request withdrawal", exact: true })
    .click();
  await expect(page.getByText(/Withdrawal requested/)).toBeVisible();
  await page
    .getByRole("heading", { name: "Earnings and withdrawals" })
    .scrollIntoViewIfNeeded();
  await page.screenshot({
    path: path.join(evidence, "doctor-withdrawal.png"),
    fullPage: true,
  });
  const reviewed = process.env.PORTAL_BROWSER_INACTIVE_POLICY !== "1";
  if (!reviewed)
    await expect(
      page.getByText(/Final payout is pending policy review/),
    ).toBeVisible();
  await page.context().clearCookies();
  await page.goto("/login");
  await page.getByLabel("Email", { exact: true }).fill(fixture.admin_email);
  await page.getByLabel("Password", { exact: true }).fill(fixture.password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page).toHaveURL(/\/admin/);
  await page
    .getByRole("tab", { name: "Doctor withdrawals", exact: true })
    .click();
  await expect(
    page.getByText("Bank account ending 7890", { exact: true }),
  ).toBeVisible();
  if (!reviewed) {
    await expect(
      page.getByText(/Final payout pending policy review/),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Mark paid", exact: true }),
    ).toHaveCount(0);
    await page.screenshot({
      path: path.join(evidence, "admin-policy-pending.png"),
      fullPage: true,
    });
  } else {
    await page
      .getByRole("button", { name: "Review payout destination", exact: true })
      .click();
    await expect(page.getByText("001234567890", { exact: true })).toBeVisible();
    await page
      .getByRole("button", { name: "Hide private details", exact: true })
      .click();
    await page
      .getByRole("button", { name: "Mark processing", exact: true })
      .click();
    await expect(page.getByText("processing", { exact: true })).toBeVisible();
    await page
      .getByText("Record completed manual transfer", { exact: true })
      .click();
    await page
      .getByLabel("Actual transferred amount (INR)", { exact: true })
      .fill("1640");
    await page
      .getByLabel("Transfer reference", { exact: true })
      .fill("SYNTHETIC-LOCAL-TRANSFER");
    const instant = new Date()
      .toLocaleString("sv-SE", { timeZone: "Asia/Kolkata" })
      .slice(0, 16)
      .replace(" ", "T");
    await page
      .getByLabel("Actual transfer time (IST)", { exact: true })
      .fill(instant);
    await page
      .getByLabel(
        "I confirm the transfer is complete and these details match the bank or UPI receipt.",
      )
      .check();
    await page.getByRole("button", { name: "Mark paid", exact: true }).click();
    await expect(page.getByText("paid", { exact: true })).toBeVisible();
    await page.screenshot({
      path: path.join(evidence, "admin-manual-paid.png"),
      fullPage: true,
    });
    await page.context().clearCookies();
    await page.goto("/login");
    await page.getByLabel("Email", { exact: true }).fill(fixture.doctor_email);
    await page.getByLabel("Password", { exact: true }).fill(fixture.password);
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    await expect(page).toHaveURL(/\/doctor$/);
    const wallet = page.getByRole("region", {
      name: "Earnings and withdrawals",
    });
    await expect(
      wallet.getByText("₹3,000.00", { exact: true }).first(),
    ).toBeVisible();
    await expect(wallet.getByText(/SYNTHETIC-LOCAL-TRANSFER/)).toBeVisible();
    await wallet.scrollIntoViewIfNeeded();
    await page.screenshot({
      path: path.join(evidence, "doctor-paid-wallet.png"),
      fullPage: true,
    });
    const refund = await request.post("http://127.0.0.1:8807/__test/payment", {
      data: { order_id: orderID, payment_id: confirmation.razorpay_payment_id, status: "captured", refunded_paise: 1 },
    });
    expect(refund.ok()).toBeTruthy();
    const receipt = await page.evaluate(() =>
      JSON.parse(sessionStorage.getItem("madamgy.consultation.receipt") ?? "null"),
    );
    const verification = await request.post(
      "http://127.0.0.1:8807/api/public/registrations/" + receipt.id + "/verify",
      {
        data: await refund.json(),
        headers: {
          Origin: "http://127.0.0.1:3307",
          "X-Madamgy-Request": "1",
          "X-Registration-Token": receipt.token,
        },
      },
    );
    expect(verification.ok()).toBeTruthy();
    await page.reload();
    await expect(wallet.getByText("₹3,000.00", { exact: true }).first()).toBeVisible();
    await expect(wallet.getByText(/Withdrawals are paused for a refund/)).toBeVisible();
    await expect(wallet.getByRole("button", { name: "Request withdrawal", exact: true })).toBeDisabled();
    await expect(wallet.getByText(/SYNTHETIC-LOCAL-TRANSFER/)).toBeVisible();
    await wallet.scrollIntoViewIfNeeded();
    await page.screenshot({
      path: path.join(evidence, "doctor-partial-refund-hold.png"),
      fullPage: true,
    });
    await page.context().clearCookies();
    await page.goto("/login");
    await page.getByLabel("Email", {exact:true}).fill(fixture.admin_email);
    await page.getByLabel("Password", {exact:true}).fill(fixture.password);
    await page.getByRole("button",{name:"Sign in",exact:true}).click();
    await expect(page).toHaveURL(new RegExp("/admin"));
    await page.getByRole("tab",{name:"Doctor withdrawals",exact:true}).click();
    const review = page.getByRole("region",{name:"Refund reconciliation",exact:true});
    await expect(review.getByText("Original doctor earning")).toBeVisible();
    await review.scrollIntoViewIfNeeded();
    await page.screenshot({path:path.join(evidence,"admin-refund-review.png"),fullPage:true});
    await review.getByLabel("Doctor earning deduction (INR)").fill("4000");
    await review.getByLabel("Reason for this decision").fill("Synthetic owner-selected adjustment after completed payout.");
    await review.getByRole("checkbox").check();
    await review.getByRole("button",{name:"Record refund decision",exact:true}).click();
    await expect(review.getByText("This decision cannot be edited.")).toBeVisible();
    await expect(page.getByText(/Available: -₹1,000.00/)).toBeVisible();
    await review.scrollIntoViewIfNeeded();
    await page.screenshot({path:path.join(evidence,"admin-refund-decided.png"),fullPage:true});
    await page.setViewportSize({width:390,height:900});
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBeTruthy();
    await review.scrollIntoViewIfNeeded();
    await page.screenshot({path:path.join(evidence,"admin-refund-mobile.png"),fullPage:true});
    await page.setViewportSize({width:1440,height:1000});
  }
  expect(errors).toEqual([]);
});
