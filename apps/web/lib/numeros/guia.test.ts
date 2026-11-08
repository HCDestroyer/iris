import { describe, expect, it } from "vitest";
import { bloquesMarkdown } from "@/componentes/manuales/markdown";
import { ANCLAS, enlaceGuia, SECCIONES_GUIA } from "./guia";
import { leerRecienCreado, dejarRecienCreado, olvidarRecienCreado, VIDA_RECIEN_CREADO_MS } from "./recien-creado";
import type { Canal } from "./tipos";

const texto = SECCIONES_GUIA.map((s) => `${s.titulo}\n${s.cuerpo}`).join("\n");

describe("guía «Cómo conectar WhatsApp»", () => {
    it("cada anclaje que usan otras pantallas existe y los ids no se repiten", () => {
        const ids = SECCIONES_GUIA.map((s) => s.id);
        expect(new Set(ids).size).toBe(ids.length);
        for (const ancla of Object.values(ANCLAS)) expect(ids).toContain(ancla);
        expect(enlaceGuia(ANCLAS.token)).toBe("/administracion/numeros/guia#token");
    });

    it("cubre los pasos que pidió el PM", () => {
        for (const frase of [
            "portafolio comercial",
            "Verificación del negocio",
            "Crear app",
            "Agregar número de teléfono",
            "Usuarios del sistema",
            "**Administrador**",
            "Asignar activos",
            "**Nunca**",
            "`whatsapp_business_messaging`",
            "`whatsapp_business_management`",
            "Phone number ID",
            "WABA ID",
            "Configuración de la app** → **Básica**",
            "**Verificar y guardar**",
            "suscríbete a `messages`",
            "Métodos de pago",
            "Plantillas de mensajes",
            "override_callback_uri",
            "`POST /{waba_id}/subscribed_apps`",
            "webhook_configuration.override_callback_uri",
            "coexistencia",
            "**Iris Cloud** (todavía no disponible)",
            "**Conectar con QR**",
            "**traer el historial**",
        ]) {
            expect(texto).toContain(frase);
        }
    });

    it("explica cada resultado de la prueba", () => {
        const estados = SECCIONES_GUIA.find((s) => s.id === ANCLAS.estados)?.cuerpo ?? "";
        for (const e of ["**Conectado**", "**Webhook sin verificar**", "**Token inválido o vencido**", "**El número no coincide**", "**Al token le faltan permisos**", "**Meta no respondió**", "**Sin probar**", "**Desactivado**"]) {
            expect(estados).toContain(e);
        }
    });

    it("no promete fechas para el servicio central y avisa que los menús de Meta cambian", () => {
        const qr = SECCIONES_GUIA.find((s) => s.id === ANCLAS.qr)?.cuerpo ?? "";
        expect(qr).toContain("todavía no disponible");
        expect(qr).not.toMatch(/\b20\d\d\b|enero|febrero|marzo|abril|mayo|junio|julio|agosto|septiembre|octubre|noviembre|diciembre|trimestre/i);
        expect(texto).toContain("Las opciones de este asistente cambian seguido");
    });

    it("es Markdown seguro: sin HTML y con enlaces solo a https", () => {
        expect(texto).not.toMatch(/<[a-z!/]/i);
        for (const [, url] of texto.matchAll(/\]\(([^)]+)\)/g)) expect(url).toMatch(/^https:\/\//);
        for (const s of SECCIONES_GUIA) expect(bloquesMarkdown(s.cuerpo).length).toBeGreaterThan(0);
    });
});

describe("verify token recién generado", () => {
    const canal = { id: 7 } as Canal;

    it("se entrega solo al número que se creó y se olvida", () => {
        dejarRecienCreado({ canal, verifyToken: "vt_x", prueba: null, errorPrueba: null });
        expect(leerRecienCreado(8)).toBeNull();
        expect(leerRecienCreado(7)).toEqual({ canal, verifyToken: "vt_x", prueba: null, errorPrueba: null });
        olvidarRecienCreado();
        expect(leerRecienCreado(7)).toBeNull();
    });

    it("vence solo al minuto aunque nadie lo tome", () => {
        dejarRecienCreado({ canal, verifyToken: "vt_x", prueba: null, errorPrueba: null }, 1_000);
        expect(leerRecienCreado(7, 1_000 + VIDA_RECIEN_CREADO_MS)?.verifyToken).toBe("vt_x");
        expect(leerRecienCreado(7, 1_001 + VIDA_RECIEN_CREADO_MS)).toBeNull();
        // Ya vencido, no vuelve.
        expect(leerRecienCreado(7, 1_000)).toBeNull();
    });
});
