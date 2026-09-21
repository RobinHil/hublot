import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// The base path is whatever GitHub Pages says it is: a project site lives under
// /<repo>/, a user site and a custom domain at the root. The workflow reads it
// from the Pages configuration and passes it here, so moving the site never
// means editing this file.
const base = process.env.SITE_BASE ?? "/";

export default defineConfig({
  base,
  plugins: [react(), tailwindcss()],
  build: {
    // The page is small enough that a single chunk beats a waterfall.
    assetsInlineLimit: 4096,
  },
});
