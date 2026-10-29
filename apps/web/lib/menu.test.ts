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

    it("sin permisos: solo Inicio", () => {
        const visibles = menuVisible([]).map((e) => e.etiqueta);
        expect(visibles).toEqual(["Inicio"]);
        expect(menuVisible(null)).toEqual(menuVisible([]));
    });

    it("prospectos.ver muestra Comercial, con pantalla", () => {
        const comercial = menuVisible(["prospectos.ver"]).find((e) => e.href === "/comercial");
        expect(comercial).toBeDefined();
        expect(comercial?.proximamente).toBeUndefined();
        expect(menuVisible(["tickets.ver"]).map((e) => e.href)).not.toContain("/comercial");
    });

    it("ordenes.ver u ordenes.ver_propias muestran Órdenes (con tablero)", () => {
        for (const p of ["ordenes.ver", "ordenes.ver_propias"] as const) {
            const ordenes = menuVisible([p]).find((e) => e.href === "/ordenes");
            expect(ordenes).toBeDefined();
            expect(ordenes?.proximamente).toBeUndefined();
        }
        expect(menuVisible(["tickets.ver"]).map((e) => e.href)).not.toContain("/ordenes");
    });

    it("agenda.ver muestra Agenda y Despacho, con pantalla (sin «Próximamente»)", () => {
        for (const href of ["/agenda", "/despacho"]) {
            const entrada = menuVisible(["agenda.ver"]).find((e) => e.href === href);
            expect(entrada).toBeDefined();
            expect(entrada?.proximamente).toBeUndefined();
            expect(menuVisible(["tickets.ver"]).map((e) => e.href)).not.toContain(href);
        }
    });

    it("tickets.ver muestra Tickets", () => {
        expect(menuVisible(["tickets.ver"]).map((e) => e.href)).toContain("/tickets");
    });

    it("usuarios.ver muestra Usuarios; buzones.ver, tokens.ver, roles.ver o permisos.ver, Administración", () => {
        expect(menuVisible(["usuarios.ver"]).map((e) => e.href)).toContain("/usuarios");
        expect(menuVisible(["usuarios.ver"]).map((e) => e.href)).not.toContain("/administracion");
        for (const p of ["buzones.ver", "tokens.ver", "roles.ver", "permisos.ver"] as const) {
            expect(menuVisible([p]).map((e) => e.href)).toContain("/administracion");
        }
    });

    it("marca la entrada activa por prefijo", () => {
        expect(entradaActiva("/")?.href).toBe("/");
        expect(entradaActiva("/usuarios/7")?.href).toBe("/usuarios");
        expect(entradaActiva("/usuariosx")).toBeUndefined();
    });
});
