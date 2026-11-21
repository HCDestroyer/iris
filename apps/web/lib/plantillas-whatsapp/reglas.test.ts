import { describe, expect, it } from "vitest";
import { definicionDe, esquemaPlantilla, revisarArchivoEjemplo, formatoDe, insertarDato, nombreMetaDe, partesVistaPrevia, revisar, sincronizarVariables, valoresDe } from "./reglas";
import type { DatoPWA, DefinicionPWA } from "./tipos";

const DATOS: DatoPWA[] = [
    { clave: "cliente_nombre", etiqueta: "Nombre del cliente", origen: "Cliente en Argos · nombre", ejemplo: "María López", codigos: [], grupo: "cliente" },
    { clave: "fecha_corte", etiqueta: "Fecha de corte", origen: "Factura · fecha de corte", ejemplo: "viernes 28 de noviembre", codigos: [], grupo: "cobro" },
    { clave: "monto_con_moneda", etiqueta: "Monto", origen: "Factura", ejemplo: "Q 250.00", codigos: [], grupo: "cobro" },
];

const def = (d: Partial<DefinicionPWA>): DefinicionPWA => ({ cuerpo: "", botones: [], variables: [], ...d });
const fallas = (d: DefinicionPWA, categoria: "UTILITY" | "MARKETING" | "AUTHENTICATION" = "UTILITY", nombre = "recordatorio_pago") =>
    revisar({ nombre, idioma: "es", categoria, definicion: d }, DATOS)
        .filter((r) => !r.ok)
        .map((r) => r.regla);

describe("revisar (reglas de Meta, espejo del servidor)", () => {
    it("una plantilla correcta pasa todas", () => {
        const d = def({
            cuerpo: "Hola {{cliente_nombre}}, tu factura de {{monto_con_moneda}} vence el {{fecha_corte}}. ¡Gracias!",
            variables: [
                { nombre: "cliente_nombre", dato: "cliente_nombre", ejemplo: "María López" },
                { nombre: "monto_con_moneda", dato: "monto_con_moneda", ejemplo: "Q 250.00" },
                { nombre: "fecha_corte", dato: "fecha_corte", ejemplo: "viernes 28" },
            ],
            botones: [{ tipo: "url", texto: "Pagar en línea", url: "https://pagos.kns.gt/pagar" }],
        });
        expect(fallas(d)).toEqual([]);
    });

    it("no empieza ni termina con un dato y lo dice con el nombre amigable", () => {
        const d = def({ cuerpo: "Tu corte es el {{fecha_corte}}", variables: [{ nombre: "fecha_corte", dato: "fecha_corte", ejemplo: "viernes" }] });
        const r = revisar({ nombre: "x", idioma: "es", categoria: "UTILITY", definicion: d }, DATOS).find((x) => x.regla === "variables_bordes");
        expect(r?.ok).toBe(false);
        expect(r?.mensaje).toContain("«Fecha de corte»");
        expect(fallas(def({ cuerpo: "{{fecha_corte}} es tu corte.", variables: [{ nombre: "fecha_corte", ejemplo: "x" }] }))).toContain("variables_bordes");
    });

    it("datos pegados, mezcla de formatos, orden de los números y ejemplos", () => {
        expect(fallas(def({ cuerpo: "Debes {{moneda}}{{monto}} hoy.", variables: [{ nombre: "moneda", ejemplo: "Q" }, { nombre: "monto", ejemplo: "1" }] }))).toContain("variables_seguidas");
        expect(fallas(def({ cuerpo: "Hola {{1}} y {{cliente_nombre}}.", variables: [{ nombre: "1", ejemplo: "a" }, { nombre: "cliente_nombre", ejemplo: "b" }] }))).toContain(
            "variables_formato",
        );
        expect(fallas(def({ cuerpo: "Hola {{2}} y {{1}}.", variables: [{ nombre: "1", ejemplo: "a" }, { nombre: "2", ejemplo: "b" }] }))).toContain("variables_formato");
        expect(fallas(def({ cuerpo: "Hola {{cliente_nombre}}.", variables: [{ nombre: "cliente_nombre", ejemplo: "" }] }))).toContain("ejemplos");
        expect(formatoDe(["1", "2"])).toBe("POSITIONAL");
        expect(formatoDe(["a", "1"])).toBe("");
    });

    it("largos y botones", () => {
        expect(fallas(def({ cuerpo: "a".repeat(1025) }))).toContain("largos");
        expect(fallas(def({ cuerpo: "Hola.", pie: "Pie con {{dato}}" }))).toContain("pie");
        const tres = [1, 2, 3].map((i) => ({ tipo: "url" as const, texto: `Página ${i}`, url: "https://kns.gt" }));
        expect(fallas(def({ cuerpo: "Hola.", botones: tres }))).toContain("botones");
        expect(
            fallas(
                def({
                    cuerpo: "Hola.",
                    botones: [
                        { tipo: "respuesta", texto: "Sí" },
                        { tipo: "url", texto: "Ver", url: "https://kns.gt" },
                        { tipo: "respuesta", texto: "No" },
                    ],
                }),
            ),
        ).toContain("botones");
        expect(fallas(def({ cuerpo: "Hola.", botones: [{ tipo: "url", texto: "Ver", url: "ftp://kns.gt" }] }))).toContain("botones");
        expect(fallas(def({ cuerpo: "Hola.", botones: [{ tipo: "url", texto: "Ver", url: "https://kns.gt/{{1}}/x" }] }))).toContain("botones");
    });

    it("verificación: sin enlaces ni emojis y con «Copiar código»", () => {
        expect(fallas(def({ botones: [{ tipo: "url", texto: "Ver", url: "https://kns.gt" }] }), "AUTHENTICATION")).toContain("autenticacion");
        expect(fallas(def({ botones: [{ tipo: "copiar_codigo", texto: "", ejemplo: "123456" }] }), "AUTHENTICATION")).toEqual([]);
    });

    it("el nombre en Meta: minúsculas, números y _", () => {
        expect(fallas(def({ cuerpo: "Hola." }), "UTILITY", "Recordatorio Pago")).toContain("nombre");
    });
});

