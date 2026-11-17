import type { Schema } from "@iris/contratos";
import type { Route } from "@playwright/test";
import type { AjusteCorreo, CausaPrueba, EntradaCorreo, EntradaPrueba, OrigenCorreo, ResultadoPrueba } from "../lib/correo/tipos";

/*
 * Correo del sistema en la API falsa (`.claude/integration/contratos/correo-sistema.md` §2 y §3),
 * tipada con `packages/contratos`. Guarda un ajuste por ámbito (`global` o el id de la empresa),
 * calcula `origen` y `enUso` con la precedencia del contrato, nunca devuelve la contraseña (solo
 * `tieneContrasena`) y aplica las reglas del servidor: `global` exige operar sin límite de empresa
 * (403), otra empresa responde 404, la contraseña guardada no se manda a otro servidor
 * (`contrasena_requerida`), sin usuario se borra, y sin cifrado no hay credenciales.
 */

type Problema = Schema<"Problema">;

export type OpcionesCorreo = {
    /** Ajustes ya guardados, por ámbito (`global` o el id de la empresa como texto). */
    guardados?: Record<string, Partial<Guardado>>;
    /** Hay `IRIS_SMTP_*` en el servidor (respaldo). */
    entorno?: boolean;
    /** El primer `GET` falla con 500. */
    fallaLeer?: boolean;
    /** El primer `guardar` (o activar/desactivar) responde 409. */
    conflicto?: boolean;
    /** El primer `guardar` responde 422 con ese código de regla (y ese campo, si lo trae). */
    rechaza?: { codigo: string; field?: string; message?: string };
    /** Resultado de `probar`: `ok` (por defecto), una causa de error o 429 con `Retry-After`. */
    prueba?: "ok" | CausaPrueba | "429" | "causa_nueva" | "demasiados_rechazos" | "cuelga";
    /** Milisegundos que tarda `probar` en responder (un servidor que no contesta tarda unos 11 s). */
    demoraPrueba?: number;
    /** El primer `guardar` responde 429 `demasiados_rechazos` con `Retry-After`. */
    demasiadosRechazos?: boolean;
};

type Guardado = Omit<AjusteCorreo, "ambito" | "empresaId" | "configurado" | "tieneContrasena" | "origen" | "origenDetalle" | "entorno" | "enUso"> & { contrasena: string };

const P = (codigo: string, status: number, title: string, extra: Partial<Problema> = {}): Problema => ({ type: `urn:iris:problema:${codigo}`, title, status, ...extra });
const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
const problema = (route: Route, p: Problema, cabeceras: Record<string, string> = {}) =>
    route.fulfill({ status: p.status, headers: { "Content-Type": "application/problem+json", ...cabeceras }, body: JSON.stringify(p) });

const VACIO: Guardado = { host: "", puerto: 587, seguridad: "starttls", usuario: "", contrasena: "", remitenteNombre: "", remitenteCorreo: "", activo: false, version: 0, actualizadoEn: null };

/** Un ajuste global completo, para los escenarios que parten de algo guardado. */
export const CORREO_GLOBAL: Partial<Guardado> = {
    host: "smtp.proveedor.example",
    puerto: 587,
    seguridad: "starttls",
    usuario: "avisos@isp.example",
    contrasena: "secreta-guardada",
    remitenteNombre: "Iris",
    remitenteCorreo: "no-responder@isp.example",
    activo: true,
    version: 3,
    actualizadoEn: "2026-11-16T18:00:00Z",
    ultimaPrueba: { estado: "ok", en: "2026-11-16T17:59:00Z", causa: null },
};

const DETALLE_CAUSA: Record<CausaPrueba, string> = {
    no_conecta: "No se pudo conectar: revisa el servidor y el puerto.",
    destino_no_permitido: "Ese servidor no se puede usar desde Iris (red interna o no permitida).",
    certificado: "El certificado del servidor no es válido para ese nombre.",
    seguridad: "El servidor no acepta la seguridad elegida (prueba STARTTLS o TLS).",
    credenciales: "El usuario o la contraseña no son correctos.",
    remitente_rechazado: "El servidor no acepta ese remitente.",
    destinatario_rechazado: "El servidor no acepta esa dirección de destino.",
    tiempo_agotado: "El servidor no respondió a tiempo.",
    otro: "El servidor rechazó el envío.",
};

type Pedido = { route: Route; metodo: string; ruta: string; cuerpo: Record<string, unknown>; idempotencia: string | undefined; puedeVer: boolean; puedeConfigurar: boolean; sinLimiteDeEmpresa: boolean; empresaActiva: number | null };

