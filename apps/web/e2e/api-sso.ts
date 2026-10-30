import type { Schema } from "@iris/contratos";
import type { Route } from "@playwright/test";

/*
 * Autologin desde Argos, re-autenticación, vínculos y conectores en la API falsa (e2e/api-falsa.ts),
 * tipados con packages/contratos y fieles a identidad/sso.go, identidad/reautenticar.go y
 * integracion (conectores, #54): el canje es público (sin CSRF), `confirmar_cambio` no consume el
 * ticket, `vincular` deja la cookie `iris_sso_vincular`, la vinculación exige correo verificado e
 * igual al de la cuenta (si no, 409 `vinculo_requiere_admin`) y una sesión `sso` pide
 * `POST /sesion/reautenticar` antes de las acciones sensibles.
 */

type Problema = Schema<"Problema">;
type Permiso = Schema<"Permiso">;
type Usuario = Schema<"Usuario">;
type Vinculo = Schema<"VinculoSSO">;
type Conector = Schema<"Conector">;
type Canje = Schema<"CanjeSSO">;
type Externo = Schema<"UsuarioExternoVisto">;

const json = (route: Route, cuerpo: unknown, status = 200, cabeceras: Record<string, string> = {}) =>
    route.fulfill({ status, contentType: "application/json", headers: cabeceras, body: JSON.stringify(cuerpo) });
const problema = (route: Route, p: Problema) => route.fulfill({ status: p.status, contentType: "application/problem+json", body: JSON.stringify(p) });

/** Tickets del escenario (43 caracteres, como los de la API). */
const tk = (semilla: string) => semilla.padEnd(43, "x");
export const TICKETS = {
    sesion: tk("SesionSofia"),
    mfa: tk("MfaMario"),
    vincular: tk("VincularCarlosSinVerificar"),
    vincularVerificado: tk("VincularCarlosVerificado"),
    /** Otro ticket con lo mismo (Argos emite uno nuevo cada vez). */
    vincularVerificado2: tk("VincularCarlosVerificadoOtraVez"),
    destinoExterno: tk("DestinoExterno"),
    usado: tk("Usado"),
    apagado: tk("ConectorApagado"),
    requiereMfa: tk("RequiereMfa"),
    inactiva: tk("CuentaInactiva"),
    canjes: tk("DemasiadosCanjes"),
} as const;

export const EXTERNO_CARLOS: Externo = { idExterno: "argos-77", correo: "carlos@iris.test", correoVerificado: false, nombre: "Carlos Ruiz (Argos)" };

export const CONECTORES_INICIALES = (): Conector[] => [
    {
        id: 1,
        sistema: "argosisp",
        estado: "activo",
        origen: "enlace_remoto",
        baseUrl: "https://argos.fibra.test",
        apiUrl: "https://argos.fibra.test",
        panelUrl: "https://panel.argos.fibra.test",
        remotoId: "c-1",
        empresas: [10],
        activadoEn: "2026-10-01T15:00:00Z",
        ultimoWebhookEn: "2026-10-29T16:00:00Z",
        creadoEn: "2026-10-01T14:00:00Z",
        actualizadoEn: "2026-10-01T15:00:00Z",
        urlPublica: "https://panel.argos.fibra.test",
        webhookUrl: "https://iris.fibra.test/api/v1/integraciones/argosisp/webhooks",
        version: 3,
    },
    {
        id: 2,
        sistema: "argosisp",
        estado: "apagado",
        origen: "enlace_local",
        baseUrl: "https://argos-nuevo.fibra.test",
        // Conector anterior a Argos 0.4.0: sin panel informado, los enlaces usan la API.
        apiUrl: "https://argos-nuevo.fibra.test",
        panelUrl: null,
        remotoId: "c-2",
        empresas: [10],
        activadoEn: null,
        ultimoWebhookEn: null,
        creadoEn: "2026-10-29T14:00:00Z",
        actualizadoEn: "2026-10-29T14:00:00Z",
        urlPublica: "https://argos-nuevo.fibra.test",
        webhookUrl: "https://iris.fibra.test/api/v1/integraciones/argosisp/webhooks",
        version: 1,
    },
];

