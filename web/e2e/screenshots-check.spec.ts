import fs from "node:fs";
import path from "node:path";
import { test } from "@playwright/test";

const dir = path.resolve(import.meta.dirname, "../../docs/screenshots");

const expected = [
  "overview.png",
  "overview-selection.png",
  "applications.png",
  "application.png",
  "catalogue.png",
  "clusters.png",
  "templates.png",
  "template-editor.png",
  "template-deploy.png",
  "template-run.png",
  "transfer.png",
  "redundancy.png",
  "providers.png",
  "dns.png",
  "registries.png",
  "images.png",
  "terminal.png",
  "terminal-pod.png",
  "mobile-overview.png",
  "mobile-application.png",
  "mobile-terminal.png",
];

test("les captures du site existent", () => {
  for (const name of expected) {
    const file = path.join(dir, name);
    if (!fs.existsSync(file)) {
      throw new Error(`capture manquante : ${name}`);
    }
    const size = fs.statSync(file).size;
    if (size < 8_000) {
      throw new Error(`capture trop petite : ${name} (${size} o)`);
    }
  }
});
