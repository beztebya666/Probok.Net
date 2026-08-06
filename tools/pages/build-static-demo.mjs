/**
 * Builds the GitHub Pages demo of the web app.
 *
 * `output: "export"` refuses to build alongside a proxy (middleware), route
 * handlers that read a Request, or pages pinned to `force-dynamic`. Those are
 * all server concerns the demo does not have: it answers from fixtures in the
 * browser and is served as plain files.
 *
 * Those three files are moved aside for the duration of the build and always
 * put back, including after a failure. Only files move, never directories: a
 * running dev server keeps Windows handles open inside `src/app`, and renaming
 * a watched directory fails with EPERM.
 *
 *   NEXT_PUBLIC_BASE_PATH=/Repo node tools/pages/build-static-demo.mjs
 *
 * Output lands in apps/web/out.
 */
import { execFileSync } from "node:child_process";
import { existsSync, renameSync, rmSync, cpSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join, resolve } from "node:path";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const webRoot = join(repoRoot, "apps", "web");
// Git Bash rewrites a leading "/" argument into a Windows path, so the base
// path is read from the environment first.
const basePath = process.env.NEXT_PUBLIC_BASE_PATH ?? process.argv[2] ?? "";
if (basePath && !basePath.startsWith("/")) throw new Error(`base path must start with "/", got ${basePath}`);

const PARKED_SUFFIX = ".static-export-parked";
const SERVER_ONLY = [
  join(webRoot, "src", "proxy.ts"),
  join(webRoot, "src", "app", "api", "health", "route.ts"),
  join(webRoot, "src", "app", "admin", "page.tsx"),
];

function restore() {
  for (const path of SERVER_ONLY) {
    const parked = `${path}${PARKED_SUFFIX}`;
    if (!existsSync(parked)) continue;
    rmSync(path, { force: true });
    renameSync(parked, path);
  }
}

// An earlier run that was killed mid-build would have left files parked.
restore();

const env = {
  ...process.env,
  NEXT_PUBLIC_STATIC_EXPORT: "true",
  NEXT_PUBLIC_BASE_PATH: basePath,
  NEXT_PUBLIC_DEMO_MODE: "true",
  // The demo carries no credentials of any kind. Without a map key the app
  // falls back to its schematic renderer, which is the honest thing to show.
  NEXT_PUBLIC_YANDEX_MAPS_API_KEY: "",
  NEXT_PUBLIC_2GIS_MAPGL_API_KEY: "",
  NEXT_PUBLIC_EDGE_API_BASE_URL: "",
  GREENROUTE_ADMIN_ENABLED: "false",
  GREENROUTE_ADMIN_IN_MENU: "false",
};

try {
  for (const path of SERVER_ONLY) {
    if (existsSync(path)) renameSync(path, `${path}${PARKED_SUFFIX}`);
  }
  rmSync(join(webRoot, "out"), { recursive: true, force: true });
  execFileSync("npx", ["next", "build", "--turbopack"], {
    cwd: webRoot,
    env,
    stdio: "inherit",
    shell: process.platform === "win32",
  });
} finally {
  restore();
}

const out = join(webRoot, "out");
if (!existsSync(out)) throw new Error("next build produced no out/ directory");

// Without this, Pages runs the output through Jekyll, which drops _next/.
writeFileSync(join(out, ".nojekyll"), "", "utf8");
// Pages serves 404.html for unknown paths; the exported not-found page belongs
// there.
const notFound = join(out, "404", "index.html");
if (existsSync(notFound)) cpSync(notFound, join(out, "404.html"));

console.log(`static demo built in ${out}${basePath ? ` for base path ${basePath}` : ""}`);
