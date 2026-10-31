import { describe, expect, it } from "vitest";
import { migasDeRuta } from "./migas";

describe("migas del panel", () => {
    it("una sección es una sola miga (la pantalla actual)", () => {
        expect(migasDeRuta("/")).toEqual([{ etiqueta: "Inicio" }]);
        expect(migasDeRuta("/tickets")).toEqual([{ etiqueta: "Tickets" }]);
    });

    it("las fichas muestran el nombre del registro cuando se conoce", () => {
        expect(migasDeRuta("/ordenes/42")).toEqual([{ etiqueta: "Órdenes", href: "/ordenes" }, { etiqueta: "Orden" }]);
        expect(migasDeRuta("/ordenes/42", "OT-000042")).toEqual([{ etiqueta: "Órdenes", href: "/ordenes" }, { etiqueta: "OT-000042" }]);
        expect(migasDeRuta("/administracion/roles/3", "Supervisor")).toEqual([
            { etiqueta: "Administración", href: "/administracion" },
            { etiqueta: "Roles y permisos", href: "/administracion/roles" },
            { etiqueta: "Supervisor" },
        ]);
    });

    it("las pantallas fijas ganan a las fichas y los tramos sin pantalla se saltan", () => {
        expect(migasDeRuta("/administracion/roles/nuevo", "x")).toEqual([
            { etiqueta: "Administración", href: "/administracion" },
            { etiqueta: "Roles y permisos", href: "/administracion/roles" },
            { etiqueta: "Nuevo rol" },
        ]);
        expect(migasDeRuta("/administracion/integraciones/argos/vuelta").map((m) => m.etiqueta)).toEqual(["Administración", "Integraciones", "Conectar con Argos"]);
        expect(migasDeRuta("/cuenta/mfa/")).toEqual([{ etiqueta: "Mi perfil", href: "/cuenta" }, { etiqueta: "Verificación en dos pasos" }]);
    });
});

describe("migas de los formularios en pantalla propia", () => {
    it("altas: la lista y el formulario", () => {
        expect(migasDeRuta("/tickets/nuevo")).toEqual([{ etiqueta: "Tickets", href: "/tickets" }, { etiqueta: "Nuevo ticket" }]);
        expect(migasDeRuta("/ordenes/nueva")).toEqual([{ etiqueta: "Órdenes", href: "/ordenes" }, { etiqueta: "Nueva orden" }]);
        expect(migasDeRuta("/administracion/buzones/nuevo").at(-1)).toEqual({ etiqueta: "Nuevo buzón" });
    });

    it("editar: la ficha de arriba lleva el nombre del registro", () => {
        expect(migasDeRuta("/usuarios/7/editar", "Ana López")).toEqual([
            { etiqueta: "Usuarios", href: "/usuarios" },
            { etiqueta: "Ana López", href: "/usuarios/7" },
            { etiqueta: "Editar" },
        ]);
        expect(migasDeRuta("/comercial/3/cotizacion")).toEqual([
            { etiqueta: "Comercial", href: "/comercial" },
            { etiqueta: "Prospecto", href: "/comercial/3" },
            { etiqueta: "Cotización" },
        ]);
        // El buzón no tiene ficha: «Buzones de correo › Editar buzón».
        expect(migasDeRuta("/administracion/buzones/4/editar").map((m) => m.etiqueta)).toEqual(["Administración", "Buzones de correo", "Editar buzón"]);
    });
});
