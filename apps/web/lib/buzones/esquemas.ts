import type { Schema } from "@iris/contratos";
import { zodResolver } from "@hookform/resolvers/zod";
import type { FieldErrors, Resolver } from "react-hook-form";
import { z } from "zod";

/*
 * Espejo del validador de buzones de la API (apps/api/internal/soporte/buzones.go,
 * `validarBuzon` y la regla de contraseña de `editarBuzon`). El formulario es plano (cadenas y
 * casillas); `aEntrada` arma el cuerpo de `POST /buzones` y `POST /buzones/{id}/editar`.
 */

export type Buzon = Schema<"Buzon">;
export type Tls = Schema<"ServidorIMAP">["tls"];
export type PrioridadBuzon = Schema<"Buzon">["prioridad"];

/** Puertos habituales por modo TLS: IMAPS 993 / STARTTLS 143; SMTPS 465 / submission 587. */
export const PUERTOS = {
    imap: { tls: 993, starttls: 143 },
    smtp: { tls: 465, starttls: 587 },
} as const satisfies Record<"imap" | "smtp", Record<Tls, number>>;

export type ValoresBuzon = {
    nombre: string;
    correo: string;
    imapHost: string;
    imapPuerto: string;
    imapTls: Tls;
    imapUsuario: string;
    imapContrasena: string;
    imapCarpeta: string;
    imapCarpetaProcesados: string;
    conSmtp: boolean;
    smtpHost: string;
    smtpPuerto: string;
    smtpTls: Tls;
    smtpUsuario: string;
    smtpContrasena: string;
    authservId: string;
    categoriaId: string;
    prioridad: PrioridadBuzon;
};

export const CAMPOS_BUZON = [
    "nombre",
    "correo",
    "imapHost",
    "imapPuerto",
    "imapTls",
    "imapUsuario",
    "imapContrasena",
    "imapCarpeta",
    "imapCarpetaProcesados",
    "smtpHost",
    "smtpPuerto",
    "smtpTls",
    "smtpUsuario",
    "smtpContrasena",
    "authservId",
    "categoriaId",
    "prioridad",
] as const satisfies readonly (keyof ValoresBuzon)[];

export type CampoBuzon = (typeof CAMPOS_BUZON)[number];

/** Valores iniciales: vacíos para el alta; los del buzón (sin contraseñas, que nunca llegan) al editar. */
export function valoresIniciales(b?: Buzon): ValoresBuzon {
    if (!b) {
        return {
            nombre: "",
            correo: "",
            imapHost: "",
            imapPuerto: String(PUERTOS.imap.tls),
            imapTls: "tls",
            imapUsuario: "",
            imapContrasena: "",
            imapCarpeta: "INBOX",
            imapCarpetaProcesados: "",
            conSmtp: true,
            smtpHost: "",
            smtpPuerto: String(PUERTOS.smtp.starttls),
            smtpTls: "starttls",
            smtpUsuario: "",
            smtpContrasena: "",
            authservId: "",
            categoriaId: "",
            prioridad: "media",
        };
    }
    return {
        nombre: b.nombre,
        correo: b.correo,
        imapHost: b.imap.host,
        imapPuerto: String(b.imap.puerto),
        imapTls: b.imap.tls,
        imapUsuario: b.imap.usuario,
        imapContrasena: "",
        imapCarpeta: b.imap.carpeta,
        imapCarpetaProcesados: b.imap.carpetaProcesados ?? "",
        conSmtp: b.smtp !== undefined,
        smtpHost: b.smtp?.host ?? "",
        smtpPuerto: String(b.smtp?.puerto ?? PUERTOS.smtp.starttls),
        smtpTls: b.smtp?.tls ?? "starttls",
        smtpUsuario: b.smtp?.usuario ?? "",
        smtpContrasena: "",
        authservId: b.authservId ?? "",
        categoriaId: b.categoriaId === null ? "" : String(b.categoriaId),
        prioridad: b.prioridad,
    };
}

const normHost = (h: string) => h.trim().toLowerCase();

/**
 * ¿Hay que volver a escribir la contraseña IMAP? Al crear, siempre. Al editar, si cambia el host,
 * el puerto, el TLS o el usuario (la guardada no viaja a otro servidor), como la API.
 */
