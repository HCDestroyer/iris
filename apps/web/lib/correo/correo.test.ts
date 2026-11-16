import { describe, expect, it } from "vitest";
import { ambitoDe, atajoDe, ATAJOS, camposDeValores, entradaCorreo, entradaPrueba, esquemaCorreo, esquemaPrueba, textoCausa, textoEspera, textoOrigen, textoResultado, valoresCorreo } from "./correo";
import type { AjusteCorreo } from "./tipos";

const GLOBAL: AjusteCorreo = {
    ambito: "global",
    empresaId: null,
    configurado: true,
    host: "smtp.proveedor.example",
    puerto: 587,
    seguridad: "starttls",
    usuario: "avisos@isp.example",
    tieneContrasena: true,
    remitenteNombre: "Iris",
    remitenteCorreo: "no-responder@isp.example",
    activo: true,
    version: 3,
    actualizadoEn: "2026-11-16T18:00:00Z",
    origen: "global",
    origenDetalle: "",
    entorno: { configurado: true, host: "localhost", remitenteCorreo: "no-responder@iris.example" },
    ultimaPrueba: null,
};
const VACIO: AjusteCorreo = { ...GLOBAL, configurado: false, host: "", usuario: "", tieneContrasena: false, remitenteNombre: "", remitenteCorreo: "", activo: false, version: 0, actualizadoEn: null, origen: "ninguno", entorno: null };

