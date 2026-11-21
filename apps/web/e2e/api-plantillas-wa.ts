import type { Schema } from "@iris/contratos";
import type { Route } from "@playwright/test";

/*
 * Plantillas de WhatsApp en la API falsa (`.claude/integration/contratos/plantillas-whatsapp.md`): la
 * cuenta y su sincronización (202 y 429), la lista con conteos, los datos amigables, las 7 sugeridas,
 * crear, editar un borrador, enviar, corregir, usos, borrar y reanudar. Tipado con packages/contratos.
 */

type Problema = Schema<"Problema">;
type Permiso = Schema<"Permiso">;
type PlantillaWA = Schema<"PlantillaWA">;
type Detalle = Schema<"PlantillaWADetalle">;
type Definicion = Schema<"DefinicionPWA">;
type Cuenta = Schema<"CuentaPWA">;
type Historial = Schema<"HistorialPWA">;
type Sugerida = Schema<"SugeridaEnCuentaPWA">;
type Resumen = Schema<"ResumenSincronizacionPWA">;

export const PERMISOS_PLANTILLAS_WA: Permiso[] = [
    "plantillas_whatsapp.ver",
    "plantillas_whatsapp.sincronizar",
    "plantillas_whatsapp.crear",
    "plantillas_whatsapp.editar",
    "plantillas_whatsapp.borrar",
];
export const PERMISOS_PLANTILLAS_WA_SOPORTE: Permiso[] = ["plantillas_whatsapp.ver", "plantillas_whatsapp.sincronizar"];

export const WABA = "102938475610293";
export const AHORA_PWA = "2026-11-21T16:42:00Z";

export type OpcionesPlantillasWA = {
    /** La cuenta trae el error de Meta de la última vez (`meta_sin_permiso`, `meta_no_responde`…). */
    errorCuenta?: string;
    /** El botón «Sincronizar» responde 429 (se sincronizó hace menos de 2 minutos). */
    sincronizadaHacePoco?: boolean;
    /** Sin cuentas: la pantalla lleva a «Números de WhatsApp». */
    sinCuentas?: boolean;
};

const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
const problema = (route: Route, p: Problema, cabeceras: Record<string, string> = {}) =>
    route.fulfill({ status: p.status, contentType: "application/problem+json", headers: cabeceras, body: JSON.stringify(p) });
const P = (codigo: string, status: number, title: string, extra: Partial<Problema> = {}): Problema => ({ type: `urn:iris:problema:${codigo}`, title, status, ...extra });
/** Campos que el servidor manda en null aunque el contrato los tipe como objeto (punteros de Go). */
const NULO = null as never;

export const DATOS_PWA: Schema<"DatoPWA">[] = [
    { clave: "cliente_nombre", etiqueta: "Nombre del cliente", origen: "Cliente en Argos · nombre", ejemplo: "María López", codigos: [], grupo: "cliente" },
    { clave: "cliente_codigo", etiqueta: "Código de cliente", origen: "Cliente en Argos · código", ejemplo: "C-1042", codigos: [], grupo: "cliente" },
    { clave: "plan", etiqueta: "Plan", origen: "Servicio en Argos · plan", ejemplo: "Fibra 100 Mbps", codigos: [], grupo: "cliente" },
    { clave: "monto_con_moneda", etiqueta: "Monto", origen: "Factura pendiente · total con moneda", ejemplo: "Q 250.00", codigos: ["factura_por_vencer", "corte_por_falta_de_pago"], grupo: "cobro" },
    { clave: "fecha_corte", etiqueta: "Fecha de corte", origen: "Factura · fecha de corte (Guatemala)", ejemplo: "viernes 28 de noviembre", codigos: ["factura_por_vencer"], grupo: "cobro" },
    { clave: "empresa_nombre", etiqueta: "Nombre de tu empresa", origen: "Empresa en Argos", ejemplo: "Kingnet Solution", codigos: [], grupo: "empresa" },
];

const variable = (nombre: string, ejemplo: string) => ({ nombre, dato: nombre, ejemplo });

