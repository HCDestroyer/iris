import type { Schema } from "@iris/contratos";
import { afterEach, describe, expect, it, vi } from "vitest";
import { HORAS_SIN_SENAL, pideCambioContrasena, RUTA_CAMBIAR_CONTRASENA, usableSinSenal, verificarDueno, type SesionCampo } from "./sesion";

const HORA = 60 * 60 * 1000;
const sesion = (verificadoEn: number): SesionCampo => ({
    usuarioId: 51,
    nombre: "Tomás",
    correo: "tomas@iris.test",
    empresas: [],
    modoEmpresa: "sin_conector",
    empresaActiva: null,
    verificadoEn,
});

function yo(id: number, empresa?: number): Schema<"Yo"> {
    return {
        actor: "usuario",
        usuario: { id, nombre: "X", correo: "x@iris.test", tipo: "persona", activo: true, mfaActivo: false, roles: [], empresas: [], version: 1, creadoEn: "", actualizadoEn: "" },
        permisos: [],
        empresas: empresa ? [{ id: empresa, nombre: "E" }] : [],
        modoEmpresa: empresa ? "activa" : "sin_conector",
        todasLasEmpresas: false,
        hayDatosSinEmpresa: false,
        ...(empresa ? { empresaActiva: { id: empresa, nombre: "E" } } : {}),
    };
}

describe("abrir sin señal", () => {
    it(`solo con una verificación en línea de hace menos de ${HORAS_SIN_SENAL} h (configurable) y nunca más de 7 días`, () => {
        const ahora = Date.parse("2026-10-28T15:00:00Z");
        expect(usableSinSenal(sesion(ahora - 2 * HORA), ahora)).toBe(true);
        expect(usableSinSenal(sesion(ahora - 13 * HORA), ahora)).toBe(false);
        expect(usableSinSenal(sesion(ahora - 13 * HORA), ahora, 24)).toBe(true);
        expect(usableSinSenal(sesion(ahora - 8 * 24 * HORA), ahora, 1000)).toBe(false);
        expect(usableSinSenal(sesion(ahora + HORA), ahora)).toBe(false);
    });
});

describe("verificarDueno (antes de cada envío del motor)", () => {
    afterEach(() => vi.unstubAllGlobals());
    const responder = (cuerpo: unknown, status = 200) =>
        vi.stubGlobal("fetch", () => Promise.resolve(new Response(JSON.stringify(cuerpo), { status, headers: { "Content-Type": "application/json" } })));

    it("pasa con el mismo usuario y empresa", async () => {
        responder(yo(51, 10));
        await expect(verificarDueno(51, 10)).resolves.toBeUndefined();
    });
    it("otra persona u otra empresa: sesion_ajena (el motor no envía)", async () => {
        responder(yo(52));
        await expect(verificarDueno(51, null)).rejects.toMatchObject({ codigo: "sesion_ajena", esDeSesion: true });
        responder(yo(51, 20));
        await expect(verificarDueno(51, 10)).rejects.toMatchObject({ codigo: "sesion_ajena" });
    });
    it("sin señal es error de red; 401 es de sesión", async () => {
        vi.stubGlobal("fetch", () => Promise.reject(new TypeError("Failed to fetch")));
        await expect(verificarDueno(51, null)).rejects.toMatchObject({ tipo: "red" });
        responder({ type: "urn:iris:problema:no_autenticado", title: "Sin sesión", status: 401 }, 401);
        await expect(verificarDueno(51, null)).rejects.toMatchObject({ status: 401, esDeSesion: true });
    });
    it("con contraseña temporal: 403 cambio_contrasena_requerido (no sale nada hasta cambiarla)", async () => {
        const temporal = yo(51);
        temporal.usuario.cambioContrasenaRequerido = true;
        responder(temporal);
        const error: unknown = await verificarDueno(51, null).catch((e: unknown) => e);
        expect(error).toMatchObject({ status: 403, esDeSesion: true, codigo: "cambio_contrasena_requerido" });
        expect(pideCambioContrasena((error as { problema: { type: string } }).problema)).toBe(true);
        expect(pideCambioContrasena({ type: "urn:iris:problema:no_autenticado" })).toBe(false);
        expect(pideCambioContrasena(null)).toBe(false);
        expect(RUTA_CAMBIAR_CONTRASENA).toBe("/cambiar-contrasena?siguiente=%2Fcampo");
    });
});
