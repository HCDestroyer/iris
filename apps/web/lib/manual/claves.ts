// Claves del manual de Iris (contrato con el manual, PR #275 `agente/manual`): cada pantalla tiene
// una clave `modulo/pantalla` y su «?» (`BotonManual`) abre `/manual/<clave>`. La tabla de claves
// es la del manual (`docs/manual/claves.md` de esa rama): aquí solo se dice qué pantalla del panel
// usa cada una. Varias pantallas pueden compartir una página (p. ej. nuevo y editar).
import { esClaveManual } from "@iris/ui";
import { RUTAS_PANEL } from "./rutas";

/** Rutas con su clave propia (escritas como en `app/(panel)`). */
const EXACTAS: Readonly<Record<string, string>> = {
    "/": "inicio/tablero",
    "/administracion": "administracion/indice",
    "/administracion/integraciones": "administracion/integraciones",
    "/agenda": "agenda/calendario",
    "/comercial": "comercial/embudo",
    "/comercial/[id]": "comercial/prospecto",
    "/comercial/[id]/cotizacion": "comercial/cotizacion",
    "/comercial/nuevo": "comercial/nuevo",
    "/conversaciones": "conversaciones/bandeja",
    "/conversaciones/[id]/boleta": "conversaciones/boleta",
    "/conversaciones/[id]/orden": "conversaciones/orden",
    "/conversaciones/[id]/prospecto": "conversaciones/prospecto",
    "/conversaciones/nueva": "conversaciones/nueva",
    "/cuenta": "cuenta/perfil",
    "/cuenta/mfa": "cuenta/mfa",
    "/despacho": "despacho/tablero",
    "/encuestas": "encuestas/lista",
    "/equipo": "equipo/chat",
    "/mapa": "mapa/tecnicos",
    "/ordenes": "ordenes/lista",
    "/ordenes/[id]": "ordenes/detalle",
    "/ordenes/nueva": "ordenes/nueva",
    "/reportes": "reportes/indicadores",
    "/tecnicos": "tecnicos/lista",
    "/tecnicos/[id]": "tecnicos/ficha",
    "/tecnicos/[id]/ausencias/nueva": "tecnicos/ausencia",
    "/tecnicos/[id]/editar": "tecnicos/formulario",
    "/tecnicos/argos": "tecnicos/argos",
    "/tecnicos/nuevo": "tecnicos/formulario",
    "/tickets": "tickets/lista",
    "/tickets/[id]": "tickets/detalle",
    "/tickets/nuevo": "tickets/nuevo",
    "/usuarios": "usuarios/lista",
    "/usuarios/[id]": "usuarios/ficha",
    "/usuarios/[id]/editar": "usuarios/formulario",
    "/usuarios/nuevo": "usuarios/formulario",
};

/** Dentro de Administración, las integraciones con página propia (y todo lo que cuelga de ellas). */
const INTEGRACIONES: Readonly<Record<string, string>> = { argos: "administracion/argos", centralita: "administracion/centralita" };

const esParametro = (s: string) => s.startsWith("[") && s.endsWith("]");

/**
 * Clave de una ruta del panel escrita como en `app/(panel)` (`/tickets/[id]`). Las de
 * Administración van por sección: todo lo de `/administracion/numeros/…` es
 * `administracion/numeros` (nuevo, editar y fichas comparten la página de la sección).
 */
export function claveDePatron(patron: string): string | null {
    const exacta = EXACTAS[patron];
    if (exacta) return exacta;
    const [modulo, seccion, sub] = patron.split("/").filter(Boolean);
    if (modulo !== "administracion" || !seccion || esParametro(seccion)) return null;
    if (seccion === "integraciones") return sub ? (INTEGRACIONES[sub] ?? null) : "administracion/integraciones";
    return `administracion/${seccion}`;
}

/** ¿La ruta real (`/tickets/42`) es la del patrón (`/tickets/[id]`)? */
function coincide(patron: string, ruta: string): boolean {
    const p = patron.split("/").filter(Boolean);
    const r = ruta.split("/").filter(Boolean);
    return p.length === r.length && p.every((s, i) => esParametro(s) || s === r[i]);
}

/** Patrón de `app/(panel)` de una ruta real; las rutas fijas ganan a las de parámetro (`/tickets/nuevo`). */
export function patronDeRuta(ruta: string): string | null {
    const candidatos = RUTAS_PANEL.filter((p) => coincide(p, ruta));
    candidatos.sort((a, b) => a.split("[").length - b.split("[").length);
    return candidatos[0] ?? null;
}

/** Clave del manual de la pantalla abierta, o `null` si la ruta no es una pantalla del panel. */
export function claveManualDeRuta(ruta: string): string | null {
    const sinConsulta = ruta.split(/[?#]/)[0] ?? "/";
    const patron = patronDeRuta(sinConsulta.length > 1 ? sinConsulta.replace(/\/$/, "") : sinConsulta);
    if (!patron) return null;
    const clave = claveDePatron(patron);
    return clave && esClaveManual(clave) ? clave : null;
}

/** Todas las pantallas con su clave, en el orden de las rutas. */
export function clavesDelPanel(): { ruta: string; clave: string | null }[] {
    return RUTAS_PANEL.map((ruta) => ({ ruta, clave: claveDePatron(ruta) }));
}
