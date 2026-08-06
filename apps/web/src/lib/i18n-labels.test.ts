import { describe, expect, it } from "vitest";
import { reasonLabel, translate, warningLabel } from "./i18n";

describe("provider-facing labels", () => {
  it("prefers the localized warning over a raw provider message", () => {
    expect(warningLabel("PROVIDER_INITIAL_CANDIDATES_UNAVAILABLE", "provider initial candidates unavailable", "ru"))
      .toBe("Провайдер маршрутов временно не вернул исходные варианты.");
  });

  it("explains 2GIS traffic evidence without presenting unknown segments as free", () => {
    expect(reasonLabel("SEGMENT_CONGESTION_UNKNOWN", "ru"))
      .toBe("Загруженность отдельных участков не раскрыта провайдером");
    expect(warningLabel("PROVIDER_SEGMENT_CONGESTION_UNAVAILABLE", undefined, "ru"))
      .toContain("не предоставляет официальную загруженность каждого участка");
  });
});

describe("Russian plural forms", () => {
  it("agrees the noun with the count instead of hardcoding one form", () => {
    const ru = (value: number) => translate("ru", "routesCount", { value });
    expect(ru(0)).toBe("0 вариантов");
    expect(ru(1)).toBe("1 вариант");
    expect(ru(2)).toBe("2 варианта");
    expect(ru(5)).toBe("5 вариантов");
    expect(ru(11)).toBe("11 вариантов");
    expect(ru(21)).toBe("21 вариант");
    expect(ru(22)).toBe("22 варианта");
    expect(translate("en", "routesCount", { value: 1 })).toBe("1 option");
    expect(translate("en", "routesCount", { value: 3 })).toBe("3 options");
  });
});
