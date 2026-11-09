import { describe, expect, it } from "vitest";
import { cambiaDestino, entradaDe, esquemaAjustesAsistente, urlValida, valoresDe } from "./ajustes";
import { aplicarSugerencia, esVigente, explicarError, explicarErrorDeApi, formatearTokens, nombreMes, porcentajeUsadas, textoPrueba } from "./presentacion";
import type { AjustesAsistente, Sugerencia } from "./tipos";

const base: Sugerencia = {
    id: "s1",
    conversacionId: 41,
    paraMensajeId: "m1",
    estado: "generando",
    texto: null,
    fuentes: [],
    calificacion: null,
    origen: "automatico",
    creadaEn: "2026-11-09T15:00:00Z",
    actualizadaEn: "2026-11-09T15:00:00Z",
};
const lista: Sugerencia = { ...base, estado: "lista", texto: "Hola", actualizadaEn: "2026-11-09T15:00:20Z" };

describe("aplicarSugerencia", () => {
    it("muestra la nueva vigente y la reemplaza al cambiar de estado", () => {
        expect(aplicarSugerencia(null, base)).toEqual(base);
        expect(aplicarSugerencia(base, lista)).toEqual(lista);
    });
    it("un estado viejo de la misma sugerencia no pisa al nuevo (feed repetido o desordenado)", () => {
        expect(aplicarSugerencia(lista, base)).toEqual(lista);
    });
    it("la misma sugerencia vencida, usada o descartada desaparece", () => {
        for (const estado of ["vencida", "usada", "descartada"] as const) {
            expect(aplicarSugerencia(lista, { ...lista, estado, actualizadaEn: "2026-11-09T15:01:00Z" })).toBeNull();
        }
    });
    it("una vieja que deja de ser vigente no oculta la nueva", () => {
        const nueva: Sugerencia = { ...base, id: "s2", creadaEn: "2026-11-09T15:02:00Z", actualizadaEn: "2026-11-09T15:02:00Z" };
        expect(aplicarSugerencia(nueva, { ...lista, estado: "vencida", actualizadaEn: "2026-11-09T15:03:00Z" })).toEqual(nueva);
    });
    it("una vigente más vieja no reemplaza a la más nueva; una más nueva sí", () => {
        const nueva: Sugerencia = { ...base, id: "s2", creadaEn: "2026-11-09T15:02:00Z" };
        expect(aplicarSugerencia(nueva, lista)).toEqual(nueva);
        expect(aplicarSugerencia(lista, nueva)).toEqual(nueva);
    });
    it("ignora la de otra conversación", () => {
        expect(aplicarSugerencia(lista, { ...base, id: "x", conversacionId: 7, creadaEn: "2026-11-09T16:00:00Z" })).toEqual(lista);
    });
    it("vigentes: generando, lista y error", () => {
        expect(["generando", "lista", "error", "usada", "vencida", "descartada"].map((estado) => esVigente({ estado: estado as Sugerencia["estado"] }))).toEqual([true, true, true, false, false, false]);
    });
});

describe("explicarError", () => {
    it("tope y sin configurar explican y no reintentan", () => {
        for (const codigo of ["tope_alcanzado", "sin_configurar", "asistente_inactivo", "proveedor_redirige"]) {
            expect(explicarError(codigo, null, "sugerencia").reintentar).toBe(false);
        }
    });
    it("los del proveedor muestran el detalle del servidor y reintentan, con el sujeto", () => {
        expect(explicarError("proveedor_no_responde", "El servidor de IA no respondió a tiempo", "sugerencia")).toEqual({
            titulo: "No se pudo preparar la sugerencia",
            texto: "El servidor de IA no respondió a tiempo",
            reintentar: true,
        });
        expect(explicarError("proveedor_error", null, "resumen").titulo).toBe("No se pudo resumir");
        expect(explicarError("sin_mensaje", null, "resumen").titulo).toBe("Todavía no hay qué resumir");
    });
    it("429: cuánto esperar (Retry-After o 30 s) y «Reintentar» deshabilitado hasta entonces", () => {
        const base = { codigo: "demasiados_pedidos", titulo: "Demasiados pedidos", detalle: undefined };
        expect(explicarErrorDeApi({ ...base, estado: 429, reintentarEn: 12 }, "sugerencia", 1000)).toEqual({
            titulo: "Demasiados pedidos",
            texto: "Vuelve a intentar en 12 s.",
            reintentar: true,
            esperarHasta: 13_000,
        });
        expect(explicarErrorDeApi({ ...base, codigo: "demasiadas_sugerencias", estado: 429, reintentarEn: undefined }, "sugerencia", 0).esperarHasta).toBe(30_000);
        expect(explicarErrorDeApi({ ...base, codigo: "tope_alcanzado", estado: 422, reintentarEn: undefined }, "resumen", 0).reintentar).toBe(false);
        expect(explicarErrorDeApi({ ...base, codigo: "sin_conexion", estado: 0, reintentarEn: undefined }, "resumen", 0).titulo).toBe("No se pudo resumir");
    });
});

