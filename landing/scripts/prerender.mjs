// Renders the page to static HTML and writes the small files a published site
// needs. Without this, dist/index.html would be an empty mount point: fine in a
// browser, useless to a crawler and to anyone with JavaScript off.

import { readFile, writeFile, rm } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, "..");
const dist = join(root, "dist");

// Where the site will live. The workflow reads both from the Pages
// configuration rather than hardcoding an address, so a custom domain works
// without touching this file.
const origin = (process.env.SITE_ORIGIN ?? "http://localhost:4173").replace(/\/$/, "");
const base = process.env.SITE_BASE ?? "/";
const canonical = `${origin}${base}`;

const { render } = await import(join(root, ".ssr", "entry-server.js"));
const markup = render();

const template = await readFile(join(dist, "index.html"), "utf8");
if (!template.includes('<div id="root"></div>')) {
  throw new Error("dist/index.html has no empty root div to fill: did the template change?");
}

const head = [
  `<link rel="canonical" href="${canonical}" />`,
  `<meta property="og:type" content="website" />`,
  `<meta property="og:title" content="hublot, a terminal UI for Docker" />`,
  `<meta property="og:description" content="htop for a Docker host: live per-container resource usage, Compose projects with drift detection, and disk pruning that shows what it would destroy before it runs." />`,
  `<meta property="og:url" content="${canonical}" />`,
  `<meta property="og:image" content="${canonical}og.png" />`,
  `<meta name="twitter:card" content="summary_large_image" />`,
  `<meta name="twitter:image" content="${canonical}og.png" />`,
].join("\n    ");

const html = template
  .replace("</head>", `  ${head}\n  </head>`)
  .replace('<div id="root"></div>', `<div id="root">${markup}</div>`);

await writeFile(join(dist, "index.html"), html);

// GitHub Pages serves 404.html for anything that is not there. The page itself
// is a single document, so the friendliest answer is the page.
await writeFile(join(dist, "404.html"), html);

await writeFile(
  join(dist, "robots.txt"),
  `User-agent: *\nAllow: /\nSitemap: ${canonical}sitemap.xml\n`,
);

await writeFile(
  join(dist, "sitemap.xml"),
  `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.w3.org/2000/sitemaps/0.9">
  <url>
    <loc>${canonical}</loc>
    <lastmod>${new Date().toISOString().slice(0, 10)}</lastmod>
  </url>
</urlset>
`,
);

// The server bundle is a build artefact of this script, not part of the site.
await rm(join(root, ".ssr"), { recursive: true, force: true });

console.log(`prerendered ${canonical}`);
