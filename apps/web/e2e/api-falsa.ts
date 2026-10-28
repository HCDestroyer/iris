import type { Schema } from "@iris/contratos";
import type { BrowserContext, Page, Route } from "@playwright/test";
import { apiAgenda, type ApiAgenda } from "./api-agenda";
import { apiCuenta, CONTRASENA_DEFECTO, type ApiCuenta } from "./api-cuenta";
import { apiOrdenes } from "./api-ordenes";
import { apiRutas, type ApiRutas } from "./api-rutas";
import { buzonesIniciales, resultadoPrueba } from "./datos-buzones";
import type { EscenarioOrdenes } from "./datos-ordenes";
import { aplicarAccion, CATEGORIAS, CLIENTES, escenarioTickets, ficha } from "./datos-tickets";

/*
 * API falsa para Playwright, con fixtures tipados con packages/contratos. Reproduce lo que
 * importa al panel: cookies iris_sesion / iris_csrf, CSRF por doble envío en métodos que cambian
 * estado (403 si falta), MFA, 401/429 y paginación por cursor.
 */

type Problema = Schema<"Problema">;
type Permiso = Schema<"Permiso">;

export const CSRF = "csrf-e2e";
export const CONTRASENA = CONTRASENA_DEFECTO;
export const CODIGO_OK = "123456";
export const CODIGO_BLOQUEA = "999999";

const ahora = "2026-10-28T15:00:00Z";

export function usuario(datos: Partial<Schema<"Usuario">> & Pick<Schema<"Usuario">, "id" | "nombre" | "correo">): Schema<"Usuario"> {
    return {
        tipo: "persona",
        activo: true,
        mfaActivo: false,
        roles: [{ rol: "lectura" }],
        empresas: [],
        version: 1,
        creadoEn: ahora,
        actualizadoEn: ahora,
        ...datos,
    };
}

export const ADMIN = usuario({ id: 1, nombre: "Ana López", correo: "ana@iris.test", roles: [{ rol: "admin" }] });
export const CON_MFA = usuario({ id: 2, nombre: "Mario Pérez", correo: "mario@iris.test", mfaActivo: true, roles: [{ rol: "supervisor" }] });
export const LECTOR = usuario({ id: 3, nombre: "Lucía Ramírez", correo: "lucia@iris.test", roles: [{ rol: "tecnico", zonaId: 4 }] });
export const SOPORTE = usuario({ id: 6, nombre: "Sofía Morales", correo: "sofia@iris.test", roles: [{ rol: "soporte" }] });
/** Rol lectura: ve tickets, pero no notas internas ni acciones. */
export const CONSULTA = usuario({ id: 7, nombre: "Carlos Ruiz", correo: "carlos@iris.test", roles: [{ rol: "lectura" }] });
/** Supervisor de zona: tiene `buzones.*`, pero con alcance de zona (las escrituras dan 403). */
export const SUPERVISOR_ZONA = usuario({ id: 8, nombre: "Zoe Hernández", correo: "zoe@iris.test", roles: [{ rol: "supervisor", zonaId: 2 }] });
/** Despacho: órdenes y pendientes en Argos, pero sin `integraciones.reintentar`. */
export const DESPACHO = usuario({ id: 9, nombre: "Diego Castro", correo: "diego@iris.test", roles: [{ rol: "despacho" }] });
/** Solo puede asignar órdenes (sin programar ni reprogramar): prueba de §10 en el despacho. */
export const ASIGNADOR = usuario({ id: 10, nombre: "Andrés Coj", correo: "andres@iris.test", roles: [{ rol: "despacho" }] });
/** Entra con una contraseña temporal (tras un reinicio de MFA): solo puede cambiarla. */
export const TEMPORAL = usuario({ id: 11, nombre: "Tomás Ajú", correo: "tomas@iris.test", cambioContrasenaRequerido: true, roles: [{ rol: "supervisor" }] });
/** Admin con MFA: prueba que «Reiniciar MFA» no se ofrece sobre uno mismo. */
export const ELENA = usuario({ id: 12, nombre: "Elena Toj", correo: "elena@iris.test", mfaActivo: true, roles: [{ rol: "admin" }] });
export const BLOQUEADO = "bloqueado@iris.test";

const PERMISOS_BUZONES: Permiso[] = ["buzones.activar", "buzones.crear", "buzones.desactivar", "buzones.editar", "buzones.ver"];

const PERMISOS_TICKETS: Permiso[] = [
    "tickets.asignar",
    "tickets.cerrar",
    "tickets.crear",
    "tickets.escalar",
    "tickets.fusionar",
    "tickets.nota_interna",
    "tickets.poner_en_espera",
    "tickets.reabrir",
    "tickets.reanudar",
    "tickets.resolver",
    "tickets.responder",
    "tickets.ver",
    "tickets.ver_notas",
];

