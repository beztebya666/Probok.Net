"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { useMemo } from "react";
import { getAdminOverview, getSession } from "@/lib/api-client";
import { useLocale } from "@/lib/i18n";
import { getRuntimeConfig } from "@/lib/runtime-config";
import { ActivityIcon, AlertIcon, ArrowIcon, RefreshIcon, ShieldIcon } from "./icons";
import { AppHeader } from "./app-header";

export function AdminDashboard() {
  const { locale, t } = useLocale();
  const config = useMemo(() => getRuntimeConfig(), []);
  const session = useQuery({
    queryKey: ["session"],
    queryFn: ({ signal }) => getSession(signal),
    enabled: config.configured && !config.demoMode,
    retry: false,
    staleTime: 30_000,
  });
  const canView = session.data?.roles.includes("admin") === true;
  const overview = useQuery({
    queryKey: ["admin-overview"],
    queryFn: ({ signal }) => getAdminOverview(signal),
    enabled: canView,
    retry: false,
    refetchInterval: 30_000,
  });

  const number = (value: number, maximumFractionDigits = 1) => new Intl.NumberFormat(locale, { maximumFractionDigits }).format(value);
  const quotaCopy = locale === "ru" ? {
    title: "API и лимиты тарифа",
    tariff: "Лимит тарифа",
    browserConfigured: "Ключ есть",
    browserMissing: "Нет ключа",
    activeAddress: "Адреса",
    reserve: "Резерв",
    activeProvider: "Маршруты",
    contractDependent: "По условиям тарифа провайдера",
    yandexDashboard: "Фактическое использование JS API достоверно только в кабинете Яндекса.",
    observed: "Локально наблюдается · с момента запуска экземпляра",
    purpose: "Зачем",
    unused: "Не используется",
    mapAndTraffic: "Карта в браузере",
    trafficIncluded: "Входит в состав MapGL",
    routing: "Маршруты и цвета участков",
    addresses: "Поиск адресов и координат",
    addressesFallback: "Резервный поиск адресов",
    trafficLayer: "Слой пробок на карте",
    paidOnly: "Только на платном тарифе",
    paidOnlyShort: "Платно",
    routingByOther: "Маршруты строит 2ГИС",
    keyMissing: "Нет ключа",
    configured: "Настроено",
    styleFromCatalogue: "Тёмная тема — стиль из каталога 2ГИС",
    suggestUnused: "Подсказки адресов отдаёт Геокодер",
    keyConfiguredUnused: "Не вызывается",
    liveStatus: "по данным адаптера",
    configStatus: "по конфигурации",
    notConnected: "не подключён",
    connected: "подключён",
  } : {
    title: "APIs and plan limits",
    tariff: "Plan limit",
    browserConfigured: "Key set",
    browserMissing: "No key",
    activeAddress: "Addresses",
    reserve: "Fallback",
    activeProvider: "Routing",
    contractDependent: "Subject to the provider plan",
    yandexDashboard: "Only the Yandex dashboard is authoritative for actual JavaScript API usage.",
    observed: "Locally observed · since this instance started",
    purpose: "Purpose",
    unused: "Not used",
    mapAndTraffic: "Browser map",
    trafficIncluded: "Included with MapGL",
    routing: "Routes and segment colours",
    addresses: "Address search and coordinates",
    addressesFallback: "Fallback address search",
    trafficLayer: "Traffic layer on the map",
    paidOnly: "Paid plan only",
    paidOnlyShort: "Paid",
    routingByOther: "Routing is served by 2GIS",
    keyMissing: "No key",
    configured: "Configured",
    styleFromCatalogue: "Dark theme uses a style from the 2GIS catalogue",
    suggestUnused: "Address hints are served by the Geocoder",
    keyConfiguredUnused: "Never called",
    liveStatus: "reported by the adapter",
    configStatus: "from configuration",
    notConnected: "not connected",
    connected: "connected",
  };
  const providerLabel = (provider: string) => {
    switch (provider.trim().toLocaleLowerCase()) {
      case "2gis": return locale === "ru" ? "2ГИС" : "2GIS";
      case "yandex": return locale === "ru" ? "Яндекс" : "Yandex";
      default: return provider;
    }
  };
  const integrationProduct = (id: string, product: string, apiVersion: string) => {
    switch (id) {
      case "yandex-route-details": return locale === "ru" ? `API Получения деталей маршрута ${apiVersion}` : `Route Details API ${apiVersion}`;
      case "yandex-http-geocoder": return locale === "ru" ? `API Геокодера ${apiVersion}` : `HTTP Geocoder API ${apiVersion}`;
      case "2gis-routing": return `Routing API ${apiVersion}`;
      case "2gis-geocoder": return `Geocoder API ${apiVersion}`;
      default: return `${product} ${apiVersion}`;
    }
  };
  const integrationLimit = (id: string) => {
    switch (id) {
      case "yandex-http-geocoder": return locale === "ru" ? "1 000 запросов / сутки" : "1,000 requests / day";
      case "2gis-routing": return locale === "ru" ? "лимиты объектов в сутки и в месяц — в Platform Manager" : "daily and monthly object limits live in Platform Manager";
      case "2gis-geocoder": return locale === "ru" ? "1 000 запросов / месяц · до 600 / мин" : "1,000 requests / month · up to 600 / min";
      default: return quotaCopy.contractDependent;
    }
  };
  const integrations = overview.data?.apiIntegrations ?? [];
  const integrationOf = (id: string) => integrations.find((integration) => integration.id === id);

  type ProviderHealth = { healthy: boolean; label: string; source: string };

  const activeRoutingProvider = (overview.data?.provider ?? "").trim().toLocaleLowerCase().replace("2гис", "2gis").replace("яндекс", "yandex");
  const rawStatus = overview.data?.status.trim().toLocaleUpperCase() ?? "";
  const routingHealthy = ["UP", "HEALTHY", "READY"].includes(rawStatus);

  /**
   * Health for one provider. The routing provider is the only one the adapter
   * actually probes; for the other the honest answer is what the configuration
   * says, and the card labels which of the two it is.
   */
  const providerHealthFor = (provider: "2gis" | "yandex"): ProviderHealth | undefined => {
    if (!overview.data) return undefined;
    if (provider === activeRoutingProvider) {
      return {
        healthy: routingHealthy,
        label: routingHealthy
          ? locale === "ru" ? "работает" : "healthy"
          : ["DOWN", "UNHEALTHY", "NOT_READY"].includes(rawStatus)
            ? locale === "ru" ? "недоступен" : "unavailable"
            : overview.data.status,
        source: quotaCopy.liveStatus,
      };
    }
    const used = integrations.some((integration) => integration.provider.toLocaleLowerCase() === provider)
      || (provider === "yandex" && Boolean(config.yandexMapsBrowserKey))
      || (provider === "2gis" && Boolean(config.twoGisMapGLBrowserKey));
    return { healthy: used, label: used ? quotaCopy.connected : quotaCopy.notConnected, source: quotaCopy.configStatus };
  };

  type ProviderModule = {
    product: string;
    role: string;
    purpose: string;
    limit: string;
    active: boolean;
    // The host this module is actually called at. Naming it leaves no doubt
    // about which product of the provider's catalogue is in use.
    endpoint?: string | undefined;
    note?: string | undefined;
  };
  const routingIntegration = integrationOf("2gis-routing");
  const dgisGeocoder = integrationOf("2gis-geocoder");
  const yandexGeocoder = integrationOf("yandex-http-geocoder");

  // Grouped by provider, because "which product of whose, and what for" is the
  // question this page exists to answer. Products that are deliberately unused
  // stay listed with the reason, so their absence is a decision and not a gap.
  const providerSections: Array<{ provider: string; health?: ProviderHealth | undefined; modules: ProviderModule[] }> = [
    {
      provider: providerLabel("2gis"),
      health: providerHealthFor("2gis"),
      modules: [
        {
          product: routingIntegration
            ? integrationProduct(routingIntegration.id, routingIntegration.product, routingIntegration.apiVersion)
            : "Routing API",
          role: routingIntegration ? quotaCopy.activeProvider : quotaCopy.unused,
          purpose: quotaCopy.routing,
          limit: integrationLimit("2gis-routing"),
          active: Boolean(routingIntegration),
          endpoint: "routing.api.2gis.com",
        },
        {
          product: "MapGL JS API",
          role: config.twoGisMapGLBrowserKey ? quotaCopy.configured : quotaCopy.keyMissing,
          purpose: quotaCopy.mapAndTraffic,
          limit: quotaCopy.contractDependent,
          active: Boolean(config.twoGisMapGLBrowserKey),
          endpoint: "mapgl.2gis.com",
          note: quotaCopy.styleFromCatalogue,
        },
        {
          product: locale === "ru" ? "Слой пробок MapGL" : "MapGL traffic layer",
          role: config.twoGisMapGLBrowserKey ? quotaCopy.configured : quotaCopy.keyMissing,
          purpose: quotaCopy.trafficLayer,
          limit: quotaCopy.trafficIncluded,
          active: Boolean(config.twoGisMapGLBrowserKey),
          endpoint: "mapgl.2gis.com",
        },
        {
          product: dgisGeocoder
            ? integrationProduct(dgisGeocoder.id, dgisGeocoder.product, dgisGeocoder.apiVersion)
            : "Geocoder API",
          role: dgisGeocoder?.role === "fallback" ? quotaCopy.reserve : dgisGeocoder ? quotaCopy.activeAddress : quotaCopy.unused,
          purpose: dgisGeocoder?.role === "fallback" ? quotaCopy.addressesFallback : quotaCopy.addresses,
          limit: integrationLimit("2gis-geocoder"),
          active: Boolean(dgisGeocoder),
          endpoint: "catalog.api.2gis.com",
        },
        {
          product: "Suggest API",
          role: quotaCopy.unused,
          purpose: quotaCopy.suggestUnused,
          limit: integrationLimit("2gis-geocoder"),
          active: false,
        },
      ],
    },
    {
      provider: providerLabel("yandex"),
      health: providerHealthFor("yandex"),
      modules: [
        // Ordered to line up column-for-column with the 2GIS group above:
        // routing, browser map, traffic layer, addresses, suggestions.
        {
          product: "Router API",
          role: quotaCopy.unused,
          purpose: quotaCopy.routingByOther,
          limit: quotaCopy.paidOnly,
          active: false,
        },
        {
          product: "JavaScript API v3",
          role: config.yandexMapsBrowserKey ? quotaCopy.browserConfigured : quotaCopy.browserMissing,
          purpose: quotaCopy.mapAndTraffic,
          limit: locale === "ru" ? "100 запросов / сутки" : "100 requests / day",
          active: Boolean(config.yandexMapsBrowserKey),
          endpoint: "api-maps.yandex.ru",
          note: quotaCopy.yandexDashboard,
        },
        {
          product: locale === "ru" ? "Слой пробок JS API" : "JS API traffic layer",
          role: config.yandexTrafficAvailable ? quotaCopy.configured : quotaCopy.paidOnlyShort,
          purpose: quotaCopy.trafficLayer,
          limit: config.yandexTrafficAvailable ? quotaCopy.contractDependent : quotaCopy.paidOnly,
          active: Boolean(config.yandexTrafficAvailable),
          endpoint: "api-maps.yandex.ru",
        },
        {
          product: yandexGeocoder
            ? integrationProduct(yandexGeocoder.id, yandexGeocoder.product, yandexGeocoder.apiVersion)
            : locale === "ru" ? "API Геокодера v1" : "HTTP Geocoder API v1",
          role: yandexGeocoder?.role === "fallback" ? quotaCopy.reserve : yandexGeocoder ? quotaCopy.activeAddress : quotaCopy.unused,
          purpose: yandexGeocoder?.role === "fallback" ? quotaCopy.addressesFallback : quotaCopy.addresses,
          limit: integrationLimit("yandex-http-geocoder"),
          active: Boolean(yandexGeocoder),
          endpoint: "geocode-maps.yandex.ru",
        },
        {
          product: locale === "ru" ? "API Геосаджеста" : "Suggest API",
          role: quotaCopy.keyConfiguredUnused,
          purpose: quotaCopy.suggestUnused,
          limit: quotaCopy.contractDependent,
          active: false,
        },
      ],
    },
  ];

  // Per-provider status is rendered in each group header by providerHealthFor;
  // only the breaker label still needs the active provider's name.
  const activeProviderLabel = providerLabel(overview.data?.provider ?? "");
  const circuitBreakerLabel = overview.data?.circuitBreaker === "MANAGED_BY_PROVIDER_ADAPTER"
    ? locale === "ru" ? `Управляет ${activeProviderLabel}` : "Provider-managed"
    : overview.data?.circuitBreaker === "CLOSED"
      ? locale === "ru" ? "Работает штатно" : "Closed"
      : overview.data?.circuitBreaker ?? "";
  const rawScoringPolicy = overview.data?.scoringPolicy ?? "";
  const scoringPolicyLabel = rawScoringPolicy
    .replace(/^greenroute[-_]?/i, "")
    .replace(/[-_]+/g, " ");

  return (
    <div className="admin-shell">
      <AppHeader demoMode={config.demoMode} />
      <main className="admin-main" id="main-content">
        <div className="admin-titlebar">
          <div>
            <Link className="text-link" href="/"><ArrowIcon className="arrow-back" />{t("backToRoute")}</Link>
            <h1>{t("adminTitle")}</h1>
          </div>
          {canView && <span className="operator-badge"><ShieldIcon />{session.data?.displayName ?? "Admin"}</span>}
        </div>

        <section className="quota-section" aria-labelledby="api-quotas-title">
          <div className="section-heading">
            <div><p className="eyebrow">Provider contracts</p><h2 id="api-quotas-title">{quotaCopy.title}</h2></div>
            {canView && overview.data && (
              <button
                className="refresh-button"
                type="button"
                onClick={() => void overview.refetch()}
                disabled={overview.isFetching}
                aria-label={t("refresh")}
                title={t("refresh")}
              >
                <RefreshIcon />
              </button>
            )}
          </div>
          {providerSections.map((section) => (
            <div className="provider-group" key={section.provider}>
              <div className="provider-group-head">
                <h3 className="provider-group-title">{section.provider}</h3>
                {section.health && (
                  <span className="provider-group-health">
                    <span className={`status-orb ${section.health.healthy ? "status-up" : "status-unhealthy"}`} />
                    {section.health.label}
                    <small>{section.health.source}</small>
                  </span>
                )}
              </div>
              <div className="quota-grid">
                {section.modules.map((module) => (
                  <article className={`quota-card ${module.active ? "" : "is-inactive"}`} key={`${section.provider}-${module.product}`}>
                    <div className="quota-card-top">
                      <span>{module.product}</span>
                      <span className={module.active ? "quota-state is-configured" : "quota-state"}>{module.role}</span>
                    </div>
                    <dl>
                      <dt>{quotaCopy.purpose}</dt><dd>{module.purpose}</dd>
                      <dt>{quotaCopy.tariff}</dt><dd className="quota-limit">{module.limit}</dd>
                    </dl>
                    {module.endpoint && <p className="quota-endpoint"><code>{module.endpoint}</code></p>}
                    {module.note && <p>{module.note}</p>}
                  </article>
                ))}
              </div>
            </div>
          ))}
        </section>

        {config.demoMode && (
          <section className="telemetry-empty" aria-labelledby="demo-telemetry-title">
            <span className="telemetry-empty-icon"><ActivityIcon /></span>
            <div>
              <p className="eyebrow">Demo mode</p>
              <h2 id="demo-telemetry-title">{t("demoTelemetryTitle")}</h2>
              <p>{t("demoTelemetryBody")}</p>
            </div>
          </section>
        )}

        {!config.configured && !config.demoMode && (
          <div className="admin-access-card notice notice-error" role="alert"><AlertIcon /><div><strong>{t("configurationError")}</strong><p>{t("configBody")}</p></div></div>
        )}

        {config.configured && !config.demoMode && session.isPending && (
          <div className="admin-access-card" role="status"><span className="spinner" aria-hidden="true" /><strong>{t("accessChecking")}</strong></div>
        )}

        {config.configured && !config.demoMode && !session.isPending && !canView && (
          <div className="admin-access-card notice notice-error" role="alert">
            <AlertIcon /><div><strong>{t("accessDenied")}</strong><p>{t("accessDeniedBody")}</p></div>
          </div>
        )}

        {canView && overview.isPending && (
          <div className="admin-access-card" role="status"><span className="spinner" aria-hidden="true" /><strong>{t("operationalData")}</strong></div>
        )}

        {canView && overview.isError && (
          <div className="admin-access-card notice notice-error" role="alert">
            <AlertIcon /><div><strong>{t("configurationError")}</strong><button className="text-button" type="button" onClick={() => void overview.refetch()}>{t("refresh")}</button></div>
          </div>
        )}

        {canView && overview.data && (
          <div className="admin-dashboard">
            <div className="observed-heading"><p className="eyebrow">{quotaCopy.observed}</p><span>{t("operationalData")}</span></div>
            <section className="metric-grid" aria-label={t("operationalData")}>
              <article className="metric-card"><span className="metric-icon"><ActivityIcon /></span><small>{t("requestCount")}</small><strong>{number(overview.data.requestCount, 0)}</strong><p>{locale === "ru" ? "С момента запуска экземпляра" : "Since this instance started"}</p></article>
              <article className="metric-card"><span className="metric-icon"><ActivityIcon /></span><small>{t("estimatedCost")}</small><strong>{number(overview.data.estimatedCost, 2)}</strong><p>{locale === "ru" ? "Локально наблюдаемые единицы" : "Locally observed provider units"}</p></article>
              <article className="metric-card"><span className="metric-icon"><ShieldIcon /></span><small>{t("circuitBreaker")}</small><strong className="metric-value-compact" title={overview.data.circuitBreaker}>{circuitBreakerLabel}</strong><p>{locale === "ru" ? "Защита запросов на стороне адаптера" : "Request protection in the provider adapter"}</p></article>
              <article className="metric-card"><span className="metric-icon"><ActivityIcon /></span><small>{t("scoringPolicy")}</small><strong className="metric-value-compact" title={overview.data.scoringPolicy}>{scoringPolicyLabel}</strong><p title={overview.data.scoringPolicy}>{locale === "ru" ? `Полная версия: ${overview.data.scoringPolicy}` : `Full version: ${overview.data.scoringPolicy}`}</p></article>
              <article className="metric-card"><span className="metric-icon"><AlertIcon /></span><small>{t("degradedPercent")}</small><strong>{number(overview.data.degradedPercent)}%</strong><p>{locale === "ru" ? "С момента запуска экземпляра" : "Since this instance started"}</p></article>
              <article className="metric-card"><span className="metric-icon"><AlertIcon /></span><small>{t("lowConfidence")}</small><strong>{number(overview.data.lowConfidencePercent)}%</strong><p>{locale === "ru" ? "С момента запуска экземпляра" : "Since this instance started"}</p></article>
              <article className="metric-card"><span className="metric-icon"><ActivityIcon /></span><small>{t("budgetExhaustion")}</small><strong>{number(overview.data.searchBudgetExhaustion, 0)}</strong><p>{locale === "ru" ? "Поиски с момента запуска" : "Searches since instance start"}</p></article>
            </section>

            <section className="flags-card" aria-labelledby="feature-flags-title">
              <div><p className="eyebrow">Runtime configuration</p><h2 id="feature-flags-title">{t("featureFlags")}</h2></div>
              <ul>{Object.entries(overview.data.featureFlags).map(([flag, enabled]) => <li key={flag}><code>{flag}</code><span className={`flag-value ${enabled ? "is-enabled" : ""}`}>{enabled ? "ON" : "OFF"}</span></li>)}</ul>
            </section>
          </div>
        )}
      </main>
    </div>
  );
}
