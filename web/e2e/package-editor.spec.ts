import { expect, test, type Page } from "@playwright/test";
import { signIn } from "./screenshots-helpers";

// The editor of a package with a chosen service: its name, what to do and the
// tabs of the service hold on two lines, right above the first field.
const pkg = {
  name: "wiki",
  services: [
    { name: "wiki-web", image: "wiki:1", public: true, port: 3000, dependsOn: ["wiki-postgresql"] },
    { name: "wiki-postgresql", image: "postgres:18", port: 5432 },
  ],
};
const row = { id: "pk1", name: "wiki", package: pkg, yaml: "name: wiki\n", ...pkg };

async function open(page: Page) {
  const saves: string[] = [];
  await page.route("**/api/packages/pk1", (route) => {
    if (route.request().method() !== "GET") saves.push(route.request().method());
    return route.fulfill({ json: row });
  });
  await signIn(page);
  await page.goto("/applications/paquets/pk1");
  return saves;
}

const box = (page: Page, name: string) => page.getByRole("group", { name: /^Plan/ }).getByRole("button", { name: new RegExp(`^Service · ${name}`) });

test.describe("package editor", () => {
  test("a chosen service takes two lines above its settings", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await open(page);
    await box(page, "wiki-postgresql").click();
    const line = page.getByRole("button", { name: "Installer" });
    const parts = page.getByRole("tablist", { name: "wiki-postgresql" });
    await expect(parts).toBeVisible();
    // The name is said once, on the line of the actions.
    const said = page.locator("form").getByText("wiki-postgresql", { exact: true }).locator("visible=true");
    await expect(said).toHaveCount(1);
    await expect(page.getByRole("heading", { name: "Services", exact: true })).toBeHidden();
    const first = await line.boundingBox();
    const second = await parts.boundingBox();
    const name = await said.boundingBox();
    const field = await page.getByLabel("Nom du service").locator("visible=true").boundingBox();
    expect(Math.abs(name!.y + name!.height / 2 - (first!.y + first!.height / 2))).toBeLessThan(8);
    expect(second!.y - first!.y).toBeLessThan(70);
    expect(field!.y - first!.y).toBeLessThan(200);
    // Removing the service follows its title.
    const remove = await page.getByRole("button", { name: "Retirer le service wiki-postgresql" }).boundingBox();
    expect(Math.abs(remove!.y + remove!.height / 2 - (name!.y + name!.height / 2))).toBeLessThan(6);
    expect(remove!.x).toBeGreaterThan(name!.x + name!.width);
    expect(remove!.x).toBeLessThan(name!.x + name!.width + 200);
  });

  test("a chosen service leaves a short page, and the plan still folds on scrolling", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await open(page);
    await box(page, "wiki-postgresql").click();
    const group = page.getByRole("group", { name: /^Plan/ });
    const open_ = (await group.boundingBox())!.height;
    const main = page.locator("main");
    expect(await main.evaluate((item) => item.scrollHeight - item.clientHeight)).toBeLessThan(open_);
    await page.mouse.move(800, 600);
    await page.mouse.wheel(0, 60);
    await expect.poll(async () => (await group.boundingBox())!.height).toBeLessThan(60);
    // What is being edited stays in front of its stack.
    await expect(box(page, "wiki-postgresql")).toBeVisible();
    await expect(box(page, "wiki-web")).toHaveCount(0);
    await page.mouse.wheel(0, 400);
    await page.waitForTimeout(400);
    await expect.poll(async () => (await group.boundingBox())!.height).toBeLessThan(60);
    await page.mouse.wheel(0, -900);
    await expect.poll(async () => Math.abs((await group.boundingBox())!.height - open_)).toBeLessThan(2);
  });

  test("the plan stays folded to the end of a short page, with what follows right under it", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 760 });
    await open(page);
    await box(page, "wiki-postgresql").click();
    const group = page.getByRole("group", { name: /^Plan/ });
    const line = page.getByRole("button", { name: "Installer" });
    await page.mouse.move(800, 600);
    await page.mouse.wheel(0, 60);
    await expect.poll(async () => (await group.boundingBox())!.height).toBeLessThan(60);
    // What the plan gave up is used at once, not left empty.
    await expect.poll(async () => {
      const plan = (await group.boundingBox())!;
      return (await line.boundingBox())!.y - (plan.y + plan.height);
    }).toBeLessThan(40);
    for (let i = 0; i < 10; i++) {
      await page.mouse.wheel(0, 120);
      await page.waitForTimeout(120);
      expect((await group.boundingBox())!.height).toBeLessThan(60);
    }
    const main = page.locator("main");
    expect(await main.evaluate((item) => item.scrollTop)).toBeGreaterThan(30);
    expect(await main.evaluate((item) => item.scrollHeight - item.clientHeight - item.scrollTop)).toBeLessThan(2);
  });

  test("a folded plan adds no empty scroll at the end of the page", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 760 });
    await open(page);
    await box(page, "wiki-postgresql").click();
    const group = page.getByRole("group", { name: /^Plan/ });
    await page.mouse.move(800, 600);
    await page.mouse.wheel(0, 60);
    await expect.poll(async () => (await group.boundingBox())!.height).toBeLessThan(60);
    for (let i = 0; i < 8; i++) {
      await page.mouse.wheel(0, 150);
      await page.waitForTimeout(100);
    }
    // At the very end, below the last button there is the page's margin only.
    const save = await page.getByRole("button", { name: "Enregistrer" }).boundingBox();
    expect(760 - (save!.y + save!.height)).toBeLessThan(60);
    expect(await page.locator("main").evaluate((item) => item.style.paddingBottom)).toBe("");
  });

  test("a fast scroll leaves no empty room at the end of the page", async ({ page }) => {
    for (const height of [760, 900]) {
      await page.setViewportSize({ width: 1440, height });
      await open(page);
      await box(page, "wiki-postgresql").click();
      await page.waitForTimeout(300);
      await page.mouse.move(800, 500);
      // All at once, faster than the plan folds.
      for (let i = 0; i < 6; i++) await page.mouse.wheel(0, 400);
      await page.waitForTimeout(700);
      const group = page.getByRole("group", { name: /^Plan/ });
      expect((await group.boundingBox())!.height).toBeLessThan(60);
      const main = page.locator("main");
      // A page long enough ends on its own margin; a shorter one gets a few
      // pixels, to stay folded.
      if (height === 760) expect(await main.evaluate((item) => item.style.paddingBottom)).toBe("");
      else expect(await main.evaluate((item) => item.scrollHeight - item.clientHeight)).toBeLessThan(60);
      if (height === 760) expect(await main.evaluate((item) => item.getBoundingClientRect().bottom - item.lastElementChild!.getBoundingClientRect().bottom)).toBeLessThan(60);
    }
  });

  test("a page that fits once folded scrolls no further than it was", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await open(page);
    await box(page, "wiki-postgresql").click();
    const group = page.getByRole("group", { name: /^Plan/ });
    const main = page.locator("main");
    await page.mouse.move(800, 600);
    await page.mouse.wheel(0, 60);
    await expect.poll(async () => (await group.boundingBox())!.height).toBeLessThan(60);
    await page.waitForTimeout(300);
    const held = await main.evaluate((item) => item.scrollTop);
    await page.mouse.wheel(0, 600);
    await page.waitForTimeout(300);
    expect(await main.evaluate((item) => item.scrollTop)).toBeLessThanOrEqual(held + 1);
    expect((await group.boundingBox())!.height).toBeLessThan(60);
  });

  test("no sliver of the page shows between the plan and the line under it", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 700 });
    await open(page);
    await box(page, "wiki-web").click();
    const seam = () =>
      page.evaluate(() => {
        const sticky = document.querySelector('[role="group"][aria-label^="Plan"]')!.closest("section")!.parentElement!;
        const line = [...document.querySelectorAll("button")].find((item) => item.textContent === "Installer")!.closest('[class*="md:sticky"]')!;
        return line.getBoundingClientRect().top - sticky.getBoundingClientRect().bottom;
      });
    await page.mouse.move(800, 500);
    for (const step of [100, 100, 150]) {
      await page.mouse.wheel(0, step);
      await page.waitForTimeout(300);
      expect(await seam()).toBeLessThanOrEqual(0);
    }
  });

  test("with nothing chosen, only the package's own settings show", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await open(page);
    await expect(page.getByLabel("Nom", { exact: true }).locator("visible=true").or(page.getByRole("textbox", { name: /^Nom/ }).locator("visible=true")).first()).toBeVisible();
    await expect(page.getByRole("button", { name: "Présentation" })).toBeVisible();
    await expect(page.locator("article").locator("visible=true")).toHaveCount(0);
    const parts = page.getByRole("tablist", { name: "Réglages du paquet" });
    await expect(parts.getByRole("tab")).toHaveText(["Questions", "Variables communes"]);
    await expect(page.getByRole("button", { name: "Ajouter une question" })).toBeVisible();
    await parts.getByRole("tab", { name: "Variables communes" }).click();
    await expect(page.getByRole("textbox", { name: "Nom de la variable" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Ajouter une question" })).toBeHidden();
    await box(page, "wiki-web").click();
    await expect(page.locator("article").locator("visible=true")).toHaveCount(1);
    await expect(parts).toBeHidden();
    await page.getByRole("button", { name: "Tout le paquet" }).click();
    await expect(parts).toBeVisible();
  });

  test("common variables are fields, read from the package and written back", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    const env = { DB_PASSWORD: "{{secret:db}}", SITE: "{{url}}", DEBUG: "true", TITLE: "wiki", ADMIN: "{{input:admin}}", MIXED: "a{{host}}b" };
    const stored = { ...row, package: { ...pkg, env, inputs: [{ key: "admin", label: "E-mail" }] } };
    let saved: { env: Record<string, string> } | null = null;
    await page.route("**/api/packages/pk1", (route) => {
      if (route.request().method() === "PUT") saved = JSON.parse(JSON.parse(route.request().postData()!).text);
      return route.fulfill({ json: stored });
    });
    await signIn(page);
    await page.goto("/applications/paquets/pk1");
    await page.getByRole("tab", { name: "Variables communes" }).click();
    const names = page.getByRole("textbox", { name: "Nom de la variable" });
    await expect(names).toHaveCount(7);
    await expect(page.locator(".cm-editor").locator("visible=true")).toHaveCount(0);
    const kinds = page.getByRole("combobox", { name: "Type" });
    expect(await kinds.evaluateAll((items) => items.map((item) => (item as HTMLSelectElement).value))).toEqual(["secret", "url", "bool", "text", "input", "text", "text"]);
    await expect(page.getByRole("textbox", { name: "Nom du mot de passe" })).toHaveValue("db");
    await expect(page.getByRole("textbox", { name: "Valeur" }).nth(1)).toHaveValue("a{{host}}b");
    const debug = page.getByRole("checkbox", { name: "Activée" });
    await expect(debug).toBeChecked();
    await debug.uncheck();
    await names.last().fill("TOKEN");
    await expect(names).toHaveCount(8);
    await kinds.nth(6).selectOption("secret");
    await expect(page.getByRole("textbox", { name: "Nom du mot de passe" }).last()).toHaveValue("token");
    await page.getByRole("button", { name: "Enregistrer" }).click();
    await expect.poll(() => saved).not.toBeNull();
    expect(saved!.env).toEqual({ ...env, DEBUG: "false", TOKEN: "{{secret:token}}" });
  });

  test("a problem of a service out of sight still shows", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.route("**/api/packages/format", (route) => route.fulfill({ json: { yaml: "", problems: [{ path: "services[1].image", message: "Image manquante" }] } }));
    await open(page);
    await expect(page.getByRole("alert").filter({ hasText: "Image manquante" }).locator("visible=true")).toBeVisible();
    await expect(box(page, "wiki-postgresql").locator(".bg-bad")).toBeVisible();
    await expect(box(page, "wiki-web").locator(".bg-bad")).toHaveCount(0);
  });

  test("renaming the chosen service keeps it open, even empty", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await open(page);
    await box(page, "wiki-postgresql").click();
    const name = page.getByLabel("Nom du service").locator("visible=true");
    await name.fill("");
    await expect(name).toBeVisible();
    await name.fill("base");
    await expect(name).toHaveValue("base");
    await expect(box(page, "base")).toHaveAttribute("aria-current", "true");
  });

  test("a chosen storage shows the shared disks alone, and keeps them through a rename", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await open(page);
    await page.getByRole("group", { name: /^Plan/ }).getByRole("button", { name: "Ajouter un stockage" }).click();
    const name = page.getByRole("textbox", { name: "Nom du disque" }).locator("visible=true");
    await expect(name).toHaveValue("media");
    await expect(page.getByRole("tablist", { name: "Réglages du paquet" })).toBeHidden();
    await name.fill("photos");
    await expect(name).toHaveValue("photos");
  });

  test("the ports are rows under a legend, with one blank row at the end", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    let saved: { services: { name: string; ports?: unknown[] }[] } | null = null;
    await page.route("**/api/packages/pk1", (route) => {
      if (route.request().method() === "PUT") saved = JSON.parse(JSON.parse(route.request().postData()!).text);
      return route.fulfill({ json: row });
    });
    await signIn(page);
    await page.goto("/applications/paquets/pk1");
    await box(page, "wiki-web").click();
    await page.getByRole("tab", { name: "Réseau" }).locator("visible=true").click();
    const ports = page.getByRole("textbox", { name: "Port", exact: true }).locator("visible=true");
    await expect(ports).toHaveCount(1);
    await expect(page.getByText(/Ports en plus|port de métriques|Ajouter un port/).locator("visible=true")).toHaveCount(0);
    const legend = page.getByText("Nom du port", { exact: true }).locator("visible=true");
    await expect(legend).toBeVisible();
    const names = page.getByRole("textbox", { name: "Nom du port" }).locator("visible=true");
    expect((await legend.boundingBox())!.x).toBeCloseTo((await names.first().boundingBox())!.x, 0);
    const portLegend = page.getByText("Port public", { exact: true }).locator("visible=true");
    await expect(portLegend).toBeVisible();
    // A name alone does not fill the row, its port does.
    await names.first().fill("metrics");
    await expect(ports).toHaveCount(1);
    await ports.first().pressSequentially("9100");
    await expect(ports).toHaveCount(2);
    await expect(ports.first()).toBeFocused();
    await ports.last().fill("53");
    await expect(ports).toHaveCount(3);
    await ports.nth(1).fill("");
    await expect(ports).toHaveCount(2);
    await page.getByRole("button", { name: "Enregistrer" }).click();
    await expect.poll(() => saved).not.toBeNull();
    expect(saved!.services[0].ports).toEqual([{ name: "metrics", port: 9100 }]);
  });

  test("a port in both protocols is one row, and two ports in the package", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    const twins = [{ name: "dns", port: 53, expose: 5353 }, { port: 53, protocol: "udp", expose: 5353 }, { port: 8125, protocol: "udp" }];
    const stored = { ...row, package: { ...pkg, services: [{ ...pkg.services[0], ports: twins }, pkg.services[1]] } };
    let saved: { services: { ports?: unknown[] }[] } | null = null;
    await page.route("**/api/packages/pk1", (route) => {
      if (route.request().method() === "PUT") saved = JSON.parse(JSON.parse(route.request().postData()!).text);
      return route.fulfill({ json: stored });
    });
    await signIn(page);
    await page.goto("/applications/paquets/pk1");
    await box(page, "wiki-web").click();
    await page.getByRole("tab", { name: "Réseau" }).locator("visible=true").click();
    const ports = page.getByRole("textbox", { name: "Port", exact: true }).locator("visible=true");
    const protocols = page.getByRole("combobox", { name: "Protocole" }).locator("visible=true");
    await expect(ports).toHaveCount(3);
    await expect(protocols.first()).toHaveValue("both");
    await expect(protocols.nth(1)).toHaveValue("udp");
    await ports.last().fill("9100");
    await protocols.nth(2).selectOption("both");
    await expect(ports).toHaveCount(4);
    await expect(protocols.nth(2)).toHaveValue("both");
    await protocols.first().selectOption("tcp");
    await expect(ports).toHaveCount(4);
    await page.getByRole("button", { name: "Enregistrer" }).click();
    await expect.poll(() => saved).not.toBeNull();
    expect(saved!.services[0].ports).toEqual([{ name: "dns", port: 53, expose: 5353 }, { port: 8125, protocol: "udp" }, { port: 9100 }, { port: 9100, protocol: "udp" }]);
  });

  // A package to save and read back, for the rows below.
  async function saving(page: Page, stored: unknown = row) {
    const sent: { current: { services: { name: string; volumes?: unknown[]; files?: unknown[]; env?: unknown; replicas?: number }[]; disks?: unknown[] } | null } = { current: null };
    await page.route("**/api/packages/pk1", (route) => {
      if (route.request().method() === "PUT") sent.current = JSON.parse(JSON.parse(route.request().postData()!).text);
      return route.fulfill({ json: stored });
    });
    await signIn(page);
    await page.goto("/applications/paquets/pk1");
    return sent;
  }
  const save = async (page: Page, sent: { current: unknown }) => {
    await page.getByRole("button", { name: "Enregistrer" }).click();
    await expect.poll(() => sent.current).not.toBeNull();
  };
  const tab = (page: Page, name: string) => page.getByRole("tab", { name }).locator("visible=true").click();

  test("a service mounts a new storage or links an existing one, each at its path", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    const stored = { ...row, package: { ...pkg, disks: [{ name: "media", sizeGi: 20 }], services: [{ ...pkg.services[0], replicas: 2, volumes: [{ path: "/own", sizeGi: 5 }] }, pkg.services[1]] } };
    const sent = await saving(page, stored);
    await box(page, "wiki-postgresql").click();
    await tab(page, "Stockage");
    await expect(page.getByText("Chemin dans le conteneur", { exact: true }).locator("visible=true")).toBeVisible();
    await expect(page.getByRole("button", { name: /^Ajouter un disque/ })).toHaveCount(0);
    const paths = page.getByRole("textbox", { name: "Chemin dans le conteneur" }).locator("visible=true");
    const sources = page.getByRole("combobox", { name: "Stockage" }).locator("visible=true");
    await expect(paths).toHaveCount(1);
    // A new one, then the shared disk, then the disk of the other service.
    // Emptied, the last row is blank again, and nothing of it is kept.
    await paths.first().fill("/tmp");
    await expect(paths).toHaveCount(2);
    await paths.first().fill("");
    await expect(paths).toHaveCount(1);
    await paths.first().fill("/var/lib/postgresql/data");
    await page.getByRole("textbox", { name: "Taille en Gio" }).locator("visible=true").first().fill("8");
    await expect(paths).toHaveCount(2);
    await paths.nth(1).fill("/media");
    await sources.nth(1).selectOption("media");
    await expect(paths).toHaveCount(3);
    await paths.nth(2).fill("/shared");
    await sources.nth(2).selectOption({ label: "wiki-web · /own" });
    await expect(sources.nth(2)).toHaveValue("wiki-web-data");
    await save(page, sent);
    const saved = sent.current!;
    expect(saved.disks).toEqual([{ name: "media", sizeGi: 20 }, { name: "wiki-web-data", sizeGi: 5 }]);
    expect(saved.services[1].volumes).toEqual([{ path: "/var/lib/postgresql/data", sizeGi: 8 }, { path: "/media", disk: "media" }, { path: "/shared", disk: "wiki-web-data" }]);
    expect(saved.services[0].volumes).toEqual([{ path: "/own", disk: "wiki-web-data" }]);
    // Who mounts a shared disk runs one copy, or the package is refused.
    expect(saved.services[0].replicas).toBeUndefined();
  });

  test("the storage page sets the size, the type shown, and who shares it", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    const sent = await saving(page);
    await page.getByRole("group", { name: /^Plan/ }).getByRole("button", { name: "Ajouter un stockage" }).click();
    const form = page.getByRole("region", { name: "Stockage" });
    await form.getByLabel("Nom du disque").fill("photos");
    await form.getByLabel("Taille en Gio").fill("50");
    await expect(form.getByRole("radio", { name: /^Simple/ })).toBeChecked();
    await expect(form.getByRole("radio", { name: /^Redondé/ })).toBeDisabled();
    await expect(form.getByText("un stockage partagé reste sur un seul serveur")).toBeVisible();
    await form.getByRole("checkbox", { name: "wiki-web" }).check();
    await form.getByRole("checkbox", { name: "wiki-postgresql" }).check();
    const paths = form.getByRole("textbox", { name: "Chemin dans le conteneur" });
    await paths.first().fill("/photos");
    await paths.last().fill("/import");
    await form.getByRole("checkbox", { name: "wiki-postgresql" }).uncheck();
    await expect(paths).toHaveCount(1);
    await form.getByRole("checkbox", { name: "wiki-postgresql" }).check();
    await form.getByRole("textbox", { name: "Chemin dans le conteneur" }).last().fill("/import");
    await save(page, sent);
    expect(sent.current!.disks).toEqual([{ name: "photos", sizeGi: 50 }]);
    expect(sent.current!.services.map((service) => service.volumes)).toEqual([[{ path: "/photos", disk: "photos" }], [{ path: "/import", disk: "photos" }]]);
  });

  test("the disk of one service opens its page, and sharing it makes it a named disk", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    const stored = { ...row, package: { ...pkg, services: [{ ...pkg.services[0], volumes: [{ path: "/data", sizeGi: 5 }] }, pkg.services[1]] } };
    const sent = await saving(page, stored);
    await page.getByRole("group", { name: /^Plan/ }).getByRole("button", { name: /wiki-web-data/ }).click();
    const form = page.getByRole("region", { name: "Stockage" });
    await expect(form.getByLabel("Taille en Gio")).toHaveValue("5");
    await expect(form.getByText(/Se décide à l'installation/)).toBeVisible();
    await expect(form.getByRole("checkbox", { name: "wiki-web" })).toBeDisabled();
    // The path keeps the page open while it changes.
    await form.getByRole("textbox", { name: "Chemin dans le conteneur" }).fill("/srv");
    await expect(form.getByRole("textbox", { name: "Chemin dans le conteneur" })).toHaveValue("/srv");
    await form.getByRole("checkbox", { name: "wiki-postgresql" }).check();
    await expect(form.getByLabel("Nom du disque")).toHaveValue("wiki-web-data");
    await form.getByRole("textbox", { name: "Chemin dans le conteneur" }).last().fill("/srv2");
    await save(page, sent);
    expect(sent.current!.disks).toEqual([{ name: "wiki-web-data", sizeGi: 5 }]);
  });

  test("deleting a storage takes it off the services", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    const stored = { ...row, package: { ...pkg, disks: [{ name: "media", sizeGi: 20 }], services: [{ ...pkg.services[0], volumes: [{ path: "/m", disk: "media" }] }, pkg.services[1]] } };
    const sent = await saving(page, stored);
    await page.getByRole("group", { name: /^Plan/ }).getByRole("button", { name: /media/ }).click();
    await page.getByRole("button", { name: "Supprimer ce stockage" }).click();
    await expect(page.getByRole("tablist", { name: "Réglages du paquet" })).toBeVisible();
    await save(page, sent);
    expect(sent.current!.disks).toBeUndefined();
    expect(sent.current!.services[0].volumes).toBeUndefined();
  });

  test("files and variables of a service are rows with a blank one at the end", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    const sent = await saving(page);
    await box(page, "wiki-web").click();
    await tab(page, "Fichiers");
    const files = page.getByRole("textbox", { name: "Chemin du fichier" }).locator("visible=true").or(page.getByPlaceholder("/etc/app/config.ini").locator("visible=true"));
    await expect(files).toHaveCount(1);
    await expect(page.getByRole("button", { name: /^Ajouter un fichier/ })).toHaveCount(0);
    await files.first().fill("/etc/app.ini");
    await expect(files).toHaveCount(2);
    await files.first().fill("");
    await expect(files).toHaveCount(1);
    await files.first().fill("/etc/app.ini");
    await tab(page, "Variables");
    const names = page.getByRole("textbox", { name: "Nom de la variable" }).locator("visible=true");
    await expect(names).toHaveCount(1);
    await expect(page.locator("article:visible .cm-editor")).toHaveCount(0);
    const kinds = page.getByRole("combobox", { name: "Type" }).locator("visible=true").first();
    await expect(kinds.getByRole("option", { name: "Mot de passe généré" })).toHaveCount(0);
    await names.first().pressSequentially("MO");
    await expect(names).toHaveCount(2);
    // Emptied letter by letter, the row stays the one being typed in.
    await page.keyboard.press("Backspace");
    await page.keyboard.press("Backspace");
    await expect(names).toHaveCount(1);
    await expect(names.first()).toBeFocused();
    await names.first().pressSequentially("MODE");
    await expect(names).toHaveCount(2);
    await expect(names.first()).toBeFocused();
    await page.getByRole("textbox", { name: "Valeur" }).locator("visible=true").first().fill("prod");
    await save(page, sent);
    expect(sent.current!.services[0].files).toEqual([{ path: "/etc/app.ini", content: "" }]);
    expect(sent.current!.services[0].env).toEqual({ MODE: "prod" });
  });

  test("a chosen service is no card, and its small nav stays under its title", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 700 });
    await open(page);
    await box(page, "wiki-web").click();
    const parts = page.getByRole("tablist", { name: "wiki-web" });
    await expect(parts).toBeVisible();
    const card = page.locator("article").locator("visible=true");
    expect(await card.evaluate((item) => getComputedStyle(item).borderTopWidth)).toBe("0px");
    expect(parseFloat(await parts.getByRole("tab").first().evaluate((item) => getComputedStyle(item).fontSize))).toBeLessThanOrEqual(13);
    await page.mouse.move(800, 500);
    await page.mouse.wheel(0, 500);
    await page.waitForTimeout(500);
    const title = await page.getByRole("button", { name: "Installer" }).boundingBox();
    const nav = await parts.boundingBox();
    expect(nav!.y).toBeGreaterThan(title!.y);
    expect(nav!.y - (title!.y + title!.height)).toBeLessThan(16);
    expect(await page.locator("main").evaluate((item) => item.scrollTop)).toBeGreaterThan(100);
  });

  test("removing the chosen service from its title leaves no wait on it", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    const sent = await saving(page);
    await box(page, "wiki-postgresql").click();
    await page.getByRole("button", { name: "Retirer le service wiki-postgresql" }).click();
    await expect(page.getByRole("tablist", { name: "Réglages du paquet" })).toBeVisible();
    await save(page, sent);
    expect(sent.current!.services.map((service) => service.name)).toEqual(["wiki-web"]);
    expect((sent.current!.services[0] as { dependsOn?: string[] }).dependsOn ?? []).toEqual([]);
  });

  test("resources are sliders, and a value between two stops stays as it is", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    const resources = { cpu: "0.5", memory: "300Mi", gpu: 1 };
    const stored = { ...row, package: { ...pkg, services: [{ ...pkg.services[0], resources }, pkg.services[1]] } };
    const sent = await saving(page, stored);
    await box(page, "wiki-web").click();
    await tab(page, "Ressources");
    const cpu = page.getByRole("slider", { name: /^Processeur réservé/ });
    const memory = page.getByRole("slider", { name: /^Mémoire/ });
    await expect(cpu).toBeVisible();
    await expect(page.locator("article:visible").getByPlaceholder("512Mi")).toHaveCount(0);
    await expect(cpu).toHaveAttribute("aria-valuetext", "0,5 cœur");
    await expect(memory).toHaveAttribute("aria-valuetext", "300 Mio");
    // Showing them changes nothing.
    await save(page, sent);
    expect((sent.current!.services[0] as { resources?: unknown }).resources).toEqual(resources);
    sent.current = null;
    await cpu.focus();
    await page.keyboard.press("ArrowRight");
    await expect(cpu).toHaveAttribute("aria-valuetext", "0,75 cœur");
    await page.keyboard.press("ArrowRight");
    await expect(cpu).toHaveAttribute("aria-valuetext", "1 cœur");
    await page.keyboard.press("ArrowRight");
    await expect(cpu).toHaveAttribute("aria-valuetext", "2 cœurs");
    await memory.focus();
    await page.keyboard.press("ArrowRight");
    await expect(memory).toHaveAttribute("aria-valuetext", "512 Mio");
    await save(page, sent);
    expect((sent.current!.services[0] as { resources?: unknown }).resources).toEqual({ cpu: "2", memory: "512Mi", gpu: 1 });
    sent.current = null;
    // Back to Automatic: nothing reserved, and what the screen does not show stays.
    await cpu.fill("0");
    await memory.fill("0");
    await expect(cpu).toHaveAttribute("aria-valuetext", "Automatique");
    await save(page, sent);
    expect((sent.current!.services[0] as { resources?: unknown }).resources).toEqual({ gpu: 1 });
  });

  test("fields out of sight do not stop saving", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await signIn(page);
    await page.goto("/applications/paquets/nouveau");
    await page.getByRole("button", { name: /Partir de zéro/ }).click();
    // The image of the first service is required, and out of sight here.
    await page.getByLabel("Nom du paquet").fill("essai");
    const sent = page.waitForRequest((request) => request.method() === "POST" && new URL(request.url()).pathname === "/api/packages");
    await page.locator("form").getByRole("button", { name: /Créer|Enregistrer/ }).click();
    await sent;
  });

  test("a phone keeps the name on the card", async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 800 });
    await open(page);
    await page.getByRole("button", { name: /^wiki-postgresql/ }).first().click();
    await expect(page.locator("article:visible header")).toContainText("wiki-postgresql");
  });

  test("the menu gives the guide and the export without saving", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    const saves = await open(page);
    await page.getByRole("button", { name: "Plus d'actions" }).click();
    await expect(page.getByRole("menuitem", { name: "Exporter en YAML" })).toBeVisible();
    await page.getByRole("menuitem", { name: "Guide" }).click();
    await page.waitForTimeout(300);
    expect(saves).toEqual([]);
  });

  test("the form is the rule, and the text is one step away in the menu", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await open(page);
    await expect(page.getByRole("tablist", { name: "Vue" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Ajouter un service" }).locator("visible=true")).toBeVisible();
    await page.getByRole("button", { name: "Plus d'actions" }).click();
    await page.getByRole("menuitem", { name: "Avancé (YAML)" }).click();
    const back = page.getByRole("button", { name: "Visuel" });
    await expect(back).toBeVisible();
    await expect(page.getByRole("button", { name: "Ajouter un service" }).locator("visible=true")).toHaveCount(0);
    await page.getByRole("button", { name: "Plus d'actions" }).click();
    await expect(page.getByRole("menuitem", { name: "Guide" })).toBeVisible();
    await expect(page.getByRole("menuitem", { name: "Avancé (YAML)" })).toHaveCount(0);
    await page.keyboard.press("Escape");
    await back.click();
    await expect(page.getByRole("button", { name: "Ajouter un service" }).locator("visible=true")).toBeVisible();
    await expect(back).toHaveCount(0);
  });

  test("choosing an element leaves the text for its form", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await open(page);
    await page.getByRole("button", { name: "Plus d'actions" }).click();
    await page.getByRole("menuitem", { name: "Avancé (YAML)" }).click();
    await expect(page.getByRole("button", { name: "Visuel" })).toBeVisible();
    await box(page, "wiki-web").click();
    await expect(page.getByRole("button", { name: "Visuel" })).toHaveCount(0);
    await expect(page.getByRole("tablist", { name: "wiki-web" })).toBeVisible();
  });

  test("each band adds its own element with a + that says so on hover", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await open(page);
    const group = page.getByRole("group", { name: /^Plan/ });
    const service = group.getByRole("button", { name: "Ajouter un service" });
    const storage = group.getByRole("button", { name: "Ajouter un stockage" });
    // The package has no disk: the band is there for its +.
    await expect(storage).toBeVisible();
    expect((await service.innerText()).trim()).toBe("");
    await expect(storage.locator("xpath=ancestor::*[@data-band][1]")).toHaveAttribute("data-band", "storage");
    await expect(service.locator("xpath=ancestor::*[@data-band][1]")).toHaveAttribute("data-band", "services");
    await storage.hover();
    await expect(storage.getByText("Ajouter un stockage")).toBeVisible();
    await service.hover();
    await expect(service.getByText("Ajouter un service")).toBeVisible();
    await expect(storage.getByText("Ajouter un stockage")).toBeHidden();
    await storage.click();
    await expect(group.getByRole("button", { name: /media/ })).toBeVisible();
    await service.click();
    await expect(page.getByRole("tablist", { name: "service-3" })).toBeVisible();
    // Nothing is left on the line under the plan.
    await expect(page.locator("form").getByRole("button", { name: /^Ajouter un (service|stockage)$/ })).toHaveCount(0);
  });

  test("the + stay on a folded plan and leave with the form", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 700 });
    await open(page);
    const group = page.getByRole("group", { name: /^Plan/ });
    await box(page, "wiki-web").click();
    await page.mouse.move(800, 500);
    await page.mouse.wheel(0, 200);
    await expect.poll(async () => (await group.boundingBox())!.height).toBeLessThan(60);
    await expect(group.getByRole("button", { name: "Ajouter un service" })).toBeVisible();
    await expect(group.getByRole("button", { name: "Ajouter un stockage" })).toBeVisible();
    await page.locator("main").evaluate((item) => item.scrollTo(0, 0));
    await page.getByRole("button", { name: "Plus d'actions" }).click();
    await page.getByRole("menuitem", { name: "Avancé (YAML)" }).click();
    await expect(page.getByRole("button", { name: "Visuel" })).toBeVisible();
    await expect(group.getByRole("button", { name: /^Ajouter/ })).toHaveCount(0);
  });

  test("a phone adds from the title of each list", async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 800 });
    await open(page);
    await page.getByRole("button", { name: "Ajouter un stockage" }).locator("visible=true").click();
    await expect(page.getByRole("button", { name: /^media/ }).locator("visible=true")).toBeVisible();
    await page.getByRole("button", { name: "Ajouter un service" }).locator("visible=true").click();
    await expect(page.getByRole("button", { name: /^service-3/ }).locator("visible=true")).toBeVisible();
  });
});