const PERMISOS_ORDENES: Permiso[] = [
    "ordenes.asignar",
    "ordenes.cancelar",
    "ordenes.cerrar",
    "ordenes.crear",
    "ordenes.fallar",
    "ordenes.iniciar_camino",
    "ordenes.llegar",
    "ordenes.programar",
    "ordenes.reabrir",
    "ordenes.registrar",
    "ordenes.reprogramar",
    "ordenes.ver",
];

export const PERMISOS_ADMIN: Permiso[] = [
    ...PERMISOS_BUZONES,
    "adjuntos.ver",
    "agenda.ver",
    "campo.ver_posiciones",
    "integraciones.reintentar",
    "rutas.proponer",
    "rutas.publicar",
    "rutas.ver",
    "integraciones.ver_pendientes",
    ...PERMISOS_ORDENES,
    "clientes.estado_cuenta",
    "clientes.ver",
    "empresas.adoptar",
    "empresas.todas",
    "permisos.ver",
    "roles.ver",
    ...PERMISOS_TICKETS,
    "tokens.crear",
    "tokens.revocar",
    "tokens.ver",
    "usuarios.crear",
    "usuarios.desactivar",
    "usuarios.editar",
    "usuarios.reiniciar_mfa",
    "usuarios.ver",
];

const permisosPorCorreo: Record<string, Permiso[]> = {
    [ADMIN.correo]: PERMISOS_ADMIN,
    [ELENA.correo]: PERMISOS_ADMIN,
    [TEMPORAL.correo]: ["tickets.ver", "usuarios.ver"],
    [CON_MFA.correo]: ["usuarios.ver"],
    [LECTOR.correo]: [],
    [SOPORTE.correo]: ["clientes.ver", "clientes.estado_cuenta", "usuarios.ver", ...PERMISOS_TICKETS],
    [CONSULTA.correo]: ["agenda.ver", "clientes.ver", "rutas.ver", "tickets.ver", "usuarios.ver"],
    [SUPERVISOR_ZONA.correo]: [...PERMISOS_BUZONES, "tickets.ver"],
    [ASIGNADOR.correo]: ["agenda.ver", "ordenes.asignar", "ordenes.ver"],
    [DESPACHO.correo]: ["adjuntos.ver", "agenda.ver", "campo.ver_posiciones", "clientes.ver", "rutas.proponer", "rutas.publicar", "rutas.ver", "integraciones.ver_pendientes", ...PERMISOS_ORDENES, "tickets.ver", "usuarios.ver"],
};
/** Usuarios cuyos permisos `buzones.*` tienen alcance de zona (no global). */
const alcanceDeZona = new Set([SUPERVISOR_ZONA.correo]);
const usuarios = [ADMIN, CON_MFA, LECTOR, SOPORTE, CONSULTA, SUPERVISOR_ZONA, DESPACHO, ASIGNADOR, TEMPORAL, ELENA];

export const PAGINA_1: Schema<"PaginaUsuario"> = {
    data: [ADMIN, CON_MFA, LECTOR, SOPORTE],
    nextCursor: "cursor-2",
};
export const PAGINA_2: Schema<"PaginaUsuario"> = {
    data: [
        usuario({ id: 4, nombre: "Integración Argos", correo: "argos@integraciones.iris", tipo: "integracion", roles: [{ rol: "integracion" }] }),
        usuario({ id: 5, nombre: "Pedro Gómez", correo: "pedro@iris.test", activo: false, roles: [] }),
        ELENA,
    ],
    nextCursor: null,
};

export const EMPRESAS: Schema<"EmpresaResumen">[] = [
    { id: 10, nombre: "Cable Norte" },
    { id: 20, nombre: "Fibra Sur" },
];

export type OpcionesApi = {
    /** Empresas registradas en que opera el usuario. Vacío (por defecto): modo sin conector. */
    empresas?: Schema<"EmpresaResumen">[];
    /** Hay empresas registradas pero el usuario no tiene ninguna (403 `sin_empresa`). */
    sinEmpresa?: boolean;
    /** `GET /yo` trae `hayDatosSinEmpresa` (solo si el usuario tiene `empresas.adoptar`). */
    hayDatosSinEmpresa?: boolean;
    /** Conector con Argos apagado: `/clientes*` responde 503 `conector_apagado`. */
    conectorApagado?: boolean;
    /** Respuesta de `GET /integraciones/enlaces` (por defecto, sin conectores activos). */
    enlaces?: Schema<"EnlacesExternos">;
    /** Buzones iniciales (por defecto, `buzonesIniciales`). */
    buzones?: Schema<"Buzon">[];
    /**
     * Escenario del despacho: coordenadas, la cita de Jorge con ventana (OT-000040), Rosa ausente,
     * los tres técnicos asignables y las reglas de la agenda al programar (422).
     */
    despacho?: boolean;
};

export type Peticion = { metodo: string; ruta: string; query: URLSearchParams; empresa: string | undefined; idempotencia: string | undefined; cuerpo: unknown };

