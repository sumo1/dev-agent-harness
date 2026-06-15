import { describe, it, expect } from "vitest";
import { resolveAssetUrlWithBase } from "./asset-url";

describe("resolveAssetUrlWithBase", () => {
  const base = "http://localhost:8080";

  it("prefixes a site-relative /uploads path with the API base (desktop cross-origin fix)", () => {
    expect(resolveAssetUrlWithBase("/uploads/workspaces/w/abc.png", base)).toBe(
      "http://localhost:8080/uploads/workspaces/w/abc.png",
    );
  });

  it("trims a trailing slash on the base before joining", () => {
    expect(resolveAssetUrlWithBase("/uploads/x.png", "http://localhost:8080/")).toBe(
      "http://localhost:8080/uploads/x.png",
    );
  });

  it("leaves absolute http(s) URLs untouched (S3 / CloudFront / LOCAL_UPLOAD_BASE_URL)", () => {
    const s3 = "https://bucket.s3.us-east-1.amazonaws.com/uploads/x.png";
    expect(resolveAssetUrlWithBase(s3, base)).toBe(s3);
  });

  it("leaves blob: and data: preview URLs untouched", () => {
    expect(resolveAssetUrlWithBase("blob:http://x/abc", base)).toBe("blob:http://x/abc");
    expect(resolveAssetUrlWithBase("data:image/png;base64,AAAA", base)).toBe(
      "data:image/png;base64,AAAA",
    );
  });

  it("does not invent an origin for non-upload relative strings", () => {
    expect(resolveAssetUrlWithBase("/api/something", base)).toBe("/api/something");
    expect(resolveAssetUrlWithBase("", base)).toBe("");
  });
});
