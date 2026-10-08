import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { StaffLogin } from "./staff-login";

const router = vi.hoisted(() => ({ replace: vi.fn(), refresh: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => router }));

const email = "staff@example.test";
const password = "local-test-password";
let fetchMock: ReturnType<typeof vi.fn>;

async function fillCredentials() {
  const user = userEvent.setup();
  await user.type(screen.getByLabelText("Email"), email);
  await user.type(screen.getByLabelText("Password"), password);
  return user;
}

describe("staff sign in", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
  });
  afterEach(() => vi.unstubAllGlobals());

  it.each(["admin", "doctor"])("posts credentials and opens /%s", async (role) => {
    fetchMock.mockResolvedValue(Response.json({ role }));
    render(<StaffLogin />);
    const user = await fillCredentials();
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [path, init] = fetchMock.mock.calls[0];
    expect(path).toBe("/api/auth/login");
    expect(init).toMatchObject({
      method: "POST",
      credentials: "include",
      cache: "no-store",
      body: JSON.stringify({ email, password }),
    });
    expect(init.headers.get("X-Madamgy-Request")).toBe("1");
    expect(init.headers.get("Content-Type")).toBe("application/json");
    await waitFor(() => expect(router.replace).toHaveBeenCalledWith(`/${role}`));
    expect(router.refresh).toHaveBeenCalledOnce();
  });

  it("submits with Enter", async () => {
    fetchMock.mockResolvedValue(Response.json({ role: "doctor" }));
    render(<StaffLogin />);
    const user = await fillCredentials();
    await user.keyboard("{Enter}");
    await waitFor(() => expect(router.replace).toHaveBeenCalledWith("/doctor"));
    expect(fetchMock).toHaveBeenCalledOnce();
  });

  it("reads filled controls even without React change events", async () => {
    fetchMock.mockResolvedValue(Response.json({ role: "admin" }));
    render(<StaffLogin />);
    const setValue = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
    setValue.call(screen.getByLabelText("Email"), email);
    setValue.call(screen.getByLabelText("Password"), password);

    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(fetchMock).toHaveBeenCalledWith("/api/auth/login", expect.objectContaining({
      body: JSON.stringify({ email, password }),
    }));
  });

  it.each(["", "not-an-email"])("surfaces API validation for %j instead of silently blocking submit", async (value) => {
    fetchMock.mockResolvedValue(Response.json({ error: "Enter an email address and password." }, { status: 400 }));
    render(<StaffLogin />);
    fireEvent.change(screen.getByLabelText("Email"), { target: { value } });
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));

    expect(fetchMock).toHaveBeenCalledOnce();
    expect(await screen.findByRole("alert")).toHaveTextContent("Enter an email address and password.");
    expect(router.replace).not.toHaveBeenCalled();
  });

  it("shows a rejected login and allows another deliberate submission", async () => {
    fetchMock.mockResolvedValue(Response.json({ error: "Email or password is incorrect." }, { status: 401 }));
    render(<StaffLogin />);
    const user = await fillCredentials();
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Email or password is incorrect.");
    expect(screen.getByRole("button", { name: "Sign in" })).toBeEnabled();
    expect(screen.getByLabelText("Password")).toBeEnabled();
    expect(router.replace).not.toHaveBeenCalled();
    expect(fetchMock).toHaveBeenCalledOnce();
  });

  it("shows an HTTP status when an upstream error has no JSON or reason phrase", async () => {
    fetchMock.mockResolvedValue(new Response("<html>Unavailable</html>", { status: 502 }));
    render(<StaffLogin />);
    const user = await fillCredentials();
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("HTTP 502");
    expect(router.replace).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Sign in" })).toBeEnabled();
  });

  it("shows network failures without retrying", async () => {
    fetchMock.mockRejectedValue(new TypeError("Failed to fetch"));
    render(<StaffLogin />);
    const user = await fillCredentials();
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Failed to fetch");
    expect(fetchMock).toHaveBeenCalledOnce();
    expect(router.replace).not.toHaveBeenCalled();
  });

  it("prevents duplicate requests while signing in", async () => {
    let finish!: (response: Response) => void;
    fetchMock.mockReturnValue(new Promise<Response>((resolve) => { finish = resolve; }));
    render(<StaffLogin />);
    const user = await fillCredentials();
    await user.dblClick(screen.getByRole("button", { name: "Sign in" }));
    expect(screen.getByRole("button", { name: "Signing in..." })).toBeDisabled();
    expect(fetchMock).toHaveBeenCalledOnce();
    finish(Response.json({ role: "admin" }));
    await waitFor(() => expect(router.replace).toHaveBeenCalledWith("/admin"));
  });
});
