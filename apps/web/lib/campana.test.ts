import { describe, expect, it } from "vitest";
import { primeroDelMesSiguiente, textoPausado, tonoFuente } from "./asistente/presentacion";
import { detalleAvisoPanel, enlaceAvisoPanel, hayQueActuar, iconoAvisoPanel, marcarLeido, sesionVencida, sondeoActivo, tonoAvisoPanel, type AvisoPanel } from "./campana";

const aviso = (x: Partial<AvisoPanel> = {}): AvisoPanel => ({
    id: 1,
    tipo: "asistente_tope",
    nivel: "alerta",
    titulo: "El asistente de IA de Cable Norte usó el 80 % de su tope del mes",
    enlace: "/administracion/asistente",
    datos: { porcentaje: 80, mes: "2026-11" },
    leido: false,
    leidoEn: null,
    creadoEn: "2026-11-10T15:00:00Z",
    empresaId: 7,
    ...x,
});

describe("avisos del Servidor de IA en la campana", () => {
    // PROVISIONAL: los tipos nuevos aún no están en el tipo generado.
    const sia = (tipo: string, datos: AvisoPanel["datos"] = {}) => aviso({ tipo: tipo as AvisoPanel["tipo"], nivel: "peligro", enlace: "/administracion/servidor-ia", datos, empresaId: null });

    it("cada tipo tiene su icono y su segunda línea en llano", () => {
        expect(iconoAvisoPanel(sia("servidor_ia_caido").tipo)).not.toBe(iconoAvisoPanel(sia("otro").tipo));
        expect(iconoAvisoPanel(sia("servidor_ia_incompatible").tipo)).not.toBe(iconoAvisoPanel(sia("servidor_ia_umbral").tipo));
        expect(detalleAvisoPanel(sia("servidor_ia_caido"))).toContain("pasa a personas");
        expect(detalleAvisoPanel(sia("servidor_ia_incompatible"))).toContain("qué hay que actualizar");
        expect(detalleAvisoPanel(sia("servidor_ia_umbral", { tipo: "temperatura", valor: 88.4, umbral: 85 }))).toContain("llegó a 88 °C");
        expect(detalleAvisoPanel(sia("servidor_ia_umbral", { tipo: "vram", valor: 97, umbral: 95 }))).toContain("al 97 % (avisa al 95 %)");
        expect(detalleAvisoPanel(sia("servidor_ia_umbral", { tipo: "saturacion", valor: 12, umbral: 10 }))).toContain("Se rechazaron 12 peticiones");
        expect(detalleAvisoPanel(sia("servidor_ia_umbral", { tipo: "disco", valor: 93, umbral: 90 }))).toContain("al 93 %");
        // Un dato que no vino bien no se inventa.
        expect(detalleAvisoPanel(sia("servidor_ia_umbral", { tipo: "temperatura", valor: "mucho" }))).toContain("se calentó de más");
        expect(enlaceAvisoPanel(sia("servidor_ia_caido").enlace)).toBe("/administracion/servidor-ia");
    });
});

