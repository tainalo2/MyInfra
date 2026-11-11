import { expect, test, type Page } from "@playwright/test";
import { signIn } from "./screenshots-helpers";

// A machine added by hand, on a made-up server served in place of the API.
const manual = { id: "man1", name: "maison", status: "pending", joinCommand: "curl -fsSL example.org/join | sudo sh", hosting: { provider: "other", name: "Chez moi" } };

async function open(page: Page, server: Record<string, unknown>) {
  await page.route(`**/api/servers/${server.id}`, (route) => route.fulfill({ json: server }));
  await page.route(`**/api/servers/${server.id}/runtime`, (route) => route.fulfill({ json: { measured: false, cpu: { used: 0, total: 0 }, memory: { used: 0, total: 0 }, disk: { used: 0, total: 0 }, pods: [] } }));
  await signIn(page);
  await page.goto(`/serveurs/${server.id}`);
  await expect(page.getByRole("tablist")).toBeVisible();
}

test.describe("server added by hand", () => {
  test("its settings wait in their tab, and keep what is typed", async ({ page }) => {
    await open(page, manual);
    const place = page.getByLabel("Lieu");
    await expect(place).toBeHidden();
    await page.getByRole("tab", { name: "Réglages" }).click();
    await expect(place).toBeVisible();
    const user = page.getByPlaceholder("root");
    await user.fill("debian");
    await page.getByRole("tab", { name: "Historique" }).click();
    await expect(user).toBeHidden();
    await page.getByRole("tab", { name: "Réglages" }).click();
    await expect(user).toHaveValue("debian");
  });

  test("the command shows until the machine has joined", async ({ page }) => {
    await open(page, manual);
    await expect(page.getByText(manual.joinCommand)).toBeVisible();
  });

  test("the command goes once the machine has joined", async ({ page }) => {
    await open(page, { ...manual, status: "linked", address: "203.0.113.10" });
    await expect(page.getByText("203.0.113.10")).toBeVisible();
    await expect(page.getByText(manual.joinCommand)).toHaveCount(0);
    await expect(page.getByRole("tab", { name: "Réglages" })).toBeVisible();
  });

  test("a lost machine offers to reinstall, and only then gives the command", async ({ page }) => {
    const lost = { ...manual, status: "linked", reach: "lost", address: "203.0.113.10" };
    await open(page, lost);
    await expect(page.getByText(manual.joinCommand)).toHaveCount(0);
    await page.route(`**/api/servers/${manual.id}/reinstall`, async (route) => {
      await page.route(`**/api/servers/${manual.id}`, (next) => next.fulfill({ json: { ...manual, address: "203.0.113.10" } }));
      return route.fulfill({ json: { ...manual, address: "203.0.113.10" } });
    });
    await page.getByRole("button", { name: "Réinstaller" }).click();
    await expect(page.getByText(manual.joinCommand)).toBeVisible();
    await expect(page.getByRole("button", { name: "Réinstaller" })).toHaveCount(0);
  });

  test("a lost machine from a provider has nothing to reinstall here", async ({ page }) => {
    await open(page, { ...manual, status: "linked", reach: "lost", providerId: "p1", provider: "hetzner", address: "203.0.113.11" });
    await expect(page.getByRole("button", { name: "Vérifier maintenant" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Réinstaller" })).toHaveCount(0);
  });

  test("a machine bought from a provider has no settings tab", async ({ page }) => {
    await open(page, { ...manual, status: "linked", providerId: "p1", provider: "hetzner", address: "203.0.113.11" });
    await expect(page.getByRole("tab", { name: "Historique" })).toBeVisible();
    await expect(page.getByRole("tab", { name: "Réglages" })).toHaveCount(0);
  });
});
