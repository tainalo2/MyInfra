import { expect, test, type Page } from "@playwright/test";

const password = "e2e-password-123";

// Each page must open without a script error nor a failed call to the API.
function watch(page: Page) {
  const problems: string[] = [];
  page.on("pageerror", (err) => problems.push(`erreur : ${err.message}`));
  page.on("response", (res) => {
    if (res.url().includes("/api/") && res.status() >= 500) problems.push(`HTTP ${res.status()} ${res.url()}`);
  });
  return problems;
}

async function signIn(page: Page) {
  await page.goto("/");
  // The first password is typed twice; signing in asks it once.
  const fields = page.locator('input[type="password"]');
  const shell = page.locator("aside");
  await expect(fields.first().or(shell).first()).toBeVisible();
  if (await fields.first().isVisible()) {
    for (const field of await fields.all()) await field.fill(password);
    await page.locator('button[type="submit"]').click();
    await page.waitForURL((url) => !/\/(depart|entree)$/.test(url.pathname));
  }
  // The guided start is skipped: the tests look at the screens themselves.
  await page.request.post("/api/onboarding", { headers: { Origin: new URL(page.url()).origin } });
}

test.describe.serial("MyInfra", () => {
  test("first password, then the setup screens", async ({ page }) => {
    const problems = watch(page);
    await signIn(page);
    expect(problems).toEqual([]);
  });

  test("every main screen opens", async ({ page }) => {
    const problems = watch(page);
    await signIn(page);
    for (const path of ["/", "/applications", "/nouveau", "/clusters", "/templates", "/templates/nouveau", "/applications/paquets", "/applications/paquets/nouveau", "/deploiements", "/cles", "/assistants", "/providers", "/dns", "/git", "/secrets", "/compte", "/copies", "/historique", "/monde", "/suppressions", "/transfert"]) {
      await page.goto(path);
      await page.waitForLoadState("networkidle");
      await expect(page.locator("main")).toBeVisible();
    }
    expect(problems).toEqual([]);
  });

  test("the setup and sign-in pages send a signed-in person home", async ({ page }) => {
    await signIn(page);
    await page.goto("/depart");
    await page.waitForURL((url) => url.pathname === "/" || url.pathname === "/mise-en-route");
    await page.goto("/entree");
    await page.waitForURL((url) => url.pathname === "/" || url.pathname === "/mise-en-route");
  });

  test("an expired session goes back to the sign-in page", async ({ page, context }) => {
    await context.addCookies([{ name: "myinfra", value: "expired", url: process.env.E2E_URL || "http://127.0.0.1:8080" }]);
    await page.goto("/clusters");
    await page.waitForURL(/\/entree$/);
  });

  test("a machine alone can be created from the clusters page", async ({ page }) => {
    await signIn(page);
    await page.goto("/clusters");
    await expect(page.getByRole("heading", { name: "Machines seules" })).toBeVisible();
    await page.getByRole("button", { name: "Nouvelle machine seule" }).click();
    await expect(page.getByText("Cette machine fait tourner ses applications seule")).toBeVisible();
  });

  test("an image application offers the port only as an option", async ({ page }) => {
    await signIn(page);
    await page.goto("/nouveau");
    await page.getByLabel("Source").selectOption("image");
    await page.getByRole("button", { name: "Réglages" }).click();
    await expect(page.getByPlaceholder("Lu dans l'image")).toBeVisible();
  });

  test("the screens follow the chosen language", async ({ page }) => {
    await signIn(page);
    await page.goto("/clusters");
    await page.locator("header select").first().selectOption("en");
    await expect(page.getByRole("heading", { name: "Standalone machines" })).toBeVisible();
    await page.locator("header select").first().selectOption("fr");
  });

  test("the overview shows the map, switches views and keeps them in the address", async ({ page }) => {
    const problems = watch(page);
    await signIn(page);
    await page.goto("/");
    await expect(page.getByRole("img", { name: /^Carte/ })).toBeVisible();
    await page.getByRole("tab", { name: "Apps" }).click();
    await expect(page).toHaveURL(/vue=apps/);
    await page.getByRole("tab", { name: "Pods" }).click();
    await expect(page).toHaveURL(/vue=pods/);
    await page.goto("/applications");
    await expect(page.getByRole("heading", { name: "Applications" })).toBeVisible();
    expect(problems).toEqual([]);
  });

  test("the overview links an app to its servers and flags what needs attention", async ({ page }) => {
    await page.route("**/api/overview", (route) => route.fulfill({ json: overviewFixture }));
    await signIn(page);
    await page.goto("/?vue=apps");
    const attention = page.getByRole("region", { name: "À traiter" });
    await expect(attention.getByText("wiki est en échec")).toBeVisible();
    await expect(attention.getByText("metube : tous les pods sur SW1")).toBeVisible();
    await page.getByRole("button", { name: /^metube En ligne/ }).click();
    await expect(page).toHaveURL(/sel=app%3Aa1|sel=app:a1/);
    const card = page.getByRole("region", { name: "Sélection" });
    await expect(card.getByText("Toutes les copies de cette app ont été placées sur SW1")).toBeVisible();
    await card.getByRole("button", { name: /SW1/ }).click();
    await expect(page).toHaveURL(/sel=server/);
    await expect(card.getByText("Héberge")).toBeVisible();
    await page.goBack();
    await expect(page).toHaveURL(/sel=app/);
    await expect(page.getByRole("region", { name: "Chiffres clés" }).getByText("1/2 en ligne")).toBeVisible();
  });

  test("the search goes to a page from anywhere", async ({ page }) => {
    await signIn(page);
    await page.goto("/clusters");
    // The shortcut is heard once the interface is up: a slow runner gets there later.
    await expect(page.getByRole("button", { name: "Rechercher une app, un serveur ou une page" })).toBeVisible();
    await page.keyboard.press("ControlOrMeta+k");
    const dialog = page.getByRole("dialog", { name: "Recherche" });
    await expect(dialog).toBeVisible();
    await dialog.getByRole("combobox").fill("templ");
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(/\/templates$/);
  });

  test("an access token for an assistant is shown once, plugs in, and is revoked", async ({ page }) => {
    const problems = watch(page);
    await signIn(page);
    await page.goto("/assistants");
    await expect(page.getByRole("heading", { name: "Assistants IA (MCP)" })).toBeVisible();
    await expect(page.locator("code").filter({ hasText: /\/mcp$/ })).toBeVisible();
    await page.getByRole("button", { name: "Nouveau jeton" }).click();
    await page.getByLabel("Nom du jeton").fill("e2e portable");
    await page.getByRole("button", { name: "Créer le jeton" }).click();
    const secret = page.getByTestId("mcp-secret");
    await expect(secret).toHaveText(/^myi_/);
    const token = (await secret.textContent()) ?? "";
    const ask = () => page.request.post("/mcp", { headers: { Authorization: `Bearer ${token}` }, data: { jsonrpc: "2.0", id: 1, method: "tools/list" } });
    const listed = await ask();
    expect(listed.status()).toBe(200);
    expect((await listed.json()).result.tools.length).toBeGreaterThan(10);

    await page.getByRole("button", { name: "Brancher mon assistant" }).click();
    const guide = page.getByRole("dialog", { name: "Brancher un assistant IA" });
    await expect(guide).toContainText("claude mcp add --transport http myinfra");
    await expect(guide).toContainText(token);
    await guide.getByRole("tab", { name: "Cursor" }).click();
    await expect(guide).toContainText("mcpServers");
    await page.keyboard.press("Escape");
    await page.getByRole("button", { name: "Je l'ai copié" }).click();
    await expect(secret).toHaveCount(0);
    await page.reload();
    await expect(page.getByText("e2e portable")).toBeVisible();
    await expect(page.getByText(token)).toHaveCount(0);

    await page.getByRole("button", { name: "Révoquer" }).first().click();
    await page.getByRole("dialog").getByRole("button", { name: "Révoquer" }).click();
    await expect(page.getByText("e2e portable")).toHaveCount(0);
    expect((await ask()).status()).toBe(401);
    expect(problems).toEqual([]);
  });
});

