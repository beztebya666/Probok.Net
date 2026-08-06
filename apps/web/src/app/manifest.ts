import type { MetadataRoute } from "next";
import { asset } from "@/lib/base-path";

// The manifest never varies by request, and a static export requires saying so.
export const dynamic = "force-static";

export default function manifest(): MetadataRoute.Manifest {
  return {
    name: "Пробок.Нет",
    short_name: "Пробок.Нет",
    description: "Маршруты с минимальной оценочной загруженностью",
    start_url: asset("/") || "/",
    display: "standalone",
    background_color: "#f5f5f3",
    theme_color: "#ffcc00",
    orientation: "any",
    lang: "ru",
    categories: ["navigation", "travel", "utilities"],
    icons: [
      { src: asset("/brand/logo-light-192.png"), sizes: "192x192", type: "image/png", purpose: "any" },
      { src: asset("/brand/logo-light-512.png"), sizes: "512x512", type: "image/png", purpose: "any" },
      { src: asset("/brand/logo-light-512.png"), sizes: "512x512", type: "image/png", purpose: "maskable" },
    ],
  };
}
