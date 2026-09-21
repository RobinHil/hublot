import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// The base path is whatever GitHub Pages says it is: a project site lives under
// /<repo>/, a user site and a custom domain at the root. The workflow reads it
// from the Pages configuration and passes it here, so moving the site never
// means editing this file.
// Pages reports a project site's base path as "/repo", with no trailing slash,
// and everything downstream joins strings to it: "/repo" + "hublot.svg" is
// "/repohublot.svg". So it is normalised once, here and in the prerender, and
// nowhere else has to remember.
const base = withTrailingSlash(process.env.SITE_BASE ?? "/");

function withTrailingSlash(path: string): string {
  return path.endsWith("/") ? path : `${path}/`;
}

export default defineConfig({
  base,
  plugins: [react(), tailwindcss()],
  build: {
    // The page is small enough that a single chunk beats a waterfall.
    assetsInlineLimit: 4096,
  },
});
