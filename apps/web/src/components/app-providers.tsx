"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Component, type ErrorInfo, type ReactNode, useEffect, useState } from "react";
import { LocaleProvider } from "@/lib/i18n";

class ClientErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    if (process.env.NODE_ENV !== "production") console.error(error, info);
  }

  render() {
    if (this.state.failed) {
      return (
        <main className="fatal-error" role="alert">
          <p className="eyebrow">Пробок.Нет</p>
          <h1>Не удалось открыть интерфейс</h1>
          <p>Обновите страницу. Если ошибка повторяется, сообщите идентификатор времени службе поддержки.</p>
          <button className="button button-primary" type="button" onClick={() => window.location.reload()}>
            Обновить
          </button>
        </main>
      );
    }
    return this.props.children;
  }
}

export function AppProviders({ children }: { children: ReactNode }) {
  const [queryClient] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            networkMode: "always",
            staleTime: 30_000,
            gcTime: 5 * 60_000,
            retry: (count, error) => count < 2 && !(error instanceof Error && error.message.includes("401")),
            refetchOnWindowFocus: false,
          },
        },
      }),
  );

  useEffect(() => {
    document.documentElement.dataset.greenrouteHydrated = "true";
    return () => { delete document.documentElement.dataset.greenrouteHydrated; };
  }, []);

  return (
    <ClientErrorBoundary>
      <QueryClientProvider client={queryClient}>
        <LocaleProvider>{children}</LocaleProvider>
      </QueryClientProvider>
    </ClientErrorBoundary>
  );
}