export type ApiFalsa = {
    /** Peticiones que cambiaron estado: método, ruta y X-CSRF-Token recibido. */
    mutaciones: { metodo: string; ruta: string; csrf: string | undefined }[];
    /** Todas las peticiones a la API (con `X-Iris-Empresa` e `Idempotency-Key`). */
    peticiones: Peticion[];
    /** Hace fallar `GET /yo` o `GET /usuarios` las próximas `n` veces con 500. */
    fallar: (ruta: "/api/v1/yo" | "/api/v1/usuarios", n: number) => void;
    /** Responde `problema` en las próximas `n` peticiones cuya ruta empiece con `prefijo`. */
    responderProblema: (prefijo: string, p: Problema, n: number) => void;
    /** Otra persona cambió el buzón: sube su versión (la próxima acción con la vieja da 409). */
    cambiarVersionBuzon: (buzonId: number) => void;
    /** Otra persona cambió el ticket: sube su versión (la próxima acción con la vieja da 409). */
    cambiarVersion: (ticketId: number) => void;
    /** Órdenes, adjuntos y pendientes del escenario (se pueden cambiar en la prueba). */
    ordenes: EscenarioOrdenes;
    /** Otra persona cambió la orden: sube su versión (la próxima acción con la vieja da 409). */
    cambiarVersionOrden: (ordenId: number) => void;
    /** El worker tocó el pendiente: sube su versión (el próximo reintento con la vieja da 409). */
    cambiarVersionPendiente: (pendienteId: number) => void;
    /** Agenda y posiciones del escenario (con `despacho`, además las reglas de la agenda). */
    agenda: ApiAgenda["escenario"];
    /** Rutas del día (detalle completo, se pueden cambiar en la prueba). */
    rutas: ApiRutas["rutas"];
    /** Cuenta y reinicio de MFA: subir la versión de un usuario, sacarlo del alcance, cuerpos recibidos. */
    cuenta: Pick<ApiCuenta, "cambiarVersion" | "sacarDelAlcance" | "reinicios">;
};

const OPERATIVAS = [
    "/api/v1/tickets",
    "/api/v1/categorias",
    "/api/v1/slas",
    "/api/v1/clientes",
    "/api/v1/buzones",
    "/api/v1/ordenes",
    "/api/v1/adjuntos",
    "/api/v1/integraciones/pendientes",
    "/api/v1/agenda",
    "/api/v1/tecnicos",
    "/api/v1/campo/posiciones",
    "/api/v1/rutas",
];

function problema(route: Route, p: Problema, cabeceras: Record<string, string> = {}) {
    return route.fulfill({
        status: p.status,
        contentType: "application/problem+json",
        headers: cabeceras,
        body: JSON.stringify(p),
    });
}

const cookiesSesion = (valor: string) =>
    [`iris_sesion=${valor}; Path=/; HttpOnly; SameSite=Lax`, `iris_csrf=${CSRF}; Path=/; SameSite=Lax`].join("\n");

/** Con sesión ya iniciada (cookies como las deja la API). */
export async function conSesion(context: BrowserContext, baseURL: string, correo: string) {
    await context.addCookies([
        { name: "iris_sesion", value: `s:${correo}`, url: baseURL, httpOnly: true, sameSite: "Lax" },
        { name: "iris_csrf", value: CSRF, url: baseURL, sameSite: "Lax" },
    ]);
}

