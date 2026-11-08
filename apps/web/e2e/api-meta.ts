import type { Schema } from "@iris/contratos";
import type { Route } from "@playwright/test";
import type { ApiNumeros } from "./api-numeros";

/*
 * Conexión con Meta, números por QR y préstamos para las pruebas del panel
 * (contratos/conversaciones.md §15), tipado con `@iris/contratos`. Usa los números de
 * `api-numeros.ts` (y la bandeja, para «Pasar a otra empresa»). Nunca llama a Meta: el `code` se
 * comprueba y se descarta, y el resultado del QR lo elige la prueba.
 */

type Canal = Schema<"Canal">;
type Numero = Schema<"NumeroConversaciones">;
type Conversacion = Schema<"ConversacionCliente">;
type ConexionMeta = Schema<"ConexionMeta">;
type Prestamo = Schema<"Prestamo">;
type Problema = Schema<"Problema">;
type Permiso = Schema<"Permiso">;

export type ResultadoQr = "ok" | "codigo_vencido" | "app_no_aprobada" | "numero_no_elegible" | "error_meta" | "base_url_requerida";

export type OpcionesMeta = {
    /** Modo de la conexión con Meta al empezar (por defecto `propia`, lista). */
    conexion?: ConexionMeta["modo"];
    /** Qué responde `POST /canales/qr` (por defecto `ok`). */
    resultadoQr?: ResultadoQr;
    /** Agrega números por QR: «Ventas tienda» (compartido), «Técnicos» (11 días sin teléfono) y «Norte cobros» (desconectado). */
    qr?: boolean;
    /** «Avisos» y «Soporte» compartidos con Cable Norte. */
    prestamos?: boolean;
    /** La empresa activa (Cable Norte, id 2) usa «Avisos de Red Sur», prestado por Red Sur. */
    prestado?: boolean;
    /** Conversaciones cuyo contacto tiene otra abierta en otra empresa del número compartido (§15.3). */
    enOtraEmpresa?: number[];
};

export type ApiMeta = {
    conexion: () => ConexionMeta;
    /** Cuerpos recibidos (el `code` incluido, para comprobar que se mandó). */
    cuerpos: { ruta: string; cuerpo: Record<string, unknown>; idempotencia: string | undefined }[];
    prestamos: Map<number, Prestamo[]>;
    resultadoQr: { valor: ResultadoQr };
    manejar: (x: { route: Route; metodo: string; ruta: string; cuerpo: unknown; idempotencia: string | undefined; puede: (p: Permiso) => boolean; empresaId: number | null }) => Promise<boolean>;
};

const P = (codigo: string, status: number, title: string, extra: Partial<Problema> = {}): Problema => ({ type: `urn:iris:problema:${codigo}`, title, status, ...extra });
const problema = (route: Route, p: Problema) => route.fulfill({ status: p.status, contentType: "application/problem+json", body: JSON.stringify(p) });
const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
const haceDias = (d: number) => new Date(Date.now() - d * 86_400_000).toISOString();
const haceMin = (m: number) => new Date(Date.now() - m * 60_000).toISOString();

export const EMPRESAS_META = [
    { id: 1, nombre: "Red Sur", colas: [{ id: 2, nombre: "Soporte" }] },
    { id: 2, nombre: "Cable Norte", colas: [{ id: 21, nombre: "Soporte Norte" }, { id: 22, nombre: "Caja Norte" }] },
    { id: 3, nombre: "Fibra Occidente", colas: [{ id: 31, nombre: "Soporte Occidente" }] },
] as const;

const FUERA = { responder: false, mensaje: "", guardia: { activa: false, usuarioId: null, etiquetaIds: [], fallaMasiva: false }, asistente: { activo: false } } satisfies Numero["fueraDeHorario"];