describe("cifras", () => {
    it("tokens en palabras", () => {
        expect(formatearTokens(2_100_000)).toBe("2.1 millones");
        expect(formatearTokens(1_000_000)).toBe("1 millón");
        expect(formatearTokens(5_000_000)).toBe("5 millones");
        expect(formatearTokens(850_000)).toBe("850 mil");
        expect(formatearTokens(1200)).toBe("1,200");
    });
    it("prueba de conexión", () => {
        expect(textoPrueba({ estado: "ok", latenciaMs: 1840, tokensPorSegundo: 37.3, detalle: "" })).toBe("Respondió en 1.8 s · 28 palabras por segundo");
        expect(textoPrueba({ estado: "ok", latenciaMs: 900, tokensPorSegundo: null, detalle: "" })).toBe("Respondió en 0.9 s");
        expect(textoPrueba({ estado: "error", latenciaMs: 0, tokensPorSegundo: null, detalle: "La clave no es válida (401)" })).toBe("La clave no es válida (401)");
    });
    it("% de sugerencias usadas, sin inventar un 0 %", () => {
        expect(porcentajeUsadas({ sugerencias: 1530, usadas: 1040 })).toBe(68);
        expect(porcentajeUsadas({ sugerencias: 0, usadas: 0 })).toBeNull();
    });
    it("nombre del mes", () => {
        expect(nombreMes("2026-11")).toBe("noviembre de 2026");
    });
});

const ajustes: AjustesAsistente = {
    activo: false,
    proveedor: "ollama",
    url: "",
    claveGuardadaEn: null,
    modelo: "",
    modo: "automatico",
    fuentes: { manuales: true, argos: true, horus: true, historial: true },
    topeTokensMes: 2_000_000,
    precioEntradaMillon: null,
    precioSalidaMillon: null,
    version: 0,
};

describe("formulario de ajustes (espejo del servidor)", () => {
    const valida = (cambios: Partial<ReturnType<typeof valoresDe>>) => esquemaAjustesAsistente.safeParse({ ...valoresDe(ajustes), ...cambios });
    const campos = (r: ReturnType<typeof valida>) => (r.success ? [] : r.error.issues.map((i) => i.path.join(".")));

    it("apagado y sin datos se puede guardar", () => {
        expect(valida({}).success).toBe(true);
    });
    it("activar pide dirección y modelo (salvo Anthropic) y clave con Anthropic", () => {
        expect(campos(valida({ activo: true }))).toEqual(["url", "modelo"]);
        expect(campos(valida({ activo: true, proveedor: "anthropic" }))).toEqual(["clave"]);
        expect(valida({ activo: true, proveedor: "anthropic", clave: "sk-1" }).success).toBe(true);
        expect(valida({ activo: true, proveedor: "anthropic", proveedorGuardado: "anthropic", claveGuardada: true, cambiarClave: false }).success).toBe(true);
    });
    it("con clave guardada, otro proveedor u otra dirección piden la clave de nuevo (422 `clave_requerida`)", () => {
        const guardada = { claveGuardada: true, cambiarClave: false, proveedorGuardado: "openai_compatible" as const, urlGuardada: "https://openrouter.ai/api/v1", proveedor: "openai_compatible" as const };
        expect(valida({ ...guardada, url: "https://openrouter.ai/api/v1" }).success).toBe(true);
        // La misma dirección normalizada (barra final, sin /v1) no cuenta como cambio.
        expect(valida({ ...guardada, url: "https://openrouter.ai/api/" }).success).toBe(true);
        expect(cambiaDestino({ ...guardada, url: "https://openrouter.ai/api/" })).toBe(false);
        expect(campos(valida({ ...guardada, url: "https://api.groq.com/openai/v1" }))).toEqual(["clave"]);
        expect(campos(valida({ ...guardada, url: "https://openrouter.ai/api/v1", proveedor: "anthropic" }))).toEqual(["clave"]);
        expect(valida({ ...guardada, url: "https://api.groq.com/openai/v1", cambiarClave: true, clave: "gsk-1" }).success).toBe(true);
        // Ollama: «Quitar clave» manda `""`.
        const ollama = { ...guardada, proveedor: "ollama" as const, url: "http://10.0.0.20:11434" };
        expect(campos(valida(ollama))).toEqual(["clave"]);
        expect(valida({ ...ollama, quitarClave: true }).success).toBe(true);
        expect(entradaDe({ ...valoresDe(ajustes), ...ollama, quitarClave: true }, 4)).toMatchObject({ clave: "" });
        // Anthropic vacía es la oficial.
        expect(cambiaDestino({ proveedor: "anthropic", url: "", proveedorGuardado: "anthropic", urlGuardada: "https://api.anthropic.com/" })).toBe(false);
    });
    it("dirección http/https sin credenciales ni parámetros", () => {
        expect(urlValida("http://10.0.0.20:11434")).toBe(true);
        expect(urlValida("ftp://x")).toBe(false);
        expect(urlValida("https://a:b@x.com")).toBe(false);
        expect(urlValida("https://x.com/v1?k=1")).toBe(false);
        expect(campos(valida({ url: "no es url" }))).toEqual(["url"]);
    });
    it("tope y precios", () => {
        expect(campos(valida({ topeTokensMes: "999" }))).toEqual(["topeTokensMes"]);
        expect(campos(valida({ topeTokensMes: "1000000001" }))).toEqual(["topeTokensMes"]);
        expect(campos(valida({ precioEntradaMillon: "3.00", precioSalidaMillon: "abc" }))).toEqual(["precioSalidaMillon"]);
    });
    it("la clave solo viaja si se escribió una nueva (nunca \"\")", () => {
        const guardada = valoresDe({ ...ajustes, claveGuardadaEn: "2026-11-01T00:00:00Z" });
        expect(entradaDe(guardada, 3)).not.toHaveProperty("clave");
        expect(entradaDe({ ...guardada, cambiarClave: true, clave: "" }, 3)).not.toHaveProperty("clave");
        expect(entradaDe({ ...guardada, cambiarClave: true, clave: " sk-2 " }, 3)).toMatchObject({ clave: "sk-2", version: 3 });
        expect(entradaDe({ ...guardada, precioEntradaMillon: "" }, 3).precioEntradaMillon).toBeNull();
    });
});