describe("avisos de la campana", () => {
    it("tono por nivel", () => {
        expect(tonoAvisoPanel("peligro")).toBe("error");
        expect(tonoAvisoPanel("alerta")).toBe("warning");
        expect(tonoAvisoPanel("info")).toBe("info");
    });

    it("al 100 % dice hasta cuándo queda pausado; al 80 %, qué pasa si sigue", () => {
        expect(detalleAvisoPanel(aviso({ nivel: "peligro", datos: { porcentaje: 100, mes: "2026-11" } }))).toBe("Pausado hasta el 1 de diciembre o hasta que subas el tope.");
        expect(detalleAvisoPanel(aviso({ nivel: "peligro", datos: {} }))).toBe("Pausado hasta el mes siguiente o hasta que subas el tope.");
        expect(detalleAvisoPanel(aviso())).toContain("Puedes subir el tope");
    });

    it("solo enlaces internos del panel", () => {
        expect(enlaceAvisoPanel("/administracion/asistente")).toBe("/administracion/asistente");
        expect(enlaceAvisoPanel("//malo.test")).toBeNull();
        expect(enlaceAvisoPanel("https://malo.test")).toBeNull();
        expect(enlaceAvisoPanel(null)).toBeNull();
    });

    it("resuelve como el navegador y exige el mismo origen", () => {
        const origen = "https://iris.fibra.test";
        expect(enlaceAvisoPanel("/administracion/asistente?mes=2026-11#uso", origen)).toBe("/administracion/asistente?mes=2026-11#uso");
        expect(enlaceAvisoPanel("/a/../administracion/asistente", origen)).toBe("/administracion/asistente");
        // El navegador quita tabuladores y saltos de línea y lee «\» como «/»: todo eso es `//evil.com`.
        for (const malo of ["/\t/evil.com", "/\n/evil.com", "//evil.com", "/\\evil.com", "/\r/evil.com", "/\u0000/evil.com", "/administracion\\asistente"]) {
            expect(enlaceAvisoPanel(malo, origen), JSON.stringify(malo)).toBeNull();
        }
        expect(enlaceAvisoPanel("javascript:alert(1)", origen)).toBeNull();
        expect(enlaceAvisoPanel("/administracion/asistente", "no es un origen")).toBeNull();
    });

    it("sondea solo con una lectura buena; un 401 es sesión vencida", () => {
        expect(sondeoActivo("listo")).toBe(true);
        expect(sondeoActivo("error")).toBe(false);
        expect(sondeoActivo("cargando")).toBe(false);
        expect(sesionVencida({ estado: 401 })).toBe(true);
        expect(sesionVencida({ estado: 500 })).toBe(false);
        expect(sesionVencida(new Error("red"))).toBe(false);
    });

    it("el punto crece solo con un `peligro` sin leer", () => {
        expect(hayQueActuar([aviso()])).toBe(false);
        expect(hayQueActuar([aviso({ nivel: "peligro" })])).toBe(true);
        expect(hayQueActuar([aviso({ nivel: "peligro", leido: true })])).toBe(false);
    });

    it("marcar leído cambia solo ese y no pisa `leidoEn`", () => {
        const lista = [aviso(), aviso({ id: 2, leido: true, leidoEn: "2026-11-01T00:00:00Z" })];
        const r = marcarLeido(lista, 1, "2026-11-10T16:00:00Z");
        expect(r[0]).toMatchObject({ leido: true, leidoEn: "2026-11-10T16:00:00Z" });
        expect(marcarLeido(lista, 2, "x")[1]?.leidoEn).toBe("2026-11-01T00:00:00Z");
    });
});

describe("textoPausado", () => {
    it("el 1 del mes siguiente, también en diciembre", () => {
        expect(primeroDelMesSiguiente("2026-11")).toBe("1 de diciembre");
        expect(primeroDelMesSiguiente("2026-12")).toBe("1 de enero");
        expect(textoPausado("2027-01")).toBe("Pausado hasta el 1 de febrero o hasta que subas el tope.");
    });
});

describe("tonoFuente", () => {
    it("Argos al día va en verde y con saldo vencido en rojo", () => {
        expect(tonoFuente({ tipo: "argos", etiqueta: "Servicio activo, al día · Argos" })).toBe("success");
        expect(tonoFuente({ tipo: "argos", etiqueta: "Con saldo vencido (2 facturas) · Argos" })).toBe("error");
    });

    it("Horus en rojo, manual normal e historial apagado", () => {
        expect(tonoFuente({ tipo: "horus", etiqueta: "Falla en Zona 4 · Horus" })).toBe("error");
        expect(tonoFuente({ tipo: "manual", etiqueta: "Manual «Sin internet»" })).toBe("neutral");
        expect(tonoFuente({ tipo: "historial", etiqueta: "Historial" })).toBe("apagado");
    });
});
