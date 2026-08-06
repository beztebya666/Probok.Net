import { networkInterfaces } from "node:os";
import type { NextConfig } from "next";

/**
 * Hosts allowed to load dev-server assets.
 *
 * The dev server refuses asset requests whose origin it does not recognise, so
 * opening the app from a phone on the same network returns 403 for every chunk
 * and the page never hydrates. Listing this machine's own private addresses
 * keeps that protection for everything else while making LAN testing work
 * without editing config by hand every time the address changes.
 */
function localNetworkOrigins(): string[] {
  if (process.env.NODE_ENV === "production") return [];
  const configured = (process.env.GREENROUTE_DEV_ORIGINS ?? "")
    .split(",")
    .map((origin) => origin.trim())
    .filter(Boolean);
  const addresses = Object.values(networkInterfaces())
    .flatMap((entries) => entries ?? [])
    .filter((entry) => entry.family === "IPv4" && !entry.internal)
    .map((entry) => entry.address);
  // Yandex refuses a bare IP in its HTTP Referer restriction, so the map key can
  // only be authorised for a domain. nip.io resolves 192-168-1-22.nip.io to
  // 192.168.1.22 without any DNS setup, which gives the LAN a real hostname.
  const wildcardDomains = addresses.map((address) => `${address.replaceAll(".", "-")}.nip.io`);
  return [...new Set([...configured, ...addresses, ...wildcardDomains])];
}

const securityHeaders = [
  { key: "Cross-Origin-Opener-Policy", value: "same-origin" },
  { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
  { key: "X-Content-Type-Options", value: "nosniff" },
  { key: "X-Frame-Options", value: "DENY" },
  { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=(self), payment=(), usb=()" },
  ...(process.env.NODE_ENV === "production"
    ? [{ key: "Strict-Transport-Security", value: "max-age=31536000; includeSubDomains" }]
    : []),
];

/**
 * The GitHub Pages demo build.
 *
 * Pages serves static files from `/<repo>/` with no Node runtime, so this mode
 * swaps `standalone` for `export`, moves every asset under the repo path and
 * drops the header rules, which a static host cannot apply. Everything the demo
 * needs runs in the browser: in demo mode the client answers from fixtures
 * instead of calling edge-api.
 */
const staticExport = process.env.NEXT_PUBLIC_STATIC_EXPORT === "true";
const basePath = (process.env.NEXT_PUBLIC_BASE_PATH ?? "").replace(/\/$/, "");

const nextConfig: NextConfig = {
  allowedDevOrigins: localNetworkOrigins(),
  poweredByHeader: false,
  reactStrictMode: true,
  devIndicators: false,
  compress: true,
  productionBrowserSourceMaps: false,
  ...(staticExport
    ? {
        output: "export",
        // Pages has no rewrite layer, so directory-style URLs are the only ones
        // that resolve without one.
        trailingSlash: true,
        ...(basePath ? { basePath, assetPrefix: basePath } : {}),
        images: { unoptimized: true },
      }
    : {
        output: "standalone",
        async headers() {
          return [{ source: "/:path*", headers: securityHeaders }];
        },
      }),
};

export default nextConfig;
