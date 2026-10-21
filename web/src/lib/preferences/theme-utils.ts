import type { ResolvedThemeMode, ThemeMode } from "./theme";

export function resolveThemeMode(mode: ThemeMode): ResolvedThemeMode {
  return mode;
}

export function applyThemeMode(mode: ThemeMode): ResolvedThemeMode {
  const resolved = resolveThemeMode(mode);
  const doc = document.documentElement;
  doc.setAttribute("data-theme-mode", mode);
  doc.classList.add("disable-transitions");
  doc.classList.toggle("dark", resolved === "dark");
  doc.style.colorScheme = resolved;
  requestAnimationFrame(() => {
    doc.classList.remove("disable-transitions");
  });
  return resolved;
}

export function applyThemePreset(value: string) {
  document.documentElement.setAttribute("data-theme-preset", value);
}
