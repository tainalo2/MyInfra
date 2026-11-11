import { expect, test, type Page } from "@playwright/test";
import { signIn } from "./screenshots-helpers";

// The plan of an application, on a made-up application served in place of the
// API: three services, of which only web declares that it needs db.
const app = {
  id: "plan1",
  name: "wiki",
  sourceKind: "image",
  source: "wiki:1",
  destinationKind: "cluster",
  clusterId: "c1",
  clusterName: "maison",
  hostname: "wiki.example.org",
  domains: [{ hostname: "wiki.example.org", kind: "default", status: "ready" }],
  services: [
    { name: "web", image: "wiki:1", published: true, disk: false, replicas: 2, dependsOn: ["db"] },
    { name: "worker", image: "wiki-worker:1", published: false, disk: false, replicas: 1 },
    { name: "db", image: "postgres:17", published: false, disk: true, replicas: 1 },
  ],
};

const gauge = { used: 1, total: 2 };
const pod = (name: string, server = "sw1") => ({ name, server, phase: "Running", reason: "", ready: true, cpu: 0, memory: 0, disk: 0 });
const runtime = (server = "sw1") => ({
  measured: true,
  cpu: gauge,
  memory: gauge,
  disk: gauge,
  pods: [pod("web-abc", server), pod("worker-abc"), pod("db-abc")],
});

async function open(page: Page, server: () => string = () => "sw1") {
  await page.route(`**/api/applications/${app.id}`, (route) => route.fulfill({ json: app }));
  await page.route(`**/api/applications/${app.id}/runtime`, (route) => route.fulfill({ json: runtime(server()) }));
  await signIn(page);
  await page.goto(`/applications/${app.id}`);
}

const plan = (page: Page) => page.getByRole("group", { name: "Plan de l'application" });
const service = (page: Page, name: string) => plan(page).getByRole("button", { name: new RegExp(`^Service · ${name}`) });

