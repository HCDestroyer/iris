import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { esClaveManual, rutaManual } from "@iris/ui";
import { PASOS_ORDEN } from "@iris/sync/campo";
import { describe, expect, it } from "vitest";
import { CLAVES_CAMPO, claveManualCampo, rutaManualCampo } from "./campo";
import { claveManualDeRuta, clavesDelPanel, patronDeRuta } from "./claves";
import { RUTAS_PANEL } from "./rutas";

const aqui = path.dirname(fileURLToPath(import.meta.url));
const panel = path.resolve(aqui, "../../app/(panel)");

/**
 * Claves de la tabla del manual (`docs/manual/claves.md` de la PR #275, `agente/manual`). Cuando
 * esa tabla esté en el repositorio, la última prueba la lee directamente.
 */
const CLAVES_DEL_MANUAL = new Set(["administracion/accesos", "administracion/actividad-app", "administracion/argos", "administracion/asistente", "administracion/bandeja", "administracion/buzones", "administracion/centralita", "administracion/colas", "administracion/conexion-meta", "administracion/correo", "administracion/cuadrillas", "administracion/datos-legales", "administracion/datos-sin-empresa", "administracion/horarios", "administracion/indice", "administracion/integraciones", "administracion/llamadas", "administracion/manuales", "administracion/numeros", "administracion/plantillas", "administracion/plantillas-whatsapp", "administracion/respuestas-rapidas", "administracion/roles", "administracion/servidor-ia", "administracion/tokens", "administracion/zonas", "agenda/calendario", "campo/acciones-rapidas", "campo/agenda", "campo/bodega", "campo/chat", "campo/checklist", "campo/cierre", "campo/fotos", "campo/hoy", "campo/llegada", "campo/materiales", "campo/orden", "campo/pausa", "campo/por-enviar", "campo/por-recibir", "campo/sin-senal", "campo/ubicacion", "campo/yo", "comercial/cotizacion", "comercial/embudo", "comercial/nuevo", "comercial/prospecto", "conversaciones/bandeja", "conversaciones/boleta", "conversaciones/nueva", "conversaciones/orden", "conversaciones/prospecto", "cuenta/mfa", "cuenta/perfil", "despacho/tablero", "encuestas/lista", "equipo/chat", "inicio/tablero", "mapa/tecnicos", "ordenes/detalle", "ordenes/lista", "ordenes/nueva", "reportes/indicadores", "soporte/caso", "soporte/mis-casos", "soporte/nuevo", "tecnicos/argos", "tecnicos/ausencia", "tecnicos/ficha", "tecnicos/formulario", "tecnicos/lista", "tickets/detalle", "tickets/lista", "tickets/nuevo", "usuarios/ficha", "usuarios/formulario", "usuarios/lista",]);

/** Rutas de `app/(panel)` según sus `page.tsx`. */
function rutasEnDisco(dir = panel, base = ""): string[] {
    const salida: string[] = [];
    for (const nombre of readdirSync(dir)) {
        const completo = path.join(dir, nombre);
        if (statSync(completo).isDirectory()) salida.push(...rutasEnDisco(completo, `${base}/${nombre}`));
        else if (nombre === "page.tsx") salida.push(base === "" ? "/" : base);
    }
    return salida;
}

describe("claves del manual", () => {
    it("la lista de pantallas sigue a los page.tsx del panel", () => {
        expect([...RUTAS_PANEL].sort()).toEqual(rutasEnDisco().sort());
    });

    it("cada pantalla del panel y cada vista de /campo usa una clave de la tabla del manual", () => {
        for (const { ruta, clave } of clavesDelPanel()) {
            expect(clave, ruta).not.toBeNull();
            expect(esClaveManual(clave ?? ""), ruta).toBe(true);
            expect(CLAVES_DEL_MANUAL.has(clave ?? ""), `${ruta} → ${clave}`).toBe(true);
        }
        for (const clave of Object.values(CLAVES_CAMPO)) expect(CLAVES_DEL_MANUAL.has(clave), clave).toBe(true);
        for (const paso of PASOS_ORDEN) expect(CLAVES_DEL_MANUAL.has(claveManualCampo({ tipo: "orden", id: 1, paso })), paso).toBe(true);
    });

    it("de la ruta real a la clave: las fijas ganan a las de parámetro; sin pantalla, nada", () => {
        expect(patronDeRuta("/tickets/nuevo")).toBe("/tickets/nuevo");
        expect(claveManualDeRuta("/tickets/42")).toBe("tickets/detalle");
        expect(claveManualDeRuta("/tickets?vista=mias")).toBe("tickets/lista");
        expect(claveManualDeRuta("/")).toBe("inicio/tablero");
        expect(claveManualDeRuta("/usuarios/3/editar")).toBe("usuarios/formulario");
        expect(claveManualDeRuta("/administracion/numeros/7/llamadas")).toBe("administracion/numeros");
        expect(claveManualDeRuta("/administracion/integraciones/argos/vuelta")).toBe("administracion/argos");
        expect(claveManualDeRuta("/no-existe")).toBeNull();
    });

    it("panel: /manual/<clave>; dentro de /campo: /campo/manual/<pantalla> (abre sin señal)", () => {
        expect(rutaManual("tickets/lista")).toBe("/manual/tickets/lista");
        expect(rutaManualCampo(claveManualCampo({ tipo: "lista" }))).toBe("/campo/manual/hoy");
        expect(rutaManualCampo(claveManualCampo({ tipo: "orden", id: 7, paso: "fotos" }))).toBe("/campo/manual/fotos");
    });

    it("si la tabla del manual ya está en el repositorio, trae todas estas claves", () => {
        const doc = path.resolve(aqui, "../../../../docs/manual/claves.md");
        if (!existsSync(doc)) return;
        const tabla = readFileSync(doc, "utf8");
        for (const clave of CLAVES_DEL_MANUAL) expect(tabla, clave).toContain(`\`${clave}\``);
    });
});
