import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { geosuggest } from "@/lib/api-client";
import { readRoutePresets, rememberRoutePreset, writeRoutePresets, type RoutePresetDraft } from "@/lib/route-presets";
import type { GeoSuggestion, RouteSearchRequest } from "@/lib/schemas";
import { AppProviders } from "./app-providers";
import { RouteForm } from "./route-form";

vi.mock("@/lib/api-client", () => ({ geosuggest: vi.fn() }));

const places: Record<string, GeoSuggestion> = {
  ВДНХ: {
    id: "moscow-vdnh",
    label: "ВДНХ",
    subtitle: "Москва, проспект Мира, 119",
    point: { latitude: 55.8299, longitude: 37.6331 },
  },
  Кремль: {
    id: "moscow-kremlin",
    label: "Московский Кремль",
    subtitle: "Москва",
    point: { latitude: 55.75222, longitude: 37.61556 },
  },
};

function renderForm(
  onSubmit: (request: RouteSearchRequest) => void | Promise<void>,
  trafficProvider: "off" | "yandex" | "2gis" = "off",
  onTrafficProviderChange = vi.fn(),
) {
  return render(
    <AppProviders>
      <RouteForm
        onSubmit={onSubmit}
        busy={false}
        configured
        trafficProvider={trafficProvider}
        onTrafficProviderChange={onTrafficProviderChange}
      />
    </AppProviders>,
  );
}

