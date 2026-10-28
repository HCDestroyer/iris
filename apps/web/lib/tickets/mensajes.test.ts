import type { Schema } from "@iris/contratos";
import { describe, expect, it } from "vitest";
import { correoRetenido } from "./mensajes";

const m = (parcial: Partial<Schema<"Mensaje">>): Schema<"Mensaje"> => ({
    id: 1,
    ticketId: 1,
    publico: false,
    autorTipo: "sistema",
    autorId: null,
    autorNombre: "Juan <juan@correo.test>",
    canal: "correo",
    cuerpo: "",
    adjuntos: null,
    creadoEn: "2026-10-28T15:00:00Z",
    ...parcial,
});

describe("correos guardados como nota interna", () => {
    it("reconoce el remitente sin verificar y el que no es el contacto", () => {
        expect(
            correoRetenido(m({ cuerpo: "Correo de juan@correo.test no verificado (el servidor de correo no autenticó el remitente): se guarda como nota interna…" }))?.motivo,
        ).toBe("remitente_no_verificado");
        expect(correoRetenido(m({ cuerpo: "Correo de otro@correo.test no es el contacto del ticket: se guarda como nota interna…" }))).toMatchObject({
            motivo: "remitente_no_es_contacto",
            texto: "Correo de otro remitente",
        });
    });

    it("no marca mensajes públicos, notas de personas ni otras notas del sistema", () => {
        expect(correoRetenido(m({ publico: true, autorTipo: "cliente", cuerpo: "Correo de x" }))).toBeNull();
        expect(correoRetenido(m({ autorTipo: "usuario", canal: "panel", cuerpo: "Correo de x no verificado (" }))).toBeNull();
        expect(correoRetenido(m({ cuerpo: "(El correo es demasiado grande…)" }))).toBeNull();
    });
});
