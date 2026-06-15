import { getApi } from "./index";

// ---------------------------------------------------------------------------
// Asset URL resolution.
//
// The LocalStorage backend (dev / self-host without LOCAL_UPLOAD_BASE_URL)
// returns site-relative upload URLs like `/uploads/workspaces/<ws>/<file>.png`.
// On the WEB app the renderer shares the API origin, so a relative `/uploads/…`
// loads fine. On the DESKTOP app the renderer is served from the Vite/Electron
// origin (e.g. http://localhost:5173) while the API + file server live at a
// DIFFERENT origin (e.g. http://localhost:8080) — so a relative `/uploads/…`
// resolves against the renderer origin and 404s. That is why a pasted image
// "can't be accessed" on desktop.
//
// `resolveAssetUrl` makes a stored asset URL loadable from any renderer origin:
// it prefixes a site-relative `/uploads/…` path with the API base URL, and
// leaves absolute URLs (S3 / CloudFront / an already-absolute LocalStorage URL
// when LOCAL_UPLOAD_BASE_URL is set) untouched.
// ---------------------------------------------------------------------------

/** Pure core of resolveAssetUrl — base is injected so it is trivially testable. */
export function resolveAssetUrlWithBase(url: string, baseUrl: string): string {
  if (!url) return url;
  // Absolute URLs (http/https) and blob:/data: previews are already loadable.
  if (/^(https?:|blob:|data:)/i.test(url)) return url;
  // Only site-relative upload paths need the API origin prefixed. Other
  // relative strings are left as-is (we never want to invent an origin for
  // something that isn't a known asset path).
  if (url.startsWith("/uploads/")) {
    return `${baseUrl.replace(/\/$/, "")}${url}`;
  }
  return url;
}

/**
 * Absolutize a stored asset URL against the configured API base so it loads
 * from any renderer origin (critical on desktop, where the renderer and API
 * live on different origins). Absolute URLs pass through unchanged.
 *
 * Reads the API base from the shared ApiClient singleton; if the singleton is
 * not yet initialised (e.g. in isolated unit tests) it degrades to returning
 * the input unchanged rather than throwing into render.
 */
export function resolveAssetUrl(url: string): string {
  if (!url || !url.startsWith("/uploads/")) return url;
  let base = "";
  try {
    base = getApi().getBaseUrl();
  } catch {
    return url;
  }
  return resolveAssetUrlWithBase(url, base);
}
