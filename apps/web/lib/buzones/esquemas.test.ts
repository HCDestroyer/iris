import { describe, expect, it } from "vitest";
import { aEntrada, campoDeError, esquemaBuzon, exigeContrasenaImap, exigeContrasenaSmtp, resolverBuzon, valoresIniciales, type Buzon, type ValoresBuzon } from "./esquemas";

const BUZON: Buzon = {
    id: 3,
    empresaId: null,
    nombre: "Soporte",
    correo: "soporte@isp.gt",
    imap: { host: "imap.isp.gt", puerto: 993, tls: "tls", usuario: "soporte@isp.gt", carpeta: "INBOX", carpetaProcesados: null },
    smtp: { host: "smtp.isp.gt", puerto: 587, tls: "starttls", usuario: "soporte@isp.gt" },
    categoriaId: 2,
    prioridad: "media",
    authservId: "mx.isp.gt",
    activo: true,
    ultimaLecturaEn: null,
    ultimoError: null,
    version: 4,
    creadoEn: "2026-10-28T15:00:00Z",
    actualizadoEn: "2026-10-28T15:00:00Z",
};

const nuevo = (parcial: Partial<ValoresBuzon> = {}): ValoresBuzon => ({
    ...valoresIniciales(),
    nombre: "Soporte",
    correo: "soporte@isp.gt",
    imapHost: "imap.isp.gt",
    imapUsuario: "soporte@isp.gt",
    imapContrasena: "secreta",
    smtpHost: "smtp.isp.gt",
    smtpUsuario: "soporte@isp.gt",
    smtpContrasena: "secreta",
    ...parcial,
});

const errores = (r: ReturnType<ReturnType<typeof esquemaBuzon>["safeParse"]>) =>
    r.success ? {} : Object.fromEntries(r.error.issues.map((i) => [i.path.join("."), i.message]));

describe("esquema de buzón (espejo de validarBuzon)", () => {
    it("alta válida y cuerpo sin campos vacíos", () => {
        const r = esquemaBuzon().safeParse(nuevo({ imapHost: "  IMAP.ISP.GT ", authservId: " MX.isp.gt " }));
        expect(r.success).toBe(true);
        const e = aEntrada(nuevo({ imapHost: "  IMAP.ISP.GT ", authservId: " MX.isp.gt ", imapCarpetaProcesados: "" }));
        expect(e.imap).toEqual({ host: "imap.isp.gt", puerto: 993, tls: "tls", usuario: "soporte@isp.gt", contrasena: "secreta", carpeta: "INBOX" });
        expect(e.smtp).toEqual({ host: "smtp.isp.gt", puerto: 587, tls: "starttls", usuario: "soporte@isp.gt", contrasena: "secreta" });
        expect(e.authservId).toBe("mx.isp.gt");
        expect(e.prioridad).toBe("media");
        expect("categoriaId" in e).toBe(false);
    });

    it("al crear exige la contraseña IMAP, y la SMTP solo con usuario", () => {
        const sin = errores(esquemaBuzon().safeParse(nuevo({ imapContrasena: "", smtpContrasena: "" })));
        expect(sin.imapContrasena).toBe("Escribe la contraseña IMAP");
        expect(sin.smtpContrasena).toMatch(/contraseña SMTP/);
        expect(errores(esquemaBuzon().safeParse(nuevo({ smtpUsuario: "", smtpContrasena: "" }))).smtpContrasena).toBeUndefined();
    });

    it("el resolver pide la contraseña desde el primer intento, aunque falten otros campos", async () => {
        const r = await resolverBuzon()(valoresIniciales(), undefined, { fields: {}, shouldUseNativeValidation: false });
        expect(r.errors.nombre?.message).toBe("Escribe un nombre");
        expect(r.errors.imapContrasena?.message).toBe("Escribe la contraseña IMAP");
        expect(r.errors.smtpHost?.message).toBe("Escribe el servidor");
        const ok = await resolverBuzon()(nuevo(), undefined, { fields: {}, shouldUseNativeValidation: false });
        expect(ok.errors).toEqual({});
    });

    it("sin usuario SMTP no se manda contraseña", () => {
        const e = aEntrada(nuevo({ smtpUsuario: "", smtpContrasena: "ignorada" }));
        expect(e.smtp).toEqual({ host: "smtp.isp.gt", puerto: 587, tls: "starttls" });
    });

    it("sin SMTP no valida sus campos ni lo manda", () => {
        const v = nuevo({ conSmtp: false, smtpHost: "", smtpPuerto: "x" });
        expect(esquemaBuzon().safeParse(v).success).toBe(true);
        expect(aEntrada(v).smtp).toBeUndefined();
    });

    it("rechaza host con esquema o ruta, puertos fuera de rango, correo inválido y authserv-id con «;»", () => {
        const e = errores(
            esquemaBuzon().safeParse(nuevo({ imapHost: "https://imap.isp.gt", imapPuerto: "70000", correo: "no-es-correo", authservId: "mx; dkim=pass", smtpPuerto: "0" })),
        );
        expect(e.imapHost).toMatch(/Servidor inválido/);
        expect(e.imapPuerto).toBe("Puerto entre 1 y 65535");
        expect(e.smtpPuerto).toBe("Puerto entre 1 y 65535");
        expect(e.correo).toBe("Correo inválido");
        expect(e.authservId).toMatch(/authserv-id inválido/);
    });

    it("TLS nunca en claro", () => {
        const r = esquemaBuzon().safeParse({ ...nuevo(), imapTls: "ninguno" });
        expect(errores(r).imapTls).toBe("Solo TLS o STARTTLS: nunca en claro");
    });
});

