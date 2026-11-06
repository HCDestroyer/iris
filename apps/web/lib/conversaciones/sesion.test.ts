import { describe, expect, it } from "vitest";
import { crearClienteIris } from "../api/cliente";
import { guardarBorrador, leerBorrador, limpiarBorradores } from "./borradores";
import { SesionCambiada, verificarSesion } from "./sesion";

function almacenMemoria() {
    const datos = new Map<string, string>();
    return {
        getItem: (k: string) => datos.get(k) ?? null,
        setItem: (k: string, v: string) => void datos.set(k, v),
        removeItem: (k: string) => void datos.delete(k),
        get length() {
            return datos.size;
        },
        key: (i: number) => [...datos.keys()][i] ?? null,
        datos,
    };
}

function clienteConYo(usuarioId: number, empresaId: number | undefined, estado = 200) {
    return crearClienteIris({
        baseUrl: "http://panel.test",
        leerCookies: () => "",
        leerEmpresa: () => undefined,
        alProblemaEmpresa: () => undefined,
        fetch: async () =>
            estado === 200
                ? Response.json({
                      usuario: { id: usuarioId, nombre: "X", correo: "x@iris.test", tipo: "persona", activo: true, mfaActivo: false, roles: [], version: 1, creadoEn: "", actualizadoEn: "" },
                      permisos: [],
                      modoEmpresa: "con_empresa",
                      ...(empresaId === undefined ? {} : { empresaActiva: { id: empresaId, nombre: "E" } }),
                  })
                : Response.json({ type: "urn:iris:problema:no_autenticado", title: "Sin sesión", status: estado }, { status: estado, headers: { "Content-Type": "application/problem+json" } }),
    });
}

describe("verificación de sesión de la bandeja", () => {
    it("pasa si el navegador sigue con el mismo usuario y empresa", async () => {
        await expect(verificarSesion({ usuarioId: 7, empresaId: 1 }, clienteConYo(7, 1))).resolves.toBeUndefined();
    });

    it("otro usuario u otra empresa: SesionCambiada", async () => {
        await expect(verificarSesion({ usuarioId: 7, empresaId: 1 }, clienteConYo(9, 1))).rejects.toBeInstanceOf(SesionCambiada);
        await expect(verificarSesion({ usuarioId: 7, empresaId: 1 }, clienteConYo(7, 2))).rejects.toMatchObject({ motivo: "otra_empresa" });
    });

    it("sin sesión: el error de la API (nada se envía)", async () => {
        await expect(verificarSesion({ usuarioId: 7, empresaId: 1 }, clienteConYo(7, 1, 401))).rejects.toMatchObject({ estado: 401 });
    });
});

describe("borradores del compositor", () => {
    it("son de un usuario y empresa: otra cuenta no los ve, y se borran al salir", () => {
        const a = almacenMemoria();
        const andrea = { usuarioId: 7, empresaId: 1 };
        const otro = { usuarioId: 9, empresaId: 1 };
        guardarBorrador(andrea, 41, "responder", "Hola Carlos", a);
        expect(leerBorrador(andrea, 41, "responder", a)).toBe("Hola Carlos");
        expect(leerBorrador(otro, 41, "responder", a)).toBe("");
        guardarBorrador(otro, 41, "nota", "nota", a);
        limpiarBorradores(otro, a);
        expect(leerBorrador(andrea, 41, "responder", a)).toBe("");
        expect(leerBorrador(otro, 41, "nota", a)).toBe("nota");
        limpiarBorradores(undefined, a);
        expect(a.datos.size).toBe(0);
    });

    it("un texto vacío quita el borrador", () => {
        const a = almacenMemoria();
        const yo = { usuarioId: 7, empresaId: undefined };
        guardarBorrador(yo, 1, "responder", "x", a);
        guardarBorrador(yo, 1, "responder", "  ", a);
        expect(a.datos.size).toBe(0);
    });
});
