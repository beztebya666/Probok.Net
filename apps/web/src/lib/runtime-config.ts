import { z } from "zod";

export type RuntimeConfig = {
  edgeApiBaseUrl?: string | undefined;
  yandexMapsBrowserKey?: string | undefined;
  twoGisMapGLBrowserKey?: string | undefined;
  // Id of the 2GIS MapGL style used when the dark theme is active.
  twoGisMapGLDarkStyleId?: string | undefined;
  providerMode?: "stub" | "yandex" | "2gis" | undefined;
  addressProviderMode?: "auto" | "yandex" | "2gis" | undefined;
  yandexGeocoderConfigured?: boolean | undefined;
  yandexGeosuggestConfigured?: boolean | undefined;
  dgisConfigured?: boolean | undefined;
  yandexTrafficAvailable: boolean;
  // The admin page is off by default and listed in the header only when it is
  // explicitly asked for as well: reaching it by URL and advertising it are
  // separate decisions.
  adminEnabled: boolean;
  adminInMenu: boolean;
  demoMode: boolean;
  configured: boolean;
  error?: string | undefined;
};

/**
 * "Dark theme MapGL JS API" from the public 2GIS style catalogue
 * (https://styles.2gis.com/). MapGL has no built-in dark scheme — a dark map is
 * always a style referenced by id — so this is the default the app ships with.
 * Override it with a style of your own from the 2GIS Style Editor, or set the
 * variable to "off" to keep the 2GIS map light in the dark theme.
 */
export const TWO_GIS_DARK_STYLE_ID = "aa9d5d91-fd85-461f-a57f-4dc5e47348ec";

function darkStyleId(raw: string | undefined): string | undefined {
  const value = raw?.trim();
  if (value?.toLocaleLowerCase() === "off") return undefined;
  return value || TWO_GIS_DARK_STYLE_ID;
}

const LOOPBACK_HOST = /^(localhost|127(\.\d{1,3}){3}|\[?::1\]?)$/i;
// RFC 1918 plus link-local: the address ranges a development machine can hold
// on a home or office network.
const PRIVATE_HOST = /^(10(\.\d{1,3}){3}|192\.168(\.\d{1,3}){2}|172\.(1[6-9]|2\d|3[01])(\.\d{1,3}){2}|169\.254(\.\d{1,3}){2})$/;

function isLoopbackHost(hostname: string): boolean {
  return LOOPBACK_HOST.test(hostname);
}

// nip.io and sslip.io map a dotted or dashed address into a hostname that
// resolves straight back to it, which is how the LAN gets a name that
// certificate- and referer-based restrictions accept.
const WILDCARD_DNS_HOST = /(^|\.)(nip\.io|sslip\.io)$/i;

function isLocalNetworkHost(hostname: string): boolean {
  return isLoopbackHost(hostname)
    || PRIVATE_HOST.test(hostname)
    || hostname.endsWith(".local")
    || WILDCARD_DNS_HOST.test(hostname);
}

/**
 * A dev server started on a loopback API URL is still reachable from other
 * devices on the same network. On those devices "localhost" is the device
 * itself, not the machine running the API, so the host that served the page is
 * the only correct one to call. Public hosts are never rewritten.
 */
function resolveEdgeApiBaseUrl(configured: string): string {
  if (typeof window === "undefined" || !window.location) return configured;
  try {
    const url = new URL(configured);
    const pageHost = window.location.hostname;
    if (!isLoopbackHost(url.hostname) || !pageHost || isLoopbackHost(pageHost)) return configured;
    url.hostname = pageHost;
    return url.toString().replace(/\/$/, "");
  } catch {
    return configured;
  }
}

const HttpsUrlSchema = z.url().refine((value) => {
  const url = new URL(value);
  return url.protocol === "https:" || (url.protocol === "http:" && isLocalNetworkHost(url.hostname));
}, "The edge API URL must use HTTPS (HTTP is allowed only on the local network)");

type PublicRuntimeEnvironment = {
  edgeApiBaseUrl?: string | undefined;
  yandexMapsBrowserKey?: string | undefined;
  twoGisMapGLBrowserKey?: string | undefined;
  twoGisMapGLDarkStyleId?: string | undefined;
  providerMode?: "stub" | "yandex" | "2gis" | undefined;
  addressProviderMode?: "auto" | "yandex" | "2gis" | undefined;
  yandexGeocoderConfigured?: boolean | undefined;
  yandexGeosuggestConfigured?: boolean | undefined;
  dgisConfigured?: boolean | undefined;
  yandexTrafficAvailable: boolean;
  adminEnabled: boolean;
  adminInMenu: boolean;
  demoMode: boolean;
};

function providerMode(raw: string | undefined): PublicRuntimeEnvironment["providerMode"] {
  const value = raw?.trim().toLocaleLowerCase();
  return value === "stub" || value === "yandex" || value === "2gis" ? value : undefined;
}