describe("RouteForm suggestion resolution", () => {
  beforeEach(() => {
    vi.mocked(geosuggest).mockReset();
    const values = new Map<string, string>();
    Object.defineProperty(window, "localStorage", {
      configurable: true,
      value: {
        get length() { return values.size; },
        clear: () => values.clear(),
        getItem: (key: string) => values.get(key) ?? null,
        key: (index: number) => [...values.keys()][index] ?? null,
        removeItem: (key: string) => { values.delete(key); },
        setItem: (key: string, value: string) => { values.set(key, value); },
      } satisfies Storage,
    });
    window.localStorage.setItem("greenroute.locale", "ru");
  });

  it("resolves both typed addresses from first API suggestions during submit", async () => {
    vi.mocked(geosuggest).mockImplementation((query) => Promise.resolve(places[query] ? [places[query]] : []));
    const onSubmit = vi.fn<(request: RouteSearchRequest) => void>();
    const user = userEvent.setup();
    const { container } = renderForm(onSubmit);
    const [origin, destination] = screen.getAllByRole("combobox");

    await user.type(origin!, "ВДНХ");
    await user.type(destination!, "Кремль");
    fireEvent.submit(container.querySelector("form")!);

    await waitFor(() => expect(onSubmit).toHaveBeenCalledOnce());
    const request = onSubmit.mock.calls[0]![0];
    expect(request.origin).toEqual(places.ВДНХ!.point);
    expect(request.destination).toEqual(places.Кремль!.point);
    expect(geosuggest).toHaveBeenCalledTimes(2);
  });

  it("does not submit or invent coordinates when an address has no API suggestion", async () => {
    vi.mocked(geosuggest).mockImplementation((query) => Promise.resolve(query === "ВДНХ" ? [places.ВДНХ!] : []));
    const onSubmit = vi.fn<(request: RouteSearchRequest) => void>();
    const user = userEvent.setup();
    const { container } = renderForm(onSubmit);
    const [origin, destination] = screen.getAllByRole("combobox");

    await user.type(origin!, "ВДНХ");
    await user.type(destination!, "Нет такого адреса");
    fireEvent.submit(container.querySelector("form")!);

    await waitFor(() => expect(geosuggest).toHaveBeenCalledTimes(2));
    expect(onSubmit).not.toHaveBeenCalled();
    expect(destination).toHaveAttribute("aria-invalid", "true");
  });

  it("coalesces repeated submit events while suggestions are being resolved", async () => {
    let release!: () => void;
    const gate = new Promise<void>((resolve) => { release = resolve; });
    vi.mocked(geosuggest).mockImplementation(async (query) => {
      await gate;
      return places[query] ? [places[query]] : [];
    });
    const onSubmit = vi.fn<(request: RouteSearchRequest) => void>();
    const user = userEvent.setup();
    const { container } = renderForm(onSubmit);
    const [origin, destination] = screen.getAllByRole("combobox");

    await user.type(origin!, "ВДНХ");
    await user.type(destination!, "Кремль");
    const form = container.querySelector("form")!;
    fireEvent.submit(form);
    fireEvent.submit(form);

    await waitFor(() => expect(geosuggest).toHaveBeenCalledTimes(2));
    release();
    await waitFor(() => expect(onSubmit).toHaveBeenCalledOnce());
  });

  it("stores a resolved route locally for quick reuse without another geocode", async () => {
    vi.mocked(geosuggest).mockImplementation((query) => Promise.resolve(places[query] ? [places[query]] : []));
    const onSubmit = vi.fn<(request: RouteSearchRequest) => void>();
    const user = userEvent.setup();
    const { container } = renderForm(onSubmit);
    const [origin, destination] = screen.getAllByRole("combobox");

    await user.type(origin!, "ВДНХ");
    await user.type(destination!, "Кремль");
    fireEvent.submit(container.querySelector("form")!);

    await waitFor(() => expect(readRoutePresets(window.localStorage)).toHaveLength(1));
    const [saved] = readRoutePresets(window.localStorage);
    expect(saved?.origin).toEqual({ label: places.ВДНХ!.label, point: places.ВДНХ!.point });
    expect(saved?.destination).toEqual({ label: places.Кремль!.label, point: places.Кремль!.point });
    expect(saved?.trafficProvider).toBe("off");
    await user.click(await screen.findByTestId("preset-tab-recent"));
    expect(await screen.findByRole("button", { name: /Заполнить маршрут/ })).toBeVisible();
  });

  it("restores coordinates and settings and persists the favourite toggle", async () => {
    const presetDraft: RoutePresetDraft = {
      origin: { label: places.ВДНХ!.label, point: places.ВДНХ!.point },
      destination: { label: places.Кремль!.label, point: places.Кремль!.point },
      waypoints: [],
      routingMode: "STRICT_GREEN",
      extraDistanceKm: 75,
      extraTimeMinutes: 55,
      avoidTolls: true,
      avoidUnpaved: false,
      trafficProvider: "2gis",
    };
    writeRoutePresets(rememberRoutePreset([], presetDraft, "saved-route", 100), window.localStorage);
    const onSubmit = vi.fn<(request: RouteSearchRequest) => void>();
    const onTrafficProviderChange = vi.fn();
    const user = userEvent.setup();
    const { container } = renderForm(onSubmit, "off", onTrafficProviderChange);

    await user.click(await screen.findByTestId("preset-tab-recent"));
    await user.click(await screen.findByRole("button", { name: /Заполнить маршрут/ }));
    const [origin, destination] = screen.getAllByRole("combobox");
    expect(origin).toHaveValue(places.ВДНХ!.label);
    expect(destination).toHaveValue(places.Кремль!.label);
    expect(screen.getByRole("radio", { name: /Только по зелёному/ })).toBeChecked();
    const ranges = container.querySelectorAll<HTMLInputElement>('input[type="range"]');
    expect(ranges[0]).toHaveValue("75");
    expect(ranges[1]).toHaveValue("55");
    expect(screen.getByRole("checkbox", { name: "Без платных дорог" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Без грунтовых дорог" })).not.toBeChecked();
    expect(onTrafficProviderChange).toHaveBeenCalledWith("2gis");

    const favourite = screen.getByRole("button", { name: /Добавить маршрут .* в избранное/ });
    await user.click(favourite);
    // Starring moves the row out of "recent" and into "favourites".
    expect(screen.queryByRole("button", { name: /Убрать маршрут .* из избранного/ })).not.toBeInTheDocument();
    await user.click(screen.getByTestId("preset-tab-favorites"));
    expect(screen.getByRole("button", { name: /Убрать маршрут .* из избранного/ })).toHaveAttribute("aria-pressed", "true");
    expect(readRoutePresets(window.localStorage)[0]?.favorite).toBe(true);

    fireEvent.submit(container.querySelector("form")!);
    await waitFor(() => expect(onSubmit).toHaveBeenCalledOnce());
    expect(onSubmit.mock.calls[0]![0]).toMatchObject({
      origin: places.ВДНХ!.point,
      destination: places.Кремль!.point,
      routingMode: "STRICT_GREEN",
      maxExtraDistanceMeters: 75_000,
      maxExtraTimeSeconds: 3_300,
      avoidTolls: true,
      avoidUnpaved: false,
    });
    expect(geosuggest).not.toHaveBeenCalled();
  });
});
