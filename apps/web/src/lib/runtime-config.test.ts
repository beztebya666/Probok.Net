import { afterEach, describe, expect, it, vi } from "vitest";
import { getRuntimeConfig, TWO_GIS_DARK_STYLE_ID } from "./runtime-config";

describe("runtime configuration", () => {
  afterEach(() => {
    vi.unstubAllEnvs();
    for (const attribute of [...document.documentElement.attributes]) {
      if (attribute.name.startsWith("data-greenroute-")) document.documentElement.removeAttribute(attribute.name);
    }
  });

  it("fails closed when edge API is absent", () => {
    vi.stubEnv("NEXT_PUBLIC_DEMO_MODE", "false");
    vi.stubEnv("NEXT_PUBLIC_EDGE_API_BASE_URL", "");
    expect(getRuntimeConfig()).toMatchObject({ configured: false, demoMode: false, yandexTrafficAvailable: false });
  });

  it("does not allow plaintext remote edge URLs", () => {
    vi.stubEnv("NEXT_PUBLIC_DEMO_MODE", "false");
    vi.stubEnv("NEXT_PUBLIC_EDGE_API_BASE_URL", "http://edge.example.com");
    expect(getRuntimeConfig()).toMatchObject({ configured: false, demoMode: false });
  });

  it("allows fixtures only after explicit opt-in", () => {
    vi.stubEnv("NEXT_PUBLIC_DEMO_MODE", "true");
    vi.stubEnv("NEXT_PUBLIC_EDGE_API_BASE_URL", "");
    expect(getRuntimeConfig()).toMatchObject({ configured: true, demoMode: true });
  });

  it("prefers runtime values rendered by the server over build-time public variables", () => {
    vi.stubEnv("NEXT_PUBLIC_DEMO_MODE", "true");
    vi.stubEnv("NEXT_PUBLIC_EDGE_API_BASE_URL", "https://build.example.com");
    document.documentElement.dataset.greenrouteRuntimeConfig = "true";
    document.documentElement.dataset.greenrouteDemoMode = "false";
    document.documentElement.dataset.greenrouteEdgeApiBaseUrl = "https://runtime.example.com/";
    document.documentElement.dataset.greenrouteYandexMapsBrowserKey = "browser-public-key";
    document.documentElement.dataset.greenrouteDgisMapglBrowserKey = "dgis-browser-public-key";
    document.documentElement.dataset.greenrouteProviderMode = "2gis";
    document.documentElement.dataset.greenrouteAddressProviderMode = "auto";
    document.documentElement.dataset.greenrouteYandexGeocoderConfigured = "true";
    document.documentElement.dataset.greenrouteYandexGeosuggestConfigured = "true";
    document.documentElement.dataset.greenrouteDgisConfigured = "true";
    document.documentElement.dataset.greenrouteYandexTrafficAvailable = "true";

    expect(getRuntimeConfig()).toEqual({
      configured: true,
      adminEnabled: false,
      adminInMenu: false,
      demoMode: false,
      edgeApiBaseUrl: "https://runtime.example.com",
      yandexMapsBrowserKey: "browser-public-key",
      twoGisMapGLBrowserKey: "dgis-browser-public-key",
      twoGisMapGLDarkStyleId: TWO_GIS_DARK_STYLE_ID,
      providerMode: "2gis",
      addressProviderMode: "auto",
      yandexGeocoderConfigured: true,
      yandexGeosuggestConfigured: true,
      dgisConfigured: true,
      yandexTrafficAvailable: true,
    });
  });

  it("exposes only validated provider modes and boolean configuration signals", () => {
    vi.stubEnv("NEXT_PUBLIC_DEMO_MODE", "true");
    vi.stubEnv("GREENROUTE_PROVIDER_MODE", "unexpected");
    vi.stubEnv("GREENROUTE_ADDRESS_PROVIDER_MODE", "YANDEX");
    vi.stubEnv("GREENROUTE_YANDEX_GEOCODER_CONFIGURED", "true");
    vi.stubEnv("GREENROUTE_YANDEX_GEOSUGGEST_CONFIGURED", "false");
    vi.stubEnv("GREENROUTE_YANDEX_TRAFFIC_AVAILABLE", "unexpected");

    expect(getRuntimeConfig()).toMatchObject({
      configured: true,
      addressProviderMode: "yandex",
      yandexGeocoderConfigured: true,
      yandexGeosuggestConfigured: false,
      yandexTrafficAvailable: false,
    });
    expect(getRuntimeConfig()).not.toHaveProperty("providerMode");
  });

  it("uses the catalogue dark style by default and honours an explicit opt-out", () => {
    document.documentElement.dataset.greenrouteRuntimeConfig = "true";
    document.documentElement.dataset.greenrouteDemoMode = "true";
    expect(getRuntimeConfig().twoGisMapGLDarkStyleId).toBe(TWO_GIS_DARK_STYLE_ID);

    document.documentElement.dataset.greenrouteDgisMapglDarkStyle = "my-own-style";
    expect(getRuntimeConfig().twoGisMapGLDarkStyleId).toBe("my-own-style");

    document.documentElement.dataset.greenrouteDgisMapglDarkStyle = "off";
    expect(getRuntimeConfig().twoGisMapGLDarkStyleId, "keeps the 2GIS map light").toBeUndefined();
  });

  it("keeps the admin console off and unlisted unless both flags are set", () => {
    document.documentElement.dataset.greenrouteRuntimeConfig = "true";
    document.documentElement.dataset.greenrouteEdgeApiBaseUrl = "https://runtime.example.com";

    expect(getRuntimeConfig().adminEnabled).toBe(false);
    expect(getRuntimeConfig().adminInMenu).toBe(false);

    // Listing it without enabling it would only link to a 404.
    document.documentElement.dataset.greenrouteAdminInMenu = "true";
    expect(getRuntimeConfig().adminInMenu).toBe(false);

    document.documentElement.dataset.greenrouteAdminEnabled = "true";
    expect(getRuntimeConfig().adminEnabled).toBe(true);
    expect(getRuntimeConfig().adminInMenu).toBe(true);

    // Reachable by URL while staying out of the header.
    document.documentElement.dataset.greenrouteAdminInMenu = "false";
    expect(getRuntimeConfig().adminEnabled).toBe(true);
    expect(getRuntimeConfig().adminInMenu).toBe(false);
  });
});
