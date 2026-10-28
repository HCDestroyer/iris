import { describe, expect, it } from "vitest";
import { entradaNuevoTicket, esquemaNuevoTicket, esquemaResponderPortal } from "./esquemas";

const mensajes = (r: { success: boolean; error?: { issues: { path: PropertyKey[]; message: string }[] } }) =>
    Object.fromEntries((r.error?.issues ?? []).map((i) => [i.path.join("."), i.message]));

describe("esquemaNuevoTicket (espejo de EntradaCrearTicketPortal)", () => {
    it("acepta asunto de 3 a 200, descripción y categoría opcional", () => {
        const r = esquemaNuevoTicket.parse({ asunto: "  Sin internet ", categoriaId: "4", descripcion: " Desde ayer " });
        expect(r).toEqual({ asunto: "Sin internet", categoriaId: 4, descripcion: "Desde ayer" });
        expect(esquemaNuevoTicket.parse({ asunto: "abc", categoriaId: "", descripcion: "x" }).categoriaId).toBeUndefined();
    });
    it("rechaza asunto vacío, corto o largo y descripción vacía o larga", () => {
        expect(mensajes(esquemaNuevoTicket.safeParse({ asunto: "", categoriaId: "", descripcion: "" }))).toEqual({
            asunto: "Escribe un asunto",
            descripcion: "Cuéntanos qué pasa",
        });
        expect(mensajes(esquemaNuevoTicket.safeParse({ asunto: "ab", categoriaId: "", descripcion: "x" })).asunto).toBe("El asunto necesita al menos 3 caracteres");
        expect(mensajes(esquemaNuevoTicket.safeParse({ asunto: "a".repeat(201), categoriaId: "", descripcion: "x" })).asunto).toBe("Admite hasta 200 caracteres");
        expect(mensajes(esquemaNuevoTicket.safeParse({ asunto: "abc", categoriaId: "", descripcion: "a".repeat(20001) })).descripcion).toMatch(/^Admite hasta 20.000 caracteres$/);
    });
    it("el cuerpo no lleva campos vacíos (la API rechaza los desconocidos)", () => {
        expect(entradaNuevoTicket({ asunto: "abc", descripcion: "x", categoriaId: undefined })).toEqual({ asunto: "abc", descripcion: "x" });
        expect(entradaNuevoTicket({ asunto: "abc", descripcion: "x", categoriaId: 3 })).toEqual({ asunto: "abc", descripcion: "x", categoriaId: 3 });
    });
});

describe("esquemaResponderPortal", () => {
    it("exige texto y respeta el máximo", () => {
        expect(esquemaResponderPortal.parse({ cuerpo: " gracias " })).toEqual({ cuerpo: "gracias" });
        expect(mensajes(esquemaResponderPortal.safeParse({ cuerpo: "  " })).cuerpo).toBe("Escribe tu mensaje");
        expect(esquemaResponderPortal.safeParse({ cuerpo: "a".repeat(20001) }).success).toBe(false);
    });
});
