import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { geosuggest } from "@/lib/api-client";
import type { GeoSuggestion } from "@/lib/schemas";
import { AppProviders } from "./app-providers";
import { LocationField, type LocationValue } from "./location-field";

vi.mock("@/lib/api-client", () => ({ geosuggest: vi.fn() }));

const moscowFirst: GeoSuggestion = {
  id: "moscow-tverskaya-1",
  label: "Тверская улица, 1",
  subtitle: "Москва",
  point: { latitude: 55.75752, longitude: 37.61388 },
};

const moscowSecond: GeoSuggestion = {
  id: "moscow-tverskaya-2",
  label: "Тверская улица, 2",
  subtitle: "Москва",
  point: { latitude: 55.75801, longitude: 37.61331 },
};

function Harness() {
  const [value, setValue] = useState<LocationValue>({ label: "" });
  return (
    <AppProviders>
      <LocationField label="Откуда" value={value} onChange={setValue} />
      <output data-testid="point">
        {value.point ? `${value.point.latitude},${value.point.longitude}` : "unresolved"}
      </output>
    </AppProviders>
  );
}

describe("LocationField suggestion resolution", () => {
  beforeEach(() => {
    vi.mocked(geosuggest).mockReset();
  });

  it("selects the first API suggestion on Enter without a mouse click", async () => {
    vi.mocked(geosuggest).mockResolvedValue([moscowFirst, moscowSecond]);
    const user = userEvent.setup();
    render(<Harness />);

    const input = screen.getByRole("combobox", { name: "Откуда" });
    await user.type(input, "Тверская 1");
    await user.keyboard("{Enter}");

    await waitFor(() => expect(input).toHaveValue(moscowFirst.label));
    expect(screen.getByTestId("point")).toHaveTextContent("55.75752,37.61388");
    expect(geosuggest).toHaveBeenCalledWith("Тверская 1", expect.stringMatching(/^(ru|en)$/), expect.any(AbortSignal));
  });

  it("does not apply a stale suggestion after the user changes the query", async () => {
    let releaseFirst!: (value: GeoSuggestion[]) => void;
    const firstResponse = new Promise<GeoSuggestion[]>((resolve) => { releaseFirst = resolve; });
    vi.mocked(geosuggest).mockImplementation((query) =>
      query === "Тверская" ? firstResponse : Promise.resolve([moscowSecond]),
    );
    const user = userEvent.setup();
    render(<Harness />);

    const input = screen.getByRole("combobox", { name: "Откуда" });
    await user.type(input, "Тверская");
    await user.keyboard("{Enter}");
    await waitFor(() => expect(geosuggest).toHaveBeenCalledWith("Тверская", expect.stringMatching(/^(ru|en)$/), expect.any(AbortSignal)));

    await user.clear(input);
    await user.type(input, "Тверская 2");
    releaseFirst([moscowFirst]);

    await waitFor(() => expect(input).toHaveValue("Тверская 2"));
    expect(screen.getByTestId("point")).toHaveTextContent("unresolved");

    await user.keyboard("{Enter}");
    await waitFor(() => expect(input).toHaveValue(moscowSecond.label));
    expect(screen.getByTestId("point")).toHaveTextContent("55.75801,37.61331");
  });

  it("keeps the address unresolved when the API has no suggestions", async () => {
    vi.mocked(geosuggest).mockResolvedValue([]);
    const user = userEvent.setup();
    render(<Harness />);

    const input = screen.getByRole("combobox", { name: "Откуда" });
    await user.type(input, "Неизвестный адрес");
    await user.keyboard("{Enter}");

    await waitFor(() => expect(geosuggest).toHaveBeenCalled());
    expect(input).toHaveValue("Неизвестный адрес");
    expect(screen.getByTestId("point")).toHaveTextContent("unresolved");
  });
});