export function apiMeta(numeros: ApiNumeros["interno"], conversaciones: Conversacion[], opciones: OpcionesMeta = {}): ApiMeta {
    const cuerpos: ApiMeta["cuerpos"] = [];
    const resultadoQr = { valor: opciones.resultadoQr ?? "ok" };
    const prestamos = new Map<number, Prestamo[]>();
    const codigosUsados = new Set<string>();
    let siguiente = 40;
    const modo = opciones.conexion ?? "propia";
    let conexion: ConexionMeta = {
        modo,
        gestionada: modo === "saas",
        appId: modo === "propia" ? "1298475561023344" : modo === "saas" ? "1100220033004400" : null,
        configId: modo === "propia" ? "874120336615092" : modo === "saas" ? "990011223344" : null,
        graphVersion: "v23.0",
        graphVersionPropia: null,
        appSecretGuardadoEn: modo === "propia" ? "2026-11-03T20:20:00Z" : null,
        servicioUrl: "https://cloud.iris.kns.gt",
        claveEnlaceGuardadaEn: modo === "compartida" ? "2026-11-05T15:00:00Z" : null,
        webhookUrl: "https://iris.redsur.com.gt/api/v1/mensajeria/meta/webhook",
        webhookUrlCompleta: true,
        verifyTokenGuardadoEn: modo === "propia" ? "2026-11-03T20:20:00Z" : null,
        webhookVerificadoEn: modo === "propia" ? "2026-11-03T20:20:00Z" : null,
        ultimoWebhookEn: modo === "propia" || modo === "saas" ? haceMin(1) : null,
        camposWebhook: ["messages", "smb_message_echoes", "history", "smb_app_state_sync", "account_update"],
        estado: modo === "propia" ? "lista" : modo === "compartida" ? "sin_servicio_central" : modo === "saas" ? "gestionada" : "sin_configurar",
        qrDisponible: modo === "propia" || modo === "saas",
        motivoQr: modo === "compartida" ? "servicio_central_no_disponible" : modo === "sin_configurar" ? "sin_configurar" : null,
        ultimaPruebaEn: modo === "propia" ? haceMin(30) : null,
        comprobaciones:
            modo === "propia"
                ? [
                      { clave: "app", estado: "ok", detalle: "Meta aceptó el app ID y el app secret: app «Red Sur Iris»." },
                      { clave: "configuracion", estado: "sin_verificar", detalle: "Iris revisó el formato; Meta no publica una lectura de la configuración." },
                      { clave: "webhook", estado: "ok", detalle: "Verificado por Meta." },
                      { clave: "tech_provider", estado: "sin_verificar", detalle: "Si la app no es Tech Provider, el QR falla al final." },
                  ]
                : [],
        version: modo === "sin_configurar" ? 0 : 3,
        actualizadoEn: "2026-11-03T20:20:00Z",
    };

    // --- Números de §15 ------------------------------------------------------------------------
    const base = (id: number, nombre: string, numero: string, usos: string[], colaId: number | null): Numero => ({
        id,
        nombre,
        numero,
        usos,
        avisosEmpresa: false,
        colaId,
        horarioId: null,
        fueraDeHorario: FUERA,
        activo: true,
        version: 1,
        iniciar: true,
        bot: { activo: false, atiende: "bot", saludo: "", menu: [], minutosSinConfirmacion: 30, minutosSinDisponibles: 5 },
    });
    const sinc = (conectado: string): Schema<"SincronizacionCanal"> => ({
        plazo: new Date(Date.parse(conectado) + 86_400_000).toISOString(),
        contactos: { estado: "sin_pedir", pedidoEn: null, total: 0, asociados: 0, progreso: 0, fase: 0, mensajes: 0, detalle: null },
        mensajes: { estado: "sin_pedir", pedidoEn: null, total: 0, asociados: 0, progreso: 0, fase: 0, mensajes: 0, detalle: null },
    });
    const qr = (conectadoEn: string) => ({ tipoConexion: "qr" as const, coexistencia: true, conectadoEn, appSecretGuardadoEn: null, sincronizacion: sinc(conectadoEn), ultimoEstado: "conectado" as const, webhookVerificado: true, webhookVerificadoEn: conectadoEn });
    if (opciones.qr) {
        numeros.agregar(base(10, "Ventas tienda", "+50255123344", ["ventas"], 4), { ...qr(haceDias(3)), ultimoEcoEn: haceMin(20), compartidoCon: 1 });
        numeros.agregar(base(11, "Técnicos", "+50255307788", ["soporte"], 2), { ...qr(haceDias(30)), ultimoEcoEn: haceDias(11) });
        numeros.agregar(base(12, "Norte cobros", "+50277651090", ["cobros"], 3), { ...qr(haceDias(40)), ultimoEcoEn: haceDias(15), desconectadoEn: haceDias(1), desconectadoMotivo: "primary_inactivity" });
    }
    const prestamo = (canalId: number, e: (typeof EMPRESAS_META)[number], usos: string[], colaId: number | null, abiertas: number, avisos: number): Prestamo => ({
        canalId,
        empresaId: e.id,
        empresaNombre: e.nombre,
        usos,
        colaId,
        colaNombre: e.colas.find((c) => c.id === colaId)?.nombre ?? null,
        avisosEmpresa: false,
        estado: "activo",
        prestadoEn: "2026-11-02T15:00:00Z",
        revocadoEn: null,
        motivoRevocacion: null,
        conversacionesAbiertas: abiertas,
        avisosProgramados: avisos,
        version: 1,
    });
    if (opciones.prestamos) {
        const cn = EMPRESAS_META[1];
        prestamos.set(2, [prestamo(2, cn, ["avisos", "soporte"], 21, 4, 12)]);
        prestamos.set(3, [prestamo(3, cn, ["soporte"], 21, 0, 0)]);
    }
    if (opciones.qr) prestamos.set(10, [prestamo(10, EMPRESAS_META[1], ["ventas"], 22, 0, 0)]);
    const actualizarCompartidos = () => {
        for (const n of numeros.todos()) {
            const e = numeros.extra(n.id);
            if (e) e.compartidoCon = (prestamos.get(n.id) ?? []).filter((p) => p.estado === "activo").length;
        }
    };
    actualizarCompartidos();
    if (opciones.prestado) {
        // Vista de Cable Norte (activa): «Avisos de Red Sur» prestado, y la entrada de la dueña
        // con el mismo id (con empresas.todas el servidor devuelve las dos; el panel filtra).
        const n = { ...base(9, "Avisos de Red Sur", "+50222000005", ["avisos", "soporte"], 21), avisosEmpresa: false };
        const vista: Canal = {
            ...numeros.vista(n),
            empresaId: 2,
            phoneNumberId: "",
            wabaId: "",
            webhookUrl: "",
            plantillas: {},
            tokenGuardadoEn: null,
            appSecretGuardadoEn: null,
            verifyTokenGuardadoEn: null,
            conexion: "conectado",
            prestadoPor: { empresaId: 1, nombre: "Red Sur" },
            // §15.3: a quien usa un número prestado, compartidoCon le llega en 0.
            compartidoCon: 0,
            prestamo: { usos: ["avisos", "soporte"], colaId: 21, avisosEmpresa: false, version: 1, prestadoEn: "2026-11-02T15:00:00Z" },
        };
        numeros.listaExtra.push(vista, { ...numeros.vista(n), empresaId: 1, conexion: "conectado", compartidoCon: 1 });
    }

    function buscarCanal(id: number): Canal | undefined {
        const extra = numeros.listaExtra.find((c) => c.id === id && c.prestadoPor);
        if (extra) return extra;
        const n = numeros.todos().find((c) => c.id === id);
        return n ? numeros.vista(n) : undefined;
    }

    async function manejar(x: { route: Route; metodo: string; ruta: string; cuerpo: unknown; idempotencia: string | undefined; puede: (p: Permiso) => boolean; empresaId: number | null }): Promise<boolean> {
        const { route, metodo, ruta, idempotencia, puede } = x;
        const cuerpo = (x.cuerpo ?? {}) as Record<string, unknown>;
        const prohibido = async () => (await problema(route, P("permiso_denegado", 403, "Sin permiso")), true);
        const noExiste = async () => (await problema(route, P("no_encontrado", 404, "No existe")), true);
        const M = "/api/v1/mensajeria";

        // Vista prestada de un número (la empresa activa lo usa por préstamo).
        const uno = /^\/api\/v1\/mensajeria\/canales\/(\d+)$/.exec(ruta);
        if (uno && metodo === "GET") {
            const c = numeros.listaExtra.find((x) => x.id === Number(uno[1]) && x.prestadoPor);
            if (c) return (await json(route, c), true);
            return false;
        }

        // --- Conexión con Meta ---
        if (ruta === `${M}/conexion-meta/registro` && metodo === "GET") {
            if (!puede("mensajeria.configurar")) return prohibido();
            const r: Schema<"RegistroQR"> = {
                disponible: conexion.qrDisponible,
                motivo: conexion.motivoQr,
                modo: conexion.modo,
                appId: conexion.qrDisponible ? conexion.appId : null,
                configId: conexion.qrDisponible ? conexion.configId : null,
                graphVersion: conexion.graphVersion,
                featureType: "whatsapp_business_app_onboarding",
                sessionInfoVersion: "3",
            };
            return (await json(route, r), true);
        }
        if (ruta.startsWith(`${M}/conexion-meta`)) {
            if (!puede("mensajeria.conexion_meta")) return prohibido();
            if (metodo === "POST") cuerpos.push({ ruta, cuerpo, idempotencia });
            if (ruta === `${M}/conexion-meta` && metodo === "GET") return (await json(route, conexion), true);
            if (ruta === `${M}/conexion-meta/probar` && metodo === "POST") {
                if (conexion.modo === "compartida") {
                    conexion = { ...conexion, estado: "sin_servicio_central" };
                    return (await json(route, conexion), true);
                }
                if (!conexion.appId) return (await problema(route, P("conexion_meta_sin_configurar", 422, "Falta configurar la conexión con Meta")), true);
                conexion = {
                    ...conexion,
                    estado: "lista",
                    ultimaPruebaEn: new Date().toISOString(),
                    comprobaciones: [
                        { clave: "app", estado: "ok", detalle: "Meta aceptó el app ID y el app secret: app «Red Sur Iris»." },
                        { clave: "configuracion", estado: "sin_verificar", detalle: "Iris revisó el formato; Meta no publica una lectura de la configuración." },
                        { clave: "webhook", estado: conexion.webhookVerificadoEn ? "ok" : "advertencia", detalle: conexion.webhookVerificadoEn ? "Verificado por Meta." : "Meta aún no verificó el webhook de la app." },
                        { clave: "tech_provider", estado: "sin_verificar", detalle: "Si la app no es Tech Provider, el QR falla al final." },
                    ],
                };
                return (await json(route, conexion), true);
            }
            if (ruta === `${M}/conexion-meta/actualizar` && metodo === "POST") {
                if (conexion.gestionada) return (await problema(route, P("conexion_gestionada", 422, "La conexión la administra Iris Cloud")), true);
                const e = cuerpo as Partial<Schema<"EntradaConexionMeta">>;
                if (e.version !== conexion.version) return (await problema(route, P("conflicto_version", 409, "Cambió")), true);
                const ahora = new Date().toISOString();
                const primera = !conexion.verifyTokenGuardadoEn;
                const generar = e.generarVerifyToken === true || (e.modo === "propia" && primera);
                const verify = generar ? `vt_${crypto.randomUUID().replaceAll("-", "")}` : undefined;
                if (e.modo === "propia") {
                    if (!e.appSecret && !conexion.appSecretGuardadoEn) return (await problema(route, P("validacion", 422, "Falta el app secret", { errors: [{ field: "body.appSecret", message: "Obligatorio la primera vez" }] } as Partial<Problema>)), true);
                    conexion = {
                        ...conexion,
                        modo: "propia",
                        appId: e.appId ?? conexion.appId,
                        configId: e.configId ?? conexion.configId,
                        graphVersionPropia: e.graphVersion ?? null,
                        graphVersion: e.graphVersion ?? "v23.0",
                        appSecretGuardadoEn: e.appSecret ? ahora : conexion.appSecretGuardadoEn,
                        verifyTokenGuardadoEn: verify ? ahora : conexion.verifyTokenGuardadoEn,
                        webhookVerificadoEn: verify ? null : conexion.webhookVerificadoEn,
                        estado: "sin_probar",
                        qrDisponible: true,
                        motivoQr: null,
                        comprobaciones: e.appSecret || e.appId !== conexion.appId ? [] : conexion.comprobaciones,
                    };
                } else {
                    if (!e.claveEnlace && !conexion.claveEnlaceGuardadaEn) return (await problema(route, P("validacion", 422, "Falta la clave", { errors: [{ field: "body.claveEnlace", message: "Obligatoria la primera vez" }] } as Partial<Problema>)), true);
                    conexion = {
                        ...conexion,
                        modo: "compartida",
                        servicioUrl: e.servicioUrl ?? "https://cloud.iris.kns.gt",
                        claveEnlaceGuardadaEn: e.claveEnlace ? ahora : conexion.claveEnlaceGuardadaEn,
                        estado: "sin_servicio_central",
                        qrDisponible: false,
                        motivoQr: "servicio_central_no_disponible",
                        comprobaciones: [],
                    };
                }
                conexion = { ...conexion, version: conexion.version + 1, actualizadoEn: ahora };
                return (await json(route, { ...conexion, ...(verify ? { verifyTokenGenerado: verify } : {}) }), true);
            }
            return false;
        }

        // --- Número por QR ---
        if (ruta === `${M}/canales/qr` && metodo === "POST") {
            if (!puede("mensajeria.configurar")) return prohibido();
            cuerpos.push({ ruta, cuerpo, idempotencia });
            const e = cuerpo as Partial<Schema<"EntradaCanalQR">>;
            if (!e.code || !/^[0-9]{1,30}$/.test(e.wabaId ?? "") || !/^[0-9]{1,30}$/.test(e.phoneNumberId ?? "") || !e.nombre) return (await problema(route, P("validacion", 422, "Datos inválidos")), true);
            if (conexion.modo === "compartida") return (await problema(route, P("servicio_central_no_disponible", 422, "Servicio central no disponible")), true);
            if (!conexion.qrDisponible) return (await problema(route, P("conexion_meta_sin_configurar", 422, "Falta la conexión con Meta")), true);
            if (codigosUsados.has(e.code)) return (await problema(route, P("codigo_vencido", 422, "Código vencido")), true);
            codigosUsados.add(e.code);
            const r = resultadoQr.valor;
            if (r === "error_meta") return (await problema(route, P("error_meta", 502, "Meta no respondió", { detail: "Código 2 · OAuthException · fbtrace Abc123" })), true);
            if (r !== "ok") return (await problema(route, P(r, 422, "Meta rechazó el registro")), true);
            const id = siguiente++;
            const ahora = new Date().toISOString();
            numeros.agregar(base(id, e.nombre, "+50255129900", e.usos ?? ["soporte"], e.colaId ?? null), { ...qr(ahora), phoneNumberId: e.phoneNumberId ?? "", wabaId: e.wabaId ?? "", ultimoEcoEn: null });
            return (await json(route, buscarCanal(id), 201), true);
        }

        const sinc = /^\/api\/v1\/mensajeria\/canales\/(\d+)\/sincronizar$/.exec(ruta);
        if (sinc && metodo === "POST") {
            if (!puede("mensajeria.configurar")) return prohibido();
            cuerpos.push({ ruta, cuerpo, idempotencia });
            const e = numeros.extra(Number(sinc[1]));
            if (!e?.sincronizacion) return (await problema(route, P("no_es_coexistencia", 422, "No es un número QR")), true);
            const ahora = new Date().toISOString();
            const s = e.sincronizacion;
            if (cuerpo.contactos) s.contactos = { ...s.contactos, estado: "listo", pedidoEn: ahora, total: 312, asociados: 184 };
            if (cuerpo.mensajes) s.mensajes = { ...s.mensajes, estado: "recibiendo", pedidoEn: ahora, progreso: 62, fase: 1, mensajes: 5210 };
            return (await json(route, buscarCanal(Number(sinc[1]))), true);
        }

        // --- Préstamos ---
        const pr = /^\/api\/v1\/mensajeria\/canales\/(\d+)\/(prestamos(?:\/opciones|\/(\d+)\/revocar)?|prestamo\/configurar)$/.exec(ruta);
        if (pr) {
            const canalId = Number(pr[1]);
            if (!puede("canales.configurar")) return prohibido();
            if (metodo === "POST") cuerpos.push({ ruta, cuerpo, idempotencia });
            const lista = prestamos.get(canalId) ?? [];
            if (pr[2] === "prestamo/configurar") {
                const c = numeros.listaExtra.find((x) => x.id === canalId && x.prestamo);
                if (!c?.prestamo) return noExiste();
                if (cuerpo.version !== c.prestamo.version) return (await problema(route, P("conflicto_version", 409, "Cambió")), true);
                if (cuerpo.avisosEmpresa && !c.prestamo.usos.includes("avisos")) return (await problema(route, P("uso_no_permitido", 422, "El préstamo no permite avisos")), true);
                c.prestamo = { ...c.prestamo, avisosEmpresa: Boolean(cuerpo.avisosEmpresa), version: c.prestamo.version + 1 };
                c.avisosEmpresa = Boolean(cuerpo.avisosEmpresa);
                return (await json(route, c), true);
            }
            if (!numeros.todos().some((n) => n.id === canalId)) return noExiste();
            if (pr[2] === "prestamos/opciones" && metodo === "GET") {
                return (await json(route, { empresas: EMPRESAS_META.filter((e) => e.id !== 1).map((e) => ({ id: e.id, nombre: e.nombre, colas: [...e.colas] })) } satisfies Schema<"OpcionesPrestamo">), true);
            }
            if (pr[2] === "prestamos" && metodo === "GET") return (await json(route, { data: lista, nextCursor: null } satisfies Schema<"ListaPrestamos">), true);
            if (pr[2] === "prestamos" && metodo === "POST") {
                const e = cuerpo as Partial<Schema<"EntradaPrestamoHTTPBody">>;
                const empresa = EMPRESAS_META.find((x) => x.id === e.empresaId && x.id !== 1);
                if (!empresa) return (await problema(route, P("empresa_no_valida", 422, "Esa empresa no puede usar el número")), true);
                if (!e.usos?.length) return (await problema(route, P("validacion", 422, "Elige al menos un uso", { errors: [{ field: "body.usos", message: "Elige al menos un uso" }] } as Partial<Problema>)), true);
                if (e.colaId != null && !empresa.colas.some((c) => c.id === e.colaId)) return (await problema(route, P("validacion", 422, "Cola inválida", { errors: [{ field: "body.colaId", message: "La cola no es de esa empresa" }] } as Partial<Problema>)), true);
                const existente = lista.find((p) => p.empresaId === empresa.id);
                if (existente && existente.estado === "activo" && e.version !== existente.version) return (await problema(route, P("conflicto_version", 409, "Cambió")), true);
                const nuevo: Prestamo = {
                    ...(existente ?? prestamo(canalId, empresa, [], null, 0, 0)),
                    usos: e.usos,
                    colaId: e.colaId ?? null,
                    colaNombre: empresa.colas.find((c) => c.id === e.colaId)?.nombre ?? null,
                    avisosEmpresa: Boolean(existente?.avisosEmpresa && e.usos.includes("avisos")),
                    estado: "activo",
                    revocadoEn: null,
                    motivoRevocacion: null,
                    prestadoEn: existente?.estado === "activo" ? existente.prestadoEn : new Date().toISOString(),
                    version: (existente?.version ?? 0) + 1,
                };
                prestamos.set(canalId, [...lista.filter((p) => p.empresaId !== empresa.id), nuevo]);
                actualizarCompartidos();
                return (await json(route, nuevo), true);
            }
            if (pr[3] && metodo === "POST") {
                const p = lista.find((x) => x.empresaId === Number(pr[3]));
                if (!p) return noExiste();
                if (cuerpo.version !== p.version) return (await problema(route, P("conflicto_version", 409, "Cambió")), true);
                const motivo = typeof cuerpo.motivo === "string" ? cuerpo.motivo.trim() : "";
                if (motivo.length < 5) return (await problema(route, P("validacion", 422, "Motivo", { errors: [{ field: "body.motivo", message: "Al menos 5 letras" }] } as Partial<Problema>)), true);
                Object.assign(p, { estado: "revocado", revocadoEn: new Date().toISOString(), motivoRevocacion: motivo, avisosEmpresa: false, version: p.version + 1 });
                actualizarCompartidos();
                return (await json(route, p), true);
            }
            return false;
        }

        // --- Otra empresa del número ya atiende al contacto (409 al reabrir, y al responder resuelta o con plantilla) ---
        const oe = /^\/api\/v1\/mensajeria\/conversaciones\/(\d+)\/(reabrir|mensajes)$/.exec(ruta);
        if (oe && metodo === "POST" && (opciones.enOtraEmpresa ?? []).includes(Number(oe[1]))) {
            const c = conversaciones.find((x) => x.id === Number(oe[1]));
            const plantilla = "plantilla" in cuerpo;
            if (c && (oe[2] === "reabrir" || c.estado === "resuelta" || plantilla)) {
                cuerpos.push({ ruta, cuerpo, idempotencia });
                return (await problema(route, P("conversacion_en_otra_empresa", 409, "Otra empresa atiende a este contacto", { detail: "Otra empresa que usa este número tiene una conversación abierta con este teléfono." })), true);
            }
        }

        // --- Pasar a otra empresa ---
        const ed = /^\/api\/v1\/mensajeria\/conversaciones\/(\d+)\/empresas-destino$/.exec(ruta);
        if (ed && metodo === "GET") {
            if (!puede("conversaciones.transferir")) return prohibido();
            const c = conversaciones.find((x) => x.id === Number(ed[1]));
            if (!c) return noExiste();
            // Solo la dueña (la empresa de la conversación es la del número y no está en solo lectura).
            const data = c.soloLectura ? [] : (prestamos.get(c.canal.id) ?? []).filter((p) => p.estado === "activo" && p.usos.some((u) => u !== "avisos")).map((p) => ({ empresaId: p.empresaId, nombre: p.empresaNombre }));
            return (await json(route, { data, nextCursor: null }), true);
        }
        const pe = /^\/api\/v1\/mensajeria\/conversaciones\/(\d+)\/pasar-empresa$/.exec(ruta);
        if (pe && metodo === "POST") {
            if (!puede("conversaciones.transferir")) return prohibido();
            cuerpos.push({ ruta, cuerpo, idempotencia });
            const c = conversaciones.find((x) => x.id === Number(pe[1]));
            if (!c) return noExiste();
            if (cuerpo.version !== c.version) return (await problema(route, P("conflicto_version", 409, "Cambió")), true);
            const ok = (prestamos.get(c.canal.id) ?? []).some((p) => p.estado === "activo" && p.empresaId === cuerpo.empresaId && p.usos.some((u) => u !== "avisos"));
            if (!ok) return (await problema(route, P("empresa_no_valida", 422, "Esa empresa no tiene este número para conversar")), true);
            Object.assign(c, { soloLectura: true, version: c.version + 1, empresaId: cuerpo.empresaId });
            return (await json(route, c), true);
        }
        return false;
    }

    return { conexion: () => conexion, cuerpos, prestamos, resultadoQr, manejar };
}
