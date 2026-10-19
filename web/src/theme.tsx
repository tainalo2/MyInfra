import { createContext, useContext, useLayoutEffect, useState, type ReactNode } from "react";

export type Theme = "light" | "dark";

const ThemeContext = createContext<{ theme: Theme; toggle: () => void } | null>(null);

function storedTheme(): Theme {
  try {
    return localStorage.getItem("myinfra-theme") === "dark" ? "dark" : "light";
  } catch {
    return "light";
  }
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setTheme] = useState<Theme>(storedTheme);

  useLayoutEffect(() => {
    document.documentElement.classList.toggle("dark", theme === "dark");
    try {
      localStorage.setItem("myinfra-theme", theme);
    } catch {
      /* le choix reste sur cette page */
    }
  }, [theme]);

  return (
    <ThemeContext.Provider value={{ theme, toggle: () => setTheme((value) => (value === "dark" ? "light" : "dark")) }}>
      {children}
    </ThemeContext.Provider>
  );
}

export function useTheme() {
  const value = useContext(ThemeContext);
  if (!value) throw new Error("thème absent");
  return value;
}
