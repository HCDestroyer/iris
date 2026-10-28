import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { entradaActiva, MENU, menuVisible } from "./menu";

const enumPermiso: string[] = JSON.parse(
    readFileSync(fileURLToPath(new URL("../../../packages/contratos/openapi.json", import.meta.url)), "utf8"),
).components.schemas.Permiso.enum;

describe("menú por permisos", () => {
    it("usa solo nombres del enum Permiso del OpenAPI", () => {
        for (const e of MENU) {
            for (const p of e.permisos ?? []) {
                expect(enumPermiso).toContain(p);
            }
        }
    });

    it("sin permisos: solo Inicio y los módulos «Próximamente» sin permiso", () => {
        const visibles = menuVisible([]).map((e) => e.etiqueta);
        expect(visibles).toEqual(["Inicio", "Tickets", "Órdenes", "Agenda", "Despacho", "Comercial"]);
        expect(menuVisible(null)).toEqual(menuVisible([]));
    });

    it("usuarios.ver muestra Usuarios; tokens.ver, roles.ver o permisos.ver, Administración", () => {
        expect(menuVisible(["usuarios.ver"]).map((e) => e.href)).toContain("/usuarios");
        expect(menuVisible(["usuarios.ver"]).map((e) => e.href)).not.toContain("/administracion");
        for (const p of ["tokens.ver", "roles.ver", "permisos.ver"] as const) {
            expect(menuVisible([p]).map((e) => e.href)).toContain("/administracion");
        }
    });

    it("marca la entrada activa por prefijo", () => {
        expect(entradaActiva("/")?.href).toBe("/");
        expect(entradaActiva("/usuarios/7")?.href).toBe("/usuarios");
        expect(entradaActiva("/usuariosx")).toBeUndefined();
    });
});
