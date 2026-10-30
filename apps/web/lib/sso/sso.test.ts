import { crearCliente } from "@iris/contratos";
import { describe, expect, it } from "vitest";
import { activoQueReemplaza, enlaceValido, textoMotivoOrigen, type Conector } from "../integraciones/presentacion";
import { descartarVinculacion, destinoSeguro, mensajeSSO } from "./sso";

const O = "http://iris.test";

describe("destino del autologin", () => {
    it("acepta solo rutas internas de ticket, orden o prospecto", () => {
        expect(destinoSeguro("/tickets/12", O)).toBe("/tickets/12");
        expect(destinoSeguro("/ordenes/3", O)).toBe("/ordenes/3");
        expect(destinoSeguro("/comercial/40", O)).toBe("/comercial/40");
    });

    it("cualquier otra cosa va al inicio (nunca a una URL externa)", () => {
        for (const ruta of ["https://malo.test/tickets/1", "//malo.test/tickets/1", "/\\malo.test", "/usuarios/1", "/tickets/1?x=https://malo", "/tickets/abc", "/entrar", "", null, undefined]) {
            expect(destinoSeguro(ruta, O)).toBe("/");
        }
    });
});

describe("mensajes del canje", () => {
    it("cada error con su texto", () => {
        expect(mensajeSSO(401, "ticket_invalido").titulo).toMatch(/venció o ya se usó/);
        expect(mensajeSSO(409, "conector_apagado").titulo).toMatch(/no está conectado/);
        expect(mensajeSSO(403, "sso_requiere_mfa").titulo).toBe("Tu cuenta necesita doble factor para entrar desde Argos");
        expect(mensajeSSO(403, "prohibido").titulo).toMatch(/no puede entrar/);
        expect(mensajeSSO(429, "demasiados_intentos").titulo).toMatch(/bloqueada/);
        expect(mensajeSSO(429, "demasiados_canjes").titulo).toMatch(/Demasiados/);
        expect(mensajeSSO(500, "error_interno").titulo).toMatch(/No se pudo entrar/);
    });
});

describe("conectores", () => {
    const c = (id: number, sistema: Conector["sistema"], estado: Conector["estado"]) => ({ id, sistema, estado }) as Conector;

    it("activar avisa del conector activo del mismo sistema que reemplaza", () => {
        const todos = [c(1, "argosisp", "activo"), c(2, "argosisp", "apagado"), c(3, "horus", "apagado")];
        expect(activoQueReemplaza(todos[1] as Conector, todos)?.id).toBe(1);
        expect(activoQueReemplaza(todos[2] as Conector, todos)).toBeUndefined();
    });

    it("el enlace lleva el código en el fragmento; el motivo de un origen no permitido tiene texto", () => {
        expect(enlaceValido("https://argos.example/conectar#abc")).toBe(true);
        expect(enlaceValido("https://argos.example/conectar")).toBe(false);
        expect(enlaceValido("javascript:alert(1)#x")).toBe(false);
        expect(textoMotivoOrigen("origen_no_permitido")).toMatch(/no está en la lista/);
        expect(textoMotivoOrigen("origenes_no_configurados")).toMatch(/IRIS_ARGOS_ORIGENES/);
    });
});

describe("«No fui yo»", () => {
    it("descartar: POST sin cuerpo, con cookies y sin cabeceras del panel; si falla no bloquea la salida", async () => {
        let pedido: Request | undefined;
        const ok = crearCliente({
            baseUrl: "http://iris.test",
            credentials: "include",
            fetch: async (entrada) => {
                pedido = entrada as Request;
                return new Response(null, { status: 204 });
            },
        });
        expect(await descartarVinculacion(ok)).toBe(true);
        expect(pedido?.url).toBe("http://iris.test/api/v1/sso/descartar");
        expect(pedido?.method).toBe("POST");
        expect(pedido?.credentials).toBe("include");
        expect(pedido?.headers.has("X-CSRF-Token")).toBe(false);
        expect(pedido?.headers.has("X-Iris-Empresa")).toBe(false);
        expect(await pedido?.text()).toBe("");
        const caida = crearCliente({
            baseUrl: "http://iris.test",
            fetch: async () => {
                throw new TypeError("sin red");
            },
        });
        expect(await descartarVinculacion(caida)).toBe(false);
    });
});