export function exigeContrasenaImap(v: Pick<ValoresBuzon, "imapHost" | "imapPuerto" | "imapTls" | "imapUsuario">, antes?: Buzon): boolean {
    if (!antes) {
        return true;
    }
    return (
        normHost(v.imapHost) !== antes.imap.host ||
        Number(v.imapPuerto) !== antes.imap.puerto ||
        v.imapTls !== antes.imap.tls ||
        v.imapUsuario.trim() !== antes.imap.usuario
    );
}

/**
 * ¿Hay que escribir la contraseña SMTP? Solo con usuario (sin usuario no se autentica). Al
 * editar se conserva si el SMTP anterior tenía el mismo host, puerto, TLS y usuario.
 */
export function exigeContrasenaSmtp(
    v: Pick<ValoresBuzon, "conSmtp" | "smtpHost" | "smtpPuerto" | "smtpTls" | "smtpUsuario">,
    antes?: Buzon,
): boolean {
    const usuario = v.smtpUsuario.trim();
    if (!v.conSmtp || usuario === "") {
        return false;
    }
    const previo = antes?.smtp;
    if (!previo || previo.usuario === null) {
        return true;
    }
    return normHost(v.smtpHost) !== previo.host || Number(v.smtpPuerto) !== previo.puerto || v.smtpTls !== previo.tls || usuario !== previo.usuario;
}

const maximo = (n: number) => `Admite hasta ${n.toLocaleString("es-GT")} caracteres`;

const obligatorio = (mensaje: string, max: number) => z.string().trim().min(1, { message: mensaje, abort: true }).max(max, maximo(max));
const opcional = (max: number) => z.string().trim().max(max, maximo(max));

