import type { Schema } from "@iris/contratos";
import { describe, expect, it } from "vitest";
import { ErrorApi } from "@/lib/api/errores";
import {
    bodegaEnEmpresa,
    conInventarioPorTecnico,
    nombreBodega,
    opcionesBodegaArgos,
    opcionTecnicoArgos,
    SIN_INVENTARIO_POR_TECNICO,
    sinListaDeArgos,
    textoBodegaTecnico,
    textoErrorVinculo,
} from "./argos";

const ana: Schema<"TecnicoDeArgos"> = {
    idExterno: "77",
    nombre: "Ana Cux",
    correo: "ana@argos.test",
    enEmpresa: true,
    bodegas: [
        { id: 30, empresaId: 10, activa: true, nombre: "Bodega Norte" },
        { id: 31, empresaId: 10, activa: false, nombre: "Bodega vieja" },
    ],
};

describe("vínculo del técnico con Argos", () => {
    it("la capacidad sale del conector de Argos activo", () => {
        expect(conInventarioPorTecnico([{ sistema: "argosisp", estado: "activo", capacidades: ["inventario_por_tecnico"] }])).toBe(true);
        expect(conInventarioPorTecnico([{ sistema: "argosisp", estado: "activo", capacidades: [] }])).toBe(false);
        expect(conInventarioPorTecnico([{ sistema: "argosisp", estado: "activo" }])).toBe(false);
        expect(conInventarioPorTecnico([{ sistema: "argosisp", estado: "apagado", capacidades: ["inventario_por_tecnico"] }])).toBeNull();
        expect(conInventarioPorTecnico([{ sistema: "horus", estado: "activo", capacidades: ["inventario_por_tecnico"] }])).toBeNull();
    });

    it("solo se elige con bodega en la empresa y sin otra ficha", () => {
        expect(opcionTecnicoArgos(ana, 21)).toEqual({ texto: "Ana Cux · ana@argos.test · Bodega Norte", elegible: true });
        expect(opcionTecnicoArgos({ ...ana, enEmpresa: false }, 21).elegible).toBe(false);
        expect(opcionTecnicoArgos({ ...ana, vinculadoA: { tecnicoId: 22, usuarioId: 9 } }, 21)).toMatchObject({ elegible: false, texto: expect.stringContaining("ya vinculado a otra ficha") });
        // Ya vinculado a esta misma ficha: se puede volver a elegir.
        expect(opcionTecnicoArgos({ ...ana, vinculadoA: { tecnicoId: 21, usuarioId: 9 } }, 21).elegible).toBe(true);
    });

    it("explica los errores de negocio", () => {
        const e = (codigo: string, estado = 422) => new ErrorApi({ estado, codigo, titulo: "x" });
        expect(textoErrorVinculo(e("argos_sin_inventario_tecnico"))).toBe(SIN_INVENTARIO_POR_TECNICO);
        expect(textoErrorVinculo(e("argos_vinculado_a_otro", 409))).toMatch(/otra ficha/);
        expect(textoErrorVinculo(e("validacion"))).toBeUndefined();
        expect(textoErrorVinculo(new Error("x"))).toBeUndefined();
    });

    it("bodega por nombre (0.6.4) y «Bodega <id>» con un Argos anterior", () => {
        expect(nombreBodega({ id: 30, nombre: " Bodega Norte " })).toBe("Bodega Norte");
        expect(nombreBodega({ id: 30, nombre: "" })).toBe("Bodega 30");
        const multi: Schema<"TecnicoDeArgos"> = { ...ana, bodegas: [...(ana.bodegas ?? []), { id: 40, empresaId: 20, activa: true, nombre: "Bodega Xela" }] };
        expect(bodegaEnEmpresa(multi, 20)?.id).toBe(40);
        expect(bodegaEnEmpresa(multi, null)?.id).toBe(30);
        expect(bodegaEnEmpresa(multi, 99)).toBeUndefined();
        expect(textoBodegaTecnico(multi, 10)).toBe("Bodega Norte · Ana Cux");
        expect(textoBodegaTecnico(multi, null)).toBe("Bodega Norte · Ana Cux");
        expect(textoBodegaTecnico({ ...ana, bodegas: [] }, 10)).toBe("Sin bodega activa · Ana Cux");
    });

    it("opciones del selector del formulario: sin bodega en la empresa no se elige", () => {
        expect(opcionesBodegaArgos([ana, { ...ana, idExterno: "78", enEmpresa: false }], 10)).toEqual([
            { valor: "77", etiqueta: "Bodega Norte · Ana Cux", detalle: "ana@argos.test", deshabilitada: false },
            { valor: "78", etiqueta: "Bodega Norte · Ana Cux", detalle: "ana@argos.test · sin bodega en esta empresa", deshabilitada: true },
        ]);
    });

    it("el respaldo a mano solo cuando Argos no puede dar la lista", () => {
        const e = (codigo: string, estado: number) => new ErrorApi({ estado, codigo, titulo: "x" });
        expect(sinListaDeArgos(e("conector_apagado", 503))).toBe(true);
        expect(sinListaDeArgos(e("argos_no_disponible", 502))).toBe(true);
        expect(sinListaDeArgos(e("argos_sin_inventario_tecnico", 422))).toBe(true);
        expect(sinListaDeArgos(e("sin_conexion", 0))).toBe(true);
        expect(sinListaDeArgos(e("prohibido", 403))).toBe(false);
        expect(sinListaDeArgos(e("validacion", 422))).toBe(false);
    });
});