const DEF_CORTE: Definicion = {
    encabezado: { tipo: "texto", texto: "Servicio suspendido" },
    cuerpo: "Hola {{cliente_nombre}}, tu servicio fue suspendido por un saldo pendiente de {{moneda}}{{monto}}. Al pagar se reactiva el mismo día.",
    pie: "Kingnet Solution",
    botones: [{ tipo: "url", texto: "Pagar en línea", url: "https://pagos.kns.gt/pagar" }],
    variables: [variable("cliente_nombre", "María López"), { nombre: "moneda", ejemplo: "GTQ" }, { nombre: "monto", ejemplo: "250.00" }],
};
const DEF_RECORDATORIO: Definicion = {
    cuerpo: "Hola {{cliente_nombre}}, tu factura de {{monto_con_moneda}} vence el {{fecha_corte}}. Si ya pagaste, ignora este mensaje.",
    pie: "Kingnet Solution",
    botones: [{ tipo: "respuesta", texto: "Ya pagué" }],
    variables: [variable("cliente_nombre", "María"), variable("monto_con_moneda", "Q 250.00"), variable("fecha_corte", "viernes 28 de noviembre")],
};
const DEF_PROMO: Definicion = {
    cuerpo: "Hola {{1}}, sube a 300 Mbps este mes por el mismo precio.",
    botones: [{ tipo: "respuesta", texto: "Me interesa" }],
    variables: [{ nombre: "1", ejemplo: "Ana" }],
};
const DEF_BIENVENIDA: Definicion = {
    cuerpo: "¡Bienvenido a Kingnet! Tu servicio {{plan}} ya está activo.",
    botones: [],
    variables: [variable("plan", "Fibra 100 Mbps")],
};

function plantilla(datos: Partial<Detalle> & Pick<Detalle, "id" | "nombre" | "etiqueta" | "estado" | "definicion">): Detalle {
    return {
        atender: null,
        borradaEn: null,
        borradaPor: null,
        calidad: "UNKNOWN",
        categoria: "UTILITY",
        categoriaAnterior: null,
        categoriaCambiadaEn: null,
        categoriaProxima: null,
        categoriaProximaEn: null,
        codigos: [],
        createdAt: "2026-11-18T22:20:00Z",
        ediciones: { puedeEditarEn: null, ultimas24h: 0, ultimos30d: 0 },
        empresaId: 10,
        enviadaEn: null,
        estadoEn: AHORA_PWA,
        formato: "NAMED",
        historial: [],
        idioma: "es",
        metaId: null,
        motivo: null,
        nombreLibreEn: null,
        origen: "iris",
        parametros: [],
        pausadaHasta: null,
        prestatarias: [],
        rechazo: NULO,
        sincronizadaEn: null,
        sugerida: null,
        updatedAt: AHORA_PWA,
        usos: [],
        version: 1,
        wabaId: WABA,
        ...datos,
    };
}

const h = (tipo: Historial["tipo"], de: string | null, a: string | null, ocurridoEn: string, origen: Historial["origen"], actorNombre: string | null = null): Historial => ({
    tipo,
    de,
    a,
    ocurridoEn,
    origen,
    actorNombre,
    actorId: actorNombre ? 1 : null,
    detalle: {},
});

