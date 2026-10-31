import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { entradaActiva, GRUPOS, MENU, menuAgrupado, menuVisible, repartoTelefono } from "./menu";

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
        for (const p of ["buzones.ver", "integraciones.ver", "tokens.ver", "roles.ver", "permisos.ver", "accesos.ver", "campo.ver_actividad"] as const) {
            expect(menuVisible([p]).map((e) => e.href)).toContain("/administracion");
        }
    });

    it("reportes.ver muestra Reportes, con pantalla; sin él no aparece", () => {
        const reportes = menuVisible(["reportes.ver"]).find((e) => e.href === "/reportes");
        expect(reportes?.etiqueta).toBe("Reportes");
        expect(reportes?.proximamente).toBeUndefined();
        expect(menuVisible(["tickets.ver", "agenda.ver", "ordenes.ver"]).map((e) => e.href)).not.toContain("/reportes");
    });

    it("encuestas.ver muestra Encuestas; reportes.ver solo, no", () => {
        expect(menuVisible(["encuestas.ver"]).map((e) => e.href)).toContain("/encuestas");
        expect(menuVisible(["reportes.ver", "tickets.ver"]).map((e) => e.href)).not.toContain("/encuestas");
    });

    it("marca la entrada activa por prefijo", () => {
        expect(entradaActiva("/")?.href).toBe("/");
        expect(entradaActiva("/usuarios/7")?.href).toBe("/usuarios");
        expect(entradaActiva("/usuariosx")).toBeUndefined();
    });

    it("agrupa como la propuesta: Operación, Equipo, Ventas, Análisis y Administración", () => {
        expect(GRUPOS.map((g) => g.etiqueta)).toEqual(["Operación", "Equipo", "Ventas", "Análisis", "Administración"]);
        const todos = [...new Set(MENU.flatMap((e) => e.permisos ?? []))];
        const grupos = menuAgrupado(todos).map((g) => [g.etiqueta, g.entradas.map((e) => e.etiqueta)]);
        expect(grupos).toEqual([
            ["Operación", ["Inicio", "Tickets", "Órdenes", "Agenda", "Despacho"]],
            ["Equipo", ["Técnicos", "Usuarios"]],
            ["Ventas", ["Comercial", "Encuestas"]],
            ["Análisis", ["Reportes"]],
            ["Administración", ["Administración"]],
        ]);
        // Sin permisos de un grupo, el grupo no aparece.
        expect(menuAgrupado(["tickets.ver"]).map((g) => g.etiqueta)).toEqual(["Operación"]);
    });

    it("teléfono: barra inferior con lo principal que se puede ver; el resto va en «Más»", () => {
        const todos = [...new Set(MENU.flatMap((e) => e.permisos ?? []))];
        const { barra, resto } = repartoTelefono(todos);
        expect(barra.map((e) => e.etiqueta)).toEqual(["Inicio", "Tickets", "Órdenes", "Agenda"]);
        expect(resto.flatMap((g) => g.entradas.map((e) => e.etiqueta))).toEqual(["Despacho", "Técnicos", "Usuarios", "Comercial", "Encuestas", "Reportes", "Administración"]);
        const soloTickets = repartoTelefono(["tickets.ver"]);
        expect(soloTickets.barra.map((e) => e.etiqueta)).toEqual(["Inicio", "Tickets"]);
        expect(soloTickets.resto).toEqual([]);
    });
});
