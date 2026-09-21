import { StrictMode } from "react";
import { hydrateRoot } from "react-dom/client";

import { App } from "./App";
import "./styles.css";

// The markup is prerendered, so the client hydrates it rather than replacing
// it: the page reads the same with JavaScript disabled.
const root = document.getElementById("root");
if (root) {
  hydrateRoot(
    root,
    <StrictMode>
      <App />
    </StrictMode>,
  );
}
