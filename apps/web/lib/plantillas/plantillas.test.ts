import type { Schema } from "@iris/contratos";
import { describe, expect, it } from "vitest";
import { agruparChecklist, formularioQuitable, segundosPausada, textoDuracion } from "../ordenes/presentacion";
import { codigoDesde, cuerpoCrearPlantilla, cuerpoEditarPlantilla, datosDePlantilla, esquemaCrearPlantilla, ITEM_VACIO, opcionesDe, PLANTILLA_VACIA } from "./esquemas";
import { puedeModificar, puedeTocarComun } from "./permisos";
import { entradaPausar, esquemaPausar } from "../ordenes/esquemas";

const mensajes = (r: { success: boolean; error?: { issues: { path: PropertyKey[]; message: string }[] } }) =>
    Object.fromEntries((r.error?.issues ?? []).map((i) => [i.path.join("."), i.message]));

const valida = {
    ...PLANTILLA_VACIA,
    codigo: "revision_poste",
    nombre: "Revisión de poste",
    items: [
        { ...ITEM_VACIO, etiqueta: "Potencia", codigo: "potencia", tipo: "medicion" as const, unidad: "dBm", obligatorio: true },
        { ...ITEM_VACIO, etiqueta: "Estado del poste", codigo: "estado", tipo: "seleccion" as const, opciones: "Bueno\nRegular\n\nMalo" },
    ],
};

describe("plantillas (espejo de EntradaCrearPlantilla)", () => {
    it("acepta una plantilla válida y arma el cuerpo con posición = orden de la lista", () => {
        const d = esquemaCrearPlantilla.parse(valida);
        expect(cuerpoCrearPlantilla(d)).toEqual({
            codigo: "revision_poste",
            nombre: "Revisión de poste",
            requiereFirma: false,
            borrador: true,
            items: [
                { codigo: "potencia", etiqueta: "Potencia", tipo: "medicion", obligatorio: true, unidad: "dBm" },
                { codigo: "estado", etiqueta: "Estado del poste", tipo: "seleccion", obligatorio: false, opciones: ["Bueno", "Regular", "Malo"] },
            ],
        });
    });

    it("valida códigos, nombre, opciones y códigos de ítem únicos", () => {
        const r = esquemaCrearPlantilla.safeParse({
            ...valida,
            codigo: "X",
            nombre: "",
            items: [
                { ...ITEM_VACIO, etiqueta: "A", codigo: "a", tipo: "seleccion", opciones: "Sí\nSí" },
                { ...ITEM_VACIO, etiqueta: "B", codigo: "a" },
                { ...ITEM_VACIO, etiqueta: "", codigo: "Mal código", tipo: "seleccion" },
            ],
        });
        expect(mensajes(r)).toMatchObject({
            codigo: "Usa minúsculas, números y _ (de 2 a 60)",
            nombre: "Escribe el nombre",
            "items.0.opciones": "Hay opciones repetidas",
            "items.2.etiqueta": "Escribe la pregunta o la indicación",
            "items.2.codigo": "Usa minúsculas, números y _ (hasta 60)",
            "items.2.opciones": "Escribe al menos una opción",
        });
        expect(mensajes(esquemaCrearPlantilla.safeParse({ ...valida, items: [valida.items[0], { ...valida.items[0], etiqueta: "Otra" }] }))["items.1.codigo"]).toBe("Ya hay otro ítem con este código");
        expect(mensajes(esquemaCrearPlantilla.safeParse({ ...valida, items: [] })).items).toBe("Agrega al menos un ítem");
    });

    it("la unidad solo va en número y medición; las opciones solo en selección", () => {
        const d = esquemaCrearPlantilla.parse({ ...valida, items: [{ ...ITEM_VACIO, etiqueta: "Foto", codigo: "foto", tipo: "foto", unidad: "x", opciones: "a" }] });
        expect(cuerpoCrearPlantilla(d).items).toEqual([{ codigo: "foto", etiqueta: "Foto", tipo: "foto", obligatorio: false }]);
    });

    it("editar: los datos de la versión leída van y vuelven, con la versión de la fila", () => {
        const p = {
            nombre: "Instalación",
            descripcion: "",
            tipo: "instalacion" as const,
            requiereFirma: true,
            items: [
                { id: 2, codigo: "b", etiqueta: "B", tipo: "seleccion" as const, obligatorio: false, unidad: "", posicion: 2, opciones: ["x", "y"] },
                { id: 1, codigo: "a", etiqueta: "A", tipo: "si_no" as const, obligatorio: true, unidad: "", posicion: 1, opciones: null },
            ],
        };
        const datos = datosDePlantilla(p);
        expect(datos.items.map((i) => i.codigo)).toEqual(["a", "b"]);
        expect(cuerpoEditarPlantilla(datos, 7)).toEqual({
            version: 7,
            nombre: "Instalación",
            requiereFirma: true,
            tipo: "instalacion",
            items: [
                { codigo: "a", etiqueta: "A", tipo: "si_no", obligatorio: true },
                { codigo: "b", etiqueta: "B", tipo: "seleccion", obligatorio: false, opciones: ["x", "y"] },
            ],
        });
    });

    it("códigos sugeridos y opciones por línea", () => {
        expect(codigoDesde("¿Potencia óptica (dBm)?")).toBe("potencia_optica_dbm");
        expect(opcionesDe(" a \n\n b\n")).toEqual(["a", "b"]);
    });

    it("lo común solo con empresas.todas global (todasLasEmpresas) o sin conector", () => {
        const editar = (p: readonly string[]) => p.includes("plantillas.editar");
        // `empresas.todas` con alcance no da `todasLasEmpresas`: no basta.
        const conAlcance = (p: readonly string[]) => p.includes("empresas.todas") || editar(p);
        expect(puedeTocarComun({ modoEmpresa: "sin_conector", todasLasEmpresas: false })).toBe(true);
        expect(puedeTocarComun({ modoEmpresa: "activa", todasLasEmpresas: false })).toBe(false);
        expect(puedeTocarComun({ modoEmpresa: "activa", todasLasEmpresas: true })).toBe(true);
        expect(puedeModificar({ empresaId: null }, "plantillas.editar", { modoEmpresa: "activa", todasLasEmpresas: false }, conAlcance)).toBe(false);
        expect(puedeModificar({ empresaId: null }, "plantillas.editar", { modoEmpresa: "activa", todasLasEmpresas: true }, editar)).toBe(true);
        expect(puedeModificar({ empresaId: null }, "plantillas.editar", { modoEmpresa: "activa", todasLasEmpresas: true }, () => false)).toBe(false);
        expect(puedeModificar({ empresaId: 10 }, "plantillas.editar", { modoEmpresa: "activa", todasLasEmpresas: false }, editar)).toBe(true);
    });
});

