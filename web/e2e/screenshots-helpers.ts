import { expect, type Page } from "@playwright/test";

const password = "e2e-password-123";

export async function signIn(page: Page) {
  await page.goto("/");
  const fields = page.locator('input[type="password"]');
  const shell = page.locator("aside");
  await expect(fields.first().or(shell).first()).toBeVisible();
  if (await fields.first().isVisible()) {
    for (const field of await fields.all()) await field.fill(password);
    await page.locator('button[type="submit"]').click();
    await page.waitForURL((url) => !/\/(depart|entree)$/.test(url.pathname));
  }
  await page.request.post("/api/onboarding", { headers: { Origin: new URL(page.url()).origin } });
}