function iniciales(): Detalle[] {
    return [
        plantilla({
            id: 501,
            nombre: "aviso_corte",
            etiqueta: "Aviso de corte",
            estado: "REJECTED",
            estadoEn: "2026-11-21T16:42:00Z",
            motivo: "INVALID_FORMAT",
            rechazo: { motivo: "El mensaje tiene dos datos pegados, Moneda y Monto, sin texto entre ellos.", recomendacion: "Deja un espacio entre los dos, o usa un solo dato «Monto» que ya trae la moneda." },
            atender: "rechazada",
            codigos: ["corte_por_falta_de_pago"],
            usos: ["avisos"],
            metaId: "1689556908129835",
            origen: "sugerida",
            sugerida: "aviso_corte",
            definicion: DEF_CORTE,
            historial: [
                h("estado", "PENDING", "REJECTED", "2026-11-21T16:42:00Z", "webhook"),
                h("enviada", "BORRADOR", "PENDING", "2026-11-21T16:15:00Z", "usuario", "Ana López"),
                h("creada", null, "BORRADOR", "2026-11-18T22:20:00Z", "usuario", "Ana López"),
            ],
        }),
        plantilla({
            id: 502,
            nombre: "recordatorio_pago",
            etiqueta: "Recordatorio de pago",
            estado: "APPROVED",
            calidad: "GREEN",
            codigos: ["factura_por_vencer"],
            usos: ["avisos", "conversaciones"],
            metaId: "1689556908129836",
            origen: "sugerida",
            sugerida: "recordatorio_pago",
            definicion: DEF_RECORDATORIO,
            estadoEn: "2026-11-20T21:05:00Z",
            prestatarias: [{ empresaId: 11, nombre: "Kingnet Norte", usos: ["avisos"] }],
            historial: [h("estado", "PENDING", "APPROVED", "2026-11-20T21:05:00Z", "webhook"), h("creada", null, "BORRADOR", "2026-11-20T20:00:00Z", "usuario", "Ana López")],
        }),
        plantilla({
            id: 503,
            nombre: "promo_fibra_300",
            etiqueta: "promo fibra 300",
            estado: "PAUSED",
            pausadaHasta: "2026-11-21T19:42:00Z",
            atender: "pausada",
            categoria: "MARKETING",
            calidad: "RED",
            formato: "POSITIONAL",
            metaId: "1689556908129837",
            origen: "meta",
            definicion: DEF_PROMO,
            estadoEn: "2026-11-21T16:42:00Z",
        }),
        plantilla({
            id: 504,
            nombre: "bienvenida_v1",
            etiqueta: "Bienvenida",
            estado: "DELETED",
            borradaPor: "meta",
            borradaEn: "2026-11-19T15:00:00Z",
            atender: "borrada_en_uso",
            codigos: ["bienvenida"],
            usos: ["avisos"],
            metaId: "1689556908129838",
            origen: "migrada",
            definicion: DEF_BIENVENIDA,
            estadoEn: "2026-11-19T15:00:00Z",
        }),
        plantilla({ id: 505, nombre: "aviso_de_visita", etiqueta: "Aviso de visita", estado: "BORRADOR", definicion: { cuerpo: "Mañana te visitamos entre {{hora}}. ¡Gracias!", botones: [], variables: [{ nombre: "hora", ejemplo: "8:00 y 12:00" }] } }),
    ];
}

const SUGERIDAS: Omit<Sugerida, "plantilla">[] = [
    {
        clave: "aviso_corte",
        etiqueta: "Aviso de corte",
        categoria: "UTILITY",
        codigo: "corte_por_falta_de_pago",
        para: "Avisos de Argos",
        usos: ["avisos"],
        definicion: {
            cuerpo: "Hola {{cliente_nombre}}, tu servicio fue suspendido por un saldo pendiente de {{monto_con_moneda}}. Al pagar se reactiva el mismo día.",
            botones: [{ tipo: "url", texto: "Pagar en línea", url: "https://pagos.kns.gt/pagar" }],
            variables: [variable("cliente_nombre", "María"), variable("monto_con_moneda", "Q 250.00")],
        },
    },
    { clave: "recordatorio_pago", etiqueta: "Recordatorio de pago", categoria: "UTILITY", codigo: "factura_por_vencer", para: "Avisos de Argos", usos: ["avisos", "conversaciones"], definicion: DEF_RECORDATORIO },
    {
        clave: "visita_programada",
        etiqueta: "Visita técnica programada",
        categoria: "UTILITY",
        codigo: null,
        para: "Avisos de órdenes",
        usos: ["avisos", "asistente"],
        definicion: {
            cuerpo: "Hola {{cliente_nombre}}, programamos tu {{tipo_orden}} para el {{fecha_visita}} entre {{ventana}}. ¿Te queda bien?",
            botones: [
                { tipo: "respuesta", texto: "Sí, confirmo" },
                { tipo: "respuesta", texto: "Cambiar fecha" },
            ],
            variables: [variable("cliente_nombre", "María"), { nombre: "tipo_orden", ejemplo: "instalación" }, { nombre: "fecha_visita", ejemplo: "martes 25 de noviembre" }, { nombre: "ventana", ejemplo: "8:00 y 12:00" }],
        },
    },
    {
        clave: "tecnico_en_camino",
        etiqueta: "Técnico en camino",
        categoria: "UTILITY",
        codigo: null,
        para: "Avisos de órdenes",
        usos: ["avisos"],
        definicion: {
            cuerpo: "Hola {{cliente_nombre}}, {{tecnico}} va en camino a tu casa y llega en unos {{minutos}}.",
            botones: [{ tipo: "telefono", texto: "Llamar a soporte", telefono: "+50222001500" }],
            variables: [variable("cliente_nombre", "María"), { nombre: "tecnico", ejemplo: "Carlos Méndez" }, { nombre: "minutos", ejemplo: "20 minutos" }],
        },
    },
    {
        clave: "orden_cerrada",
        etiqueta: "Orden cerrada",
        categoria: "UTILITY",
        codigo: null,
        para: "Avisos de órdenes",
        usos: ["avisos"],
        definicion: {
            cuerpo: "Terminamos tu {{tipo_orden}} (orden {{numero_orden}}). ¿Quedó todo funcionando?",
            botones: [
                { tipo: "respuesta", texto: "Todo bien" },
                { tipo: "respuesta", texto: "Tengo un problema" },
            ],
            variables: [{ nombre: "tipo_orden", ejemplo: "instalación" }, { nombre: "numero_orden", ejemplo: "OT-001284" }],
        },
    },
    {
        clave: "codigo_verificacion",
        etiqueta: "Código de verificación",
        categoria: "AUTHENTICATION",
        codigo: null,
        para: "El texto lo pone Meta; solo eliges los minutos y el botón.",
        usos: [],
        definicion: { cuerpo: "", botones: [{ tipo: "copiar_codigo", texto: "Copiar código", ejemplo: "482915" }], variables: [], autenticacion: { avisoSeguridad: true, caducaMinutos: 10 } },
    },
    {
        clave: "corte_programado",
        etiqueta: "Mantenimiento en tu zona",
        categoria: "UTILITY",
        codigo: "corte_programado",
        para: "Cortes programados",
        usos: ["avisos"],
        definicion: {
            cuerpo: "Haremos mantenimiento en {{zona}} el {{fecha}} de {{horario}}. Tu internet puede fallar en ese horario.",
            botones: [],
            variables: [{ nombre: "zona", ejemplo: "Mixco Centro" }, { nombre: "fecha", ejemplo: "sábado 29" }, { nombre: "horario", ejemplo: "01:00 a 04:00" }],
        },
    },
];

