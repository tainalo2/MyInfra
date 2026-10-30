import { createContext, useContext, useEffect, useLayoutEffect, useState, type ReactNode } from "react";

export type Theme = "light" | "dark";
export type ThemeChoice = Theme | "system";

const ThemeContext = createContext<{ theme: Theme; choice: ThemeChoice; choose: (choice: ThemeChoice) => void; toggle: () => void } | null>(null);

const darkQuery = "(prefers-color-scheme: dark)";

function storedChoice(): ThemeChoice {
  try {
    const value = localStorage.getItem("myinfra-theme");
    return value === "dark" || value === "system" ? value : "light";
  } catch {
    return "light";
  }
}

function systemTheme(): Theme {
  return typeof window !== "undefined" && window.matchMedia?.(darkQuery).matches ? "dark" : "light";
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [choice, setChoice] = useState<ThemeChoice>(storedChoice);
  const [system, setSystem] = useState<Theme>(systemTheme);
  const theme = choice === "system" ? system : choice;

  // The system theme is followed while the page is open.
  useEffect(() => {
    if (choice !== "system" || !window.matchMedia) return;
    const query = window.matchMedia(darkQuery);
    const change = () => setSystem(query.matches ? "dark" : "light");
    change();
    query.addEventListener("change", change);
    return () => query.removeEventListener("change", change);
  }, [choice]);

  useLayoutEffect(() => {
    document.documentElement.classList.toggle("dark", theme === "dark");
  }, [theme]);

  useLayoutEffect(() => {
    try {
      localStorage.setItem("myinfra-theme", choice);
    } catch {
      /* le choix reste sur cette page */
    }
  }, [choice]);

  return (
    <ThemeContext.Provider value={{ theme, choice, choose: setChoice, toggle: () => setChoice(theme === "dark" ? "light" : "dark") }}>
      {children}
    </ThemeContext.Provider>
  );
}

export function useTheme() {
  const value = useContext(ThemeContext);
  if (!value) throw new Error("thème absent");
  return value;
}
