// The page is written in English. A translation replaces each block of text by
// its version in the chosen language, keyed by the English text itself, so the
// page needs no key on any element. Icons, buttons and commands stay as they are.
(function () {
  const langs = ["en", "fr", "de", "it", "pt"];
  const storageKey = "myinfra-page-lang";
  // Children a translation never replaces.
  const kept = (node) =>
    node.nodeType === 1 && (node.matches("svg, button, select, img, i, .dot, picture, pre") || node.querySelector("svg, button, select, img, pre"));
  const skipped = "script, style, pre, code, svg, select, option, .chips .chip, [data-i18n-skip]";

  function key(text) {
    return text.replace(/\s+/g, " ").trim();
  }

  // blocks are the elements that hold text directly: a title, a paragraph, a list item, a link.
  function blocks(root) {
    const out = [];
    const walk = (el) => {
      if (el.matches(skipped)) return;
      const direct = [...el.childNodes].some((n) => n.nodeType === 3 && n.textContent.trim());
      if (direct) {
        out.push(el);
        return;
      }
      for (const child of el.children) walk(child);
    };
    walk(root);
    return out;
  }

  function contentOf(el) {
    const parts = [...el.childNodes].filter((n) => !kept(n));
    const holder = document.createElement("div");
    for (const n of parts) holder.appendChild(n.cloneNode(true));
    return key(holder.innerHTML);
  }

  const originals = new WeakMap();
  const attrs = ["alt", "aria-label", "title", "content"];

  function collect() {
    const texts = new Set();
    for (const el of blocks(document.body)) texts.add(contentOf(el));
    document.querySelectorAll("[alt], [aria-label], [title], meta[name='description'], meta[property^='og:']").forEach((el) => {
      for (const a of attrs) if (el.hasAttribute(a) && el.getAttribute(a).trim()) texts.add(key(el.getAttribute(a)));
    });
    texts.add(key(document.title));
    return [...texts].filter((t) => /[A-Za-z]/.test(t));
  }

  function apply(lang) {
    const table = (window.MYINFRA_PAGE_I18N || {})[lang] || {};
    const t = (text) => (lang === "en" ? text : table[key(text)] ?? text);
    for (const el of blocks(document.body)) {
      if (!originals.has(el)) originals.set(el, contentOf(el));
      const source = originals.get(el);
      const next = t(source);
      if (next === contentOf(el)) continue;
      const nodes = [...el.childNodes];
      const anchor = nodes.find((n) => !kept(n)) || null;
      const holder = document.createElement("template");
      holder.innerHTML = next;
      el.insertBefore(holder.content, anchor);
      for (const n of nodes) if (!kept(n)) n.remove();
    }
    document.querySelectorAll("[alt], [aria-label], [title], meta[name='description'], meta[property^='og:']").forEach((el) => {
      for (const a of attrs) {
        if (!el.hasAttribute(a)) continue;
        const store = "data-en-" + a;
        if (!el.hasAttribute(store)) el.setAttribute(store, el.getAttribute(a));
        el.setAttribute(a, t(el.getAttribute(store)));
      }
    });
    if (!document.documentElement.dataset.enTitle) document.documentElement.dataset.enTitle = document.title;
    document.title = t(document.documentElement.dataset.enTitle);
    document.documentElement.lang = lang;
    window.MYINFRA_T = t;
    document.dispatchEvent(new CustomEvent("myinfra:lang", { detail: lang }));
  }

  function chosen() {
    const asked = new URLSearchParams(location.search).get("lang");
    if (langs.includes(asked)) return asked;
    try {
      const stored = localStorage.getItem(storageKey);
      if (langs.includes(stored)) return stored;
    } catch {
      // Storage blocked: the browser language decides.
    }
    for (const preferred of navigator.languages || [navigator.language || "en"]) {
      const short = preferred.slice(0, 2).toLowerCase();
      if (langs.includes(short)) return short;
    }
    return "en";
  }

  window.MYINFRA_PAGE = { collect, apply, langs };
  document.addEventListener("DOMContentLoaded", () => {
    const lang = chosen();
    const select = document.getElementById("page-lang");
    if (select) {
      select.value = lang;
      select.addEventListener("change", () => {
        try {
          localStorage.setItem(storageKey, select.value);
        } catch {
          // Storage blocked: the choice lasts for this visit.
        }
        const url = new URL(location.href);
        url.searchParams.delete("lang");
        history.replaceState(null, "", url);
        apply(select.value);
      });
    }
    apply(lang);
  });
})();
