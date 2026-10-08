import { afterEach, describe, it, expect, vi } from "vitest";
import { request, resolveBaseUrl } from "./api";

describe("request errors", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("preserves the server's error message", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ error: "Request origin is not allowed." }, { status: 403 })));
    await expect(request("/api/auth/login")).rejects.toMatchObject({
      status: 403,
      message: "Request origin is not allowed.",
    });
  });

  it.each(["", "<html>Bad Gateway</html>", "null", "{}", '{"error":""}', '{"error":"   "}', '{"error":{}}'])("provides a useful fallback for body %j", async (body) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(body, { status: 502 })));
    await expect(request("/api/auth/login")).rejects.toMatchObject({
      status: 502,
      message: expect.stringContaining("HTTP 502"),
    });
  });
});

// A scheme-less base url is the failure worth pinning: fetch() reads it as a relative path,
// so every API call would go to the console's own origin and return the app's own 404 page
// instead of erroring. That looks like a missing endpoint and is really a bad URL.
describe("resolveBaseUrl", () => {
  it("leaves an explicit scheme alone", () => {
    expect(resolveBaseUrl("http://localhost:8080")).toBe("http://localhost:8080");
    expect(resolveBaseUrl("https://madamgy-api.onrender.com")).toBe("https://madamgy-api.onrender.com");
  });

  it("adds https to a bare host, which is how Render supplies one service to another", () => {
    expect(resolveBaseUrl("madamgy-api.onrender.com")).toBe("https://madamgy-api.onrender.com");
  });

  it("adds http to a local address, which has no certificate", () => {
    expect(resolveBaseUrl("localhost:8080")).toBe("http://localhost:8080");
    expect(resolveBaseUrl("127.0.0.1:8080")).toBe("http://127.0.0.1:8080");
  });

  it("never leaves a trailing slash, which would double the one in every path", () => {
    expect(resolveBaseUrl("https://madamgy-api.onrender.com/")).toBe("https://madamgy-api.onrender.com");
    expect(resolveBaseUrl("madamgy-api.onrender.com//")).toBe("https://madamgy-api.onrender.com");
  });
});