const ORIGENES_PERMITIDOS = ["https://argos.fibra.test", "https://argos-nuevo.fibra.test", "https://horus.fibra.test"];

export type DepsSSO = {
    usuarios: () => Usuario[];
    ver: (u: Usuario) => Usuario;
    contrasenaValida: (u: Usuario, contrasena: string) => boolean;
    /** Deja la sesión pendiente de MFA de ese correo (como el login). */
    marcarMfa: (correo: string) => void;
    /** Set-Cookie de la sesión (`s:` completa o `p:` pendiente de MFA). */
    cookiesSesion: (valor: string) => string;
};

export type ApiSSO = {
    /** Cuerpos y cabeceras de cada canje. */
    canjes: { cuerpo: Schema<"EntradaCanjearSSO">; csrf: boolean; empresa: boolean; sesion: string | undefined }[];
    /** Vinculaciones pedidas (`POST /sso/vincular`). */
    vinculaciones: { idExterno: string; correo: string; cookie: boolean }[];
    reautenticaciones: number;
    /** `POST /sso/descartar` recibidos («No fui yo»). */
    descartes: number;
    vinculos: Map<number, Vinculo[]>;
    conectores: Conector[];
    /** «Conectar con Argos» (0.4.0 §1.4): autorizaciones iniciadas, por state. */
    autorizaciones: Map<string, { panelUrl: string; usada: boolean }>;
    /** Cuerpos de `POST …/autorizaciones/completar` con su Idempotency-Key. */
    completados: { cuerpo: Schema<"EntradaCompletarAutorizacion">; idempotencia: string | undefined }[];
    /** States cuya primera vuelta responde 409 idempotencia_en_proceso (otro envío en curso). */
    enProceso: Set<string>;
    /** Correos con una sesión abierta por autologin (piden re-autenticarse). */
    sesionesSSO: Set<string>;
    /** Al iniciar sesión con contraseña, la sesión deja de ser `sso`. */
    sesionConContrasena: (correo: string) => void;
    manejarPublica: (c: { route: Route; metodo: string; ruta: string; cabeceras: Record<string, string>; cuerpo: unknown; sesion: string | undefined }) => Promise<boolean>;
    manejar: (c: {
        route: Route;
        metodo: string;
        ruta: string;
        actual: Usuario;
        puede: (p: Permiso) => boolean;
        cuerpo: unknown;
        cookies: string;
        idempotencia?: string | undefined;
    }) => Promise<boolean>;
};