describe("contraseña al editar (la guardada no viaja a otro servidor)", () => {
    const base = valoresIniciales(BUZON);

    it("los valores iniciales nunca traen contraseñas", () => {
        expect(base.imapContrasena).toBe("");
        expect(base.smtpContrasena).toBe("");
        expect(base.conSmtp).toBe(true);
        expect(base.categoriaId).toBe("2");
    });

    it("sin cambiar servidor ni usuario se conserva (contraseña omitida)", () => {
        expect(exigeContrasenaImap(base, BUZON)).toBe(false);
        expect(exigeContrasenaSmtp(base, BUZON)).toBe(false);
        expect(esquemaBuzon(BUZON).safeParse({ ...base, nombre: "Soporte técnico", imapHost: " IMAP.isp.gt" }).success).toBe(true);
        const e = aEntrada(base);
        expect(e.imap.contrasena).toBeUndefined();
        expect(e.smtp?.contrasena).toBeUndefined();
    });

    it.each([
        ["host", { imapHost: "imap2.isp.gt" }],
        ["puerto", { imapPuerto: "143" }],
        ["TLS", { imapTls: "starttls" as const }],
        ["usuario", { imapUsuario: "otro@isp.gt" }],
    ])("cambiar el %s de IMAP exige reescribirla", (_, cambio) => {
        const v = { ...base, ...cambio };
        expect(exigeContrasenaImap(v, BUZON)).toBe(true);
        expect(errores(esquemaBuzon(BUZON).safeParse(v)).imapContrasena).toMatch(/escribe la contraseña de nuevo/);
        expect(esquemaBuzon(BUZON).safeParse({ ...v, imapContrasena: "nueva" }).success).toBe(true);
    });

    it("SMTP: cambiar el servidor exige reescribirla; agregar usuario donde no había, también", () => {
        expect(exigeContrasenaSmtp({ ...base, smtpPuerto: "465" }, BUZON)).toBe(true);
        expect(exigeContrasenaSmtp({ ...base, smtpUsuario: "" }, BUZON)).toBe(false);
        const sinUsuario: Buzon = { ...BUZON, smtp: { host: "smtp.isp.gt", puerto: 587, tls: "starttls", usuario: null } };
        expect(exigeContrasenaSmtp({ ...valoresIniciales(sinUsuario), smtpUsuario: "nuevo@isp.gt" }, sinUsuario)).toBe(true);
        const sinSmtp: Buzon = { ...BUZON, smtp: undefined };
        expect(exigeContrasenaSmtp({ ...valoresIniciales(sinSmtp), conSmtp: true, smtpHost: "smtp.isp.gt", smtpUsuario: "a@isp.gt" }, sinSmtp)).toBe(true);
    });

    it("vacío borra el authserv-id (editar reemplaza todo)", () => {
        expect(aEntrada({ ...base, authservId: "  " }).authservId).toBe("");
    });
});

describe("errores de la API a campos", () => {
    it("acepta la ruta del caso de uso y la de huma", () => {
        expect(campoDeError("imap.contrasena")).toBe("imapContrasena");
        expect(campoDeError("body.smtp.host")).toBe("smtpHost");
        expect(campoDeError("authservId")).toBe("authservId");
        expect(campoDeError("otro")).toBeUndefined();
    });
});