test.describe("plan", () => {
  test("what follows the plan exists once on a wide screen", async ({ page }) => {
    await open(page);
    await service(page, "web").click();
    await expect(page.getByText("web-abc")).toHaveCount(1);
    await expect(page.getByText("web-abc")).toBeVisible();
  });

  test("what follows the plan exists once on a phone", async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 800 });
    await open(page);
    await expect(plan(page)).toBeHidden();
    await page.getByRole("button", { name: /^web/ }).first().click();
    await expect(page.getByText("web-abc")).toHaveCount(1);
    await expect(page.getByText("web-abc")).toBeVisible();
  });

  test("the parts of the application stay in sight under the folded plan", async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 420 });
    await open(page);
    await page.getByRole("tab", { name: /^Pods/ }).click();
    await page.locator("main").evaluate((item) => item.scrollTo(0, 600));
    await expect.poll(async () => (await plan(page).boundingBox())!.height).toBeLessThan(60);
    const nav = page.getByRole("tablist");
    await expect.poll(async () => (await nav.boundingBox())!.y).toBeLessThan(120);
    expect((await nav.boundingBox())!.y).toBeGreaterThanOrEqual((await plan(page).boundingBox())!.y + 40);
  });

  test("no title on a wide screen, and the actions stay on the line of the nav", async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 420 });
    await open(page);
    await expect(page).toHaveTitle(/^wiki · MyInfra/);
    const title = await page.getByRole("heading", { level: 1, name: "wiki" }).boundingBox();
    expect(title!.width).toBeLessThanOrEqual(1);
    const pause = page.getByRole("button", { name: "Mettre en pause" });
    const more = page.getByRole("button", { name: "Plus d'actions" });
    await page.getByRole("tab", { name: /^Pods/ }).click();
    await page.locator("main").evaluate((item) => item.scrollTo(0, 600));
    await expect.poll(async () => (await plan(page).boundingBox())!.height).toBeLessThan(60);
    const nav = (await page.getByRole("tablist").boundingBox())!;
    for (const action of [pause, more]) {
      const box = (await action.boundingBox())!;
      expect(box.y + box.height / 2).toBeGreaterThan(nav.y);
      expect(box.y + box.height / 2).toBeLessThan(nav.y + nav.height);
    }
  });

  test("a phone keeps the title, and the actions out of the nav", async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 800 });
    await open(page);
    await expect(page.getByRole("heading", { level: 1, name: "wiki" })).toBeVisible();
    const action = (await page.getByRole("button", { name: "Mettre en pause" }).boundingBox())!;
    expect(action.y + action.height).toBeLessThan((await page.getByRole("tablist").boundingBox())!.y);
  });

  test("another part starts at its top, under the nav", async ({ page }) => {
    const pods = Array.from({ length: 12 }, (_, at) => pod(`web-${at}`));
    await page.route(`**/api/applications/${app.id}/releases`, (route) => route.fulfill({ json: { services: [] } }));
    await open(page);
    await page.route(`**/api/applications/${app.id}/runtime`, (route) => route.fulfill({ json: { ...runtime(), pods } }));
    await page.reload();
    await page.setViewportSize({ width: 1280, height: 500 });
    await page.getByRole("tab", { name: /^Pods/ }).click();
    await page.locator("main").evaluate((item) => item.scrollTo(0, 900));
    await expect.poll(async () => (await plan(page).boundingBox())!.height).toBeLessThan(60);
    await page.getByRole("tab", { name: "Services" }).click();
    const nav = (await page.getByRole("tablist").boundingBox())!;
    const first = (await page.getByLabel("Image de web").boundingBox())!;
    expect(first.y).toBeGreaterThan(nav.y + nav.height);
    expect(first.y).toBeLessThan(nav.y + nav.height + 120);
  });

  test("a chosen service brings its own parts, and only what is its own", async ({ page }) => {
    await open(page);
    await expect(page.getByRole("tab", { selected: true })).toHaveText("Services");
    await service(page, "worker").click();
    await expect(page.getByRole("tab", { selected: true })).toHaveText("Pods · 1");
    await expect(page.getByText("worker-abc")).toBeVisible();
    await expect(page.getByText("web-abc")).toHaveCount(0);
    await page.getByRole("tab", { name: "Connexions" }).click();
    await expect(page.getByText("Aucune connexion.")).toBeVisible();
    await service(page, "web").click();
    await expect(page.getByRole("tab", { selected: true })).toHaveText("Connexions");
    await page.getByRole("tabpanel").getByRole("button", { name: /^db/ }).click();
    await expect(plan(page).getByRole("button", { name: /^Service · db/ })).toHaveAttribute("aria-current", "true");
  });

  test("an image being typed survives a change of part", async ({ page }) => {
    await open(page);
    const image = page.getByLabel("Image de web");
    await image.fill("wiki:2");
    await page.getByRole("tab", { name: /^Pods/ }).click();
    await expect(image).toBeHidden();
    await page.getByRole("tab", { name: "Services" }).click();
    await expect(image).toHaveValue("wiki:2");
  });

  test("an address changes in place, and the others go with it", async ({ page }) => {
    const named = {
      ...app,
      domains: [
        { hostname: "wiki.example.org", kind: "custom", status: "ready", zone: "example.org" },
        { hostname: "docs.example.org", kind: "custom", status: "ready", zone: "example.org" },
      ],
    };
    const sent: { subdomain: string; zone: string }[][] = [];
    await page.route("**/api/dns/zones", (route) => route.fulfill({ json: [{ name: "example.org", accountId: "a1", account: "dns", provider: "x" }] }));
    await open(page);
    await page.route(`**/api/applications/${app.id}`, (route) => route.fulfill({ json: named }));
    await page.route(`**/api/applications/${app.id}/names`, (route) => {
      sent.push(route.request().postDataJSON().names);
      return route.fulfill({ json: named });
    });
    await page.reload();
    await page.getByRole("tab", { name: "Adresses" }).click();

    await page.getByRole("button", { name: "Modifier wiki.example.org" }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Enregistrer" })).toBeDisabled();
    await page.getByLabel("Sous-domaine 1").fill("kb");
    await expect(page.getByText("Ne répondra plus : wiki.example.org")).toBeVisible();
    await page.getByRole("button", { name: "Enregistrer" }).click();
    await expect.poll(() => sent.length).toBe(1);
    expect(sent[0]).toEqual([{ subdomain: "kb", zone: "example.org" }, { subdomain: "docs", zone: "example.org" }]);

    await page.getByRole("button", { name: "Ajouter un nom" }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Enregistrer" })).toBeDisabled();
    await page.getByLabel("Sous-domaine 3").fill("new");
    await page.getByLabel("Sous-domaine 3").press("Enter");
    await expect.poll(() => sent.length).toBe(2);
    expect(sent[1]).toHaveLength(3);

    await page.getByRole("button", { name: "Modifier docs.example.org" }).click();
    await page.getByRole("tabpanel").getByRole("button", { name: "Retirer" }).click();
    await expect.poll(() => sent.length).toBe(2);
    await page.getByRole("tabpanel").getByRole("button", { name: "Retirer" }).click();
    await expect.poll(() => sent.length).toBe(3);
    expect(sent[2]).toEqual([{ subdomain: "wiki", zone: "example.org" }]);
  });

  test("an address being typed survives a change of part", async ({ page }) => {
    await page.route("**/api/dns/zones", (route) => route.fulfill({ json: [{ name: "example.org", accountId: "a1", account: "dns", provider: "x" }] }));
    await open(page);
    await page.getByRole("tab", { name: "Adresses" }).click();
    await page.getByRole("button", { name: "Ajouter un nom" }).click();
    const field = page.getByRole("textbox", { name: /^Sous-domaine/ });
    await field.fill("kb");
    await page.getByRole("tab", { name: "Services" }).click();
    await page.getByRole("tab", { name: "Adresses" }).click();
    await expect(field).toHaveValue("kb");
    await field.press("Escape");
    await expect(field).toHaveCount(0);
  });

  test("a failing pod opens the pods, marked", async ({ page }) => {
    await page.route(`**/api/applications/${app.id}/releases`, (route) => route.fulfill({ json: { services: [] } }));
    await open(page);
    await page.route(`**/api/applications/${app.id}/runtime`, (route) => route.fulfill({ json: { ...runtime(), pods: [{ ...pod("web-abc"), reason: "CrashLoopBackOff", ready: false }] } }));
    await page.reload();
    const tab = page.getByRole("tab", { name: /^Pods/ });
    await expect(tab).toHaveAttribute("aria-selected", "true");
    await expect(tab.getByRole("img")).toBeVisible();
  });

  test("only the links the services declare are drawn inside", async ({ page }) => {
    await open(page);
    await plan(page).getByRole("button", { name: /^Interactions/ }).click();
    const rows = page.getByRole("region", { name: /^Interactions/ }).getByRole("listitem");
    await expect(rows.filter({ hasText: "Réseau interne" })).toHaveCount(1);
    await expect(rows.filter({ hasText: "Réseau interne" })).toContainText("web");
    await expect(rows.filter({ hasText: "Réseau interne" })).toContainText("db");
    await expect(rows.filter({ hasText: "worker" })).toHaveCount(0);
  });

  test("columns too narrow for the boxes leave the plan top to bottom", async ({ page }) => {
    await page.setViewportSize({ width: 800, height: 800 });
    await open(page, () => "un-serveur-au-nom-bien-plus-long");
    await expect(service(page, "web")).toContainText("un-serveur");
    await expect(plan(page).getByRole("button", { name: "De gauche à droite" })).toBeVisible();
  });

  test("a link goes around the box between its two ends", async ({ page }) => {
    await open(page);
    const turn = plan(page).getByRole("button", { name: /^(De gauche à droite|De haut en bas)$/ });
    if ((await turn.innerText()) === "De haut en bas") await turn.click();
    await expect(turn).toHaveText("De gauche à droite");
    // web and db are side by side in a band, with worker between them.
    const crossing = () =>
      plan(page).evaluate((frame) => {
        const rect = (name: string) => [...frame.querySelectorAll("button")].find((item) => item.getAttribute("aria-label")?.startsWith(`Service · ${name}`))!.getBoundingClientRect();
        const base = frame.getBoundingClientRect();
        const worker = rect("worker");
        const inside = (x: number, y: number) => x + base.left > worker.left && x + base.left < worker.right && y + base.top > worker.top && y + base.top < worker.bottom;
        return [...frame.querySelectorAll<SVGPathElement>("svg path")].filter((path) => {
          const length = path.getTotalLength();
          return Array.from({ length: 60 }, (_, i) => path.getPointAtLength((length * i) / 59)).some((point) => inside(point.x, point.y));
        }).length;
      });
    await expect.poll(crossing).toBe(0);
  });

  test("the plan chooses its direction, and the button turns it both ways", async ({ page }) => {
    await open(page);
    const turn = plan(page).getByRole("button", { name: /^(De gauche à droite|De haut en bas)$/ });
    const first = await turn.innerText();
    const across = async () => {
      const a = await service(page, "web").boundingBox();
      const b = await plan(page).getByRole("button", { name: /Internet/ }).boundingBox();
      return Math.abs(a!.x - b!.x) > Math.abs(a!.y - b!.y);
    };
    const start = await across();
    expect(first).toBe(start ? "De haut en bas" : "De gauche à droite");
    await turn.click();
    await expect(turn).not.toHaveText(first);
    await expect.poll(across).toBe(!start);
    await turn.click();
    await expect(turn).toHaveText(first);
    await expect.poll(across).toBe(start);
  });

  test("folded, the plan is one line, and it opens as it was", async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 420 });
    await open(page);
    const turn = plan(page).getByRole("button", { name: /^(De gauche à droite|De haut en bas)$/ });
    const direction = await turn.innerText();
    const open_ = (await plan(page).boundingBox())!.height;
    const main = page.locator("main");
    await main.evaluate((item) => item.scrollTo(0, 400));
    await expect(plan(page).locator("[data-stack]").first()).toBeVisible();
    await expect.poll(async () => (await plan(page).boundingBox())!.height).toBeLessThan(60);
    // A selection reaching several elements of a band leaves it one line high.
    await plan(page).getByRole("button", { name: /Service · web/ }).click();
    await expect.poll(async () => (await plan(page).boundingBox())!.height).toBeLessThan(60);
    await plan(page).getByRole("button", { name: /Service · web/ }).click();
    await main.evaluate((item) => item.scrollTo(0, 0));
    await expect(turn).toHaveText(direction);
    await expect.poll(async () => Math.abs((await plan(page).boundingBox())!.height - open_)).toBeLessThan(2);
  });

  test("a direction forced by the button comes back after folding", async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 420 });
    await open(page);
    const turn = plan(page).getByRole("button", { name: /^(De gauche à droite|De haut en bas)$/ });
    if ((await turn.innerText()) === "De haut en bas") await turn.click();
    await expect(turn).toHaveText("De gauche à droite");
    const main = page.locator("main");
    await main.evaluate((item) => item.scrollTo(0, 400));
    await expect.poll(async () => (await plan(page).boundingBox())!.height).toBeLessThan(60);
    await main.evaluate((item) => item.scrollTo(0, 0));
    await expect(turn).toHaveText("De gauche à droite");
  });

  test("a fast scroll ends on the end of a long page, with nothing added", async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 420 });
    await open(page);
    await expect(service(page, "web")).toBeVisible();
    await page.mouse.move(800, 300);
    for (let i = 0; i < 12; i++) await page.mouse.wheel(0, 600);
    await page.waitForTimeout(700);
    await expect.poll(async () => (await plan(page).boundingBox())!.height).toBeLessThan(60);
    const main = page.locator("main");
    expect(await main.evaluate((item) => item.style.paddingBottom)).toBe("");
    expect(await main.evaluate((item) => item.getBoundingClientRect().bottom - item.lastElementChild!.getBoundingClientRect().bottom)).toBeLessThan(60);
  });

  test("a dimmed stack is dimmed once", async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 420 });
    await open(page);
    await expect(service(page, "web")).toBeVisible();
    await page.locator("main").evaluate((main) => main.scrollTo(0, 400));
    await expect(plan(page).locator("[data-stack]").first()).toBeVisible();
    // A selection that reaches nothing in the entry band leaves it dimmed.
    await plan(page).getByRole("button", { name: /^Stockage|^Disque/ }).first().click();
    const dimmed = plan(page).locator("[data-stack].opacity-40");
    await expect(dimmed.first()).toBeVisible();
    await expect(dimmed.first().locator("button")).not.toHaveClass(/opacity-40/);
  });

  test("the links follow a box whose words change", async ({ page }) => {
    let server = "sw1";
    await open(page, () => server);
    const turn = plan(page).getByRole("button", { name: /^(De gauche à droite|De haut en bas)$/ });
    // The button names the other direction; boxes keep their width in columns.
    if ((await turn.innerText()) === "De haut en bas") await turn.click();
    await expect(turn).toHaveText("De gauche à droite");
    // A link ends in the middle of one side of its box.
    const gap = () =>
      plan(page).evaluate((frame) => {
        const box = [...frame.querySelectorAll("button")].find((item) => item.getAttribute("aria-label")?.startsWith("Service · web"))!;
        const base = frame.getBoundingClientRect();
        const rect = box.getBoundingClientRect();
        const ends = [...frame.querySelectorAll("svg path")].flatMap((path) => {
          const d = path.getAttribute("d")!.match(/-?[\d.]+/g)!.map(Number);
          return [{ x: d[0], y: d[1] }, { x: d[d.length - 2], y: d[d.length - 1] }];
        });
        const x = rect.left - base.left;
        const y = rect.top - base.top;
        const sides = [
          { x: x + rect.width / 2, y },
          { x: x + rect.width / 2, y: y + rect.height },
          { x, y: y + rect.height / 2 },
          { x: x + rect.width, y: y + rect.height / 2 },
        ];
        return Math.min(...ends.flatMap((end) => sides.map((side) => Math.abs(end.x - side.x) + Math.abs(end.y - side.y))));
      });
    await expect.poll(gap).toBeLessThan(2);
    server = "un-serveur-au-nom-bien-plus-long";
    await expect(service(page, "web")).toContainText(server, { timeout: 12_000 });
    await expect.poll(gap).toBeLessThan(2);
  });

  test("only a template with something public has a way in from the Internet", async ({ page }) => {
    const template = (published: boolean) => ({
      id: "plan2",
      name: "modèle",
      createdAt: "2026-11-01T00:00:00Z",
      body: { applications: [{ name: "seule", sourceKind: "image", source: "seule:1", services: [{ name: "seule", image: "seule:1", published, disk: false }] }] },
    });
    let published = true;
    await page.route("**/api/templates/plan2", (route) => route.fulfill({ json: template(published) }));
    await signIn(page);
    const group = page.getByRole("group", { name: /^Plan/ });
    await page.goto("/templates/plan2/deploy");
    await expect(group.getByRole("button", { name: /seule/ })).toBeVisible();
    await expect(group.getByRole("button", { name: /Proxy/ })).toBeVisible();
    await expect(group.getByRole("button", { name: /Internet/ })).toBeVisible();
    published = false;
    await page.reload();
    await expect(group.getByRole("button", { name: /seule/ })).toBeVisible();
    await expect(group.getByRole("button", { name: /Proxy/ })).toHaveCount(0);
    await expect(group.getByRole("button", { name: /Internet/ })).toHaveCount(0);
  });
});