describe("correo del sistema (contratos/correo-sistema.md)", () => {
    it("ámbito: sin límite de empresa elige; los demás, solo su empresa", () => {
        const empresaActiva = { id: 10 };
        expect(ambitoDe({ todasLasEmpresas: true, empresaActiva }, null)).toBe("global");
        expect(ambitoDe({ todasLasEmpresas: true, empresaActiva }, "empresa")).toBe(10);
        // Un valor cualquiera en la URL no cambia de ámbito.
        expect(ambitoDe({ todasLasEmpresas: true, empresaActiva }, "11")).toBe("global");
        expect(ambitoDe({ todasLasEmpresas: true }, "empresa")).toBe("global");
        // Con límite de empresa nunca se llega al general, diga lo que diga la URL.
        expect(ambitoDe({ todasLasEmpresas: false, empresaActiva }, null)).toBe(10);
        expect(ambitoDe({ todasLasEmpresas: false, empresaActiva }, "global")).toBe(10);
        expect(ambitoDe({ todasLasEmpresas: false }, null)).toBeNull();
    });

    it("lo guardado llega al formulario sin contraseña y vuelve igual si no se toca", () => {
        const v = valoresCorreo(GLOBAL);
        expect(v).toEqual({ host: "smtp.proveedor.example", puerto: "587", seguridad: "starttls", usuario: "avisos@isp.example", contrasena: "", quitarContrasena: false, remitenteNombre: "Iris", remitenteCorreo: "no-responder@isp.example" });
        expect(esquemaCorreo.safeParse(v).success).toBe(true);
        const e = entradaCorreo(GLOBAL, v);
        // Sin tocar la contraseña, no viaja: el servidor conserva la guardada.
        expect(e).toEqual({ host: "smtp.proveedor.example", puerto: 587, seguridad: "starttls", usuario: "avisos@isp.example", remitenteNombre: "Iris", remitenteCorreo: "no-responder@isp.example", version: 3, activo: true });
        expect(e).not.toHaveProperty("contrasena");
    });

    it("contraseña: ausente la conserva, un valor la reemplaza y «quitar» manda vacío", () => {
        const v = valoresCorreo(GLOBAL);
        expect(camposDeValores({ ...v, contrasena: " nueva con espacios " }).contrasena).toBe(" nueva con espacios ");
        expect(camposDeValores({ ...v, quitarContrasena: true }).contrasena).toBe("");
        // Escribir una nueva gana a «quitar».
        expect(camposDeValores({ ...v, contrasena: "otra", quitarContrasena: true }).contrasena).toBe("otra");
        // La prueba usa lo mismo, más el destinatario.
        expect(entradaPrueba({ ...v, contrasena: "otra" }, " ana@isp.example ")).toMatchObject({ contrasena: "otra", para: "ana@isp.example", host: "smtp.proveedor.example" });
        expect(entradaPrueba(v, "ana@isp.example")).not.toHaveProperty("contrasena");
        expect(entradaPrueba(v, "ana@isp.example")).not.toHaveProperty("version");
    });

    it("un ajuste nuevo nace encendido; uno apagado se guarda apagado", () => {
        const v = { ...valoresCorreo(VACIO), host: "smtp.gmail.com", remitenteCorreo: "avisos@isp.example" };
        expect(entradaCorreo(VACIO, v)).toMatchObject({ version: 0, activo: true });
        expect(entradaCorreo({ ...GLOBAL, activo: false }, valoresCorreo(GLOBAL))).toMatchObject({ version: 3, activo: false });
    });

    it("espejo del servidor: host sin esquema ni puerto, puertos permitidos, un solo correo y nada de saltos de línea", () => {
        const v = valoresCorreo(GLOBAL);
        const falla = (cambio: Partial<typeof v>) => esquemaCorreo.safeParse({ ...v, ...cambio }).success === false;
        for (const host of ["", "https://smtp.gmail.com", "smtp.gmail.com:587", "smtp.gmail.com/ruta", "smtp gmail", "a".repeat(254)]) expect(falla({ host })).toBe(true);
        for (const host of ["smtp.gmail.com", "10.0.0.5", "relay-interno", " smtp.office365.com "]) expect(falla({ host })).toBe(false);
        for (const puerto of ["22", "80", "0", "", "587.5"]) expect(falla({ puerto })).toBe(true);
        for (const puerto of ["25", "465", "587", "2525"]) expect(falla({ puerto })).toBe(false);
        expect(falla({ seguridad: "ssl" as never })).toBe(true);
        for (const remitenteCorreo of ["", "sin-arroba", "a@b", "a@b.com, c@d.com", "Ana <a@b.com>", "a@b.com\nBcc: x@y.com"]) expect(falla({ remitenteCorreo })).toBe(true);
        expect(falla({ remitenteNombre: "Iris\r\nBcc: x@y.com" })).toBe(true);
        expect(falla({ remitenteNombre: "x".repeat(81) })).toBe(true);
        expect(falla({ usuario: "ana\u0000" })).toBe(true);
        // Sin usuario = sin autenticación: vale.
        expect(falla({ usuario: "" })).toBe(false);
        expect(falla({ remitenteNombre: "" })).toBe(false);
        expect(esquemaPrueba.safeParse({ para: "ana@isp.example" }).success).toBe(true);
        for (const para of ["", "ana", "ana@isp.example; otro@isp.example", "ana@isp.example\r\nCc: x@y.com"]) expect(esquemaPrueba.safeParse({ para }).success).toBe(false);
    });

    it("atajos: solo rellenan servidor, puerto y seguridad, y se marca el que coincide", () => {
        expect(ATAJOS.map((a) => a.id)).toEqual(["google", "microsoft", "otro"]);
        expect(ATAJOS[0]?.relleno).toEqual({ host: "smtp.gmail.com", puerto: "587", seguridad: "starttls" });
        expect(ATAJOS[1]?.relleno).toEqual({ host: "smtp.office365.com", puerto: "587", seguridad: "starttls" });
        expect(atajoDe({ host: "SMTP.Gmail.com ", puerto: "587", seguridad: "starttls" })).toBe("google");
        expect(atajoDe({ host: "smtp.office365.com", puerto: "587", seguridad: "starttls" })).toBe("microsoft");
        // Editado después (otro puerto) deja de ser el atajo.
        expect(atajoDe({ host: "smtp.gmail.com", puerto: "465", seguridad: "tls" })).toBe("otro");
        expect(atajoDe({ host: "smtp.proveedor.example", puerto: "587", seguridad: "starttls" })).toBe("otro");
        expect(atajoDe({ host: "", puerto: "587", seguridad: "starttls" })).toBeNull();
    });

    it("la línea de arriba dice en llano por dónde salen hoy los correos", () => {
        expect(textoOrigen({ origen: "empresa", entorno: null }, 10)).toMatchObject({ tono: "ok", titulo: "Hoy los correos salen por el correo de esta empresa" });
        expect(textoOrigen({ origen: "global", entorno: null }, "global")).toMatchObject({ tono: "ok", titulo: "Hoy los correos salen por el correo general" });
        // Desde una empresa sin correo propio encendido: usa el general, y se dice.
        expect(textoOrigen({ origen: "global", entorno: null }, 10).detalle).toContain("no tiene uno propio encendido");
        const entorno = textoOrigen({ origen: "entorno", entorno: { configurado: true, host: "localhost", remitenteCorreo: "no-responder@iris.example" } }, "global");
        expect(entorno).toMatchObject({ tono: "info", titulo: "Hoy los correos salen por el correo del servidor" });
        expect(entorno.detalle).toContain("no-responder@iris.example");
        // Nunca el host ni un usuario del servidor.
        expect(entorno.detalle).not.toContain("localhost");
        expect(textoOrigen({ origen: "ninguno", entorno: null }, "global")).toEqual({ tono: "aviso", titulo: "Iris no envía correos todavía", detalle: "Los avisos llegan por la campana. Configura un correo aquí para que también lleguen al buzón." });
        // Un origen que el panel no conoce no afirma que hay correo.
        expect(textoOrigen({ origen: "otro" as never, entorno: null }, "global").tono).toBe("aviso");
    });

    it("resultado de la prueba: icono, texto y causa en llano; una causa desconocida, el texto genérico", () => {
        expect(textoResultado({ estado: "ok", causa: null, detalle: "El correo de prueba salió. Revisa la bandeja de ana@isp.example." }, "ana@isp.example")).toEqual({ ok: true, titulo: "El correo de prueba salió", detalle: "El correo de prueba salió. Revisa la bandeja de ana@isp.example." });
        expect(textoResultado({ estado: "ok", causa: null, detalle: "" }, "ana@isp.example").detalle).toBe("Revisa la bandeja de ana@isp.example.");
        expect(textoResultado({ estado: "error", causa: "credenciales", detalle: "" }, "x")).toEqual({ ok: false, titulo: "No se pudo enviar", detalle: "El usuario o la contraseña no son correctos." });
        expect(textoResultado({ estado: "error", causa: "certificado", detalle: "El certificado venció." }, "x").detalle).toBe("El certificado venció.");
        for (const c of ["no_conecta", "destino_no_permitido", "certificado", "seguridad", "credenciales", "remitente_rechazado", "destinatario_rechazado", "tiempo_agotado", "otro"]) expect(textoCausa(c)).toMatch(/\.$/);
        for (const c of [null, undefined, "causa_nueva", "toString"]) expect(textoCausa(c)).toBe("El servidor rechazó el envío.");
    });

    it("429: cuánto esperar", () => {
        expect(textoEspera(720)).toBe("Inténtalo en 12 min.");
        expect(textoEspera(40)).toBe("Inténtalo en 40 s.");
        expect(textoEspera(undefined)).toBe("Inténtalo más tarde.");
        expect(textoEspera(0)).toBe("Inténtalo más tarde.");
    });
});
