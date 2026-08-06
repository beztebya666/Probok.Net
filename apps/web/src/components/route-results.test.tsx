import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/lib/i18n";
import { createDemoSearch, getDemoSearch } from "@/lib/demo-fixtures";
import type { RouteSearchRequest } from "@/lib/schemas";
import { RouteResults } from "./route-results";

const request: RouteSearchRequest = {
  requestId: "229525e1-3ad4-43da-a3d6-f54ee241e916",
  origin: { latitude: 55.75222, longitude: 37.61556 },
  destination: { latitude: 55.8299, longitude: 37.6331 },
  waypoints: [],
  departureTime: "2026-08-05T12:00:00.000Z",
  routingMode: "GREENEST",
  maxExtraDistanceMeters: 30_000,
  maxExtraDistancePercent: 100,
  maxExtraTimeSeconds: 1_200,
  avoidTolls: false,
  avoidUnpaved: true,
  strictness: 0.82,
  maxProviderRequests: 12,
  searchDeadlineMs: 20_000,
};

describe("RouteResults", () => {
  it("renders recommendation, confidence and lets the user select an alternative", async () => {
    createDemoSearch(request);
    const result = getDemoSearch(`demo-${request.requestId}`, true);
    const select = vi.fn();
    render(<LocaleProvider><RouteResults result={result} selectedCandidateId="route-green" onSelect={select} /></LocaleProvider>);

    expect(screen.getByText("Рекомендуем")).toBeInTheDocument();
    expect(screen.getAllByText(/Высокая|Средняя/).length).toBeGreaterThan(0);
    const fastest = screen.getByTestId("route-card-route-fastest");
    await userEvent.click(within(fastest).getByRole("button", { name: /Выбрать/ }));
    expect(select).toHaveBeenCalledWith("route-fastest");
  });

  it("never exposes the fastest reference as a strict-green result", () => {
    createDemoSearch(request);
    const result = getDemoSearch(`demo-${request.requestId}`, true);
    result.selectedRoute = null;

    render(
      <LocaleProvider>
        <RouteResults result={result} routingMode="STRICT_GREEN" onSelect={vi.fn()} />
      </LocaleProvider>,
    );

    // ADR-014: the fast route is never offered as a strict-green answer.
    expect(screen.queryByText("Самый быстрый")).not.toBeInTheDocument();
    expect(screen.queryByTestId("route-card-route-fastest")).not.toBeInTheDocument();
  });

  it("still lists what the search found when strict mode qualifies nothing", async () => {
    createDemoSearch(request);
    const result = getDemoSearch(`demo-${request.requestId}`, true);
    result.selectedRoute = null;
    const preview = vi.fn();

    render(
      <LocaleProvider>
        <RouteResults result={result} routingMode="STRICT_GREEN" onSelect={vi.fn()} onPreview={preview} />
      </LocaleProvider>,
    );

    const cards = screen.getAllByTestId(/^route-card-/);
    expect(cards.length).toBeGreaterThan(0);
    const share = cards[0]!.querySelector(".route-green-share");
    expect(share).toHaveTextContent(/%\s*времени по зелёному/);

    await userEvent.click(share as HTMLElement);
    expect(preview).toHaveBeenCalledTimes(1);
    expect(preview.mock.calls[0]![0]).toEqual(expect.any(String));
  });

  it("orders the routes by green share, best first, with the reference last", () => {
    createDemoSearch(request);
    const result = getDemoSearch(`demo-${request.requestId}`, true);

    render(<LocaleProvider><RouteResults result={result} routingMode="GREENEST" onSelect={vi.fn()} /></LocaleProvider>);

    const cards = screen.getAllByTestId(/^route-card-/);
    const reference = cards.findIndex((card) => card.textContent?.includes("Самый быстрый"));
    const green = cards
      .filter((card) => !card.textContent?.includes("Самый быстрый"))
      .map((card) => Number(/(\d+)%/.exec(card.querySelector(".route-green-share strong")?.textContent ?? "")?.[1] ?? "0"));
    expect(green.length).toBeGreaterThan(1);
    expect([...green].sort((a, b) => b - a)).toEqual(green);
    // The route being compared against belongs at the end of the comparison.
    if (reference >= 0) expect(reference).toBe(cards.length - 1);
  });
});
