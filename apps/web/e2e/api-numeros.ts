import type { Schema } from "@iris/contratos";
import type { Route } from "@playwright/test";

/*
 * Números de WhatsApp para las pruebas del panel: alta, credenciales y conexión
 * (contratos/conversaciones.md §1.1), tipado con `@iris/contratos`. Comparte los números con la
 * bandeja (`api-conversaciones.ts`): «Configurar» cambia el mismo objeto que lista `GET /canales`.
 * Las credenciales nunca vuelven en una respuesta; solo sus `*GuardadoEn`.
 */

type Canal = Schema<"Canal">;
type Numero = Schema<"NumeroConversaciones">;
type PruebaCanal = Schema<"PruebaCanal">;
type Problema = Schema<"Problema">;
type Permiso = Schema<"Permiso">;
type EstadoPrueba = PruebaCanal["estado"];

/** Lo que responde Meta en la prueba de cada número (lo cambia la prueba). */
export type ResultadoMeta = { estado: EstadoPrueba; webhookVerificado: boolean };

export type OpcionesNumeros = {
    /** La empresa aún no tiene números (los de la bandeja no salen en `GET /canales`). */
    vacio?: boolean;
    /** Agrega «Ventas (anterior)», desactivado. */
    conInactivo?: boolean;
    /** Lo que responde Meta para los números que se creen en la prueba (por defecto: webhook sin verificar). */
    resultadoNuevo?: ResultadoMeta;
};

export type ApiNumeros = {
    /** Resultado de Meta por id de número. */
    resultados: Map<number, ResultadoMeta>;
    /** Cuerpos recibidos (sin tocar) por ruta, para comprobar que lo vacío no se manda. */
    cuerpos: { ruta: string; cuerpo: Record<string, unknown>; idempotencia: string | undefined }[];
    /** Credenciales guardadas (para comprobar que lo vacío no cambió nada). */
    secretos: Map<number, { token: string; appSecret: string; verifyToken: string }>;
    /** El verify token se generó con esta prueba (el número creado). */
    ultimoCreado: () => Canal | null;
    /** Para `api-meta.ts` (§15): números, sus datos de Meta y entradas extra de la lista. */
    interno: {
        todos: () => Numero[];
        extra: (id: number) => Extra | undefined;
        vista: (n: Numero) => Canal;
        agregar: (n: Numero, extra: Partial<Extra>) => void;
        /** Entradas que `GET /canales` agrega tal cual (p. ej. la vista prestada de un número). */
        listaExtra: Canal[];
    };
    manejar: (x: { route: Route; metodo: string; ruta: string; cuerpo: unknown; idempotencia: string | undefined; puede: (p: Permiso) => boolean; empresaId: number | null }) => Promise<boolean>;
};

const AHORA = () => new Date().toISOString();
const ORIGEN = "https://iris.redsur.com.gt";
const P = (codigo: string, status: number, title: string, extra: Partial<Problema> = {}): Problema => ({ type: `urn:iris:problema:${codigo}`, title, status, ...extra });
const problema = (route: Route, p: Problema) => route.fulfill({ status: p.status, contentType: "application/problem+json", body: JSON.stringify(p) });
const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });

export type Extra = Omit<Canal, keyof Numero | "bot" | "fueraDeHorario" | "verifyTokenGenerado"> & { webhookVerificado: boolean };

const META_ID: Record<number, string> = { 2: "104455667788990", 3: "104455667788991", 4: "104455667788992", 5: "104455667788993", 6: "104455667788994" };

function conexionDe(n: Pick<Numero, "activo">, e: Pick<Extra, "ultimoEstado" | "webhookVerificado" | "desconectadoEn">): Canal["conexion"] {
    if (!n.activo) return "desactivado";
    if (e.desconectadoEn) return "desconectado";
    if (!e.ultimoEstado) return "sin_probar";
    if (e.ultimoEstado !== "conectado") return e.ultimoEstado;
    return e.webhookVerificado ? "conectado" : "webhook_sin_verificar";
}

