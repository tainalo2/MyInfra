import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { RouterProvider, createBrowserRouter } from "react-router-dom";
import { App } from "./App";
import { detectLang, setLang } from "./i18n";
import { SessionProvider } from "./session";
import { ThemeProvider } from "./theme";
import "./index.css";

setLang(detectLang());

// The routes stay in App; a data router is what lets a page hold a navigation
// back, to offer saving first.
const router = createBrowserRouter([{ path: "*", element: <App /> }]);

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ThemeProvider>
      <SessionProvider>
        <RouterProvider router={router} />
      </SessionProvider>
    </ThemeProvider>
  </StrictMode>,
);