function addressProviderMode(raw: string | undefined): PublicRuntimeEnvironment["addressProviderMode"] {
  const value = raw?.trim().toLocaleLowerCase();
  return value === "auto" || value === "yandex" || value === "2gis" ? value : undefined;
}

function configuredFlag(raw: string | undefined): boolean | undefined {
  if (raw === undefined) return undefined;
  return raw.trim().toLocaleLowerCase() === "true";
}

function runtimeEnvironmentValue(name: string): string | undefined {
  return (process.env as Record<string, string | undefined>)[name];
}

function publicRuntimeEnvironment(): PublicRuntimeEnvironment {
  if (typeof document !== "undefined") {
    const root = document.documentElement;
    if (root.dataset.greenrouteRuntimeConfig === "true") {
      const edgeApiBaseUrl = root.dataset.greenrouteEdgeApiBaseUrl?.trim();
      const yandexMapsBrowserKey = root.dataset.greenrouteYandexMapsBrowserKey?.trim();
      const twoGisMapGLBrowserKey = root.dataset.greenrouteDgisMapglBrowserKey?.trim();
      const twoGisMapGLDarkStyleId = darkStyleId(root.dataset.greenrouteDgisMapglDarkStyle);
      const mode = providerMode(root.dataset.greenrouteProviderMode);
      const addressMode = addressProviderMode(root.dataset.greenrouteAddressProviderMode);
      const yandexGeocoderConfigured = configuredFlag(root.dataset.greenrouteYandexGeocoderConfigured);
      const yandexGeosuggestConfigured = configuredFlag(root.dataset.greenrouteYandexGeosuggestConfigured);
      const dgisConfigured = configuredFlag(root.dataset.greenrouteDgisConfigured);
      const yandexTrafficAvailable = configuredFlag(root.dataset.greenrouteYandexTrafficAvailable) ?? false;
      return {
        demoMode: root.dataset.greenrouteDemoMode === "true",
        yandexTrafficAvailable,
        adminEnabled: configuredFlag(root.dataset.greenrouteAdminEnabled) ?? false,
        adminInMenu: configuredFlag(root.dataset.greenrouteAdminInMenu) ?? false,
        ...(edgeApiBaseUrl ? { edgeApiBaseUrl } : {}),
        ...(yandexMapsBrowserKey ? { yandexMapsBrowserKey } : {}),
        ...(twoGisMapGLBrowserKey ? { twoGisMapGLBrowserKey } : {}),
        ...(twoGisMapGLDarkStyleId ? { twoGisMapGLDarkStyleId } : {}),
        ...(mode ? { providerMode: mode } : {}),
        ...(addressMode ? { addressProviderMode: addressMode } : {}),
        ...(yandexGeocoderConfigured !== undefined ? { yandexGeocoderConfigured } : {}),
        ...(yandexGeosuggestConfigured !== undefined ? { yandexGeosuggestConfigured } : {}),
        ...(dgisConfigured !== undefined ? { dgisConfigured } : {}),
      };
    }
  }

  // Dynamic lookup is intentional: Next.js otherwise freezes NEXT_PUBLIC_* at
  // image build time. The server renders these public values into the HTML.
  const edgeApiBaseUrl = runtimeEnvironmentValue("NEXT_PUBLIC_EDGE_API_BASE_URL")?.trim();
  const yandexMapsBrowserKey = runtimeEnvironmentValue("NEXT_PUBLIC_YANDEX_MAPS_API_KEY")?.trim();
  const twoGisMapGLBrowserKey = runtimeEnvironmentValue("NEXT_PUBLIC_2GIS_MAPGL_API_KEY")?.trim();
  const twoGisMapGLDarkStyleId = darkStyleId(runtimeEnvironmentValue("NEXT_PUBLIC_2GIS_MAPGL_DARK_STYLE_ID"));
  const mode = providerMode(runtimeEnvironmentValue("GREENROUTE_PROVIDER_MODE"));
  const addressMode = addressProviderMode(runtimeEnvironmentValue("GREENROUTE_ADDRESS_PROVIDER_MODE"));
  const yandexGeocoderConfigured = configuredFlag(runtimeEnvironmentValue("GREENROUTE_YANDEX_GEOCODER_CONFIGURED"));
  const yandexGeosuggestConfigured = configuredFlag(runtimeEnvironmentValue("GREENROUTE_YANDEX_GEOSUGGEST_CONFIGURED"));
  const dgisConfigured = configuredFlag(runtimeEnvironmentValue("GREENROUTE_DGIS_CONFIGURED"));
  const yandexTrafficAvailable = configuredFlag(runtimeEnvironmentValue("GREENROUTE_YANDEX_TRAFFIC_AVAILABLE")) ?? false;
  return {
    demoMode: runtimeEnvironmentValue("NEXT_PUBLIC_DEMO_MODE") === "true",
    yandexTrafficAvailable,
    adminEnabled: configuredFlag(runtimeEnvironmentValue("GREENROUTE_ADMIN_ENABLED")) ?? false,
    adminInMenu: configuredFlag(runtimeEnvironmentValue("GREENROUTE_ADMIN_IN_MENU")) ?? false,
    ...(edgeApiBaseUrl ? { edgeApiBaseUrl } : {}),
    ...(yandexMapsBrowserKey ? { yandexMapsBrowserKey } : {}),
    ...(twoGisMapGLBrowserKey ? { twoGisMapGLBrowserKey } : {}),
    ...(twoGisMapGLDarkStyleId ? { twoGisMapGLDarkStyleId } : {}),
    ...(mode ? { providerMode: mode } : {}),
    ...(addressMode ? { addressProviderMode: addressMode } : {}),
    ...(yandexGeocoderConfigured !== undefined ? { yandexGeocoderConfigured } : {}),
    ...(yandexGeosuggestConfigured !== undefined ? { yandexGeosuggestConfigured } : {}),
    ...(dgisConfigured !== undefined ? { dgisConfigured } : {}),
  };
}

