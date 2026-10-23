import type { CatalogueCard, CatalogueInput } from "./api";
import { categoryLabel, currentLang, text } from "./i18n";

// localizeCard gives a catalogue card in the language of the screen: each
// application carries its own words, in its package.
export function localizeCard(card: CatalogueCard): CatalogueCard {
  const words = card.translations?.[currentLang()];
  return {
    ...card,
    summary: words?.summary || card.summary,
    description: words?.description || card.description,
    inputs: card.inputs.map(localizeInput),
  };
}

function localizeInput(input: CatalogueInput): CatalogueInput {
  if (input.key === "email") return { ...input, label: text("input.email") };
  return input;
}

export function categoryName(name: string): string {
  return categoryLabel(name);
}

export function cardMatches(card: CatalogueCard, needle: string): boolean {
  if (!needle) return true;
  const local = localizeCard(card);
  const hay = `${card.name} ${card.summary} ${card.description} ${local.summary} ${local.description}`.toLowerCase();
  return hay.includes(needle);
}