export function apiNumeros(canales: Numero[], opciones: OpcionesNumeros = {}): ApiNumeros {
    const guardado = "2026-11-03T20:20:00Z";
    const resultados = new Map<number, ResultadoMeta>([
        [2, { estado: "conectado", webhookVerificado: true }],
        [3, { estado: "conectado", webhookVerificado: true }],
        [4, { estado: "token_invalido", webhookVerificado: true }],
        [5, { estado: "conectado", webhookVerificado: false }],
        [6, { estado: "conectado", webhookVerificado: true }],
    ]);
    const extras = new Map<number, Extra>();
    const secretos = new Map<number, { token: string; appSecret: string; verifyToken: string }>();
    const cuerpos: ApiNumeros["cuerpos"] = [];
    const propios: Numero[] = [];
    let siguiente = 7;
    let creado: Canal | null = null;
    const listaExtra: Canal[] = [];

    function extraBase(id: number, r: ResultadoMeta | undefined): Extra {
        return {
            empresaId: null,
            tipo: "whatsapp_cloud",
            canal: "whatsapp",
            phoneNumberId: META_ID[id] ?? String(109876543210000 + id),
            wabaId: "102345678901234",
            plantillas: {},
            pausadoHasta: null,
            webhookRuta: `/api/v1/mensajeria/canales/${id}/webhook`,
            webhookUrl: `${ORIGEN}/api/v1/mensajeria/canales/${id}/webhook`,
            webhookUrlCompleta: true,
            creadoEn: "2026-10-01T15:00:00Z",
            estado: "activo",
            conexion: "sin_probar",
            ultimoEstado: r?.estado ?? null,
            ultimoDetalle: r && r.estado !== "conectado" ? "Meta rechazó el token (401)." : null,
            ultimaPruebaEn: r ? "2026-11-08T14:00:00Z" : null,
            webhookVerificadoEn: r?.webhookVerificado ? guardado : null,
            ultimoMensajeEn: r?.webhookVerificado ? new Date(Date.now() - 5 * 60_000).toISOString() : null,
            tokenGuardadoEn: guardado,
            appSecretGuardadoEn: guardado,
            verifyTokenGuardadoEn: guardado,
            webhookVerificado: r?.webhookVerificado ?? false,
            // §15: por defecto, números por datos de la API, sin préstamos.
            tipoConexion: "api",
            coexistencia: false,
            ultimoEcoEn: null,
            conectadoEn: null,
            desconectadoEn: null,
            desconectadoMotivo: null,
            compartidoCon: 0,
        };
    }
    for (const n of canales) {
        extras.set(n.id, extraBase(n.id, resultados.get(n.id)));
        secretos.set(n.id, { token: `EAAG-guardado-${n.id}-xxxxxxxxxxxx`, appSecret: `secreto-guardado-${n.id}`, verifyToken: `vt_guardado_${n.id}` });
    }
    if (opciones.conInactivo) {
        const n: Numero = { id: 6, nombre: "Ventas (anterior)", numero: "+50222000019", usos: ["ventas"], avisosEmpresa: false, colaId: 4, horarioId: null, fueraDeHorario: { responder: false, mensaje: "", guardia: { activa: false, usuarioId: null, etiquetaIds: [], fallaMasiva: false }, asistente: { activo: false } }, activo: false, version: 3, iniciar: false, bot: canales[0]?.bot ?? { activo: false, atiende: "bot", saludo: "", menu: [], minutosSinConfirmacion: 30, minutosSinDisponibles: 5 } };
        propios.push(n);
        extras.set(6, extraBase(6, resultados.get(6)));
        secretos.set(6, { token: "EAAG-guardado-6-xxxxxxxxxxxx", appSecret: "secreto-guardado-6", verifyToken: "vt_guardado_6" });
    }

    const todos = () => [...(opciones.vacio ? [] : canales), ...propios];
    function vista(n: Numero): Canal {
        const e = extras.get(n.id) ?? extraBase(n.id, undefined);
        const resto: Partial<Extra> = { ...e };
        delete resto.webhookVerificado;
        return { ...n, ...(resto as Omit<Extra, "webhookVerificado">), estado: n.activo ? "activo" : "inactivo", conexion: conexionDe(n, e) };
    }
    function generarVerify(): string {
        return `vt_${crypto.randomUUID().replaceAll("-", "")}`;
    }

    async function manejar(x: { route: Route; metodo: string; ruta: string; cuerpo: unknown; idempotencia: string | undefined; puede: (p: Permiso) => boolean; empresaId: number | null }): Promise<boolean> {
        const { route, metodo, ruta, idempotencia } = x;
        const m = /^\/api\/v1\/mensajeria\/canales(?:\/(\d+)(?:\/(actualizar|probar|activar|desactivar))?)?$/.exec(ruta);
        if (!m) return false;
        const cuerpo = (x.cuerpo ?? {}) as Record<string, unknown>;
        if (!x.puede("mensajeria.configurar")) return (await problema(route, P("permiso_denegado", 403, "Sin permiso")), true);
        if (metodo === "POST") cuerpos.push({ ruta, cuerpo, idempotencia });
        const id = m[1] ? Number(m[1]) : null;
        const accion = m[2];

        if (id === null) {
            if (metodo === "GET") return (await json(route, { data: [...todos().map(vista), ...listaExtra], nextCursor: null } satisfies Schema<"ListaCanales">), true);
            if (metodo !== "POST") return false;
            if (!idempotencia) return (await problema(route, P("validacion", 422, "Falta Idempotency-Key")), true);
            const e = cuerpo as Partial<Schema<"EntradaCrearCanal">>;
            const errores: { field: string; message: string }[] = [];
            if (!/^\+[1-9][0-9]{7,14}$/.test(e.numero ?? "")) errores.push({ field: "body.numero", message: "Número inválido (E.164)" });
            if (!/^[0-9]{1,30}$/.test(e.phoneNumberId ?? "")) errores.push({ field: "body.phoneNumberId", message: "Phone number ID inválido" });
            if ((e.token ?? "").length < 20) errores.push({ field: "body.token", message: "Token muy corto" });
            if ((e.appSecret ?? "").length < 16) errores.push({ field: "body.appSecret", message: "App secret muy corto" });
            if (errores.length) return (await problema(route, P("validacion", 422, "Datos inválidos", { errors: errores } as Partial<Problema>)), true);
            if (todos().some((n) => n.activo && extras.get(n.id)?.phoneNumberId === e.phoneNumberId)) {
                return (await problema(route, P("canal_existente", 409, "Ya existe un número con ese Phone number ID")), true);
            }
            const nuevoId = siguiente++;
            const n: Numero = {
                id: nuevoId,
                nombre: e.nombre ?? "",
                numero: e.numero ?? "",
                usos: e.usos ?? ["avisos", "soporte"],
                avisosEmpresa: e.avisosEmpresa ?? false,
                colaId: e.colaId ?? null,
                horarioId: null,
                fueraDeHorario: { responder: false, mensaje: "", guardia: { activa: false, usuarioId: null, etiquetaIds: [], fallaMasiva: false }, asistente: { activo: false } },
                activo: true,
                version: 1,
                iniciar: true,
                bot: canales[0]?.bot ?? { activo: false, atiende: "bot", saludo: "", menu: [], minutosSinConfirmacion: 30, minutosSinDisponibles: 5 },
            };
            if (n.avisosEmpresa) for (const otro of todos()) otro.avisosEmpresa = false;
            propios.push(n);
            const ahora = AHORA();
            extras.set(nuevoId, {
                ...extraBase(nuevoId, undefined),
                phoneNumberId: e.phoneNumberId ?? "",
                wabaId: e.wabaId ?? "",
                creadoEn: ahora,
                tokenGuardadoEn: ahora,
                appSecretGuardadoEn: ahora,
                verifyTokenGuardadoEn: ahora,
            });
            const verifyToken = e.verifyToken || generarVerify();
            secretos.set(nuevoId, { token: e.token ?? "", appSecret: e.appSecret ?? "", verifyToken });
            resultados.set(nuevoId, opciones.resultadoNuevo ?? { estado: "conectado", webhookVerificado: false });
            creado = { ...vista(n), ...(e.verifyToken ? {} : { verifyTokenGenerado: verifyToken }) };
            return (await json(route, creado, 201), true);
        }

        const n = todos().find((c) => c.id === id);
        const e = n ? extras.get(n.id) : undefined;
        if (!n || !e) return (await problema(route, P("no_encontrado", 404, "No existe")), true);
        if (metodo === "GET" && !accion) return (await json(route, vista(n)), true);
        if (metodo !== "POST") return false;

        if (accion === "probar") {
            const r = resultados.get(n.id) ?? { estado: "error_meta", webhookVerificado: false };
            const ahora = AHORA();
            e.ultimoEstado = r.estado;
            e.ultimaPruebaEn = ahora;
            e.webhookVerificado = r.webhookVerificado;
            e.webhookVerificadoEn = r.webhookVerificado ? (e.webhookVerificadoEn ?? new Date(Date.now() - 8 * 60_000).toISOString()) : null;
            const detalles: Record<EstadoPrueba, string | null> = {
                conectado: null,
                token_invalido: "Meta rechazó el token (401).",
                numero_no_coincide: "Meta tiene otro número para ese Phone number ID.",
                sin_permiso: "Meta respondió 403.",
                error_meta: "Meta no respondió a tiempo.",
            };
            e.ultimoDetalle = detalles[r.estado];
            const respuesta: PruebaCanal = {
                canalId: n.id,
                estado: r.estado,
                detalle: detalles[r.estado],
                probadoEn: ahora,
                numeroMeta: r.estado === "numero_no_coincide" ? "+50222009999" : r.estado === "conectado" ? n.numero : null,
                nombreVerificado: r.estado === "conectado" ? "Red Sur Soporte" : null,
                calidad: r.estado === "conectado" ? "alta" : null,
                webhookVerificado: r.webhookVerificado,
                webhookVerificadoEn: e.webhookVerificadoEn,
                ultimoMensajeEn: e.ultimoMensajeEn,
                conexion: ((c) => (c === "desconectado" ? "sin_probar" : c))(conexionDe(n, e)),
            };
            return (await json(route, respuesta), true);
        }
        if (accion === "activar" || accion === "desactivar") {
            if (accion === "activar" && todos().some((o) => o.id !== n.id && o.activo && extras.get(o.id)?.phoneNumberId === e.phoneNumberId)) {
                return (await problema(route, P("canal_existente", 409, "Ya existe un número activo con ese Phone number ID")), true);
            }
            n.activo = accion === "activar";
            n.version++;
            return (await json(route, vista(n)), true);
        }
        // actualizar
        const a = cuerpo as Partial<Schema<"EntradaActualizarCanal">>;
        if (a.version !== n.version) return (await problema(route, P("conflicto_version", 409, "Alguien más cambió el número")), true);
        if (a.generarVerifyToken && a.verifyToken) return (await problema(route, P("validacion", 422, "No se puede generar y mandar el verify token a la vez")), true);
        const errores: { field: string; message: string }[] = [];
        if (a.token && a.token.length < 20) errores.push({ field: "body.token", message: "Token muy corto" });
        if (a.appSecret && a.appSecret.length < 16) errores.push({ field: "body.appSecret", message: "App secret muy corto" });
        if (errores.length) return (await problema(route, P("validacion", 422, "Datos inválidos", { errors: errores } as Partial<Problema>)), true);
        const s = secretos.get(n.id) ?? { token: "", appSecret: "", verifyToken: "" };
        const ahora = AHORA();
        const cambiaPrueba = Boolean(a.token) || (a.phoneNumberId !== undefined && a.phoneNumberId !== e.phoneNumberId) || (a.numero !== undefined && a.numero !== n.numero);
        if (a.token) {
            s.token = a.token;
            e.tokenGuardadoEn = ahora;
        }
        if (a.appSecret) {
            s.appSecret = a.appSecret;
            e.appSecretGuardadoEn = ahora;
        }
        let generado: string | undefined;
        if (a.generarVerifyToken || a.verifyToken) {
            generado = a.generarVerifyToken ? generarVerify() : undefined;
            s.verifyToken = generado ?? a.verifyToken ?? s.verifyToken;
            e.verifyTokenGuardadoEn = ahora;
            e.webhookVerificadoEn = null;
            e.webhookVerificado = false;
        }
        secretos.set(n.id, s);
        n.nombre = a.nombre ?? n.nombre;
        n.numero = a.numero ?? n.numero;
        e.phoneNumberId = a.phoneNumberId ?? e.phoneNumberId;
        e.wabaId = a.wabaId ?? e.wabaId;
        if (cambiaPrueba) {
            e.ultimoEstado = null;
            e.ultimoDetalle = null;
            e.ultimaPruebaEn = null;
            // Lo que se pegó ahora es lo bueno: Meta lo acepta en la próxima prueba.
            if (a.token) resultados.set(n.id, { estado: "conectado", webhookVerificado: e.webhookVerificado });
        }
        n.version++;
        return (await json(route, { ...vista(n), ...(generado ? { verifyTokenGenerado: generado } : {}) }), true);
    }

    const interno: ApiNumeros["interno"] = {
        todos,
        extra: (id) => extras.get(id),
        vista,
        agregar: (n, extra) => {
            propios.push(n);
            extras.set(n.id, { ...extraBase(n.id, resultados.get(n.id)), ...extra });
        },
        listaExtra,
    };
    return { resultados, cuerpos, secretos, ultimoCreado: () => creado, manejar, interno };
}
