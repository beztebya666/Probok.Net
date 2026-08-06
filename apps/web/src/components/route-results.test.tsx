import { render, screen } from "@testing-library/react";
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
    await userEvent.click(fastest.querySelector("button")!);
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

    expect(screen.queryByTestId("route-card-route-fastest")).not.toBeInTheDocument();
    expect(screen.queryByText("Самый быстрый")).not.toBeInTheDocument();
  });

  it("proves what the search found with a green podium even when strict mode selects nothing", async () => {
    createDemoSearch(request);
    const result = getDemoSearch(`demo-${request.requestId}`, true);
    result.selectedRoute = null;
    const preview = vi.fn();

    render(
      <LocaleProvider>
        <RouteResults result={result} routingMode="STRICT_GREEN" onSelect={vi.fn()} onPreview={preview} />
      </LocaleProvider>,
    );

    const podium = screen.getByTestId("green-podium");
    expect(podium).toHaveTextContent("Топ-3 по зелени");
    const first = screen.getByTestId("green-rank-1");
    expect(first).toHaveTextContent(/%\s*времени по зелёному/);

    await userEvent.click(first);
    expect(preview).toHaveBeenCalledTimes(1);
    expect(preview.mock.calls[0]![0]).toEqual(expect.any(String));
  });

  it("orders the podium by green share, best first", () => {
    createDemoSearch(request);
    const result = getDemoSearch(`demo-${request.requestId}`, true);

    render(<LocaleProvider><RouteResults result={result} routingMode="GREENEST" onSelect={vi.fn()} /></LocaleProvider>);

    const shares = [1, 2, 3]
      .map((rank) => screen.queryByTestId(`green-rank-${rank}`))
      .filter((row): row is HTMLElement => Boolean(row))
      .map((row) => Number(/(\d+)%/.exec(row.querySelector("strong")?.textContent ?? "")?.[1] ?? "0"));
    expect(shares.length).toBeGreaterThan(1);
    expect([...shares].sort((a, b) => b - a)).toEqual(shares);
  });
});
