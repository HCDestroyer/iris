/**
 * Paleta de gráficos del ecosistema Horus (copia de Horus Flow, apps/frontend/app/utils/chart-palette.ts).
 * Categórica en orden fijo, nunca cíclica; los colores de estado (success/warning/error) nunca se
 * usan como series. Validada para daltonismo (5 ranuras):
 * - claro sobre #ffffff: CVD adyacente ΔE ≥ 9,1; aguamarina, amarillo y magenta < 3:1 de contraste
 *   ⇒ leyenda/etiquetas visibles y tabla alternativa obligatorias.
 * - oscuro: CVD ≥ 8,4, todas ≥ 3:1.
 * Los canvas (ECharts, Chart.js) necesitan sRGB literal, no var() ni oklch: por eso hex.
 */
export const CATEGORICAL = {
  light: ['#2a78d6', '#eb6834', '#1baf7a', '#eda100', '#e87ba4'],
  dark: ['#3987e5', '#d95926', '#199e70', '#c98500', '#d55181'],
} as const

/**
 * Tinta del gráfico por tema, con los mismos valores que los tokens de main.css (neutros
 * zinc; el canvas de ECharts necesita colores sRGB literales, no `var()` ni oklch).
 * Al cambiar el tema se re-tematiza sin recargar datos (§5.3).
 */
export const CHART_INK = {
  light: {
    text: '#3f3f46',
    muted: '#52525b',
    grid: '#e4e4e7',
    axis: '#a1a1aa',
    surface: '#ffffff',
  },
  dark: { text: '#e4e4e7', muted: '#a1a1aa', grid: '#232a33', axis: '#4b5563', surface: '#131820' },
} as const

export type ChartMode = keyof typeof CHART_INK

/** Líneas de umbral (aviso / crítico): los colores de estado, solo como referencia, nunca como serie. */
export const THRESHOLD_INK = {
  light: { warning: '#a16207', critical: '#dc2626' },
  dark: { warning: '#e9b949', critical: '#f25c69' },
} as const