export function getRuntimeConfig(): RuntimeConfig {
  const runtime = publicRuntimeEnvironment();
  const configuredBase = runtime.edgeApiBaseUrl?.replace(/\/$/, "");
  const rawBase = configuredBase ? resolveEdgeApiBaseUrl(configuredBase) : configuredBase;
  const browserKey = runtime.yandexMapsBrowserKey;
  const twoGisMapGLBrowserKey = runtime.twoGisMapGLBrowserKey;
  const twoGisMapGLDarkStyleId = runtime.twoGisMapGLDarkStyleId;
  const demoMode = runtime.demoMode;
  const providerDetails = {
    ...(runtime.providerMode ? { providerMode: runtime.providerMode } : {}),
    ...(runtime.addressProviderMode ? { addressProviderMode: runtime.addressProviderMode } : {}),
    ...(runtime.yandexGeocoderConfigured !== undefined ? { yandexGeocoderConfigured: runtime.yandexGeocoderConfigured } : {}),
    ...(runtime.yandexGeosuggestConfigured !== undefined ? { yandexGeosuggestConfigured: runtime.yandexGeosuggestConfigured } : {}),
    ...(runtime.dgisConfigured !== undefined ? { dgisConfigured: runtime.dgisConfigured } : {}),
    yandexTrafficAvailable: runtime.yandexTrafficAvailable,
    adminEnabled: runtime.adminEnabled,
    // Listing it without enabling it would link to a 404.
    adminInMenu: runtime.adminEnabled && runtime.adminInMenu,
  };

  if (demoMode) {
    return {
      demoMode: true,
      configured: true,
      ...(browserKey ? { yandexMapsBrowserKey: browserKey } : {}),
      ...(twoGisMapGLBrowserKey ? { twoGisMapGLBrowserKey } : {}),
      ...(twoGisMapGLDarkStyleId ? { twoGisMapGLDarkStyleId } : {}),
      ...(rawBase ? { edgeApiBaseUrl: rawBase } : {}),
      ...providerDetails,
    };
  }

  if (!rawBase) {
    return {
      demoMode: false,
      configured: false,
      error: "NEXT_PUBLIC_EDGE_API_BASE_URL is not configured",
      ...(browserKey ? { yandexMapsBrowserKey: browserKey } : {}),
      ...(twoGisMapGLBrowserKey ? { twoGisMapGLBrowserKey } : {}),
      ...(twoGisMapGLDarkStyleId ? { twoGisMapGLDarkStyleId } : {}),
      ...providerDetails,
    };
  }

  const parsed = HttpsUrlSchema.safeParse(rawBase);
  if (!parsed.success) {
    return {
      demoMode: false,
      configured: false,
      error: parsed.error.issues[0]?.message ?? "Invalid edge API URL",
      ...(browserKey ? { yandexMapsBrowserKey: browserKey } : {}),
      ...(twoGisMapGLBrowserKey ? { twoGisMapGLBrowserKey } : {}),
      ...(twoGisMapGLDarkStyleId ? { twoGisMapGLDarkStyleId } : {}),
      ...providerDetails,
    };
  }

  return {
    edgeApiBaseUrl: parsed.data,
    demoMode: false,
    configured: true,
    ...(browserKey ? { yandexMapsBrowserKey: browserKey } : {}),
    ...(twoGisMapGLBrowserKey ? { twoGisMapGLBrowserKey } : {}),
    ...(twoGisMapGLDarkStyleId ? { twoGisMapGLDarkStyleId } : {}),
    ...providerDetails,
  };
}