export function apiSSO(deps: DepsSSO): ApiSSO {
    const canjes: ApiSSO["canjes"] = [];
    const vinculaciones: ApiSSO["vinculaciones"] = [];
    const sesionesSSO = new Set<string>();
    const reautenticadas = new Set<string>();
    const conectores = CONECTORES_INICIALES();
    const vinculos = new Map<number, Vinculo[]>([[6, [{ id: 1, conectorId: 1, sistema: "argosisp", usuarioExterno: "argos-6", creadoEn: "2026-10-20T15:00:00Z" }]]]);
    let siguienteVinculo = 100;
    let siguienteConector = 10;
    // Vinculación pendiente (cookie iris_sso_vincular) del último canje `vincular`.
    let pendiente: { externo: Externo; destino: Canje["destino"]; desde: number } | null = null;
    const usados = new Set<string>();
    const api: ApiSSO = {
        canjes,
        vinculaciones,
        reautenticaciones: 0,
        descartes: 0,
        vinculos,
        conectores,
        autorizaciones: new Map(),
        completados: [],
        enProceso: new Set(),
        sesionesSSO,
        sesionConContrasena: (correo) => {
            sesionesSSO.delete(correo);
            reautenticadas.delete(correo);
        },
        async manejarPublica({ route, metodo, ruta, cabeceras, cuerpo, sesion }) {
            if (metodo === "POST" && ruta === "/api/v1/sso/descartar") {
                // Contrato acordado con I1: borra la cookie y anula la vinculación pendiente.
                api.descartes += 1;
                pendiente = null;
                await route.fulfill({ status: 204, headers: { "Set-Cookie": "iris_sso_vincular=; Path=/api/v1/sso; Max-Age=0" } });
                return true;
            }
            if (metodo !== "POST" || ruta !== "/api/v1/sso/canjear") return false;
            const entrada = (cuerpo ?? {}) as Schema<"EntradaCanjearSSO">;
            canjes.push({ cuerpo: entrada, csrf: Boolean(cabeceras["x-csrf-token"]), empresa: Boolean(cabeceras["x-iris-empresa"]), sesion });
            const t = entrada.ticket;
            const err = (status: number, codigo: string, title: string) => problema(route, { type: `urn:iris:problema:${codigo}`, title, status });
            if (t === TICKETS.usado || usados.has(t)) return (await err(401, "ticket_invalido", "Ticket inválido"), true);
            if (t === TICKETS.apagado) return (await err(409, "conector_apagado", "Conector apagado"), true);
            if (t === TICKETS.requiereMfa) return (await err(403, "sso_requiere_mfa", "Requiere MFA"), true);
            if (t === TICKETS.inactiva) return (await err(403, "prohibido", "Sin permiso"), true);
            if (t === TICKETS.canjes) return (await err(429, "demasiados_canjes", "Demasiados canjes"), true);

            const destino = (tipo: "ticket" | "orden" | "prospecto", id: number, ruta: string) => ({ tipo, id, ruta });
            const usuario = (correo: string) => {
                const u = deps.usuarios().find((x) => x.correo === correo);
                return u ? deps.ver(u) : undefined;
            };
            const entrar = async (correo: string, d: Canje["destino"]) => {
                const u = usuario(correo);
                if (!u) return err(403, "prohibido", "Sin permiso");
                const actual = sesion?.startsWith("s:") ? decodeURIComponent(sesion.slice(2)) : undefined;
                if (actual && actual !== correo && !entrada.confirmar) {
                    // No consume el ticket.
                    return json(route, { estado: "confirmar_cambio", destino: d, entrarComo: { nombreIris: u.nombre, idExterno: `argos-${u.id}`, nombreExterno: `${u.nombre} (Argos)` } } satisfies Canje);
                }
                usados.add(t);
                if (actual === correo) {
                    return json(route, { estado: "sesion", destino: d, sesionReutilizada: true } satisfies Canje);
                }
                if (u.mfaActivo) {
                    deps.marcarMfa(correo);
                    sesionesSSO.add(correo);
                    return json(route, { estado: "mfa", destino: d, sesion: { csrfToken: "csrf-e2e", mfaRequerido: true, expiraEn: "2026-10-30T23:00:00Z" } } satisfies Canje, 200, {
                        "Set-Cookie": deps.cookiesSesion(`p:${correo}`),
                    });
                }
                sesionesSSO.add(correo);
                reautenticadas.delete(correo);
                return json(route, { estado: "sesion", destino: d, sesion: { csrfToken: "csrf-e2e", mfaRequerido: false, expiraEn: "2026-10-30T23:00:00Z", usuario: u } } satisfies Canje, 200, {
                    "Set-Cookie": deps.cookiesSesion(`s:${correo}`),
                });
            };
            if (t === TICKETS.sesion) return (await entrar("sofia@iris.test", destino("ticket", 1, "/tickets/1")), true);
            if (t === TICKETS.destinoExterno) return (await entrar("sofia@iris.test", destino("ticket", 1, "https://malo.test/tickets/1")), true);
            if (t === TICKETS.mfa) return (await entrar("mario@iris.test", destino("orden", 1, "/ordenes/1")), true);
            if (t === TICKETS.vincular || t === TICKETS.vincularVerificado || t === TICKETS.vincularVerificado2) {
                usados.add(t);
                const externo: Externo = { ...EXTERNO_CARLOS, correoVerificado: t !== TICKETS.vincular };
                const d = destino("prospecto", 4, "/comercial/4");
                pendiente = { externo, destino: d, desde: Date.now() };
                await json(route, { estado: "vincular", destino: d, externo, vinculacionExpiraEn: "2026-10-30T18:10:00Z" } satisfies Canje, 200, {
                    "Set-Cookie": "iris_sso_vincular=v1; Path=/api/v1/sso; HttpOnly; SameSite=Lax",
                });
                return true;
            }
            await err(401, "ticket_invalido", "Ticket inválido");
            return true;
        },
        async manejar({ route, metodo, ruta, actual, puede, cuerpo, cookies, idempotencia }) {
            const sso = sesionesSSO.has(actual.correo) && !reautenticadas.has(actual.correo);
            const pedirReautenticacion = () =>
                problema(route, {
                    type: "urn:iris:problema:reautenticacion_requerida",
                    title: "Re-autenticación requerida",
                    status: 403,
                    detail: "Entraste desde Argos: confirma tu contraseña antes de esta acción",
                });
            const prohibido = () => problema(route, { type: "urn:iris:problema:prohibido", title: "Sin permiso", status: 403 });

            if (metodo === "POST" && ruta === "/api/v1/sesion/reautenticar") {
                const { contrasena } = (cuerpo ?? {}) as Schema<"EntradaReautenticar">;
                if (!deps.contrasenaValida(actual, contrasena)) {
                    await problema(route, { type: "urn:iris:problema:credenciales_invalidas", title: "Credenciales inválidas", status: 401 });
                    return true;
                }
                api.reautenticaciones += 1;
                reautenticadas.add(actual.correo);
                await json(route, { validaHasta: "2026-10-30T18:15:00Z" } satisfies Schema<"Reautenticado">);
                return true;
            }

            if (metodo === "POST" && ruta === "/api/v1/sso/vincular") {
                const { id_externo } = (cuerpo ?? {}) as Schema<"EntradaVincularSSO">;
                const cookie = /(?:^|;\s*)iris_sso_vincular=/.test(cookies);
                vinculaciones.push({ idExterno: id_externo, correo: actual.correo, cookie });
                if (!pendiente || !cookie || pendiente.externo.idExterno !== id_externo) {
                    await problema(route, { type: "urn:iris:problema:vinculacion_invalida", title: "Vinculación inválida", status: 422 });
                    return true;
                }
                if (!pendiente.externo.correoVerificado || pendiente.externo.correo.toLowerCase() !== actual.correo.toLowerCase()) {
                    await problema(route, { type: "urn:iris:problema:vinculo_requiere_admin", title: "Vínculo requiere administrador", status: 409 });
                    return true;
                }
                const v: Vinculo = { id: siguienteVinculo++, conectorId: 1, sistema: "argosisp", usuarioExterno: id_externo, creadoEn: new Date().toISOString() };
                vinculos.set(actual.id, [...(vinculos.get(actual.id) ?? []), v]);
                const destino = pendiente.destino;
                pendiente = null;
                await json(route, { vinculo: v, destino } satisfies Schema<"VinculoSSOCreado">, 200, { "Set-Cookie": "iris_sso_vincular=; Path=/api/v1/sso; Max-Age=0" });
                return true;
            }

            const detalle = /^\/api\/v1\/usuarios\/(\d+)$/.exec(ruta);
            if (detalle && metodo === "GET") {
                if (!puede("usuarios.ver")) return (await prohibido(), true);
                const u = deps.usuarios().find((x) => x.id === Number(detalle[1]));
                if (!u) await problema(route, { type: "urn:iris:problema:no_encontrado", title: "No encontrado", status: 404 });
                else await json(route, deps.ver(u));
                return true;
            }

            const vin = /^\/api\/v1\/usuarios\/(\d+)\/vinculos-sso(?:\/(\d+)\/borrar)?$/.exec(ruta);
            if (vin) {
                const id = Number(vin[1]);
                const propio = id === actual.id;
                if (metodo === "GET" && !vin[2]) {
                    if (!propio && !puede("usuarios.ver")) return (await prohibido(), true);
                    await json(route, { data: vinculos.get(id) ?? [], nextCursor: null } satisfies Schema<"PaginaVinculoSSO">);
                    return true;
                }
                if (metodo !== "POST") return false;
                if (!propio && !puede("usuarios.editar")) return (await prohibido(), true);
                if (sso) return (await pedirReautenticacion(), true);
                if (vin[2]) {
                    const lista = vinculos.get(id) ?? [];
                    const v = lista.find((x) => x.id === Number(vin[2]));
                    if (!v) return (await problema(route, { type: "urn:iris:problema:no_encontrado", title: "No encontrado", status: 404 }), true);
                    vinculos.set(
                        id,
                        lista.filter((x) => x !== v),
                    );
                    await json(route, v);
                    return true;
                }
                const entrada = (cuerpo ?? {}) as Schema<"EntradaCrearVinculoSSO">;
                if (!conectores.some((c) => c.id === entrada.conector_id && c.sistema === "argosisp" && c.estado === "activo")) {
                    return (await problema(route, { type: "urn:iris:problema:conector_invalido", title: "Conector inválido", status: 422 }), true);
                }
                if ((vinculos.get(id) ?? []).length > 0) {
                    return (await problema(route, { type: "urn:iris:problema:vinculo_existente", title: "Vínculo existente", status: 409 }), true);
                }
                if ([...vinculos.values()].flat().some((x) => x.usuarioExterno === entrada.id_externo)) {
                    return (await problema(route, { type: "urn:iris:problema:cuenta_externa_vinculada", title: "Cuenta externa vinculada", status: 409 }), true);
                }
                const v: Vinculo = { id: siguienteVinculo++, conectorId: entrada.conector_id, sistema: "argosisp", usuarioExterno: entrada.id_externo, creadoEn: new Date().toISOString() };
                vinculos.set(id, [v]);
                await json(route, v, 201);
                return true;
            }

            if (ruta === "/api/v1/integraciones/conectores" && metodo === "GET") {
                if (!puede("integraciones.ver")) return (await prohibido(), true);
                await json(route, { data: conectores, nextCursor: null } satisfies Schema<"PaginaConector">);
                return true;
            }
            const cambio = /^\/api\/v1\/integraciones\/conectores\/(\d+)\/(activar|desactivar)$/.exec(ruta);
            if (cambio && metodo === "POST") {
                if (!puede(cambio[2] === "activar" ? "integraciones.activar" : "integraciones.desactivar")) return (await prohibido(), true);
                if (sso) return (await pedirReautenticacion(), true);
                const c = conectores.find((x) => x.id === Number(cambio[1]));
                const { version } = (cuerpo ?? {}) as Schema<"EntradaCambioConector">;
                if (!c) return (await problema(route, { type: "urn:iris:problema:no_encontrado", title: "No encontrado", status: 404 }), true);
                if (c.version !== version) return (await problema(route, { type: "urn:iris:problema:version_obsoleta", title: "Versión obsoleta", status: 409 }), true);
                if (cambio[2] === "activar") {
                    for (const otro of conectores) {
                        if (otro.sistema === c.sistema && otro.estado === "activo") {
                            otro.estado = "apagado";
                            otro.version++;
                        }
                    }
                    c.estado = "activo";
                    c.activadoEn = new Date().toISOString();
                } else {
                    c.estado = "apagado";
                }
                c.version++;
                await json(route, c);
                return true;
            }
            if (ruta === "/api/v1/integraciones/argosisp/autorizaciones" && metodo === "POST") {
                if (!puede("integraciones.conectar")) return (await prohibido(), true);
                if (sso) return (await pedirReautenticacion(), true);
                const { panelUrl } = (cuerpo ?? {}) as Schema<"EntradaAutorizar">;
                let panel = "";
                try {
                    panel = new URL(panelUrl).origin;
                } catch {
                    return (await problema(route, { type: "urn:iris:problema:validacion", title: "Datos inválidos", status: 422 }), true);
                }
                const state = `st${api.autorizaciones.size + 1}`.padEnd(43, "s");
                api.autorizaciones.set(state, { panelUrl: panel, usada: false });
                const q = new URLSearchParams({ iris: "http://127.0.0.1", state, code_challenge: "c".repeat(43), code_challenge_method: "S256" });
                await json(
                    route,
                    { url: `${panel}/integraciones/iris/autorizar?${q.toString()}`, panelUrl: panel, expiraEn: new Date(Date.now() + 600_000).toISOString() } satisfies Schema<"AutorizacionIniciada">,
                    201,
                );
                return true;
            }
            if (ruta === "/api/v1/integraciones/argosisp/autorizaciones/completar" && metodo === "POST") {
                if (!puede("integraciones.conectar")) return (await prohibido(), true);
                if (sso) return (await pedirReautenticacion(), true);
                const entrada = (cuerpo ?? {}) as Schema<"EntradaCompletarAutorizacion">;
                api.completados.push({ cuerpo: entrada, idempotencia: idempotencia });
                if (api.enProceso.delete(entrada.state)) {
                    return (await problema(route, { type: "urn:iris:problema:idempotencia_en_proceso", title: "En proceso", status: 409 }), true);
                }
                const aut = api.autorizaciones.get(entrada.state);
                const no = (codigo: string, title: string) => problema(route, { type: `urn:iris:problema:${codigo}`, title, status: 422 });
                if (!aut || aut.usada) return (await no("state_invalido", "Autorización inválida"), true);
                aut.usada = true;
                if (entrada.error) return (await no("rechazado", "Conexión rechazada"), true);
                if (!entrada.api || !ORIGENES_PERMITIDOS.includes(entrada.api)) return (await no("origen_no_permitido", "Origen no permitido"), true);
                if (entrada.codigo === "codigo-malo") return (await no("codigo_invalido", "Código inválido"), true);
                const nuevo: Conector = {
                    ...(CONECTORES_INICIALES()[1] as Conector),
                    id: siguienteConector++,
                    baseUrl: entrada.api,
                    apiUrl: entrada.api,
                    panelUrl: aut.panelUrl,
                    urlPublica: aut.panelUrl,
                    origen: "autorizacion",
                    alcanceGlobal: false,
                    estado: "apagado",
                    creadoEn: new Date().toISOString(),
                    version: 1,
                };
                conectores.push(nuevo);
                await json(
                    route,
                    {
                        conector: nuevo,
                        empresas: [
                            { id: 10, nombre: "Fibra Norte", nit: "1234567" },
                            { id: 11, nombre: null, nit: null },
                        ],
                    } satisfies Schema<"AutorizacionCompletada">,
                    201,
                );
                return true;
            }
            const conexion = /^\/api\/v1\/integraciones\/(argosisp|horus)\/(conexion\/previsualizar|conectar)$/.exec(ruta);
            if (conexion && metodo === "POST") {
                if (!puede("integraciones.conectar")) return (await prohibido(), true);
                const sistema = conexion[1] as Conector["sistema"];
                const { enlace } = (cuerpo ?? {}) as Schema<"EntradaConectar">;
                let origen = "";
                try {
                    origen = new URL(enlace).origin;
                } catch {
                    return (await problema(route, { type: "urn:iris:problema:validacion", title: "Datos inválidos", status: 422, detail: "Enlace inválido" }), true);
                }
                const activo = conectores.find((c) => c.sistema === sistema && c.estado === "activo");
                const permitido = ORIGENES_PERMITIDOS.includes(origen) || activo?.baseUrl === origen;
                if (conexion[2] === "conexion/previsualizar") {
                    await json(route, {
                        sistema,
                        origen,
                        permitido,
                        conectorExistente: activo?.baseUrl === origen,
                        motivo: permitido ? null : "origen_no_permitido",
                    } satisfies Schema<"Previsualizacion">);
                    return true;
                }
                if (sso) return (await pedirReautenticacion(), true);
                if (!permitido) return (await problema(route, { type: "urn:iris:problema:origen_no_permitido", title: "Origen no permitido", status: 422 }), true);
                const nuevo: Conector = {
                    ...(CONECTORES_INICIALES()[1] as Conector),
                    id: siguienteConector++,
                    sistema,
                    baseUrl: origen,
                    apiUrl: origen,
                    panelUrl: null,
                    urlPublica: origen,
                    estado: "apagado",
                    origen: "enlace_local",
                    creadoEn: new Date().toISOString(),
                    version: 1,
                };
                conectores.push(nuevo);
                await json(route, nuevo, 201);
                return true;
            }
            return false;
        },
    };
    return api;
}
