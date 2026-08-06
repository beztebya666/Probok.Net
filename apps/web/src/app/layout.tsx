import type { Metadata, Viewport } from "next";
import { headers } from "next/headers";
import { connection } from "next/server";
import { AppProviders } from "@/components/app-providers";
import { PwaRegister } from "@/components/pwa-register";
import { asset } from "@/lib/base-path";
import { getRuntimeConfig } from "@/lib/runtime-config";
import { themeBootstrapScript } from "@/lib/theme";
import "./globals.css";

export const metadata: Metadata = {
  metadataBase: new URL("https://greenroute.example"),
  title: {
    default: "Пробок.Нет — маршрут по зелёным участкам",
    template: "%s · Пробок.Нет",
  },
  description: "Маршруты с понятной оценкой загруженности, ограничениями объезда и честным confidence.",
  applicationName: "Пробок.Нет",
  manifest: asset("/manifest.webmanifest"),
  icons: {
    // The tab icon follows the operating system scheme, not the in-app toggle:
    // browsers resolve favicon media queries before any page script runs.
    icon: [
      { url: asset("/brand/logo-light-32.png"), type: "image/png", media: "(prefers-color-scheme: light)" },
      { url: asset("/brand/logo-dark-32.png"), type: "image/png", media: "(prefers-color-scheme: dark)" },
      { url: asset("/brand/logo-light-64.png"), type: "image/png", sizes: "64x64" },
    ],
    apple: [{ url: asset("/brand/logo-light-180.png"), sizes: "180x180", type: "image/png" }],
  },
  robots: { index: false, follow: false },
};

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  // Pinching belongs to the map, which handles its own gestures. Letting the
  // browser zoom the document instead pushed the layout wider than the screen
  // and left the page scrolling sideways.
  maximumScale: 1,
  userScalable: false,
  viewportFit: "cover",
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "#f5f5f3" },
    { media: "(prefers-color-scheme: dark)", color: "#21201f" },
  ],
};

function runtimeConnectPolicy(edgeApiBaseUrl?: string, pageHost?: string): string {
  const sources = [
    "'self'",
    "https://api-maps.yandex.ru",
    "https://*.maps.yandex.net",
    "https://*.yandex.ru",
    "https://yastatic.net",
    "https://mapgl.2gis.com",
    "https://*.2gis.com",
    "https://*.2gis.ru",
    "wss://*.2gis.com",
    "wss://*.2gis.ru",
  ];
  if (edgeApiBaseUrl) {
    try {
      const origin = new URL(edgeApiBaseUrl);
      sources.push(origin.origin);
      // The browser may have reached this page by the machine's LAN address, in
      // which case the client rewrites a loopback API URL to that same host.
      // The policy must name that origin too, or the call is blocked before it
      // is ever sent.
      if (pageHost && ["localhost", "127.0.0.1"].includes(origin.hostname)) {
        origin.hostname = pageHost;
        sources.push(origin.origin);
      }
    } catch {
      // getRuntimeConfig already reports the malformed value to the UI.
    }
  }
  return `connect-src ${sources.join(" ")}`;
}

// A static export is rendered once at build time: there is no request to read,
// and no proxy to issue a nonce, so the demo build must not ask for either.
const STATIC_EXPORT = process.env.NEXT_PUBLIC_STATIC_EXPORT === "true";

async function requestContext(): Promise<{ nonce?: string | undefined; pageHost?: string | undefined }> {
  if (STATIC_EXPORT) return {};
  await connection();
  const requestHeaders = await headers();
  return {
    nonce: requestHeaders.get("x-nonce") ?? undefined,
    pageHost: requestHeaders.get("host")?.split(":")[0]?.trim(),
  };
}

export default async function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  const runtime = getRuntimeConfig();
  const { nonce, pageHost } = await requestContext();
  return (
    <html
      lang="ru"
      suppressHydrationWarning
      style={{
        "--logo-image-light": `url(${asset("/brand/logo-light-192.png")})`,
        "--logo-image-dark": `url(${asset("/brand/logo-dark-192.png")})`,
      } as React.CSSProperties}
      data-greenroute-runtime-config="true"
      data-greenroute-demo-mode={String(runtime.demoMode)}
      data-greenroute-edge-api-base-url={runtime.edgeApiBaseUrl}
      data-greenroute-yandex-maps-browser-key={runtime.yandexMapsBrowserKey}
      data-greenroute-dgis-mapgl-browser-key={runtime.twoGisMapGLBrowserKey}
      data-greenroute-dgis-mapgl-dark-style={runtime.twoGisMapGLDarkStyleId}
      data-greenroute-provider-mode={runtime.providerMode}
      data-greenroute-address-provider-mode={runtime.addressProviderMode}
      data-greenroute-yandex-geocoder-configured={runtime.yandexGeocoderConfigured === undefined ? undefined : String(runtime.yandexGeocoderConfigured)}
      data-greenroute-yandex-geosuggest-configured={runtime.yandexGeosuggestConfigured === undefined ? undefined : String(runtime.yandexGeosuggestConfigured)}
      data-greenroute-dgis-configured={runtime.dgisConfigured === undefined ? undefined : String(runtime.dgisConfigured)}
      data-greenroute-yandex-traffic-available={String(runtime.yandexTrafficAvailable)}
      data-greenroute-admin-enabled={String(runtime.adminEnabled)}
      data-greenroute-admin-in-menu={String(runtime.adminInMenu)}
    >
      <head>
        <meta httpEquiv="Content-Security-Policy" content={runtimeConnectPolicy(runtime.edgeApiBaseUrl, pageHost)} />
        {/* Resolves the stored theme before first paint so the page never flashes
            light and then swaps. The proxy issues a nonce-based CSP, so this
            inline script must carry that nonce or the browser will refuse it. */}
        <script nonce={nonce} dangerouslySetInnerHTML={{ __html: themeBootstrapScript }} />
      </head>
      <body>
        <a className="skip-link" href="#main-content">К основному содержимому</a>
        <AppProviders>{children}</AppProviders>
        <PwaRegister />
      </body>
    </html>
  );
}
