import { describe, expect, it } from "vitest";
import { esquemaConexionMeta, esquemaRevocar } from "./esquemas";
import { diasSinTelefono, errorQr, etiquetaTipoConexion, iniciales, plazoRestante, prestamoConversa, presentarConexionMeta, presentarSincronizacion, sePuedePedir, SERVICIO_CENTRAL, textoDesconexion, textoMotivoQr } from "./presentacion";
import { opcionesLogin } from "./sdk";
import type { Sincronizacion } from "./tipos";

const AHORA = Date.parse("2026-11-08T16:00:00Z");

describe("conexión con Meta", () => {
    it("servicio central: Iris Cloud en cloud.iris.kns.gt, todavía no disponible", () => {
        expect(SERVICIO_CENTRAL).toEqual({ nombre: "Iris Cloud", url: "https://cloud.iris.kns.gt" });
        expect(presentarConexionMeta({ estado: "sin_servicio_central", modo: "compartida", comprobaciones: [] })).toMatchObject({ titulo: "Servicio central no disponible todavía", tono: "warning" });
        expect(textoMotivoQr("servicio_central_no_disponible")).toContain("Servicio central no disponible todavía");
        expect(textoMotivoQr("sin_configurar")).toBe("Falta configurar la conexión con Meta.");
    });

    it("lista: verde solo si todo está ok; si falta confirmar Tech Provider, advertencia", () => {
        expect(presentarConexionMeta({ estado: "lista", modo: "propia", comprobaciones: [{ clave: "app", estado: "ok", detalle: "" }] }).tono).toBe("success");
        expect(presentarConexionMeta({ estado: "lista", modo: "propia", comprobaciones: [{ clave: "tech_provider", estado: "sin_verificar", detalle: "" }] })).toMatchObject({ titulo: "Lista, falta confirmar la aprobación de Meta", tono: "warning" });
        expect(presentarConexionMeta({ estado: "error", modo: "propia", comprobaciones: [] }).tono).toBe("error");
    });

    it("FB.login con config_id, code y los extras del registro", () => {
        expect(opcionesLogin("889900112233", "whatsapp_business_app_onboarding", "3")).toEqual({
            config_id: "889900112233",
            response_type: "code",
            override_default_response_type: true,
            extras: { setup: {}, featureType: "whatsapp_business_app_onboarding", sessionInfoVersion: "3" },
        });
    });

    it("esquema espejo del servidor", () => {
        const base = { appId: "1298475561023344", configId: "874120336615092", graphVersion: "", appSecret: "", servicioUrl: "", claveEnlace: "", requiereSecreto: false, requiereClave: true };
        expect(esquemaConexionMeta.safeParse({ ...base, modo: "propia" }).success).toBe(true);
        expect(esquemaConexionMeta.safeParse({ ...base, modo: "propia", appId: "12a" }).success).toBe(false);
        expect(esquemaConexionMeta.safeParse({ ...base, modo: "propia", graphVersion: "21" }).success).toBe(false);
        expect(esquemaConexionMeta.safeParse({ ...base, modo: "propia", graphVersion: "v21.0" }).success).toBe(true);
        expect(esquemaConexionMeta.safeParse({ ...base, modo: "propia", requiereSecreto: true }).success).toBe(false);
        expect(esquemaConexionMeta.safeParse({ ...base, modo: "compartida" }).success).toBe(false);
        expect(esquemaConexionMeta.safeParse({ ...base, modo: "compartida", claveEnlace: "clave" }).success).toBe(true);
        expect(esquemaConexionMeta.safeParse({ ...base, modo: "compartida", claveEnlace: "clave", servicioUrl: "http://cloud.iris.kns.gt" }).success).toBe(false);
        expect(esquemaConexionMeta.safeParse({ ...base, modo: "compartida", claveEnlace: "x".repeat(513) }).success).toBe(false);
    });
});

describe("QR", () => {
    it("cada error tiene título y texto; lo desconocido es un error de Meta", () => {
        for (const c of ["cancelado", "codigo_vencido", "app_no_aprobada", "numero_no_elegible", "error_meta", "meta", "sin_datos", "canal_existente", "sdk", "servicio_central_no_disponible", "conexion_meta_sin_configurar"]) {
            expect(errorQr(c).titulo.length).toBeGreaterThan(0);
        }
        expect(errorQr("numero_no_elegible").revisa).toHaveLength(3);
        expect(errorQr("app_no_aprobada")).toMatchObject({ ofrecerApi: true, ofrecerConexion: true });
        expect(errorQr("otro_codigo")).toEqual(errorQr("error_meta"));
    });

    it("tipo de conexión, teléfono sin actividad y desconexión", () => {
        expect(etiquetaTipoConexion("qr")).toBe("QR · WhatsApp Business");
        expect(etiquetaTipoConexion("api")).toBe("API");
        expect(diasSinTelefono("2026-11-02T16:00:00Z", null, AHORA)).toBeNull();
        expect(diasSinTelefono("2026-10-28T15:00:00Z", null, AHORA)).toBe(11);
        expect(diasSinTelefono(null, "2026-10-20T16:00:00Z", AHORA)).toBe(19);
        expect(diasSinTelefono(null, null, AHORA)).toBeNull();
        expect(textoDesconexion("primary_inactivity")).toBe("El teléfono pasó 14 días sin abrir la app.");
    });

    it("plazo de 24 h para el historial y qué se puede pedir", () => {
        expect(plazoRestante("2026-11-09T15:40:00Z", AHORA)).toEqual({ vencido: false, texto: "23 h 40 min" });
        expect(plazoRestante("2026-11-08T16:25:00Z", AHORA)).toEqual({ vencido: false, texto: "25 min" });
        expect(plazoRestante("2026-11-08T15:00:00Z", AHORA).vencido).toBe(true);
        const parte = { pedidoEn: null, total: 0, asociados: 0, progreso: 0, fase: 0, mensajes: 0, detalle: null };
        const s: Sincronizacion = { plazo: "2026-11-09T15:40:00Z", contactos: { ...parte, estado: "listo" }, mensajes: { ...parte, estado: "fallido" } };
        expect(sePuedePedir(s, "contactos", AHORA)).toBe(false);
        expect(sePuedePedir(s, "mensajes", AHORA)).toBe(true);
        expect(sePuedePedir({ ...s, plazo: "2026-11-08T15:00:00Z" }, "mensajes", AHORA)).toBe(false);
        expect(presentarSincronizacion("rechazado")).toEqual({ texto: "No aceptado en el teléfono", tono: "warning" });
    });
});

describe("préstamos", () => {
    it("iniciales, usos de conversación y motivo para dejar de compartir", () => {
        expect(iniciales("Cable Norte")).toBe("CN");
        expect(iniciales("Fibra Occidente S.A.")).toBe("FO");
        expect(prestamoConversa(["avisos"])).toBe(false);
        expect(prestamoConversa(["avisos", "soporte"])).toBe(true);
        expect(esquemaRevocar.safeParse({ motivo: "ya no" }).success).toBe(true);
        expect(esquemaRevocar.safeParse({ motivo: " no " }).success).toBe(false);
        expect(esquemaRevocar.safeParse({ motivo: "x".repeat(301) }).success).toBe(false);
    });
});
