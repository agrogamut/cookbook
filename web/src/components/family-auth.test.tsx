import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { FamilyAuth } from "./family-portal";
import { familyChildAccess, redeemFamilyAccess } from "@/lib/api";
import { clearFamilyAccessGrant, registrationReceiptKey } from "@/lib/portal-utils";

const { replace } = vi.hoisted(() => ({ replace: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace }) }));
vi.mock("@/lib/api", () => ({ familyChildAccess: vi.fn(), redeemFamilyAccess: vi.fn() }));
const credential = "a".repeat(64);
const grant = "b".repeat(64);
const guardian = { id: "family", name: "Guardian", email: "", active: true, registration_id: "registration" };

beforeEach(() => {
  sessionStorage.clear();
  clearFamilyAccessGrant();
  window.history.replaceState(null, "", "/family/login");
  vi.resetAllMocks();
  vi.mocked(familyChildAccess).mockResolvedValue(guardian);
  vi.mocked(redeemFamilyAccess).mockResolvedValue({ id: "registration", token: credential });
});

async function enterChild() {
  await userEvent.type(screen.getByLabelText("Child’s full name"), "Test Child");
  await userEvent.type(screen.getByLabelText("Child’s date of birth"), "08102021");
}

describe("private child sign-in", () => {
  it("accepts a private link opened while already on the sign-in page", async () => {
    render(<FamilyAuth />);
    act(() => {
      window.history.replaceState(null, "", "/family/login#access=" + grant);
      window.dispatchEvent(new HashChangeEvent("hashchange"));
    });
    expect(await screen.findByText(/private access link is ready/)).toBeVisible();
    expect(window.location.hash).toBe("");
    expect(redeemFamilyAccess).not.toHaveBeenCalled();
  });

  it("always shows child fields and blocks submission without saved private access", async () => {
    render(<FamilyAuth />);
    expect(screen.getByLabelText("Child’s full name")).toBeVisible();
    expect(screen.getByLabelText("Child’s date of birth")).toBeVisible();
    expect(screen.queryByLabelText("Email")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
    expect(screen.queryByText("Create one")).not.toBeInTheDocument();
    await enterChild();
    await userEvent.click(screen.getByRole("button", { name: "Open family portal" }));
    expect(screen.getByRole("alert")).toHaveTextContent("no saved registration");
    expect(familyChildAccess).not.toHaveBeenCalled();
    expect(redeemFamilyAccess).not.toHaveBeenCalled();
  });

  it("uses the original saved credential and opens the existing child registration", async () => {
    sessionStorage.setItem(registrationReceiptKey, JSON.stringify({ id: "registration", token: credential }));
    render(<FamilyAuth />);
    await enterChild();
    await userEvent.click(screen.getByRole("button", { name: "Open family portal" }));
    await waitFor(() => expect(familyChildAccess).toHaveBeenCalledWith("Test Child", "2021-10-08", credential));
    expect(replace).toHaveBeenCalledWith("/family");
    expect(redeemFamilyAccess).not.toHaveBeenCalled();
  });

  it("does not trust a malformed saved credential", async () => {
    sessionStorage.setItem(registrationReceiptKey, JSON.stringify({ token: "not-a-private-credential" }));
    render(<FamilyAuth />);
    await enterChild();
    await userEvent.click(screen.getByRole("button", { name: "Open family portal" }));
    expect(familyChildAccess).not.toHaveBeenCalled();
  });

  it("removes the recovery secret from the URL without consuming it until submission", async () => {
    window.history.replaceState(null, "", "/family/login#access=" + grant);
    render(<FamilyAuth />);
    expect(await screen.findByText(/private access link is ready/)).toBeVisible();
    expect(window.location.hash).toBe("");
    expect(redeemFamilyAccess).not.toHaveBeenCalled();
    await enterChild();
    await userEvent.click(screen.getByRole("button", { name: "Open family portal" }));
    await waitFor(() => expect(replace).toHaveBeenCalledWith("/family"));
    expect(redeemFamilyAccess).toHaveBeenCalledWith("Test Child", "2021-10-08", grant);
    expect(familyChildAccess).toHaveBeenCalledWith("Test Child", "2021-10-08", credential);
    expect(JSON.parse(sessionStorage.getItem(registrationReceiptKey)!)).toEqual({ id: "registration", token: credential });
  });

  it("shows rejected recovery without opening records or replacing saved access", async () => {
    window.history.replaceState(null, "", "/family/login#access=" + grant);
    vi.mocked(redeemFamilyAccess).mockRejectedValue(new Error("Access link expired."));
    render(<FamilyAuth />);
    await enterChild();
    await userEvent.click(screen.getByRole("button", { name: "Open family portal" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Access link expired.");
    expect(familyChildAccess).not.toHaveBeenCalled();
    expect(replace).not.toHaveBeenCalled();
    expect(sessionStorage.getItem(registrationReceiptKey)).toBeNull();
  });

  it("retries sign-in with recovered access without consuming the link twice", async () => {
    window.history.replaceState(null, "", "/family/login#access=" + grant);
    vi.mocked(familyChildAccess).mockRejectedValueOnce(new Error("Temporary server error"));
    render(<FamilyAuth />);
    await enterChild();
    await userEvent.click(screen.getByRole("button", { name: "Open family portal" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Temporary server error");
    await userEvent.click(screen.getByRole("button", { name: "Open family portal" }));
    await waitFor(() => expect(replace).toHaveBeenCalledWith("/family"));
    expect(redeemFamilyAccess).toHaveBeenCalledTimes(1);
    expect(familyChildAccess).toHaveBeenCalledTimes(2);
  });
});