export function apiCorreo(opciones: OpcionesCorreo | undefined) {
    const filas = new Map<string, Guardado>(Object.entries(opciones?.guardados ?? {}).map(([ambito, g]) => [ambito, { ...VACIO, version: 1, ...g }]));
    const guardados: { ambito: string; cuerpo: EntradaCorreo; idempotencia: string | undefined }[] = [];
    const pruebas: { ambito: string; cuerpo: EntradaPrueba }[] = [];
    const cambios: { ambito: string; accion: "activar" | "desactivar"; version: unknown; idempotencia: string | undefined }[] = [];
    let fallaLeer = opciones?.fallaLeer ?? false;
    let conflicto = opciones?.conflicto ?? false;
    let rechaza = opciones?.rechaza;
    let demasiadosRechazos = opciones?.demasiadosRechazos ?? false;
    const entorno = opciones?.entorno ? { configurado: true, host: "localhost", remitenteCorreo: "no-responder@iris.example" } : { configurado: false, host: "", remitenteCorreo: "" };

    /** Precedencia de §2: empresa activa → global activo → entorno → ninguno. */
    function origenDe(ambito: string): OrigenCorreo {
        if (ambito !== "global" && filas.get(ambito)?.activo) return "empresa";
        if (filas.get("global")?.activo) return "global";
        return entorno.configurado ? "entorno" : "ninguno";
    }

    /** La frase lista del servidor (`origenDetalle`). */
    const DETALLE_ORIGEN: Record<OrigenCorreo, string> = {
        empresa: "Los correos de esta empresa salen por su propio ajuste.",
        global: "Los correos de esta instalación salen por el ajuste global.",
        entorno: "Los correos salen por la configuración del servidor (.env).",
        ninguno: "No hay correo configurado: los avisos llegan solo por la campana.",
    };

    function ver(ambito: string): AjusteCorreo {
        const g = filas.get(ambito);
        const { contrasena, ...publico } = g ?? VACIO;
        const origen = origenDe(ambito);
        return {
            ...publico,
            ambito,
            empresaId: ambito === "global" ? null : Number(ambito),
            configurado: g !== undefined,
            tieneContrasena: contrasena !== "",
            origen,
            origenDetalle: DETALLE_ORIGEN[origen],
            // Este ajuste es el que se usa hoy.
            enUso: g !== undefined && g.activo && origen === (ambito === "global" ? "global" : "empresa"),
            // 1.1.0: el host y el remitente del servidor solo se dicen en el ámbito global.
            entorno: ambito === "global" ? entorno : { configurado: entorno.configurado },
        };
    }

    async function manejar(x: Pedido): Promise<boolean> {
        const m = /^\/api\/v1\/correo-sistema\/([^/]+)(?:\/(guardar|probar|activar|desactivar))?$/.exec(x.ruta);
        if (!m) return false;
        const { route, metodo, cuerpo } = x;
        const ambito = m[1] ?? "";
        const accion = m[2];
        const prohibido = () => problema(route, P("prohibido", 403, "Sin permiso"));
        // `global` exige operar sin límite de empresa (403); el de otra empresa no existe para ti (404).
        if (ambito === "global" && !x.sinLimiteDeEmpresa) return (await prohibido(), true);
        if (ambito !== "global" && String(x.empresaActiva) !== ambito) return (await problema(route, P("no_encontrado", 404, "No encontrado")), true);
        if (!accion && metodo === "GET") {
            if (!x.puedeVer && !x.puedeConfigurar) return (await prohibido(), true);
            if (fallaLeer) {
                fallaLeer = false;
                return (await problema(route, P("error_interno", 500, "Error del servidor")), true);
            }
            return (await json(route, ver(ambito)), true);
        }
        if (metodo !== "POST" || !accion) return false;
        if (!x.puedeConfigurar) return (await prohibido(), true);
        const actual = filas.get(ambito);
        const regla = (codigo: string, field: string, message: string) => problema(route, P(codigo, 422, message, { errors: [{ field, message, rule: codigo }] }));
        /** Las reglas de `guardar` y `probar` sobre las credenciales; devuelve la respuesta 422 o `null`. */
        const reglaDeCredenciales = () => {
            const e = cuerpo as unknown as EntradaPrueba;
            const usuario = e.usuario ?? "";
            // 1.1.0: «sin cifrado» solo en el ámbito global.
            if (e.seguridad === "ninguna" && ambito !== "global") return regla("sin_cifrado_solo_global", "seguridad", "Sin cifrado solo en el ajuste global");
            if (e.seguridad === "ninguna" && usuario !== "") return regla("sin_cifrado_sin_credenciales", "usuario", "Sin cifrado, el usuario va vacío");
            if (e.seguridad === "ninguna" && (e.contrasena ?? "") !== "") return regla("sin_cifrado_sin_credenciales", "contrasena", "Sin cifrado no se guarda contraseña");
            if (usuario === "" || (e.contrasena ?? "") !== "") return null;
            if (e.contrasena === "" || !actual || actual.contrasena === "") return regla("contrasena_requerida", "contrasena", "Escribe la contraseña");
            const otroDestino = e.host !== actual.host || e.puerto !== actual.puerto || usuario !== (actual.usuario ?? "");
            return otroDestino ? regla("contrasena_requerida", "contrasena", "Vuelve a escribir la contraseña") : null;
        };
        if (accion === "probar") {
            pruebas.push({ ambito, cuerpo: cuerpo as unknown as EntradaPrueba });
            const rechazo = reglaDeCredenciales();
            if (rechazo) return (await rechazo, true);
            const prueba = opciones?.prueba ?? "ok";
            if (opciones?.demoraPrueba) await new Promise((r) => setTimeout(r, opciones.demoraPrueba));
            // El servidor de Iris nunca contesta: la petición queda abierta hasta que el panel la corta.
            if (prueba === "cuelga") return true;
            if (prueba === "demasiados_rechazos") return (await problema(route, P("demasiados_rechazos", 429, "Demasiados intentos"), { "Retry-After": "600" }), true);
            if (prueba === "429") return (await problema(route, P("demasiadas_pruebas", 429, "Demasiadas pruebas"), { "Retry-After": "720" }), true);
            const resultado: ResultadoPrueba =
                prueba === "ok"
                    ? { estado: "ok", causa: null, detalle: `El correo de prueba salió. Revisa la bandeja de ${String(cuerpo.para)}.` }
                    : prueba === "causa_nueva"
                      ? { estado: "error", causa: "causa_nueva" as CausaPrueba, detalle: "" }
                      : { estado: "error", causa: prueba, detalle: DETALLE_CAUSA[prueba] };
            // La última prueba del ámbito queda registrada (la devuelve el `GET`).
            if (actual) filas.set(ambito, { ...actual, ultimaPrueba: { estado: resultado.estado, en: "2026-11-17T00:30:00Z", causa: prueba === "ok" || prueba === "causa_nueva" ? null : (resultado.causa ?? null) } });
            return (await json(route, resultado), true);
        }
        if (conflicto || (accion !== "guardar" && cuerpo.version !== actual?.version) || (accion === "guardar" && cuerpo.version !== (actual?.version ?? 0))) {
            conflicto = false;
            return (await problema(route, P("conflicto_version", 409, "Alguien más cambió el correo")), true);
        }
        if (accion === "guardar") {
            const entrada = cuerpo as unknown as EntradaCorreo;
            guardados.push({ ambito, cuerpo: entrada, idempotencia: x.idempotencia });
            if (demasiadosRechazos) {
                demasiadosRechazos = false;
                return (await problema(route, P("demasiados_rechazos", 429, "Demasiados intentos"), { "Retry-After": "600" }), true);
            }
            if (rechaza) {
                const r = rechaza;
                rechaza = undefined;
                const errors = r.field ? [{ field: r.field, message: r.message ?? "Dato inválido", rule: r.codigo }] : [];
                return (await problema(route, P(r.codigo, 422, r.message ?? "Datos inválidos", { errors })), true);
            }
            const rechazo = reglaDeCredenciales();
            if (rechazo) return (await rechazo, true);
            const { contrasena, version: _version, ...resto } = entrada;
            void _version;
            filas.set(ambito, {
                ...(actual ?? VACIO),
                ...resto,
                usuario: entrada.usuario ?? "",
                remitenteNombre: entrada.remitenteNombre ?? "",
                // Ausente = se conserva la guardada; "" = se borra; un valor = la reemplaza. Sin usuario se borra sola.
                contrasena: (entrada.usuario ?? "") === "" ? "" : contrasena === undefined ? (actual?.contrasena ?? "") : contrasena,
                version: (actual?.version ?? 0) + 1,
                actualizadoEn: "2026-11-16T19:00:00Z",
            });
            return (await json(route, ver(ambito)), true);
        }
        if (!actual) return (await problema(route, P("no_encontrado", 404, "No hay un correo guardado")), true);
        cambios.push({ ambito, accion: accion === "activar" ? "activar" : "desactivar", version: cuerpo.version, idempotencia: x.idempotencia });
        filas.set(ambito, { ...actual, activo: accion === "activar", version: actual.version + 1 });
        return (await json(route, ver(ambito)), true);
    }

    return { manejar, guardados, pruebas, cambios, filas };
}

export type ApiCorreo = ReturnType<typeof apiCorreo>;
