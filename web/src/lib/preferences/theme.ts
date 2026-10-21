export const THEME_MODE_VALUES = ["light", "dark"] as const;
export type ThemeMode = (typeof THEME_MODE_VALUES)[number];
export type ResolvedThemeMode = ThemeMode;

export const THEME_PRESET_OPTIONS = [
  { value: "red", primary: { light: "#B51B2B", dark: "#C81E30" } },
  { value: "blue", primary: { light: "#1E46B8", dark: "#2F5FDC" } },
] as const;
export const THEME_PRESET_VALUES = THEME_PRESET_OPTIONS.map((preset) => preset.value);
export type ThemePreset = (typeof THEME_PRESET_VALUES)[number];

export const THEME_VARIANTS = [
  { value: "red-light", preset: "red", mode: "light", color: "#B51B2B" },
  { value: "red-dark", preset: "red", mode: "dark", color: "#C81E30" },
  { value: "blue-light", preset: "blue", mode: "light", color: "#1E46B8" },
  { value: "blue-dark", preset: "blue", mode: "dark", color: "#2F5FDC" },
] as const;

export type ThemeVariant = (typeof THEME_VARIANTS)[number];
