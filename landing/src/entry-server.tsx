import { renderToString } from "react-dom/server";

import { App } from "./App";

/**
 * Renders the page to the HTML the build writes into dist/index.html.
 *
 * renderToString rather than renderToStaticMarkup: the client hydrates this
 * markup, and only renderToString emits the markers hydration needs around
 * text nodes. With static markup the page still looks right and then throws a
 * hydration mismatch in the console on every load.
 */
export function render(): string {
  return renderToString(<App />);
}
