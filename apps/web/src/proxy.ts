import { NextResponse, type NextRequest } from "next/server";

/**
 * Permits the host that served the page, on any port. In development the UI is
 * reachable both as localhost and as this machine's LAN address, and the edge
 * API lives on another port of whichever host the browser used.
 */
function developmentHostSources(request: NextRequest): string[] {
  const host = request.headers.get("host")?.split(":")[0]?.trim();
  if (!host || host === "localhost" || host === "127.0.0.1") return [];
  return [`http://${host}:*`, `https://${host}:*`];
}

export function proxy(request: NextRequest) {
  const isDevelopment = process.env.NODE_ENV === "development";
  const nonce = Buffer.from(crypto.randomUUID()).toString("base64");
  const connectSources = [
    "'self'",
    // Next's isolated proxy runtime freezes environment variables at image
    // build time. This outer policy permits transport classes; the runtime
    // HTML adds a second, exact-origin connect-src policy, and browsers enforce
    // the intersection of both policies.
    "https:",
    "http://localhost:*",
    "http://127.0.0.1:*",
    // Opened from a phone on the same network, the page calls the edge API by
    // the same host it was served from. CSP host wildcards only work on a
    // leading label, so the exact request host is added instead of a mask.
    ...(isDevelopment ? developmentHostSources(request) : []),
    "https://api-maps.yandex.ru",
    "https://*.maps.yandex.net",
    "https://*.yandex.ru",
    "https://*.2gis.com",
    "https://*.2gis.ru",
    isDevelopment ? "ws:" : undefined,
  ].filter((source): source is string => Boolean(source));
  const policy = [
    "default-src 'self'",
    `script-src 'self' 'nonce-${nonce}' 'strict-dynamic' https://api-maps.yandex.ru https://yastatic.net https://mapgl.2gis.com${isDevelopment ? " 'unsafe-eval'" : ""}`,
    "style-src 'self' 'unsafe-inline' https://yastatic.net https://*.2gis.com https://*.2gis.ru",
    "img-src 'self' data: blob: https://*.maps.yandex.net https://yastatic.net https://*.yandex.ru https://*.2gis.com https://*.2gis.ru",
    "font-src 'self' data: https://yastatic.net https://*.2gis.com https://*.2gis.ru",
    `connect-src ${connectSources.join(" ")}`,
    // Yandex JS API v3 bootstraps its documented workers from data: URLs;
    // those workers then import the pinned implementation from yastatic.net.
    "worker-src 'self' blob: data:",
    "manifest-src 'self'",
    "object-src 'none'",
    "frame-src 'none'",
    "frame-ancestors 'none'",
    "base-uri 'self'",
    "form-action 'self'",
    ...(!isDevelopment ? ["upgrade-insecure-requests"] : []),
  ].join("; ");

  const requestHeaders = new Headers(request.headers);
  requestHeaders.set("x-nonce", nonce);
  requestHeaders.set("Content-Security-Policy", policy);
  const response = NextResponse.next({ request: { headers: requestHeaders } });
  response.headers.set("Content-Security-Policy", policy);
  response.headers.set("Cache-Control", "private, no-store");
  return response;
}

export const config = {
  matcher: [
    {
      source: "/((?!api|_next/static|_next/image|icon(?:-maskable)?\\.svg|sw\\.js|manifest\\.webmanifest).*)",
      missing: [
        { type: "header", key: "next-router-prefetch" },
        { type: "header", key: "purpose", value: "prefetch" },
      ],
    },
  ],
};