export async function simularApi(page: Page, opciones: OpcionesApi = {}): Promise<ApiFalsa> {
    const mutaciones: ApiFalsa["mutaciones"] = [];
    const peticiones: Peticion[] = [];
    const fallos = new Map<string, number>();
    const problemas: { prefijo: string; p: Problema; n: number }[] = [];
    const mfaPendiente = new Map<string, number>();
    const empresas = opciones.sinEmpresa ? [] : (opciones.empresas ?? []);
    const hayRegistradas = opciones.sinEmpresa === true || empresas.length > 0;
    let hayDatosSinEmpresa = opciones.hayDatosSinEmpresa ?? false;
    const esc = escenarioTickets(Date.now(), empresas.map((e) => e.id));
    let siguienteTicket = 1000;
    let buzones: Schema<"Buzon">[] | undefined = opciones.buzones;
    let siguienteBuzon = 100;
    // La agenda valida contra las órdenes: se crean juntas (solo con `despacho` se aplican sus reglas).
    const enlace: { agenda?: ApiAgenda } = {};
    const ordenes = apiOrdenes(
        empresas.map((e) => e.id),
        opciones.despacho ? { tecnicos: [3, 11, 12], validarCita: (o, e, accion) => enlace.agenda?.validarCita(o, e, accion) } : {},
    );
    const agenda = apiAgenda(Date.now(), ordenes.escenario, empresas[0]?.id ?? null, opciones.despacho === true);
    enlace.agenda = agenda;
    const rutas = apiRutas(Date.now(), ordenes.escenario, empresas[0]?.id ?? null, opciones.despacho === true);
    const listables = [...(PAGINA_1.data ?? []), ...(PAGINA_2.data ?? [])];
    const cuenta = apiCuenta(() => [...usuarios, ...listables.filter((u) => !usuarios.some((x) => x.id === u.id))]);

    await page.route("**/api/v1/**", async (route) => {
        const req = route.request();
        const url = new URL(req.url());
        const ruta = url.pathname;
        const metodo = req.method();
        const cabeceras = await req.allHeaders();
        const cookies = cabeceras.cookie ?? "";
        const sesion = /(?:^|;\s*)iris_sesion=([^;]+)/.exec(cookies)?.[1];
        const correoSesion = sesion?.startsWith("s:") ? decodeURIComponent(sesion.slice(2)) : undefined;
        const csrf = cabeceras["x-csrf-token"];

        if (metodo !== "GET") {
            mutaciones.push({ metodo, ruta, csrf });
        }
        let cuerpo: unknown = undefined;
        try {
            cuerpo = req.postDataJSON();
        } catch {
            cuerpo = undefined;
        }
        peticiones.push({ metodo, ruta, query: url.searchParams, empresa: cabeceras["x-iris-empresa"], idempotencia: cabeceras["idempotency-key"], cuerpo });
        const forzado = problemas.find((x) => x.n > 0 && ruta.startsWith(x.prefijo));
        if (forzado) {
            forzado.n--;
            return problema(route, forzado.p);
        }
        const pendientes = fallos.get(ruta) ?? 0;
        if (pendientes > 0) {
            fallos.set(ruta, pendientes - 1);
            return problema(route, { type: "urn:iris:problema:error_interno", title: "Error interno", status: 500 });
        }
        // CSRF por doble envío (salvo el inicio de sesión), como identidad/middleware.go.
        if (metodo !== "GET" && sesion && !(metodo === "POST" && ruta === "/api/v1/sesion") && csrf !== CSRF) {
            return problema(route, { type: "urn:iris:problema:csrf_invalido", title: "CSRF inválido", status: 403 });
        }

        if (metodo === "POST" && ruta === "/api/v1/sesion") {
            const entrada = req.postDataJSON() as Schema<"EntradaIniciarSesion">;
            if (entrada.correo === BLOQUEADO) {
                return problema(
                    route,
                    {
                        type: "urn:iris:problema:demasiados_intentos",
                        title: "Demasiados intentos",
                        status: 429,
                        detail: "Demasiados intentos fallidos con este correo; espera antes de volver a intentar",
                    },
                    { "Retry-After": "540" },
                );
            }
            const leido = usuarios.find((x) => x.correo === entrada.correo);
            const u = leido ? cuenta.ver(leido) : undefined;
            if (!u || !cuenta.contrasenaValida(u, entrada.contrasena)) {
                return problema(route, {
                    type: "urn:iris:problema:credenciales_invalidas",
                    title: "Credenciales inválidas",
                    status: 401,
                    detail: "Correo o contraseña incorrectos",
                });
            }
            const respuesta: Schema<"SesionIniciada"> = u.mfaActivo
                ? { csrfToken: CSRF, mfaRequerido: true, expiraEn: "2026-10-28T23:00:00Z" }
                : { csrfToken: CSRF, mfaRequerido: false, expiraEn: "2026-10-28T23:00:00Z", usuario: u };
            if (u.mfaActivo) {
                mfaPendiente.set(u.correo, 0);
            }
            return route.fulfill({
                status: 200,
                contentType: "application/json",
                headers: { "Set-Cookie": cookiesSesion(u.mfaActivo ? `p:${u.correo}` : `s:${u.correo}`) },
                body: JSON.stringify(respuesta),
            });
        }

        if (metodo === "POST" && ruta === "/api/v1/sesion/mfa") {
            const correo = sesion?.startsWith("p:") ? decodeURIComponent(sesion.slice(2)) : undefined;
            const fallidos = correo === undefined ? undefined : mfaPendiente.get(correo);
            if (correo === undefined || fallidos === undefined) {
                return problema(route, { type: "urn:iris:problema:no_autenticado", title: "No autenticado", status: 401 });
            }
            const { codigo } = req.postDataJSON() as Schema<"EntradaVerificarMFA">;
            if (codigo === CODIGO_BLOQUEA || fallidos >= 4) {
                mfaPendiente.delete(correo);
                return problema(route, {
                    type: "urn:iris:problema:mfa_bloqueado",
                    title: "Demasiados códigos inválidos",
                    status: 401,
                    detail: "La sesión se cerró; vuelve a iniciar sesión",
                });
            }
            if (codigo !== CODIGO_OK) {
                mfaPendiente.set(correo, fallidos + 1);
                return problema(route, {
                    type: "urn:iris:problema:codigo_invalido",
                    title: "Código inválido",
                    status: 401,
                    detail: "El código de verificación no es válido o ya se usó",
                });
            }
            mfaPendiente.delete(correo);
            const leido = usuarios.find((x) => x.correo === correo);
            const u = leido ? cuenta.ver(leido) : undefined;
            const respuesta: Schema<"SesionIniciada"> = { csrfToken: CSRF, mfaRequerido: false, expiraEn: "2026-10-28T23:00:00Z", ...(u ? { usuario: u } : {}) };
            return route.fulfill({
                status: 200,
                contentType: "application/json",
                headers: { "Set-Cookie": cookiesSesion(`s:${correo}`) },
                body: JSON.stringify(respuesta),
            });
        }

        const leido = usuarios.find((x) => x.correo === correoSesion);
        if (!leido) {
            return problema(route, { type: "urn:iris:problema:no_autenticado", title: "No autenticado", status: 401 });
        }
        const actual = cuenta.ver(leido);
        // Contraseña temporal: todo salvo /yo, cambiarla y salir responde 403 (identidad/middleware.go).
        if (await cuenta.cortar(route, actual, metodo, ruta)) {
            return;
        }
        const permisos = permisosPorCorreo[actual.correo] ?? [];

        // Empresa activa, como identidad.MiddlewareEmpresa (i1.md).
        const cabeceraEmpresa = cabeceras["x-iris-empresa"];
        let activa: Schema<"EmpresaResumen"> | undefined;
        if (cabeceraEmpresa !== undefined) {
            activa = empresas.find((e) => String(e.id) === cabeceraEmpresa);
            if (!activa) {
                return problema(route, { type: "urn:iris:problema:empresa_no_permitida", title: "Empresa no permitida", status: 403, detail: "No operas en esa empresa" });
            }
        } else if (empresas.length === 1) {
            activa = empresas[0];
        }
        const modo: Schema<"Yo">["modoEmpresa"] = !hayRegistradas ? "sin_conector" : empresas.length === 0 ? "sin_empresa" : activa ? "activa" : "por_elegir";
        if (OPERATIVAS.some((r) => ruta.startsWith(r))) {
            if (modo === "sin_empresa") {
                return problema(route, { type: "urn:iris:problema:sin_empresa", title: "Sin empresa", status: 403, detail: "No tienes empresas asignadas" });
            }
            if (modo === "por_elegir") {
                return problema(route, { type: "urn:iris:problema:empresa_requerida", title: "Elige una empresa", status: 422, detail: "Indica la empresa con X-Iris-Empresa" });
            }
        }
        const empresaId = activa?.id ?? null;
        const visibles = esc.tickets.filter((t) => t.empresaId === empresaId);
        const puede = (p: Permiso) => permisos.includes(p);
        const prohibido = () => problema(route, { type: "urn:iris:problema:prohibido", title: "Sin permiso", status: 403 });
        const noEncontrado = () => problema(route, { type: "urn:iris:problema:no_encontrado", title: "No encontrado", status: 404 });

        if (metodo === "DELETE" && ruta === "/api/v1/sesion") {
            return route.fulfill({
                status: 204,
                headers: {
                    "Set-Cookie": [
                        "iris_sesion=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax",
                        "iris_csrf=; Path=/; Max-Age=0; SameSite=Lax",
                    ].join("\n"),
                },
            });
        }
        if (metodo === "GET" && ruta === "/api/v1/yo") {
            const yo: Schema<"Yo"> = {
                actor: "usuario",
                usuario: actual,
                permisos,
                empresas,
                ...(activa ? { empresaActiva: activa } : {}),
                modoEmpresa: modo,
                todasLasEmpresas: puede("empresas.todas"),
                hayDatosSinEmpresa: hayDatosSinEmpresa && puede("empresas.adoptar"),
            };
            return route.fulfill({ json: yo });
        }
        if (metodo === "GET" && ruta === "/api/v1/usuarios") {
            if (!permisos.includes("usuarios.ver")) {
                return problema(route, { type: "urn:iris:problema:prohibido", title: "Sin permiso", status: 403 });
            }
            const visible = (u: Schema<"Usuario">) => !cuenta.fueraDeAlcance(u.id);
            const q = url.searchParams.get("q")?.toLowerCase();
            if (q) {
                const data = listables.map(cuenta.ver).filter((u) => visible(u) && (u.correo.toLowerCase().includes(q) || u.nombre.toLowerCase().includes(q)));
                return route.fulfill({ json: { data, nextCursor: null } satisfies Schema<"PaginaUsuario"> });
            }
            const pagina = url.searchParams.get("cursor") === PAGINA_1.nextCursor ? PAGINA_2 : PAGINA_1;
            return route.fulfill({ json: { ...pagina, data: (pagina.data ?? []).map(cuenta.ver).filter(visible) } satisfies Schema<"PaginaUsuario"> });
        }
        if (await cuenta.manejar({ route, metodo, ruta, actual, puede: (p) => permisos.includes(p), cuerpo, idempotencia: cabeceras["idempotency-key"] })) {
            return;
        }

        const adopcion = /^\/api\/v1\/empresas\/(\d+)\/adoptar-sin-empresa$/.exec(ruta);
        if (metodo === "POST" && adopcion) {
            if (!puede("empresas.adoptar")) return prohibido();
            hayDatosSinEmpresa = false;
            const datos: Schema<"DatosAdoptados"> = { empresaId: Number(adopcion[1]), filas: { "soporte.tickets": 12, "soporte.ticket_mensajes": 30, "soporte.ticket_eventos": 41 }, total: 83 };
            return route.fulfill({ json: datos });
        }

        if (metodo === "GET" && ruta === "/api/v1/categorias") {
            if (!puede("tickets.ver")) return prohibido();
            return route.fulfill({ json: { data: CATEGORIAS, nextCursor: null } satisfies Schema<"PaginaCategoria"> });
        }

        if (metodo === "GET" && ruta === "/api/v1/integraciones/enlaces") {
            if (!puede("clientes.ver")) return prohibido();
            return route.fulfill({ json: opciones.enlaces ?? ({} satisfies Schema<"EnlacesExternos">) });
        }

        if (ruta.startsWith("/api/v1/buzones")) {
            buzones ??= buzonesIniciales(empresaId);
            const lista = buzones;
            const m = /^\/api\/v1\/buzones(?:\/(\d+)(?:\/([a-z]+))?)?$/.exec(ruta);
            if (!m) return noEncontrado();
            const b = m[1] ? lista.find((x) => x.id === Number(m[1]) && x.empresaId === empresaId) : undefined;
            const sub = m[2];
            if (metodo === "GET") {
                if (!puede("buzones.ver")) return prohibido();
                if (!m[1]) return route.fulfill({ json: { data: lista.filter((x) => x.empresaId === empresaId), nextCursor: null } satisfies Schema<"PaginaBuzon"> });
                return b ? route.fulfill({ json: b }) : noEncontrado();
            }
            const permiso: Permiso = !m[1] ? "buzones.crear" : sub === "probar" ? "buzones.editar" : (`buzones.${sub ?? ""}` as Permiso);
            if (!puede(permiso)) return prohibido();
            if (alcanceDeZona.has(actual.correo)) {
                return problema(route, { type: "urn:iris:problema:prohibido", title: "Permiso insuficiente", status: 403, detail: `El permiso ${permiso} debe tener alcance global (sin zona ni cuadrilla)` });
            }
            if (!cabeceras["idempotency-key"]) {
                return problema(route, { type: "urn:iris:problema:validacion", title: "Falta Idempotency-Key", status: 422 });
            }
            const contrasenaRequerida = (campo: string) =>
                problema(route, {
                    type: "urn:iris:problema:validacion",
                    title: "Datos inválidos",
                    status: 422,
                    errors: [{ field: campo, message: "Escribe la contraseña (es nueva o cambió el servidor o el usuario)" }],
                });
            const ahoraIso = new Date().toISOString();
            if (!m[1]) {
                const e = cuerpo as Schema<"EntradaCrearBuzon">;
                if (!e.imap.contrasena) return contrasenaRequerida("imap.contrasena");
                const nuevo: Schema<"Buzon"> = {
                    id: siguienteBuzon++,
                    empresaId,
                    nombre: e.nombre,
                    correo: e.correo,
                    imap: { host: e.imap.host, puerto: e.imap.puerto, tls: e.imap.tls, usuario: e.imap.usuario, carpeta: e.imap.carpeta ?? "INBOX", carpetaProcesados: e.imap.carpetaProcesados ?? null },
                    ...(e.smtp ? { smtp: { host: e.smtp.host, puerto: e.smtp.puerto, tls: e.smtp.tls, usuario: e.smtp.usuario ?? null } } : {}),
                    categoriaId: e.categoriaId ?? null,
                    prioridad: e.prioridad ?? "media",
                    authservId: e.authservId || null,
                    activo: false,
                    ultimaLecturaEn: null,
                    ultimoError: null,
                    version: 1,
                    creadoEn: ahoraIso,
                    actualizadoEn: ahoraIso,
                };
                lista.push(nuevo);
                return route.fulfill({ status: 201, json: nuevo });
            }
            if (!b) return noEncontrado();
            if (sub === "probar") {
                return route.fulfill({ json: resultadoPrueba(b) });
            }
            const version = (cuerpo as { version?: number } | undefined)?.version;
            if (version !== b.version) {
                return problema(route, { type: "urn:iris:problema:conflicto_version", title: "Conflicto de versión", status: 409, detail: "El buzón cambió desde que lo leíste" });
            }
            if (sub === "editar") {
                const e = cuerpo as Schema<"EntradaEditarBuzon">;
                const cambiaImap = e.imap.host !== b.imap.host || e.imap.puerto !== b.imap.puerto || e.imap.tls !== b.imap.tls || e.imap.usuario !== b.imap.usuario;
                if (cambiaImap && !e.imap.contrasena) return contrasenaRequerida("imap.contrasena");
                Object.assign(b, {
                    nombre: e.nombre,
                    correo: e.correo,
                    imap: { host: e.imap.host, puerto: e.imap.puerto, tls: e.imap.tls, usuario: e.imap.usuario, carpeta: e.imap.carpeta ?? "INBOX", carpetaProcesados: e.imap.carpetaProcesados ?? null },
                    smtp: e.smtp ? { host: e.smtp.host, puerto: e.smtp.puerto, tls: e.smtp.tls, usuario: e.smtp.usuario ?? null } : undefined,
                    categoriaId: e.categoriaId ?? null,
                    prioridad: e.prioridad ?? "media",
                    authservId: e.authservId || null,
                } satisfies Partial<Schema<"Buzon">>);
            } else if (sub === "activar" || sub === "desactivar") {
                b.activo = sub === "activar";
            } else {
                return noEncontrado();
            }
            b.version++;
            b.actualizadoEn = ahoraIso;
            return route.fulfill({ json: b });
        }

        if (ruta.startsWith("/api/v1/clientes")) {
            if (!puede("clientes.ver")) return prohibido();
            if (opciones.conectorApagado) {
                return problema(route, { type: "urn:iris:problema:conector_apagado", title: "Conector con Argos apagado", status: 503, detail: "No hay un conector de Argos activo" }, { "Retry-After": "60" });
            }
            if (ruta === "/api/v1/clientes") {
                const q = (url.searchParams.get("q") ?? "").toLowerCase();
                const data = CLIENTES.filter((c) => c.nombre.toLowerCase().includes(q) || c.codigo.toLowerCase().includes(q));
                return route.fulfill({ json: { data, nextCursor: null, stale: false } satisfies Schema<"ResultadoBusqueda"> });
            }
            const id = Number(ruta.split("/").pop());
            const f = ficha(id, id === 502);
            return f ? route.fulfill({ json: f }) : noEncontrado();
        }

        if (await agenda.manejar({ route, metodo, ruta, query: url.searchParams, puede, empresaId })) {
            return;
        }
        if (await rutas.manejar({ route, metodo, ruta, query: url.searchParams, cuerpo, idempotencia: cabeceras["idempotency-key"], puede, empresaId })) {
            return;
        }

        if (
            await ordenes.manejar({
                route,
                metodo,
                ruta,
                query: url.searchParams,
                cuerpo,
                idempotencia: cabeceras["idempotency-key"],
                actual,
                puede,
                empresaId,
                tickets: visibles,
            })
        ) {
            return;
        }

        if (ruta === "/api/v1/tickets") {
            if (metodo === "GET") {
                if (!puede("tickets.ver")) return prohibido();
                const q = url.searchParams;
                // La API lee los arreglos separados por coma (explode=false): `estado=nuevo,abierto`.
                if (q.getAll("estado").length > 1 || q.getAll("prioridad").length > 1) {
                    return problema(route, { type: "urn:iris:problema:validacion", title: "Datos inválidos", status: 422, detail: "estado repetido" });
                }
                const estados = q.get("estado")?.split(",");
                const prioridades = q.get("prioridad")?.split(",");
                const texto = q.get("q")?.toLowerCase();
                let lista = visibles
                    .filter((t) => !estados || estados.includes(t.estado))
                    .filter((t) => !prioridades || prioridades.includes(t.prioridad))
                    .filter((t) => !q.get("categoriaId") || t.categoriaId === Number(q.get("categoriaId")))
                    .filter((t) => !q.get("asignadoA") || t.asignadoA === Number(q.get("asignadoA")))
                    .filter((t) => q.get("sinAsignar") !== "true" || t.asignadoA === null)
                    .filter((t) => !q.get("ticketPadreId") || t.ticketPadreId === Number(q.get("ticketPadreId")))
                    .filter((t) => !texto || t.numero.toLowerCase().includes(texto) || t.asunto.toLowerCase().includes(texto))
                    // El orden es cosa de la API (id desc); aquí ascendente para que los casos de SLA salgan en la primera página.
                    .sort((a, b) => a.id - b.id);
                const desde = Number(q.get("cursor")?.replace("c:", "") ?? 0);
                const limite = Number(q.get("limit") ?? 50);
                lista = lista.slice(desde, desde + limite + 1);
                const hayMas = lista.length > limite;
                const pagina: Schema<"PaginaTicket"> = { data: lista.slice(0, limite), nextCursor: hayMas ? `c:${desde + limite}` : null };
                return route.fulfill({ json: pagina });
            }
            if (metodo === "POST") {
                if (!puede("tickets.crear")) return prohibido();
                const entrada = cuerpo as Schema<"EntradaCrearTicket">;
                if (!entrada.asunto || entrada.asunto.length < 3) {
                    return problema(route, { type: "urn:iris:problema:validacion", title: "Datos inválidos", status: 422, errors: [{ field: "body.asunto", message: "mínimo 3 caracteres" }] });
                }
                const id = siguienteTicket++;
                const ahora = new Date().toISOString();
                const base = esc.tickets[4];
                if (!base) return noEncontrado();
                const nuevo: Schema<"Ticket"> = {
                    ...base,
                    id,
                    numero: `TK-${String(id).padStart(6, "0")}`,
                    empresaId,
                    asunto: entrada.asunto,
                    prioridad: entrada.prioridad ?? "media",
                    categoriaId: entrada.categoriaId ?? null,
                    clienteId: entrada.clienteId ?? null,
                    contacto: { nombre: entrada.contacto?.nombre ?? null, telefono: entrada.contacto?.telefono ?? null, correo: entrada.contacto?.correo ?? null, verificado: true },
                    estado: "nuevo",
                    asignadoA: null,
                    version: 1,
                    creadoEn: ahora,
                    actualizadoEn: ahora,
                };
                esc.tickets.push(nuevo);
                esc.eventos.push({ id: 5000 + id, ticketId: id, tipo: "creado", de: null, a: "nuevo", motivo: null, datos: {}, actorTipo: "usuario", actorId: actual.id, actorNombre: actual.nombre, creadoEn: ahora });
                if (entrada.descripcion) {
                    esc.mensajes.push({ id: 5000 + id, ticketId: id, publico: true, autorTipo: "usuario", autorId: actual.id, autorNombre: actual.nombre, canal: "panel", cuerpo: entrada.descripcion, adjuntos: null, creadoEn: ahora });
                }
                return route.fulfill({ status: 201, json: nuevo });
            }
        }

        const deTicket = /^\/api\/v1\/tickets\/(\d+)(?:\/([a-z-]+))?$/.exec(ruta);
        if (deTicket) {
            if (!puede("tickets.ver")) return prohibido();
            const t = visibles.find((x) => x.id === Number(deTicket[1]));
            if (!t) return noEncontrado();
            const sub = deTicket[2];
            if (metodo === "GET" && !sub) {
                return route.fulfill({ json: t });
            }
            if (metodo === "GET" && sub === "mensajes") {
                const soloPublicos = url.searchParams.get("soloPublicos") === "true" || !puede("tickets.ver_notas");
                const data = esc.mensajes.filter((m) => m.ticketId === t.id && (m.publico || !soloPublicos));
                return route.fulfill({ json: { data, nextCursor: null } satisfies Schema<"PaginaMensaje"> });
            }
            if (metodo === "GET" && sub === "eventos") {
                const data = esc.eventos.filter((e) => e.ticketId === t.id);
                return route.fulfill({ json: { data, nextCursor: null } satisfies Schema<"PaginaEventoTicket"> });
            }
            if (metodo === "POST" && sub) {
                const permiso = `tickets.${sub.replaceAll("-", "_")}` as Permiso;
                if (!puede(permiso)) return prohibido();
                if (!cabeceras["idempotency-key"]) {
                    return problema(route, { type: "urn:iris:problema:validacion", title: "Falta Idempotency-Key", status: 422 });
                }
                const r = aplicarAccion(esc, t, sub, (cuerpo ?? {}) as Record<string, unknown>, actual);
                return route.fulfill({ status: r.estado, contentType: r.estado >= 400 ? "application/problem+json" : "application/json", body: JSON.stringify(r.cuerpo) });
            }
        }
        return noEncontrado();
    });

    return {
        mutaciones,
        peticiones,
        responderProblema: (prefijo, p, n) => {
            problemas.push({ prefijo, p, n });
        },
        cambiarVersionBuzon: (buzonId) => {
            const b = buzones?.find((x) => x.id === buzonId);
            if (b) b.version++;
        },
        cambiarVersion: (ticketId) => {
            const t = esc.tickets.find((x) => x.id === ticketId);
            if (t) {
                t.version++;
                t.prioridad = "alta";
            }
        },
        fallar: (ruta, n) => {
            fallos.set(ruta, n);
        },
        ordenes: ordenes.escenario,
        agenda: agenda.escenario,
        rutas: rutas.rutas,
        cuenta: { cambiarVersion: cuenta.cambiarVersion, sacarDelAlcance: cuenta.sacarDelAlcance, reinicios: cuenta.reinicios },
        cambiarVersionOrden: (ordenId) => {
            const o = ordenes.escenario.ordenes.find((x) => x.id === ordenId);
            if (o) o.version++;
        },
        cambiarVersionPendiente: (pendienteId) => {
            const p = ordenes.escenario.pendientes.find((x) => x.id === pendienteId);
            if (p) p.version++;
        },
    };
}