describe("formularios y pausas de la orden", () => {
    const f = (id: string, origen: Schema<"FormularioOrden">["origen"], conRespuestas = false): Schema<"FormularioOrden"> => ({
        id,
        plantillaId: 1,
        codigo: id,
        nombre: id,
        versionId: 1,
        numeroVersion: 1,
        requiereFirma: false,
        origen,
        agregadoPor: null,
        agregadoEn: "2026-10-28T15:00:00Z",
        conRespuestas,
    });
    const item = (itemId: number, formularioId: string | undefined, posicion: number): Schema<"ItemChecklist"> => ({
        itemId,
        ...(formularioId ? { formularioId } : {}),
        codigo: `c${itemId}`,
        etiqueta: `I${itemId}`,
        tipo: "si_no",
        obligatorio: false,
        unidad: "",
        posicion,
        valor: null,
        marcadoPor: null,
        ocurridoEn: null,
    });

    it("agrupa el checklist por formulario, en su orden; lo suelto al final", () => {
        const grupos = agruparChecklist({ formularios: [f("a", "tipo"), f("b", "despacho")], checklist: [item(3, "b", 1), item(2, "a", 2), item(1, "a", 1), item(9, undefined, 1)] });
        expect(grupos.map((g) => [g.formulario?.id ?? null, g.items.map((i) => i.itemId)])).toEqual([
            ["a", [1, 2]],
            ["b", [3]],
            [null, [9]],
        ]);
    });

    it("se quita solo sin respuestas y nunca el del tipo", () => {
        expect(formularioQuitable(f("x", "despacho"))).toBe(true);
        expect(formularioQuitable(f("x", "tecnico"))).toBe(true);
        expect(formularioQuitable(f("x", "tipo"))).toBe(false);
        expect(formularioQuitable(f("x", "despacho", true))).toBe(false);
    });

    it("tiempo pausado: las terminadas más la que sigue abierta", () => {
        const ahora = Date.parse("2026-10-28T16:00:00Z");
        expect(segundosPausada({ estado: "pausada", tiempoPausadoSeg: 600, pausadaEn: "2026-10-28T15:30:00Z" }, ahora)).toBe(600 + 1800);
        expect(segundosPausada({ estado: "en_sitio", tiempoPausadoSeg: 600 }, ahora)).toBe(600);
        expect(textoDuracion(2400)).toBe("40 min");
        expect(textoDuracion(3600 * 2 + 300)).toBe("2 h 5 min");
        expect(textoDuracion(20)).toBe("menos de 1 min");
    });

    it("pausar: con «otro» la nota es obligatoria", () => {
        expect(mensajes(esquemaPausar.safeParse({ motivo: "otro", nota: " " }))).toEqual({ nota: "Con «Otro», explica el motivo" });
        expect(entradaPausar(esquemaPausar.parse({ motivo: "clima", nota: "" }), 4)).toEqual({ version: 4, motivo: "clima" });
        expect(entradaPausar(esquemaPausar.parse({ motivo: "otro", nota: "Perro bravo" }), 4)).toEqual({ version: 4, motivo: "otro", nota: "Perro bravo" });
    });
});
