"use client";

import Image from "next/image";
import Link from "next/link";
import { useMemo } from "react";
import { asset } from "@/lib/base-path";
import { useLocale } from "@/lib/i18n";
import { getRuntimeConfig } from "@/lib/runtime-config";
import { useResolvedTheme } from "@/lib/use-resolved-theme";
import { ActivityIcon, ShieldIcon } from "./icons";
import { ThemeToggle } from "./theme-toggle";

export function AppHeader({ demoMode }: { demoMode?: boolean }) {
  const { locale, setLocale, t } = useLocale();
  const resolvedTheme = useResolvedTheme();
  const adminInMenu = useMemo(() => getRuntimeConfig().adminInMenu, []);

  return (
    <header className="app-header">
      <Link className="brand" href="/" aria-label={t("brandName")}>
        <Image
          className="brand-mark"
          src={asset(resolvedTheme === "dark" ? "/brand/logo-dark-192.png" : "/brand/logo-light-192.png")}
          alt=""
          width={36}
          height={36}
          priority
        />
        <strong>{t("brandName")}</strong>
      </Link>
      <nav className="header-actions" aria-label="Utility navigation">
        {demoMode && <span className="demo-badge"><ActivityIcon />{t("demo")}</span>}
        <ThemeToggle />
        <div className="language-switch" role="group" aria-label={t("language")}>
          <button type="button" className={locale === "ru" ? "is-active" : ""} aria-pressed={locale === "ru"} onClick={() => setLocale("ru")}>RU</button>
          <button type="button" className={locale === "en" ? "is-active" : ""} aria-pressed={locale === "en"} onClick={() => setLocale("en")}>EN</button>
        </div>
        {adminInMenu && <Link className="header-link" href="/admin" title={t("admin")}><ShieldIcon /><span>{t("admin")}</span></Link>}
      </nav>
    </header>
  );
}
