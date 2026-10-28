import { ordenDePrueba, itemDePrueba } from "@iris/sync/pruebas";
import type { OrdenVista } from "@iris/sync";
import { describe, expect, it } from "vitest";
import { interpretarCodigo, normalizarMac } from "./codigos";
import { esquemaEquipo, esquemaFallar, esquemaMaterial, esquemaNota, esquemaNumeroItem } from "./esquemas";
import { hoyEnGuatemala } from "./fechas";
import { enlaceComoLlegar } from "./gps";
import { MANIFEST_CAMPO } from "./manifest";
import { accionesDe, faltantesCierre } from "./reglas";
import { describirCampo, detallesProblema, textoProblema } from "./textos";
import { urlDeVista, vistaDeUrl } from "./vista";

const vista = (parcial: Partial<OrdenVista> = {}): OrdenVista => ({
    ...ordenDePrueba(1),
    reasignada: false,
    pendientes: 0,
    rechazos: 0,
    bloqueada: false,
    esperandoAdjuntos: false,
    subidasPendientes: 0,
    ...parcial,
});

describe("acciones visibles según estado y tipo", () => {
    it("programada: en camino, llegué y no se pudo; nada de trabajo", () => {
        expect(accionesDe(vista({ estado: "programada" }))).toMatchObject({ iniciarCamino: true, llegar: true, trabajo: false, cerrar: false, fallar: true });
    });
    it("en sitio: trabajo y cierre; instalar solo en instalación y cambio de equipo, retirar en retiro y cambio", () => {
        expect(accionesDe(vista({ estado: "en_sitio", tipo: "instalacion" }))).toMatchObject({ trabajo: true, cerrar: true, instalar: true, retirar: false, llegar: false });
        expect(accionesDe(vista({ estado: "en_sitio", tipo: "retiro" }))).toMatchObject({ instalar: false, retirar: true });
        expect(accionesDe(vista({ estado: "en_sitio", tipo: "cambio_equipo" }))).toMatchObject({ instalar: true, retirar: true });
        expect(accionesDe(vista({ estado: "en_sitio", tipo: "visita" }))).toMatchObject({ instalar: false, retirar: false });
    });
    it("cerrada, cancelada o reasignada: solo notas (no en reasignada)", () => {
        expect(accionesDe(vista({ estado: "cerrada" }))).toMatchObject({ trabajo: false, fallar: false, iniciarCamino: false, notas: true });
        expect(accionesDe(vista({ estado: "cancelada" }))).toMatchObject({ trabajo: false, notas: true });
        expect(accionesDe(vista({ estado: "en_sitio", reasignada: true }))).toMatchObject({ trabajo: false, notas: false });
        expect(accionesDe(vista({ estado: "pendiente" }))).toMatchObject({ iniciarCamino: false, llegar: false, fallar: false });
    });
});

describe("faltantes para cerrar", () => {
    it("checklist obligatorio, equipos del tipo y firma", () => {
        const o = vista({
            estado: "en_sitio",
            tipo: "cambio_equipo",
            requiereFirma: true,
            checklist: [itemDePrueba(1, { etiqueta: "Potencia", obligatorio: true }), itemDePrueba(2, { etiqueta: "Opcional" })],
        });
        expect(faltantesCierre(o)).toEqual([
            "Falta «Potencia» del checklist.",
            "Registra el equipo instalado.",
            "Registra el equipo retirado.",
            "Falta la firma del cliente.",
        ]);
        expect(faltantesCierre(vista({ tipo: "visita", checklist: [itemDePrueba(1, { obligatorio: true, valor: false as never })] }))).toEqual([]);
    });
});

describe("textos de rechazo", () => {
    const orden = { checklist: [itemDePrueba(3, { codigo: "fachada", etiqueta: "Foto de la fachada" })] };
    it("explica el código y cada campo que falta", () => {
        const problema = {
            type: "urn:iris:problema:faltan_adjuntos",
            title: "Faltan adjuntos",
            status: 422,
            errors: [
                { field: "adjuntos.foto:fachada", message: "x" },
                { field: "adjuntos.firma", message: "x" },
            ],
        };
        expect(textoProblema(problema)).toBe("Faltan fotos o la firma por terminar de subir.");
        expect(detallesProblema(problema, orden)).toEqual(["Falta subir la foto «Foto de la fachada».", "Falta subir la firma del cliente."]);
        expect(describirCampo("checklist.fachada", "", orden)).toBe("Falta el ítem «Foto de la fachada» del checklist.");
        expect(describirCampo("equipos.retirar", "")).toBe("Registra el equipo retirado.");
        expect(describirCampo("datos.cantidad", "Debe ser mayor que 0")).toBe("Debe ser mayor que 0");
    });
    it("un código desconocido usa el detalle de la API", () => {
        expect(textoProblema({ type: "urn:iris:problema:otro", title: "Otro", status: 422, detail: "Algo pasó" })).toBe("Algo pasó");
        expect(textoProblema(null)).toBe("El servidor no aceptó la acción.");
    });
});

