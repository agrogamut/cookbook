import { describe, it, expect } from "vitest";
import { resolveBaseUrl } from "./api";

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
