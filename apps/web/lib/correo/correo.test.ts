import { describe, expect, it } from "vitest";
import {
    ambitoDe,
    atajoDe,
    ATAJOS,
    camposDeValores,
    entradaCorreo,
    entradaPrueba,
    esquemaCorreo,
    esquemaPrueba,
    faltaContrasena,
    reglaDeCorreo,
    seBorraLaContrasena,
    seguridadesDe,
    TEXTO_CONTRASENA_OTRA_VEZ,
    textoDemasiados,
    textoCausa,
    textoEspera,
    textoOrigen,
    textoResultado,
    valoresCorreo,
} from "./correo";
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
    enUso: true,
    entorno: { configurado: true, host: "localhost", remitenteCorreo: "no-responder@iris.example" },
};
const SIN_ENTORNO = { configurado: false, host: "", remitenteCorreo: "" };
const VACIO: AjusteCorreo = { ...GLOBAL, configurado: false, host: "", usuario: "", tieneContrasena: false, remitenteNombre: "", remitenteCorreo: "", activo: false, version: 0, actualizadoEn: null, origen: "ninguno", enUso: false, entorno: SIN_ENTORNO };

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
        expect(ambitoDe({ todasLasEmpresas: false, modoEmpresa: "sin_empresa" }, null)).toBeNull();
        expect(ambitoDe({ todasLasEmpresas: false, modoEmpresa: "por_elegir" }, null)).toBeNull();
        // Instalación sin empresas (sin conector): solo existe el general y lo ve quien tenga el permiso.
        expect(ambitoDe({ todasLasEmpresas: false, modoEmpresa: "sin_conector" }, null)).toBe("global");
        expect(ambitoDe({ todasLasEmpresas: false, modoEmpresa: "sin_conector" }, "empresa")).toBe("global");
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

    it("la línea de arriba: el título por origen y, de detalle, la frase del servidor tal cual", () => {
        const con = (origen: AjusteCorreo["origen"], origenDetalle = "") => ({ origen, origenDetalle });
        expect(textoOrigen(con("empresa", "Los correos de esta empresa salen por su propio ajuste."), 10)).toEqual({ tono: "ok", titulo: "Hoy los correos salen por el correo de esta empresa", detalle: "Los correos de esta empresa salen por su propio ajuste." });
        expect(textoOrigen(con("global", "Frase del servidor."), "global")).toEqual({ tono: "ok", titulo: "Hoy los correos salen por el correo general", detalle: "Frase del servidor." });
        // Desde una empresa sin correo propio encendido: usa el general (información, no éxito).
        expect(textoOrigen(con("global", "Frase del servidor."), 10).tono).toBe("info");
        expect(textoOrigen(con("entorno", "Frase del servidor."), "global")).toMatchObject({ tono: "info", titulo: "Hoy los correos salen por el correo del servidor" });
        expect(textoOrigen(con("ninguno", "Frase del servidor."), "global")).toMatchObject({ tono: "aviso", titulo: "Iris no envía correos todavía", detalle: "Frase del servidor." });
        // Sin frase del servidor queda la de respaldo.
        expect(textoOrigen(con("ninguno"), "global").detalle).toBe("Los avisos llegan por la campana. Configura un correo aquí para que también lleguen al buzón.");
        expect(textoOrigen(con("global"), 10).detalle).toContain("no tiene uno propio encendido");
        // Un origen que el panel no conoce no afirma que hay correo.
        expect(textoOrigen(con("otro" as never), "global").tono).toBe("aviso");
    });

    it("la contraseña guardada no se manda a otro servidor: si cambia el servidor, el puerto o el usuario hay que escribirla", () => {
        const v = valoresCorreo(GLOBAL);
        expect(faltaContrasena(GLOBAL, v)).toBeNull();
        for (const cambio of [{ host: "smtp.otro.example" }, { puerto: "465" }, { usuario: "otro@isp.example" }]) expect(faltaContrasena(GLOBAL, { ...v, ...cambio })).toBe(TEXTO_CONTRASENA_OTRA_VEZ);
        expect(TEXTO_CONTRASENA_OTRA_VEZ).toBe("Cambiaste el servidor, el puerto o el usuario: escribe la contraseña otra vez.");
        // Mayúsculas o espacios en el servidor no son un cambio; escribirla lo resuelve.
        expect(faltaContrasena(GLOBAL, { ...v, host: " SMTP.proveedor.example " })).toBeNull();
        expect(faltaContrasena(GLOBAL, { ...v, host: "smtp.otro.example", contrasena: "nueva" })).toBeNull();
        // Cambiar el remitente o la seguridad no la pide.
        expect(faltaContrasena(GLOBAL, { ...v, remitenteNombre: "Otro", seguridad: "tls" })).toBeNull();
        // Con usuario y sin ninguna (nueva, o guardada que se quita): hace falta.
        expect(faltaContrasena(VACIO, { ...valoresCorreo(VACIO), usuario: "ana@isp.example" })).toBe("Con usuario hace falta la contraseña: escríbela.");
        expect(faltaContrasena(GLOBAL, { ...v, quitarContrasena: true })).toBe("Con usuario hace falta la contraseña: escríbela.");
        // Sin usuario no hace falta contraseña…
        expect(faltaContrasena(GLOBAL, { ...v, usuario: "", host: "smtp.otro.example" })).toBeNull();
        // …pero la guardada se borra al guardar: se avisa.
        expect(seBorraLaContrasena(GLOBAL, { usuario: "  " })).toBe(true);
        expect(seBorraLaContrasena(GLOBAL, { usuario: "ana@isp.example" })).toBe(false);
        expect(seBorraLaContrasena(VACIO, { usuario: "" })).toBe(false);
    });

    it("reglas del servidor (422) en llano y con su campo; sin cifrado no hay credenciales", () => {
        expect(reglaDeCorreo("contrasena_requerida", GLOBAL)).toEqual({ campo: "contrasena", mensaje: TEXTO_CONTRASENA_OTRA_VEZ });
        expect(reglaDeCorreo("contrasena_requerida", VACIO)).toEqual({ campo: "contrasena", mensaje: "Con usuario hace falta la contraseña: escríbela." });
        expect(reglaDeCorreo("destino_no_permitido", GLOBAL)).toMatchObject({ campo: "host" });
        expect(reglaDeCorreo("sin_cifrado_solo_red_interna", GLOBAL)).toMatchObject({ campo: "seguridad" });
        expect(reglaDeCorreo("sin_cifrado_sin_credenciales", GLOBAL)).toMatchObject({ campo: "usuario" });
        expect(reglaDeCorreo("sin_cifrado_sin_credenciales", GLOBAL, "contrasena")).toMatchObject({ campo: "contrasena" });
        // Falta la clave de cifrado de la instalación: no es de ningún campo.
        expect(reglaDeCorreo("cifrado_no_configurado", GLOBAL)).toMatchObject({ campo: null });
        expect(reglaDeCorreo("cifrado_no_configurado", GLOBAL)?.mensaje).not.toContain("IRIS_");
        for (const c of ["validacion", "regla_nueva", "toString", ""]) expect(reglaDeCorreo(c, GLOBAL)).toBeNull();
        // Espejo en el formulario: con «Ninguna» no se aceptan usuario ni contraseña.
        const v = valoresCorreo(GLOBAL);
        expect(esquemaCorreo.safeParse({ ...v, seguridad: "ninguna" }).success).toBe(false);
        expect(esquemaCorreo.safeParse({ ...v, seguridad: "ninguna", usuario: "", contrasena: "x" }).success).toBe(false);
        expect(esquemaCorreo.safeParse({ ...v, seguridad: "ninguna", usuario: "" }).success).toBe(true);
    });

    it("contrato 1.1.0: «sin cifrado» solo en el general, y los 429 con cuánto esperar", () => {
        expect(seguridadesDe("global", "starttls")).toEqual(["starttls", "tls", "ninguna"]);
        expect(seguridadesDe(10, "starttls")).toEqual(["starttls", "tls"]);
        // Una empresa que ya lo tenía guardado lo sigue viendo, para poder cambiarlo.
        expect(seguridadesDe(10, "ninguna")).toEqual(["starttls", "tls", "ninguna"]);
        expect(reglaDeCorreo("sin_cifrado_solo_global", GLOBAL)).toEqual({ campo: "seguridad", mensaje: "«Sin cifrado» solo se puede usar en el correo general: elige STARTTLS o TLS." });
        expect(textoDemasiados("demasiados_rechazos", 600)).toBe("Demasiados intentos con servidores no permitidos. Inténtalo en 10 min.");
        expect(textoDemasiados("demasiados_rechazos", undefined)).toBe("Demasiados intentos con servidores no permitidos. Inténtalo más tarde.");
        expect(textoDemasiados("demasiadas_pruebas", 720)).toBe("Ya se hicieron muchas pruebas seguidas. Inténtalo en 12 min.");
        // `entorno` sin host ni remitente (ámbito de empresa) no rompe nada.
        expect(textoOrigen({ origen: "entorno", origenDetalle: "" }, 10).titulo).toBe("Hoy los correos salen por el correo del servidor");
    });

    it("un servidor que no manda `usuario` o `ultimaPrueba` no rompe el formulario", () => {
        const { ultimaPrueba: _u, ...sin } = { ...GLOBAL, ultimaPrueba: undefined };
        void _u;
        expect(valoresCorreo(sin).usuario).toBe("avisos@isp.example");
        expect(entradaCorreo(VACIO, { ...valoresCorreo(VACIO), host: "relay-interno", seguridad: "ninguna", remitenteCorreo: "a@b.example" })).toMatchObject({ usuario: "", version: 0, activo: true });
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
