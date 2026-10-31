import { describe, expect, it } from "vitest";
import { filtrarPantallas, fuentesActivas, normalizar, pantallasVisibles, resultadoCliente } from "./global";

describe("buscador global", () => {
    it("pantallas: solo las que el usuario puede abrir, sin importar tildes ni mayúsculas", () => {
        const soloTickets = pantallasVisibles(["tickets.ver"]).map((p) => p.etiqueta);
        expect(soloTickets).toEqual(["Inicio", "Tickets", "Mi perfil"]);
        const admin = pantallasVisibles(["roles.ver", "buzones.ver", "tokens.ver"]);
        expect(admin.map((p) => p.href)).toEqual(expect.arrayContaining(["/administracion", "/administracion/roles", "/administracion/buzones"]));
        // «Tokens de API» aún no tiene pantalla: no se ofrece.
        expect(admin.map((p) => p.etiqueta)).not.toContain("Tokens de API");
        expect(filtrarPantallas(admin, "ROLES").map((p) => p.href)).toEqual(["/administracion/roles"]);
        expect(filtrarPantallas(pantallasVisibles(["ordenes.ver"]), "ordenes").map((p) => p.href)).toEqual(["/ordenes"]);
        expect(normalizar("  Órdenes ")).toBe("ordenes");
    });

    it("cada fuente se consulta con su permiso y desde su mínimo de letras (clientes: 3)", () => {
        const todos = ["tickets.ver", "ordenes.ver_propias", "prospectos.ver", "clientes.ver"] as const;
        expect(fuentesActivas(todos, "a")).toEqual([]);
        expect(fuentesActivas(todos, "ab")).toEqual(["tickets", "ordenes", "prospectos"]);
        expect(fuentesActivas(todos, "abc")).toEqual(["tickets", "ordenes", "prospectos", "clientes"]);
        expect(fuentesActivas(["tickets.ver"], "TK-000101")).toEqual(["tickets"]);
    });

    it("un cliente lleva a la bandeja filtrada por ese cliente", () => {
        const r = resultadoCliente({ id: 7, nombre: "Ferretería El Roble", codigo: "C-2041", direccion: "Zona 7" } as Parameters<typeof resultadoCliente>[0]);
        expect(r.href).toBe("/tickets?clienteId=7&cliente=Ferreter%C3%ADa+El+Roble");
        expect(r.detalle).toBe("C-2041 · Zona 7");
    });
});
