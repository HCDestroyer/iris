import { describe, expect, it } from "vitest";
import { cuando, presentarConexion, presentarPrueba, textoUltimoMensaje, webhookUrlDe } from "./conexion";
import type { Conexion, PruebaCanal } from "./tipos";

const AHORA = Date.parse("2026-11-08T16:50:00Z"); // 10:50 en Guatemala

function prueba(datos: Partial<PruebaCanal>): PruebaCanal {
    return {
        canalId: 7,
        estado: "conectado",
        detalle: null,
        probadoEn: "2026-11-08T16:45:00Z",
        numeroMeta: "+50222000040",
        nombreVerificado: "Red Sur Soporte",
        calidad: "alta",
        webhookVerificado: true,
        webhookVerificadoEn: "2026-11-08T16:42:00Z",
        ultimoMensajeEn: null,
        conexion: "conectado",
        ...datos,
    };
}

describe("columna «Conexión»", () => {
    it("cada estado del servidor tiene texto, tono e icono (sin recalcular)", () => {
        const todos: Conexion[] = ["desactivado", "sin_probar", "conectado", "webhook_sin_verificar", "token_invalido", "numero_no_coincide", "sin_permiso", "error_meta"];
        for (const c of todos) {
            const p = presentarConexion(c);
            expect(p.texto.length).toBeGreaterThan(0);
        }
        expect(presentarConexion("conectado")).toMatchObject({ texto: "Conectado", tono: "success", icono: "punto" });
        expect(presentarConexion("token_invalido")).toMatchObject({ texto: "Token vencido", tono: "error", credenciales: true });
        expect(presentarConexion("webhook_sin_verificar")).toMatchObject({ texto: "Webhook sin verificar", tono: "warning", credenciales: false });
        expect(presentarConexion("desactivado")).toMatchObject({ texto: "Desactivado", tono: "neutral", icono: "apagado" });
    });
});

describe("URL del webhook", () => {
    it("usa la URL completa del servidor; sin IRIS_BASE_URL antepone el origen del panel", () => {
        expect(webhookUrlDe({ webhookUrl: "https://iris.redsur.com.gt/api/v1/mensajeria/canales/7/webhook", webhookUrlCompleta: true, webhookRuta: "/api/v1/mensajeria/canales/7/webhook" }, "https://panel.local")).toBe(
            "https://iris.redsur.com.gt/api/v1/mensajeria/canales/7/webhook",
        );
        expect(webhookUrlDe({ webhookUrl: "/api/v1/mensajeria/canales/7/webhook", webhookUrlCompleta: false, webhookRuta: "/api/v1/mensajeria/canales/7/webhook" }, "https://iris.ejemplo/")).toBe(
            "https://iris.ejemplo/api/v1/mensajeria/canales/7/webhook",
        );
    });
});

describe("resultado de «Probar conexión»", () => {
    it("conectado: credenciales con nombre verificado y calidad, webhook con la hora de Guatemala", () => {
        const p = presentarPrueba(prueba({}), "+50222000040", AHORA);
        expect(p).toMatchObject({ titulo: "Conectado", tono: "success", accion: "lista" });
        expect(p.credenciales.texto).toBe("Número +502 2200 0040 «Red Sur Soporte», calidad alta.");
        expect(p.webhook.texto).toBe("Meta lo verificó hoy a las 10:42.");
    });

    it("credenciales bien pero el webhook sin verificar: volver a comprobar", () => {
        const p = presentarPrueba(prueba({ webhookVerificado: false, webhookVerificadoEn: null, conexion: "webhook_sin_verificar" }), "+50222000040", AHORA);
        expect(p).toMatchObject({ titulo: "Webhook sin verificar todavía", tono: "warning", accion: "volver_a_comprobar" });
        expect(p.webhook.tono).toBe("warning");
    });

    it("token inválido o sin permiso: cambiar token; el webhook queda sin probar", () => {
        const t = presentarPrueba(prueba({ estado: "token_invalido", conexion: "token_invalido", detalle: "Meta respondió 401." }), "+50222000040");
        expect(t).toMatchObject({ titulo: "Token inválido o vencido", tono: "error", accion: "cambiar_token" });
        expect(t.texto).toBe("Meta respondió 401. Los mensajes no van a salir por este número.");
        expect(presentarPrueba(prueba({ estado: "token_invalido", conexion: "token_invalido", detalle: null }), "+50222000040").texto).toBe("Meta rechazó el token. Los mensajes no van a salir por este número.");
        expect(t.webhook.texto).toBe("Sin probar: primero las credenciales.");
        expect(presentarPrueba(prueba({ estado: "sin_permiso", conexion: "sin_permiso" }), "+50222000040").accion).toBe("cambiar_token");
    });

    it("número que no coincide: corregir datos y lo que dice Meta", () => {
        const p = presentarPrueba(prueba({ estado: "numero_no_coincide", conexion: "numero_no_coincide", numeroMeta: "+50222009999" }), "+50222000040");
        expect(p).toMatchObject({ titulo: "El número no coincide", accion: "corregir_datos" });
        expect(p.credenciales.texto).toContain("Meta dice: +50222009999.");
    });

    it("error de Meta: aviso (no error) y volver a comprobar", () => {
        expect(presentarPrueba(prueba({ estado: "error_meta", conexion: "error_meta" }), "+50222000040")).toMatchObject({ tono: "warning", accion: "volver_a_comprobar" });
    });
});

describe("fechas", () => {
    it("hoy o con fecha, en hora de Guatemala", () => {
        expect(cuando("2026-11-08T16:42:00Z", AHORA)).toBe("hoy a las 10:42");
        expect(cuando("2026-11-03T20:20:00Z", AHORA)).toMatch(/^el 3 nov\.? 2026, 14:20$/);
    });

    it("último mensaje", () => {
        expect(textoUltimoMensaje(null, AHORA)).toBe("Todavía no llega ningún mensaje de Meta a este número.");
        expect(textoUltimoMensaje("2026-11-08T16:45:00Z", AHORA)).toBe("Último mensaje recibido hace 5 minutos.");
        expect(textoUltimoMensaje("2026-11-08T16:49:00Z", AHORA)).toBe("Último mensaje recibido hace 1 minuto.");
    });
});
