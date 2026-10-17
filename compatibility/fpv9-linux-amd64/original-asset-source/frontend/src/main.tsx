import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { App } from "./App";
import "./design-tokens.css";
import "./styles.css";
import "./workspace-v2.css";
import "./workbench-layout.css";

const root = document.getElementById("root");

if (!root) {
  throw new Error("missing #root element");
}

createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
