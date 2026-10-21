import fs from "node:fs";
import path from "node:path";
import { expect, test, type Page } from "@playwright/test";

import { signIn } from "./screenshots-helpers";

const outDir = path.resolve(import.meta.dirname, "../../docs/screenshots");

type Shot = { route: string; file: string; viewport?: { width: number; height: number } };

const desktop: Shot[] = [
  { route: "/", file: "overview.png" },
  { route: "/", file: "overview-selection.png" },
  { route: "/applications", file: "applications.png" },
  { route: "/nouveau", file: "catalogue.png" },
  { route: "/clusters", file: "clusters.png" },
  { route: "/templates", file: "templates.png" },
  { route: "/templates/nouveau", file: "template-editor.png" },
  { route: "/deploiements", file: "template-run.png" },
  { route: "/transfert", file: "transfer.png" },
  { route: "/providers", file: "providers.png" },
  { route: "/dns", file: "dns.png" },
  { route: "/git", file: "registries.png" },
  { route: "/cles", file: "images.png" },
  { route: "/terminal", file: "terminal.png" },
];

const mobile: Shot[] = [
  { route: "/", file: "mobile-overview.png", viewport: { width: 390, height: 844 } },
  { route: "/applications", file: "mobile-application.png", viewport: { width: 390, height: 844 } },
  { route: "/terminal", file: "mobile-terminal.png", viewport: { width: 390, height: 844 } },
];

async function capture(page: Page, shot: Shot) {
  if (shot.viewport) {
    await page.setViewportSize(shot.viewport);
  } else {
    await page.setViewportSize({ width: 1280, height: 800 });
  }
  await page.goto(shot.route);
  await page.waitForLoadState("networkidle");
  await expect(page.locator("main")).toBeVisible();
  await page.screenshot({ path: path.join(outDir, shot.file), fullPage: false });
}

test.describe.configure({ mode: "serial" });

test.describe("regénération des captures", () => {
  test.skip(!process.env.UPDATE_SCREENSHOTS, "définir UPDATE_SCREENSHOTS=1 pour écrire dans docs/screenshots");

  test.beforeAll(() => {
    fs.mkdirSync(outDir, { recursive: true });
  });

  test("écrans bureau", async ({ page }) => {
    await signIn(page);
    for (const shot of desktop) {
      await capture(page, shot);
    }
  });

  test("écrans mobile", async ({ page }) => {
    await signIn(page);
    for (const shot of mobile) {
      await capture(page, shot);
    }
  });
});
