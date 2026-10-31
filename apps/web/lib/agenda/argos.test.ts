import type { Schema } from "@iris/contratos";
import { describe, expect, it } from "vitest";
import { ErrorApi } from "@/lib/api/errores";
import { conInventarioPorTecnico, opcionTecnicoArgos, SIN_INVENTARIO_POR_TECNICO, textoErrorVinculo } from "./argos";

const ana: Schema<"TecnicoDeArgos"> = {
    idExterno: "77",
    nombre: "Ana Cux",
    correo: "ana@argos.test",
    enEmpresa: true,
    bodegas: [
        { id: 30, empresaId: 10, activa: true },
        { id: 31, empresaId: 10, activa: false },
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
        expect(opcionTecnicoArgos(ana, 21)).toEqual({ texto: "Ana Cux · ana@argos.test · bodega 30", elegible: true });
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
});