const RESUMEN: Resumen = {
    total: 5,
    origen: "manual",
    nuevas: [{ id: 503, nombre: "promo_fibra_300", etiqueta: "promo fibra 300", estado: "PAUSED", codigos: [], usos: [] }],
    cambiadas: [{ id: 501, nombre: "aviso_corte", etiqueta: "Aviso de corte", estado: "REJECTED", codigos: ["corte_por_falta_de_pago"], usos: ["avisos"] }],
    borradas: [{ id: 504, nombre: "bienvenida_v1", etiqueta: "Bienvenida", estado: "DELETED", codigos: ["bienvenida"], usos: ["avisos"] }],
};

function deLista(d: Detalle): PlantillaWA {
    const p: Partial<Detalle> = { ...d };
    delete p.historial;
    delete p.prestatarias;
    return p as PlantillaWA;
}

export type ApiPlantillasWA = ReturnType<typeof apiPlantillasWA>;

export function apiPlantillasWA(opciones: OpcionesPlantillasWA = {}) {
    let plantillas = iniciales();
    let siguiente = 600;
    let marketing = { marketingPermitido: false, version: 0 };
    const cuenta: Cuenta = {
        wabaId: WABA,
        numeros: ["+502 2200-0040", "+502 2200-0041"],
        nombres: ["Avisos", "Soporte"],
        sincronizadaEn: "2026-11-21T16:30:00Z",
        resumen: NULO,
        error: opciones.errorCuenta ? { codigo: opciones.errorCuenta, en: "2026-11-21T16:30:00Z" } : NULO,
    };
    const recibido = { creadas: [] as Schema<"EntradaCrearPWA">[], sugeridas: [] as string[], usos: [] as Schema<"EntradaUsosPWA">[], borradas: [] as number[], sincronizaciones: 0 };

    const conteos = (lista: Detalle[]): Schema<"ConteosPWA"> => ({
        todas: lista.length,
        activas: lista.filter((p) => p.estado === "APPROVED" || p.estado === "FLAGGED").length,
        revision: lista.filter((p) => p.estado === "PENDING" || p.estado === "IN_APPEAL").length,
        atender: lista.filter((p) => p.atender !== null).length,
    });

    function guardar(p: Detalle): Detalle {
        const g = { ...p, version: p.version + 1, updatedAt: AHORA_PWA };
        plantillas = plantillas.map((x) => (x.id === p.id ? g : x));
        return g;
    }

    async function manejar(x: { route: Route; metodo: string; ruta: string; query: URLSearchParams; cuerpo: unknown; puede: (p: Permiso) => boolean }): Promise<boolean> {
        const { route, metodo, ruta, query, puede } = x;
        const BASE = "/api/v1/mensajeria/plantillas-whatsapp";
        if (!ruta.startsWith(BASE)) return false;
        const cuerpo = (x.cuerpo ?? {}) as Record<string, unknown>;
        const sub = ruta.slice(BASE.length);
        const prohibido = async () => (await problema(route, P("prohibido", 403, "Sin permiso")), true);
        if (!puede("plantillas_whatsapp.ver")) return prohibido();

        if (sub === "/cuentas" && metodo === "GET") return (await json(route, { data: opciones.sinCuentas ? [] : [cuenta] }), true);
        if (sub === "/datos" && metodo === "GET") return (await json(route, { data: DATOS_PWA }), true);
        if (sub === "/ajustes" && metodo === "GET") return (await json(route, marketing), true);
        if (sub === "/ajustes" && metodo === "POST") {
            if (!puede("plantillas_whatsapp.editar")) return prohibido();
            marketing = { marketingPermitido: Boolean(cuerpo.marketingPermitido), version: marketing.version + 1 };
            return (await json(route, marketing), true);
        }
        if (sub === `/cuentas/${WABA}/sincronizar` && metodo === "POST") {
            if (!puede("plantillas_whatsapp.sincronizar")) return prohibido();
            if (opciones.sincronizadaHacePoco) {
                return (
                    await problema(route, P("sincronizada_hace_poco", 429, "Se sincronizó hace poco", { detail: "Esta cuenta se sincronizó hace menos de 2 minutos." }), { "Retry-After": "75" }),
                    true
                );
            }
            const antes = cuenta.sincronizadaEn;
            recibido.sincronizaciones++;
            // El job termina «enseguida»: la próxima lectura de la cuenta ya trae el resultado.
            cuenta.sincronizadaEn = AHORA_PWA;
            cuenta.resumen = RESUMEN;
            cuenta.error = NULO;
            return (await json(route, { wabaId: WABA, sincronizadaEn: antes }, 202), true);
        }
        if (sub === "/sugeridas" && metodo === "GET") {
            const data: Sugerida[] = SUGERIDAS.map((s) => {
                const ya = plantillas.find((p) => p.sugerida === s.clave && p.estado !== "DELETED");
                return { ...s, plantilla: ya ? { id: ya.id, nombre: ya.nombre, estado: ya.estado } : NULO };
            });
            return (await json(route, { data, empresa: "Kingnet Solution", faltan: data.filter((s) => !s.plantilla).length }), true);
        }
        const sug = /^\/sugeridas\/([a-z_]+)\/crear$/.exec(sub);
        if (sug && metodo === "POST") {
            if (!puede("plantillas_whatsapp.crear")) return prohibido();
            const s = SUGERIDAS.find((y) => y.clave === sug[1]);
            if (!s) return (await problema(route, P("no_encontrado", 404, "No existe")), true);
            recibido.sugeridas.push(s.clave);
            const p = plantilla({
                id: siguiente++,
                nombre: s.clave,
                etiqueta: s.etiqueta,
                estado: "PENDING",
                categoria: s.categoria,
                definicion: s.definicion,
                usos: s.usos,
                codigos: s.codigo ? [s.codigo] : [],
                origen: "sugerida",
                sugerida: s.clave,
                metaId: String(1700000000000 + siguiente),
            });
            plantillas.push(p);
            return (await json(route, { ...deLista(p), errorEnvio: NULO }, 201), true);
        }
        if (sub === "" && metodo === "GET") {
            const grupo = query.get("grupo");
            const buscar = (query.get("buscar") ?? "").toLowerCase();
            const visibles = plantillas.filter((p) => p.estado !== "DELETED" || p.atender === "borrada_en_uso");
            const data = visibles
                .filter((p) => !grupo || (grupo === "atender" ? p.atender !== null : grupo === "activas" ? p.estado === "APPROVED" : p.estado === "PENDING"))
                .filter((p) => !buscar || p.etiqueta.toLowerCase().includes(buscar) || p.nombre.includes(buscar))
                .map(deLista);
            return (await json(route, { data, nextCursor: null, conteos: conteos(visibles) }), true);
        }
        if (sub === "" && metodo === "POST") {
            if (!puede("plantillas_whatsapp.crear")) return prohibido();
            const e = cuerpo as Schema<"EntradaCrearPWA">;
            if (plantillas.some((p) => p.nombre === e.nombre && p.estado !== "DELETED")) {
                return (await problema(route, P("nombre_en_uso", 409, "Nombre en uso", { detail: "Ya hay una con ese nombre", sugerencia: `${e.nombre}_2` } as Partial<Problema>)), true);
            }
            recibido.creadas.push(e);
            const p = plantilla({
                id: siguiente++,
                nombre: e.nombre ?? "plantilla",
                etiqueta: e.etiqueta,
                estado: e.enviar ? "PENDING" : "BORRADOR",
                categoria: e.categoria,
                idioma: e.idioma,
                usos: e.usos ?? [],
                definicion: e.definicion,
                ...(e.enviar ? { metaId: String(1700000000000 + siguiente) } : {}),
                historial: [h("creada", null, "BORRADOR", AHORA_PWA, "usuario", "Ana López")],
            });
            plantillas.push(p);
            return (await json(route, { ...deLista(p), errorEnvio: NULO }, 201), true);
        }
        const m = /^\/(\d+)(?:\/(actualizar|enviar|editar|usos|borrar|reanudar))?$/.exec(sub);
        if (!m) return false;
        const p = plantillas.find((y) => y.id === Number(m[1]));
        if (!p) return (await problema(route, P("no_encontrado", 404, "Plantilla no encontrada")), true);
        const accion = m[2];
        if (!accion && metodo === "GET") return (await json(route, p), true);
        if (metodo !== "POST" || !accion) return false;
        if (typeof cuerpo.version === "number" && cuerpo.version !== p.version) return (await problema(route, P("conflicto_version", 409, "Alguien más lo cambió")), true);
        switch (accion) {
            case "actualizar": {
                if (!puede("plantillas_whatsapp.editar")) return prohibido();
                const e = cuerpo as Schema<"EntradaEditarPWA">;
                return (await json(route, deLista(guardar({ ...p, etiqueta: e.etiqueta, nombre: e.nombre, idioma: e.idioma, categoria: e.categoria, definicion: e.definicion }))), true);
            }
            case "enviar":
                if (!puede("plantillas_whatsapp.crear")) return prohibido();
                return (await json(route, deLista(guardar({ ...p, estado: "PENDING", metaId: String(1700000000000 + p.id), enviadaEn: AHORA_PWA }))), true);
            case "editar": {
                if (!puede("plantillas_whatsapp.editar")) return prohibido();
                const e = cuerpo as Schema<"EntradaEditarMetaPWA">;
                return (await json(route, deLista(guardar({ ...p, estado: "PENDING", atender: null, rechazo: NULO, motivo: null, definicion: e.definicion }))), true);
            }
            case "usos": {
                if (!puede("plantillas_whatsapp.editar")) return prohibido();
                const e = cuerpo as Schema<"EntradaUsosPWA">;
                recibido.usos.push(e);
                if (p.categoria === "MARKETING" && !marketing.marketingPermitido && ((e.usos ?? []).length > 0 || (e.codigos ?? []).length > 0)) {
                    return (await problema(route, P("marketing_no_permitido", 422, "Marketing apagado")), true);
                }
                return (await json(route, deLista(guardar({ ...p, usos: e.usos ?? [], codigos: e.codigos ?? [] }))), true);
            }
            case "borrar":
                if (!puede("plantillas_whatsapp.borrar")) return prohibido();
                recibido.borradas.push(p.id);
                return (await json(route, deLista(guardar({ ...p, estado: "DELETED", borradaPor: "iris", borradaEn: AHORA_PWA, codigos: [], usos: [], atender: null }))), true);
            case "reanudar":
                if (!puede("plantillas_whatsapp.editar")) return prohibido();
                return (await json(route, deLista(guardar({ ...p, estado: "APPROVED", pausadaHasta: null, atender: null }))), true);
        }
        return false;
    }

    return { manejar, recibido, cuenta, plantillas: () => plantillas };
}
