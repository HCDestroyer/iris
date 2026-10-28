import { describe, expect, it } from "vitest";
import {
    entradaCrearTicket,
    esquemaAsignar,
    esquemaCerrar,
    esquemaCrearTicket,
    esquemaEscalar,
    esquemaFusionar,
    esquemaPonerEnEspera,
    esquemaReanudar,
    esquemaResolver,
    esquemaResponder,
    normalizarNumeroTicket,
} from "./esquemas";

const errores = (r: { success: boolean; error?: { issues: { path: PropertyKey[]; message: string }[] } }) =>
    r.success ? [] : (r.error?.issues.map((i) => `${i.path.join(".")}: ${i.message}`) ?? []);

describe("esquemas de acciones (espejo de la API)", () => {
    it("responder exige texto (1–20 000) y recorta espacios", () => {
        expect(errores(esquemaResponder.safeParse({ cuerpo: "   " }))).toEqual(["cuerpo: Escribe la respuesta"]);
        expect(esquemaResponder.parse({ cuerpo: "  hola  " })).toEqual({ cuerpo: "hola" });
        expect(esquemaResponder.safeParse({ cuerpo: "x".repeat(20001) }).success).toBe(false);
    });

    it("motivos de 3 a 500 caracteres", () => {
        expect(errores(esquemaPonerEnEspera.safeParse({ motivo: "ok" }))).toEqual(["motivo: El motivo necesita al menos 3 caracteres"]);
        expect(esquemaPonerEnEspera.safeParse({ motivo: "x".repeat(501) }).success).toBe(false);
        expect(esquemaResolver.safeParse({ solucion: "Se cambió la ONT" }).success).toBe(true);
    });

    it("reanudar: motivo opcional (vacío = ausente)", () => {
        expect(esquemaReanudar.parse({ motivo: "  " })).toEqual({ motivo: undefined });
    });

    it("asignar: vacío = sin asignar; si no, id positivo", () => {
        expect(esquemaAsignar.parse({ usuarioId: "" })).toEqual({ usuarioId: undefined });
        expect(esquemaAsignar.parse({ usuarioId: "7" })).toEqual({ usuarioId: 7 });
    });

    it("escalar: la prioridad nueva debe ser mayor que la actual", () => {
        const e = esquemaEscalar("alta");
        expect(e.parse({ motivo: "cliente VIP", prioridad: "" })).toEqual({ motivo: "cliente VIP", prioridad: undefined });
        expect(e.parse({ motivo: "cliente VIP", prioridad: "critica" })).toEqual({ motivo: "cliente VIP", prioridad: "critica" });
        expect(errores(e.safeParse({ motivo: "cliente VIP", prioridad: "media" }))).toEqual(["prioridad: Elige una prioridad mayor que la actual"]);
    });

    it("cerrar: motivo obligatorio solo si no está resuelto", () => {
        expect(esquemaCerrar(true).parse({ motivo: "" })).toEqual({ motivo: undefined });
        expect(esquemaCerrar(false).safeParse({ motivo: "" }).success).toBe(false);
    });

    it("fusionar: número legible o id", () => {
        expect(esquemaFusionar.safeParse({ destino: "TK-000123", motivo: "" }).success).toBe(true);
        expect(esquemaFusionar.safeParse({ destino: "123", motivo: "" }).success).toBe(true);
        expect(esquemaFusionar.safeParse({ destino: "ticket", motivo: "" }).success).toBe(false);
        expect(normalizarNumeroTicket("tk123")).toEqual({ numero: "TK-000123", digitos: 123 });
    });
});

describe("alta rápida", () => {
    const vacio = { asunto: "", categoriaId: "", prioridad: "media" as const, descripcion: "", contacto: { nombre: "", telefono: "", correo: "" } };

    it("asunto de 3 a 200 y correo válido", () => {
        expect(errores(esquemaCrearTicket.safeParse(vacio))).toEqual(["asunto: Escribe el asunto"]);
        expect(errores(esquemaCrearTicket.safeParse({ ...vacio, asunto: "ab" }))).toEqual(["asunto: El asunto necesita al menos 3 caracteres"]);
        expect(errores(esquemaCrearTicket.safeParse({ ...vacio, asunto: "Sin servicio", contacto: { ...vacio.contacto, correo: "x" } }))).toEqual([
            "contacto.correo: Escribe un correo válido, como nombre@empresa.com",
        ]);
    });

    it("el cuerpo no lleva claves vacías", () => {
        const datos = esquemaCrearTicket.parse({ ...vacio, asunto: " Sin servicio ", categoriaId: "2" });
        expect(entradaCrearTicket(datos)).toEqual({ asunto: "Sin servicio", prioridad: "media", categoriaId: 2 });
        const conContacto = esquemaCrearTicket.parse({ ...vacio, asunto: "Sin servicio", contacto: { nombre: "Ana", telefono: "", correo: "ana@x.gt" } });
        expect(entradaCrearTicket(conContacto)).toEqual({ asunto: "Sin servicio", prioridad: "media", contacto: { nombre: "Ana", correo: "ana@x.gt" } });
    });
});
