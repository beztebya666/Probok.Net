import { expect, test } from "@playwright/test";

test("serves a nonce-based CSP and installable manifest", async ({ request }) => {
  const pageResponse = await request.get("/");
  expect(pageResponse.ok()).toBe(true);
  const csp = pageResponse.headers()["content-security-policy"] ?? "";
  const scriptDirective = csp.split(";").find((directive) => directive.trim().startsWith("script-src")) ?? "";
  expect(scriptDirective).toContain("'nonce-");
  expect(scriptDirective).toContain("'strict-dynamic'");
  expect(scriptDirective).toContain("https://mapgl.2gis.com");
  expect(scriptDirective).not.toContain("'unsafe-inline'");
  const workerDirective = csp.split(";").find((directive) => directive.trim().startsWith("worker-src")) ?? "";
  expect(workerDirective).toContain("blob:");
  expect(workerDirective).toContain("data:");
  expect(pageResponse.headers()["cache-control"]).toMatch(/no-store|no-cache/);

  const manifestResponse = await request.get("/manifest.webmanifest");
  expect(manifestResponse.ok()).toBe(true);
  const manifest = await manifestResponse.json();
  expect(manifest).toMatchObject({ name: "Пробок.Нет", display: "standalone", start_url: "/" });
  expect(manifest.icons).toEqual(expect.arrayContaining([expect.objectContaining({ purpose: "maskable" })]));
});
