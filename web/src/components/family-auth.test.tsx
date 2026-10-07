import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { FamilyAuth } from "./family-portal";
import { attachFamilyRegistration, familyChildAccess, familySignIn, familySignUp } from "@/lib/api";
import { registrationReceiptKey } from "@/lib/portal-utils";

const { replace } = vi.hoisted(() => ({ replace: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace }) }));
vi.mock("@/lib/api", () => ({
  attachFamilyRegistration: vi.fn(), familyChildAccess: vi.fn(), familySignIn: vi.fn(), familySignUp: vi.fn(),
}));
const credential = "a".repeat(64);
const guardian = { id: "family", name: "Guardian", email: "family@example.invalid", active: true };

beforeEach(() => {
  sessionStorage.clear();
  vi.resetAllMocks();
  vi.mocked(familyChildAccess).mockResolvedValue(guardian);
  vi.mocked(familySignIn).mockResolvedValue(guardian);
  vi.mocked(familySignUp).mockResolvedValue(guardian);
  vi.mocked(attachFamilyRegistration).mockResolvedValue({ id: "registration" });
});

describe("family sign-in without manual credentials from the receipt", () => {
  it("offers account login when this browser has no saved registration", () => {
    render(<FamilyAuth mode="login" />);
    expect(screen.getByLabelText("Email")).toBeInTheDocument();
    expect(screen.getByLabelText("Password")).toBeInTheDocument();
    expect(screen.queryByLabelText("Private registration token")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Child’s full name")).not.toBeInTheDocument();
  });

  it("checks the saved registration with the entered child details", async () => {
    sessionStorage.setItem(registrationReceiptKey, JSON.stringify({ id: "registration", token: credential }));
    render(<FamilyAuth mode="login" />);
    await userEvent.type(screen.getByLabelText("Child’s full name"), "Test Child");
    await userEvent.type(screen.getByLabelText("Child’s date of birth"), "08102021");
    expect(screen.queryByLabelText("Private registration token")).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Open family portal" }));
    await waitFor(() => expect(familyChildAccess).toHaveBeenCalledWith("Test Child", "2021-10-08", credential));
    expect(replace).toHaveBeenCalledWith("/family");
  });

  it("retries linking without creating the account twice", async () => {
    sessionStorage.setItem(registrationReceiptKey, JSON.stringify({ id: "registration", token: credential }));
    vi.mocked(attachFamilyRegistration).mockRejectedValueOnce(new Error("Temporary link error"));
    render(<FamilyAuth mode="register" />);
    await userEvent.type(screen.getByLabelText("Your name"), "Guardian");
    await userEvent.type(screen.getByLabelText("Email"), "family@example.invalid");
    await userEvent.type(screen.getByLabelText("Password"), "family-password-123");
    await userEvent.click(screen.getByRole("button", { name: "Create account" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Temporary link error");
    expect(screen.getByLabelText("Email")).toBeDisabled();
    await userEvent.click(screen.getByRole("button", { name: "Retry linking registration" }));
    await waitFor(() => expect(replace).toHaveBeenCalledWith("/family"));
    expect(familySignUp).toHaveBeenCalledTimes(1);
    expect(attachFamilyRegistration).toHaveBeenCalledTimes(2);
  });
});
