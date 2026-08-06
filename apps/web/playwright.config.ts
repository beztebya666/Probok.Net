import { defineConfig, devices } from "@playwright/test";

const externalBaseURL = process.env.PLAYWRIGHT_BASE_URL?.trim();
// A demo-mode server that is already running. Next refuses a second dev server
// for the same directory, so this is how the suite runs while someone is
// working on the app; unlike PLAYWRIGHT_BASE_URL it serves the demo fixtures,
// so the whole suite applies.
const demoBaseURL = process.env.PLAYWRIGHT_DEMO_BASE_URL?.trim();
const baseURL = externalBaseURL || demoBaseURL || "http://127.0.0.1:3100";
const chromiumExecutablePath = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH?.trim();

export default defineConfig({
  testDir: "../../tests/e2e",
  ...(externalBaseURL
    ? { testMatch: ["full-stack.spec.ts", "security-pwa.spec.ts"] }
    : {}),
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 2 : 0,
  ...(process.env.CI ? { workers: 2 } : {}),
  reporter: process.env.CI ? [["line"], ["html", { open: "never" }]] : "line",
  use: {
    baseURL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
    locale: "ru-RU",
    timezoneId: "Europe/Moscow",
    channel: "chromium",
    ...(chromiumExecutablePath ? { launchOptions: { executablePath: chromiumExecutablePath } } : {}),
  },
  ...(externalBaseURL || demoBaseURL
    ? {}
    : {
        webServer: {
          command: "npm run dev -- --hostname 127.0.0.1 --port 3100",
          url: "http://127.0.0.1:3100/api/health",
          reuseExistingServer: !process.env.CI,
          timeout: 120_000,
          env: {
            NEXT_PUBLIC_DEMO_MODE: "true",
            NEXT_PUBLIC_YANDEX_MAPS_API_KEY: "",
          },
        },
      }),
  projects: [
    { name: "chromium", use: { ...devices["Desktop Chrome"] } },
    { name: "mobile-chromium", use: { ...devices["Pixel 7"] } },
  ],
});