const server = (id: string, name: string, location: string, clusterId?: string) => ({
  id, name, status: "linked", clusterId, clusterName: clusterId ? "prod" : undefined, joinCommand: "", provider: "scaleway", providerName: "Scaleway",
  plan: { offerId: "o", name: "DEV1-S", location, period: "hour", hourly: "0.0099", vcpu: 2, memoryMb: 2048, diskGb: 20, currency: "EUR" },
});

const overviewFixture = {
  servers: [server("s1", "SW1", "fr-par-1", "c1"), server("s2", "SW2", "nl-ams-1", "c1"), server("m1", "Seule", "fsn1")],
  apps: [
    { id: "a1", name: "metube", sourceKind: "image", source: "metube", destinationKind: "cluster", clusterId: "c1", hostname: "metube.example.org", runStatus: "running" },
    { id: "a2", name: "wiki", sourceKind: "image", source: "wiki", destinationKind: "server", serverId: "m1", hostname: "wiki.example.org", runStatus: "failed", services: [{ name: "web", image: "wiki", published: true, disk: false, replicas: 1 }] },
  ],
  pods: [
    { name: "metube-1", appId: "a1", serverId: "s1", phase: "Running", ready: true },
    { name: "metube-2", appId: "a1", serverId: "s1", phase: "Running", ready: true },
    { name: "wiki-web", appId: "a2", serverId: "m1", phase: "Failed", ready: false },
  ],
};
