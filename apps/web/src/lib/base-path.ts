/**
 * Prefix for everything served from `public/`.
 *
 * GitHub Pages serves a project site from `/<repo>/`, so absolute asset paths
 * that work locally 404 there. Next rewrites its own routes and `next/image`
 * sources, but not strings we hand to metadata, the manifest or CSS, which is
 * what this is for. Empty in every normal deployment.
 */
export const BASE_PATH = (process.env.NEXT_PUBLIC_BASE_PATH ?? "").replace(/\/$/, "");

export function asset(path: string): string {
  return `${BASE_PATH}${path}`;
}
