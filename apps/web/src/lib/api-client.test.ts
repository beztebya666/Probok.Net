import { afterEach, describe, expect, it, vi } from "vitest";
import { geosuggest } from "./api-client";

describe("edge API client", () => {
  afterEach(() => {
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
  });

  it.each([
    ["ru", "ru_RU"],
    ["en", "en_US"],
  ] as const)("maps %s locale to provider-neutral edge locale %s", async (locale, expected) => {
    vi.stubEnv("NEXT_PUBLIC_DEMO_MODE", "false");
    vi.stubEnv("NEXT_PUBLIC_EDGE_API_BASE_URL", "https://edge.greenroute.example");
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ suggestions: [] }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    }));
    vi.stubGlobal("fetch", fetchMock);

    await geosuggest("Moscow", locale);

    const requestedUrl = new URL(String(fetchMock.mock.calls[0]?.[0]));
    expect(requestedUrl.searchParams.get("lang")).toBe(expected);
    expect(requestedUrl.searchParams.get("q")).toBe("Moscow");
  });
});