describe("escáner", () => {
    it("normaliza MAC y separa serie y MAC de un QR", () => {
        expect(normalizarMac("a0b1c2d3e4f5")).toBe("A0:B1:C2:D3:E4:F5");
        expect(normalizarMac("A0-B1-C2-D3-E4-F5")).toBe("A0:B1:C2:D3:E4:F5");
        expect(normalizarMac("ZTEG12345678")).toBeNull();
        expect(interpretarCodigo("ZTEGC8F1A2B3")).toEqual({ serie: "ZTEGC8F1A2B3" });
        expect(interpretarCodigo("a0:b1:c2:d3:e4:f5")).toEqual({ mac: "A0:B1:C2:D3:E4:F5" });
        expect(interpretarCodigo("SN:HWTC1234;MAC:a0b1c2d3e4f5")).toEqual({ serie: "HWTC1234", mac: "A0:B1:C2:D3:E4:F5" });
        expect(interpretarCodigo('{"sn":"X1","mac":"a0b1c2d3e4f5"}')).toEqual({ serie: "X1", mac: "A0:B1:C2:D3:E4:F5" });
    });
});

describe("formularios (espejo de la API)", () => {
    it("material: cantidad > 0 con hasta 3 decimales y coma decimal", () => {
        expect(esquemaMaterial.safeParse({ productoId: "900", cantidad: "2,5" }).data).toEqual({ productoId: 900, cantidad: 2.5 });
        expect(esquemaMaterial.safeParse({ productoId: "900", cantidad: "0" }).success).toBe(false);
        expect(esquemaMaterial.safeParse({ productoId: "900", cantidad: "1.2345" }).success).toBe(false);
        expect(esquemaMaterial.safeParse({ productoId: "", cantidad: "1" }).success).toBe(false);
    });
    it("equipo: serie o unidad; MAC válida; condición al retirar", () => {
        const base = { accion: "instalar", serie: "S1", mac: "", modelo: "", unidadId: "", condicion: "" } as const;
        expect(esquemaEquipo.safeParse(base).success).toBe(true);
        expect(esquemaEquipo.safeParse({ ...base, serie: "" }).success).toBe(false);
        expect(esquemaEquipo.safeParse({ ...base, mac: "xyz" }).success).toBe(false);
        expect(esquemaEquipo.safeParse({ ...base, accion: "retirar" }).success).toBe(false);
        expect(esquemaEquipo.safeParse({ ...base, accion: "retirar", condicion: "danada" }).success).toBe(true);
    });
    it("fallar: detalle obligatorio con «otro»; nota sin vacíos ni NUL", () => {
        expect(esquemaFallar.safeParse({ motivoFalla: "otro", detalle: "" }).success).toBe(false);
        expect(esquemaFallar.safeParse({ motivoFalla: "cliente_ausente", detalle: "" }).success).toBe(true);
        expect(esquemaNota.safeParse({ texto: "  " }).success).toBe(false);
        expect(esquemaNota.safeParse({ texto: "a\u0000b" }).success).toBe(false);
        expect(esquemaNumeroItem.safeParse("-18,5").data).toBe(-18.5);
    });
});

describe("varios", () => {
    it("el día del paquete es el de Guatemala", () => {
        expect(hoyEnGuatemala(Date.parse("2026-10-29T05:30:00Z"))).toBe("2026-10-28");
        expect(hoyEnGuatemala(Date.parse("2026-10-29T06:30:00Z"))).toBe("2026-10-29");
    });
    it("«Cómo llegar» con coordenadas o con la dirección", () => {
        expect(enlaceComoLlegar(14.6, -90.5, "x")).toBe("https://www.google.com/maps/dir/?api=1&destination=14.6,-90.5");
        expect(enlaceComoLlegar(null, null, "6a avenida 1-23, zona 1")).toContain("destination=6a%20avenida");
        expect(enlaceComoLlegar(null, null, " ")).toBeNull();
    });
    it("la vista va en la URL (?orden= / ?vista=pendientes)", () => {
        expect(vistaDeUrl("?orden=12")).toEqual({ tipo: "orden", id: 12 });
        expect(vistaDeUrl("?vista=pendientes")).toEqual({ tipo: "pendientes" });
        expect(vistaDeUrl("?orden=abc")).toEqual({ tipo: "lista" });
        expect(urlDeVista({ tipo: "orden", id: 3 })).toBe("/campo?orden=3");
    });
    it("manifest: Iris Campo, standalone, alcance /campo y el logo", () => {
        expect(MANIFEST_CAMPO).toMatchObject({ name: "Iris Campo", display: "standalone", start_url: "/campo", scope: "/campo", theme_color: "#0f766e" });
        expect(MANIFEST_CAMPO.icons[0]?.src).toBe("/icon.svg");
    });
});
