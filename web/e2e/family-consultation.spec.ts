import { expect, test } from "@playwright/test";
import path from "node:path";
import fs from "node:fs/promises";

const evidence = path.resolve("../docs/evidence/consultation-flow");
test("register, hold, pay, confirm, access and download", async ({
  page,
  request,
}) => {
  expect((await request.post("http://127.0.0.1:8807/__test/reset")).ok()).toBeTruthy();
  const fixture = await (
    await request.get("http://127.0.0.1:8807/__test/fixture")
  ).json();
  await fs.mkdir(evidence, { recursive: true });
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));

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
  await page.goto("/");
  await page.getByLabel("Guardian’s name").fill("Browser Guardian");
  await page
    .getByLabel("Child’s name", { exact: true })
    .fill("New Browser Child");
  await page.getByLabel("Child’s date of birth").fill("2022-05-01");
  await page.getByLabel("Phone number").fill("9876543210");
  await page
    .getByRole("button", { name: "Request a consultation", exact: true })
    .click();
  await expect(page.getByText("Your request is with us.")).toBeVisible();
  await expect(
    page.getByRole("button", { name: /Pay .*for consultation/ }),
  ).toHaveCount(0);
  await page
    .getByLabel("Doctor", { exact: true })
    .selectOption(fixture.doctor_id);
  await page.getByLabel("Date", { exact: true }).fill(fixture.date);
  await page.getByRole("button", { name: "Find available times" }).click();
  await page
    .getByRole("button", { name: /Test Doctor A:/ })
    .first()
    .click();
  await page.getByLabel("Start time").fill("10:00");
  await page.getByLabel("End time").fill("10:30");
  await page.getByRole("button", { name: "Continue to payment" }).click();
  await expect(
    page.getByText("awaiting payment", { exact: true }),
  ).toBeVisible();
  await page.locator("#consultation").scrollIntoViewIfNeeded();
  await page.screenshot({
    path: path.join(evidence, "held-appointment.png"),
    fullPage: true,
    mask: [page.locator("#consultation code")],
  });

  // Only the external checkout script is substituted. All application calls use the real API and database.
  await page.route("https://checkout.razorpay.com/v1/checkout.js", (route) =>
    route.fulfill({
      contentType: "application/javascript",
      body: `window.Razorpay = class { constructor(options) { this.options = options; } on() {} open() { window.__checkoutOptions = this.options; } };`,
    }),
  );
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
  await page.getByRole("button", { name: "Confirm", exact: true }).click();
  await expect(page.locator('[data-slot="badge"]').filter({ hasText: /^confirmed$/ })).toBeVisible();
  await page
    .getByText("Booking requests", { exact: true })
    .scrollIntoViewIfNeeded();
  await page.screenshot({
    path: path.join(evidence, "admin-confirmed.png"),
    fullPage: true,
  });

  await page.goto("/family/login");
  await page.getByLabel("Child’s full name").fill("Browser Child");
  await page.getByLabel("Child’s date of birth").fill("2022-05-02");
  await page.getByLabel("Private registration token").fill(fixture.token);
  await page.getByRole("button", { name: "Open family portal" }).click();
  await expect(page.locator("main").getByRole("alert")).toHaveText(
    "The child details or private token are incorrect.",
  );
  await page.getByLabel("Child’s date of birth").fill("2022-05-01");
  await page.getByRole("button", { name: "Open family portal" }).click();
  await expect(page).toHaveURL(/\/family$/);
  await expect(page.getByText("Browser Child", { exact: true })).toBeVisible();
  await expect(page.getByText("Private Sibling", { exact: true })).toHaveCount(
    0,
  );
  await expect(
    page.getByRole("button", { name: "Download", exact: true }),
  ).toHaveCount(1);
  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "Download", exact: true }).click();
  expect((await download).suggestedFilename()).toBe("madamgy-book.pdf");
  const pending = await page.request.get(
    `/api/family/book-releases/${fixture.pending_id}/download`,
  );
  expect(pending.status()).toBe(404);
  await page.screenshot({
    path: path.join(evidence, "family-files.png"),
    fullPage: true,
  });
  await page.getByRole("button", { name: "Book a consultation" }).click();
  await page.getByLabel("Date", { exact: true }).fill(fixture.date);
  await page.getByRole("button", { name: "Find available times" }).click();
  await page
    .getByRole("button", { name: /Available consultation/ })
    .first()
    .click();
  await page.getByLabel("Start time").fill("11:00");
  await page.getByLabel("End time").fill("11:30");
  const booking = page.waitForRequest(
    (r) =>
      r.url().endsWith("/api/family/appointments") && r.method() === "POST",
  );
  await page.getByRole("button", { name: "Continue to payment" }).click();
  expect((await booking).postDataJSON().doctor_id).toBeUndefined();
  await expect(page.getByRole("button", { name: /^Pay / })).toBeVisible();
  await page.getByRole("button", { name: "Cancel request" }).click();
  await expect(
    page.getByRole("button", { name: "Book a consultation" }),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: /^Pay / })).toHaveCount(0);
  expect(errors).toEqual([]);
});