describe("ayudas del formulario", () => {
    it("propone el nombre en Meta desde el nombre para personas", () => {
        expect(nombreMetaDe("Recordatorio de pago")).toBe("recordatorio_de_pago");
        expect(nombreMetaDe("  ¡Técnico en camino!  ")).toBe("tecnico_en_camino");
        expect(nombreMetaDe("😀")).toBe("plantilla");
    });

    it("inserta el dato en el cursor con un espacio si hace falta", () => {
        expect(insertarDato("Hola", 4, 4, "cliente_nombre")).toEqual({ texto: "Hola {{cliente_nombre}}", cursor: 23 });
        expect(insertarDato("Hola ,", 5, 5, "plan").texto).toBe("Hola {{plan}},");
    });

    it("las variables siguen al texto y conservan su ejemplo", () => {
        const d = def({ cuerpo: "Hola {{cliente_nombre}}, vence {{fecha_corte}}.", variables: [{ nombre: "fecha_corte", dato: "fecha_corte", ejemplo: "lunes" }] });
        expect(sincronizarVariables(d, DATOS)).toEqual([
            { nombre: "cliente_nombre", dato: "cliente_nombre", ejemplo: "María López" },
            { nombre: "fecha_corte", dato: "fecha_corte", ejemplo: "lunes" },
        ]);
    });

    it("vista previa en partes resaltadas", () => {
        expect(partesVistaPrevia("Hola {{a}}!", () => "Ana")).toEqual([
            { texto: "Hola ", dato: false },
            { texto: "Ana", dato: true },
            { texto: "!", dato: false },
        ]);
    });

    it("ida y vuelta entre valores del formulario y definición", () => {
        const d = def({ cuerpo: "Hola {{cliente_nombre}}.", pie: "Kingnet", encabezado: { tipo: "texto", texto: "Aviso" }, variables: [{ nombre: "cliente_nombre", dato: "cliente_nombre", ejemplo: "Ana" }] });
        const v = valoresDe({ etiqueta: "Aviso", nombre: "aviso", idioma: "es", categoria: "UTILITY", usos: ["avisos"], definicion: d });
        expect(esquemaPlantilla.safeParse(v).success).toBe(true);
        expect(definicionDe(v)).toMatchObject({ cuerpo: d.cuerpo, pie: "Kingnet", encabezado: { tipo: "texto", texto: "Aviso" } });
        expect(esquemaPlantilla.safeParse({ ...v, nombre: "Con Mayúsculas" }).success).toBe(false);
    });
});

describe("archivo de ejemplo del encabezado (antes de subirlo)", () => {
    const MB = 1024 * 1024;
    it("acepta el tipo y el tamaño que Meta pide para cada encabezado", () => {
        expect(revisarArchivoEjemplo("imagen", { type: "image/png", size: 2 * MB })).toBeNull();
        expect(revisarArchivoEjemplo("video", { type: "video/mp4", size: 15 * MB })).toBeNull();
        expect(revisarArchivoEjemplo("documento", { type: "application/pdf", size: MB })).toBeNull();
    });
    it("rechaza otro tipo o uno demasiado pesado", () => {
        expect(revisarArchivoEjemplo("imagen", { type: "application/pdf", size: MB })).toMatch(/JPG o PNG/);
        expect(revisarArchivoEjemplo("imagen", { type: "image/webp", size: MB })).toMatch(/JPG o PNG/);
        expect(revisarArchivoEjemplo("imagen", { type: "image/jpeg", size: 6 * MB })).toMatch(/5 MB/);
        expect(revisarArchivoEjemplo("video", { type: "video/mp4", size: 17 * MB })).toMatch(/16 MB/);
        expect(revisarArchivoEjemplo("documento", { type: "image/png", size: MB })).toMatch(/PDF/);
    });
});