/** Host: nombre o IP, sin espacios ni `/ \ @ ? #` (la API revisa las redes internas al conectar). */
const host = z
    .string()
    .trim()
    .min(1, { message: "Escribe el servidor", abort: true })
    .max(253, maximo(253))
    .refine((h) => !/[\s/\\@?#]/.test(h), "Servidor inválido: solo el nombre o la IP, sin «https://» ni rutas");

const puerto = z
    .string()
    .trim()
    .min(1, { message: "Escribe el puerto", abort: true })
    .refine((p) => /^\d+$/.test(p) && Number(p) >= 1 && Number(p) <= 65535, "Puerto entre 1 y 65535");

const tls = z.enum(["tls", "starttls"], { message: "Solo TLS o STARTTLS: nunca en claro" });

const contrasena = z.string().max(512, maximo(512));

export const PRIORIDADES_BUZON = ["baja", "media", "alta", "critica"] as const satisfies readonly PrioridadBuzon[];

/**
 * Esquema del formulario. `antes` es el buzón que se edita (sin él, alta). Con SMTP apagado sus
 * campos no se validan.
 */
export function esquemaBuzon(antes?: Buzon) {
    return z
        .object({
            nombre: obligatorio("Escribe un nombre", 120),
            correo: z.string().trim().min(1, { message: "Escribe la dirección del buzón", abort: true }).max(254, maximo(254)).pipe(z.email("Correo inválido")),
            imapHost: host,
            imapPuerto: puerto,
            imapTls: tls,
            imapUsuario: obligatorio("Escribe el usuario IMAP", 254),
            imapContrasena: contrasena,
            imapCarpeta: opcional(200),
            imapCarpetaProcesados: opcional(200),
            conSmtp: z.boolean(),
            smtpHost: z.string(),
            smtpPuerto: z.string(),
            smtpTls: tls,
            smtpUsuario: z.string(),
            smtpContrasena: contrasena,
            authservId: opcional(253).refine((a) => !/[\s;()"]/.test(a), "authserv-id inválido: un nombre, sin espacios ni «;»"),
            categoriaId: z.string().refine((c) => c === "" || /^[1-9]\d*$/.test(c), "Categoría inválida"),
            prioridad: z.enum(PRIORIDADES_BUZON),
        })
        .superRefine((v, ctx) => {
            for (const e of erroresCruzados(v, antes)) {
                ctx.addIssue({ code: "custom", path: [e.campo], message: e.mensaje });
            }
        });
}

/**
 * Reglas entre campos (SMTP según la casilla y contraseñas según lo que cambió). Zod no corre el
 * `superRefine` del objeto si otro campo ya falló; `resolverBuzon` las suma siempre, para que la
 * contraseña se pida desde el primer intento.
 */
export function erroresCruzados(v: ValoresBuzon, antes?: Buzon): { campo: CampoBuzon; mensaje: string }[] {
    const errores: { campo: CampoBuzon; mensaje: string }[] = [];
    if (exigeContrasenaImap(v, antes) && v.imapContrasena === "") {
        errores.push({
            campo: "imapContrasena",
            mensaje: antes ? "Cambiaste el servidor, el puerto, el TLS o el usuario: escribe la contraseña de nuevo" : "Escribe la contraseña IMAP",
        });
    }
    if (!v.conSmtp) {
        return errores;
    }
    for (const [campo, esquema] of [
        ["smtpHost", host],
        ["smtpPuerto", puerto],
        ["smtpUsuario", opcional(254)],
    ] as const) {
        const r = esquema.safeParse(v[campo]);
        if (!r.success) {
            errores.push({ campo, mensaje: r.error.issues[0]?.message ?? "Valor inválido" });
        }
    }
    if (exigeContrasenaSmtp(v, antes) && v.smtpContrasena === "") {
        errores.push({
            campo: "smtpContrasena",
            mensaje:
                antes?.smtp && antes.smtp.usuario !== null
                    ? "Cambiaste el servidor, el puerto, el TLS o el usuario: escribe la contraseña de nuevo"
                    : "Escribe la contraseña SMTP (o deja el usuario vacío si no se autentica)",
        });
    }
    return errores;
}

/** Resolver de react-hook-form: el esquema zod más las reglas entre campos, aunque otro campo falle. */
export function resolverBuzon(antes?: Buzon): Resolver<ValoresBuzon> {
    const base = zodResolver(esquemaBuzon(antes));
    return async (valores, contexto, opciones) => {
        const r = await base(valores, contexto, opciones);
        if (Object.keys(r.errors).length === 0) {
            return r;
        }
        const errores: FieldErrors<ValoresBuzon> = { ...r.errors };
        for (const e of erroresCruzados(valores, antes)) {
            errores[e.campo] ??= { type: "custom", message: e.mensaje };
        }
        return { values: {}, errors: errores };
    };
}

export type EntradaBuzon = Schema<"EntradaCrearBuzon">;

/** Cuerpo para crear o editar (sin `version`). Contraseña vacía = se omite (al editar, se conserva). */
export function aEntrada(v: ValoresBuzon): EntradaBuzon {
    const entrada: EntradaBuzon = {
        nombre: v.nombre.trim(),
        correo: v.correo.trim(),
        imap: {
            host: normHost(v.imapHost),
            puerto: Number(v.imapPuerto),
            tls: v.imapTls,
            usuario: v.imapUsuario.trim(),
            ...(v.imapContrasena !== "" ? { contrasena: v.imapContrasena } : {}),
            ...(v.imapCarpeta.trim() !== "" ? { carpeta: v.imapCarpeta.trim() } : {}),
            ...(v.imapCarpetaProcesados.trim() !== "" ? { carpetaProcesados: v.imapCarpetaProcesados.trim() } : {}),
        },
        prioridad: v.prioridad,
        // Vacío borra el authserv-id (editar reemplaza todos los datos).
        authservId: v.authservId.trim().toLowerCase(),
        ...(v.categoriaId !== "" ? { categoriaId: Number(v.categoriaId) } : {}),
    };
    if (v.conSmtp) {
        const usuario = v.smtpUsuario.trim();
        entrada.smtp = {
            host: normHost(v.smtpHost),
            puerto: Number(v.smtpPuerto),
            tls: v.smtpTls,
            ...(usuario !== "" ? { usuario } : {}),
            ...(usuario !== "" && v.smtpContrasena !== "" ? { contrasena: v.smtpContrasena } : {}),
        };
    }
    return entrada;
}

const RUTAS_ERROR: Record<string, CampoBuzon> = {
    nombre: "nombre",
    correo: "correo",
    "imap.host": "imapHost",
    "imap.puerto": "imapPuerto",
    "imap.tls": "imapTls",
    "imap.usuario": "imapUsuario",
    "imap.contrasena": "imapContrasena",
    "imap.carpeta": "imapCarpeta",
    "imap.carpetaProcesados": "imapCarpetaProcesados",
    "smtp.host": "smtpHost",
    "smtp.puerto": "smtpPuerto",
    "smtp.tls": "smtpTls",
    "smtp.usuario": "smtpUsuario",
    "smtp.contrasena": "smtpContrasena",
    authservId: "authservId",
    categoriaId: "categoriaId",
    prioridad: "prioridad",
};

/** Campo del formulario para un `errors[].field` de la API (`imap.host` o `body.imap.host`). */
export function campoDeError(field: string): CampoBuzon | undefined {
    return RUTAS_ERROR[field.replace(/^body\./, "")];
}
