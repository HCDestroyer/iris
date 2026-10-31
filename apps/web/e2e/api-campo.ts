import type { Schema } from "@iris/contratos";
import type { BrowserContext, Page, Route } from "@playwright/test";
import { CSRF } from "./api-falsa";
import { PERMISOS_TECNICO, TECNICO, TECNICO_B, bodegaDelTecnico, existenciasDelTecnico, ordenesDeB, ordenesDelDia, trasladosDelTecnico } from "./datos-campo";

/*
 * API falsa de la app de campo para Playwright, con la forma exacta de I2 (paquete, cambios,
 * lote) e I1 (alta de adjuntos y tus 1.0), tipada con packages/contratos. Lleva su propio estado
 * de servidor: versiones, transiciones, requisitos del cierre (cierre_incompleto y
 * faltan_adjuntos con las claves `adjuntos.firma` / `adjuntos.foto:<código>`) y subidas.
 */

type OrdenCampo = Schema<"OrdenCampo">;
type OperacionLote = Schema<"OperacionLote">;
type ResultadoOperacion = Schema<"ResultadoOperacion">;
type Problema = Schema<"Problema">;

interface Subida {
    entrada: Schema<"EntradaRegistrar">;
    recibido: number;
}

export interface OpcionesCampo {
    empresas?: Schema<"EmpresaResumen">[];
    ordenes?: OrdenCampo[];
    /** Requisitos de fotos del servidor (código de ítem) además de los del checklist del paquete. */
    fotosExigidas?: string[];
    /** Ruta publicada del técnico en el paquete (`RutaCampo`; hoy la API no la manda). */
    ruta?: Schema<"RutaCampo">;
    /** Catálogo de plantillas activas (paquete y cambios). */
    plantillas?: Schema<"PlantillaFormulario">[];
    /** Turno de la agenda para el día pedido (`undefined` = ese día no trabaja). */
    turno?: (fecha: string) => Schema<"TurnoCampo"> | undefined;
    /** Traslados de Argos hacia la bodega de Tomás (por defecto, 31 y 32 enviados). */
    traslados?: Schema<"Traslado">[];
}

export interface ApiCampo {
    /** Sin red: toda petición a la API falla como `internetdisconnected` (junto con setOffline). */
    sinRed: boolean;
    /** Las PATCH de tus fallan (la subida queda a medias) mientras sea true. */
    pausarSubidas: boolean;
    lotes: Schema<"EntradaLote">[];
    /** Quién mandó cada lote (correo de la cookie de sesión). */
    lotesDe: string[];
    resultados: ResultadoOperacion[];
    peticiones: { metodo: string; ruta: string; canal: string | undefined; csrf: string | undefined; empresa: string | undefined; usuario: string | undefined }[];
    ordenes: Map<number, OrdenCampo>;
    subidas: Map<string, Subida>;
    /** Rechaza la próxima operación que cumpla `cuando` con este problema. */
    rechazar: (cuando: (op: OperacionLote) => boolean, problema: Problema) => void;
    /** Cambio del lado del servidor que llega por GET /campo/cambios. */
    cambiar: (cambio: Schema<"CambioOrden">) => void;
    /** Lotes de POST /campo/posiciones que respondieron 200. */
    posiciones: Schema<"EntradaPosiciones">[];
    /** Hora (ms) de cada POST /campo/posiciones (también los 429). */
    intentosPosiciones: number[];
    /** Respuesta de cada lote aceptado. */
    respuestasPosiciones: Schema<"RespuestaPosiciones">[];
    /** Las próximas `veces` peticiones de posiciones responden 429 con `Retry-After` (segundos). */
    limitarPosiciones: (veces: number, retryAfter?: number) => void;
    /** Día pedido en cada GET /campo/paquete. */
    fechasPaquete: string[];
    /** Si no es null, GET /campo/paquete responde con este problema. */
    fallarPaquete: Problema | null;
    /** El técnico tiene una contraseña temporal (`usuario.cambioContrasenaRequerido` en GET /yo). */
    contrasenaTemporal: boolean;
    /** Catálogo de plantillas del servidor (despacho puede desactivar una: se quita de aquí). */
    plantillas: Schema<"PlantillaFormulario">[];
    /** Traslados que Iris conoce (webhook de Argos), por id. */
    traslados: Map<number, Schema<"Traslado">>;
    /** Recepciones aplicadas (`traslado.recibir`), en orden. */
    recepciones: Schema<"EntradaRecibirTraslado">[];
    /** Las próximas N altas de la firma de un traslado responden 404 (el traslado aún no llega). */
    firmaTrasladoNoLlega: number;
    /** Si no es null, GET /campo/inventario/mi-bodega responde con este problema. */
    fallarBodega: Problema | null;
    /** Mientras sea true, `traslado.recibir` responde 503 `traslado_sin_confirmar` (Argos no contesta). */
    trasladoSinConfirmar: boolean;
}

/** En la explicación previa de la ubicación (la primera vez por usuario), «Permitir ubicación». */
export async function aceptarUbicacion(page: Page) {
    await page.getByRole("dialog", { name: "¿Compartir tu ubicación?" }).getByRole("button", { name: "Permitir ubicación" }).click();
}

/** Abre la explicación previa al permiso de ubicación y responde «Ahora no» (sin GPS). */
export async function declinarUbicacion(page: Page) {
    await page.getByRole("dialog", { name: "¿Compartir tu ubicación?" }).getByRole("button", { name: "Ahora no" }).click();
}

const ESTADO_TRAS: Partial<Record<OperacionLote["tipo"], OrdenCampo["estado"]>> = {
    "orden.iniciar_camino": "en_camino",
    "orden.llegar": "en_sitio",
    "orden.cerrar": "cerrada",
    "orden.fallar": "fallida",
};
const DESDE: Partial<Record<OperacionLote["tipo"], OrdenCampo["estado"][]>> = {
    "orden.iniciar_camino": ["programada"],
    "orden.llegar": ["programada", "en_camino"],
    "orden.cerrar": ["en_sitio"],
    "orden.fallar": ["programada", "en_camino", "en_sitio"],
    "orden.pausar": ["en_camino", "en_sitio"],
    // Con la orden pausada se admite la evidencia (contratos/sync.md §3).
    "checklist.marcar": ["en_camino", "en_sitio", "pausada"],
    "material.usar": ["en_camino", "en_sitio", "pausada"],
    "equipo.instalar": ["en_sitio", "pausada"],
    "equipo.retirar": ["en_sitio", "pausada"],
    "firma.capturar": ["en_sitio", "pausada"],
};

export const problema = (codigo: string, status: number, title: string, errors?: Problema["errors"]): Problema => ({
    type: `urn:iris:problema:${codigo}`,
    title,
    status,
    ...(errors ? { errors } : {}),
});

/** Con sesión de técnico ya iniciada (cookies como las deja la API); por defecto, Tomás. */
export async function conSesionTecnico(context: BrowserContext, baseURL: string, usuario: Schema<"Usuario"> = TECNICO) {
    await context.addCookies([
        { name: "iris_sesion", value: `s:${usuario.correo}`, url: baseURL, httpOnly: true, sameSite: "Lax" },
        { name: "iris_csrf", value: CSRF, url: baseURL, sameSite: "Lax" },
    ]);
}

export async function simularApiCampo(page: Page, opciones: OpcionesCampo = {}): Promise<ApiCampo> {
    const ordenesIniciales = [...(opciones.ordenes ?? ordenesDelDia()), ...ordenesDeB()];
    const usuarios = new Map([TECNICO, TECNICO_B].map((u) => [u.correo, u]));
    /** dispositivoId → correo de su primer usuario (409 dispositivo_ajeno con otro). */
    const dispositivos = new Map<string, string>();
    const empresas = opciones.empresas ?? [];
    const pendientesRechazo: { cuando: (op: OperacionLote) => boolean; problema: Problema }[] = [];
    const cambiosPendientes: Schema<"CambioOrden">[] = [];
    let cursor = 1;
    const limite429 = { veces: 0, retryAfter: 20 };
    /** Horas ya guardadas por técnico (misma hora = duplicada, como la API). */
    const guardadas = new Map<string, Set<number>>();
    const api: ApiCampo = {
        sinRed: false,
        pausarSubidas: false,
        lotes: [],
        lotesDe: [],
        resultados: [],
        peticiones: [],
        ordenes: new Map(ordenesIniciales.map((o) => [o.id, structuredClone(o)])),
        subidas: new Map(),
        rechazar: (cuando, p) => pendientesRechazo.push({ cuando, problema: p }),
        cambiar: (c) => cambiosPendientes.push(c),
        posiciones: [],
        intentosPosiciones: [],
        respuestasPosiciones: [],
        fechasPaquete: [],
        fallarPaquete: null,
        contrasenaTemporal: false,
        plantillas: structuredClone(opciones.plantillas ?? []),
        traslados: new Map((opciones.traslados ?? trasladosDelTecnico()).map((t) => [t.id, structuredClone(t)])),
        recepciones: [],
        firmaTrasladoNoLlega: 0,
        fallarBodega: null,
        trasladoSinConfirmar: false,
        limitarPosiciones: (veces, retryAfter = 20) => {
            limite429.veces = veces;
            limite429.retryAfter = retryAfter;
        },
    };

    const completo = (ordenId: number, tipo: string, clave?: string) =>
        [...api.subidas.values()].some(
            (s) =>
                s.entrada.entidadId === String(ordenId) &&
                s.entrada.tipo === tipo &&
                (clave === undefined || s.entrada.claveChecklist === clave) &&
                s.recibido === s.entrada.tamano,
        );

    /** `traslado.recibir` (contratos/sync.md §3): agrupada por traslado, sin orden. */
    function recibir(op: OperacionLote): ResultadoOperacion {
        const d = op.datos as Schema<"EntradaRecibirTraslado">;
        const i = pendientesRechazo.findIndex((r) => r.cuando(op));
        if (i >= 0) {
            const [r] = pendientesRechazo.splice(i, 1);
            return { id: op.id, estado: "rechazada", error: r?.problema ?? problema("validacion", 422, "Inválido") };
        }
        if (api.trasladoSinConfirmar) {
            return { id: op.id, estado: "rechazada", error: problema("traslado_sin_confirmar", 503, "Argos no respondió") };
        }
        const t = api.traslados.get(d.trasladoId);
        if (!t || t.estado !== "enviado") {
            return { id: op.id, estado: "rechazada", error: problema("traslado_no_encontrado", 404, "Traslado no encontrado") };
        }
        api.recepciones.push(d);
        const firmaLista = [...api.subidas.values()].some((s) => s.entrada.id === d.firmaAdjuntoId && s.recibido === s.entrada.tamano);
        t.recepcion = { trasladoId: t.id, pendienteId: api.recepciones.length, estado: firmaLista ? "enviando" : "esperando_firma", actualizadoEn: new Date().toISOString() };
        return { id: op.id, estado: "aplicada", recepcion: t.recepcion };
    }

    function aplicar(op: OperacionLote, rechazadas: Set<number>, usuario: Schema<"Usuario">): ResultadoOperacion {
        if (op.tipo === "traslado.recibir") {
            return recibir(op);
        }
        const ordenId = op.ordenId ?? 0;
        const o = api.ordenes.get(ordenId);
        if (!o || o.tecnicoId !== usuario.id) {
            return { id: op.id, estado: "rechazada", error: problema("no_encontrado", 404, "No encontrada") };
        }
        const conOrden = (r: ResultadoOperacion): ResultadoOperacion => ({ ...r, orden: { version: o.version, estado: o.estado } });
        if (rechazadas.has(ordenId)) {
            return conOrden({ id: op.id, estado: "rechazada", error: problema("bloqueada_por_rechazo", 409, "Bloqueada por un rechazo anterior") });
        }
        const i = pendientesRechazo.findIndex((r) => r.cuando(op));
        if (i >= 0) {
            const [r] = pendientesRechazo.splice(i, 1);
            rechazadas.add(ordenId);
            return conOrden({ id: op.id, estado: "rechazada", error: r?.problema ?? problema("validacion", 422, "Inválido") });
        }
        if (o.estado === "pausada" && ["orden.cerrar", "orden.fallar", "orden.llegar", "orden.iniciar_camino"].includes(op.tipo)) {
            rechazadas.add(ordenId);
            return conOrden({ id: op.id, estado: "rechazada", error: problema("orden_pausada", 422, "La orden está pausada") });
        }
        if (op.tipo === "orden.pausar" && o.estado === "pausada") {
            rechazadas.add(ordenId);
            return conOrden({ id: op.id, estado: "rechazada", error: problema("orden_ya_pausada", 409, "Ya pausada") });
        }
        if (op.tipo === "orden.reanudar" && o.estado !== "pausada") {
            rechazadas.add(ordenId);
            return conOrden({ id: op.id, estado: "rechazada", error: problema("orden_no_pausada", 409, "No pausada") });
        }
        const desde = DESDE[op.tipo];
        if (desde && !desde.includes(o.estado)) {
            rechazadas.add(ordenId);
            return conOrden({ id: op.id, estado: "rechazada", error: problema("transicion_invalida", 422, "Transición inválida") });
        }
        const d = (op.datos ?? {}) as Record<string, unknown>;
        const cuando = op.ocurridoEn ?? new Date().toISOString();
        if (op.tipo === "orden.cerrar") {
            const faltan: { field: string; message: string }[] = [];
            for (const it of o.checklist ?? []) {
                if (it.obligatorio && it.valor === null) {
                    const f = (o.formularios ?? []).find((x) => x.id === it.formularioId);
                    faltan.push(
                        f
                            ? { field: `formularios.${f.id}.checklist.${it.codigo}`, message: `Falta: ${it.etiqueta} (${f.nombre})` }
                            : { field: `checklist.${it.codigo}`, message: "Falta" },
                    );
                }
            }
            if (o.requiereFirma && !o.firma) {
                faltan.push({ field: "firma", message: "Falta la firma" });
            }
            if (o.tipo === "instalacion" && !(o.equipos ?? []).some((e) => e.accion === "instalar")) {
                faltan.push({ field: "equipos.instalar", message: "Falta el equipo" });
            }
            if (faltan.length > 0) {
                rechazadas.add(ordenId);
                return conOrden({ id: op.id, estado: "rechazada", error: problema("cierre_incompleto", 422, "Cierre incompleto", faltan) });
            }
            const fotos = new Set([
                ...(o.checklist ?? []).filter((it) => it.tipo === "foto" && it.obligatorio).map((it) => it.codigo),
                ...(opciones.fotosExigidas ?? []),
            ]);
            const faltanAdj = [
                ...[...fotos].filter((c) => !completo(o.id, "foto", c)).map((c) => ({ field: `adjuntos.foto:${c}`, message: "Falta la foto" })),
                ...(o.requiereFirma && !completo(o.id, "firma") ? [{ field: "adjuntos.firma", message: "Falta la firma" }] : []),
            ];
            if (faltanAdj.length > 0) {
                rechazadas.add(ordenId);
                return conOrden({ id: op.id, estado: "rechazada", error: problema("faltan_adjuntos", 422, "Faltan adjuntos", faltanAdj) });
            }
        }
        switch (op.tipo) {
            case "checklist.marcar":
                o.checklist = (o.checklist ?? []).map((it) =>
                    it.itemId === d.itemId ? { ...it, valor: d.valor as never, ocurridoEn: cuando, marcadoPor: TECNICO.id } : it,
                );
                break;
            case "nota.agregar":
                o.notas = [...(o.notas ?? []), { id: op.id, texto: String(d.texto), ocurridoEn: cuando, autorId: TECNICO.id, autorNombre: TECNICO.nombre, canal: "campo" }];
                break;
            case "material.usar":
                o.materiales = [
                    ...(o.materiales ?? []),
                    { id: op.id, productoId: Number(d.productoId), cantidad: Number(d.cantidad), descripcion: String(d.descripcion ?? ""), bodegaId: o.bodegaId, excedeExistencia: false, ocurridoEn: cuando },
                ];
                break;
            case "equipo.instalar":
            case "equipo.retirar":
                o.equipos = [
                    ...(o.equipos ?? []),
                    {
                        id: op.id,
                        accion: op.tipo === "equipo.instalar" ? "instalar" : "retirar",
                        serie: (d.serie as string | undefined) ?? null,
                        mac: (d.mac as string | undefined) ?? null,
                        modelo: String(d.modelo ?? ""),
                        unidadId: (d.unidadId as number | undefined) ?? null,
                        condicion: (d.condicion as "buena" | undefined) ?? null,
                        ocurridoEn: cuando,
                        validado: false,
                    },
                ];
                break;
            case "orden.pausar": {
                const desdeEstado = o.estado === "en_camino" ? "en_camino" : "en_sitio";
                o.pausadaDesde = desdeEstado;
                o.pausadaEn = cuando;
                o.pausas = [
                    ...(o.pausas ?? []),
                    {
                        id: (o.pausas ?? []).length + 1,
                        motivo: d.motivo as Schema<"PausaOrden">["motivo"],
                        nota: (d.nota as string | undefined) ?? null,
                        desdeEstado,
                        inicio: cuando,
                        fin: null,
                        duracionSeg: null,
                        cierre: null,
                        pausadaPor: TECNICO.id,
                        cerradaPor: null,
                    },
                ];
                o.estado = "pausada";
                break;
            }
            case "orden.reanudar": {
                const seg = Math.max(0, Math.floor((Date.parse(cuando) - Date.parse(o.pausadaEn ?? cuando)) / 1000));
                o.tiempoPausadoSeg = (o.tiempoPausadoSeg ?? 0) + seg;
                o.pausas = (o.pausas ?? []).map((p) => (p.fin === null ? { ...p, fin: cuando, duracionSeg: seg, cierre: "reanudada", cerradaPor: TECNICO.id } : p));
                o.estado = o.pausadaDesde ?? "en_sitio";
                delete o.pausadaDesde;
                delete o.pausadaEn;
                break;
            }
            case "orden.agregar_formulario": {
                const p = api.plantillas.find((x) => x.id === d.plantillaId && x.activa);
                if (!p) {
                    rechazadas.add(ordenId);
                    return conOrden({ id: op.id, estado: "rechazada", error: problema("plantilla_inactiva", 422, "Plantilla inactiva") });
                }
                const existente = (o.formularios ?? []).find((f) => f.plantillaId === p.id);
                if (existente) {
                    rechazadas.add(ordenId);
                    return conOrden({ id: op.id, estado: "rechazada", error: { ...problema("formulario_duplicado", 409, "Duplicado"), detail: existente.id } });
                }
                o.formularios = [
                    ...(o.formularios ?? []),
                    {
                        id: op.id,
                        plantillaId: p.id,
                        codigo: p.codigo,
                        nombre: p.nombre,
                        versionId: Number(d.versionId ?? p.versionId),
                        numeroVersion: p.numeroVersion,
                        requiereFirma: p.requiereFirma,
                        origen: "tecnico",
                        agregadoPor: TECNICO.id,
                        agregadoEn: cuando,
                        conRespuestas: false,
                    },
                ];
                o.checklist = [
                    ...(o.checklist ?? []),
                    ...(p.items ?? []).map((i) => ({
                        itemId: i.id,
                        codigo: i.codigo,
                        etiqueta: i.etiqueta,
                        tipo: i.tipo,
                        obligatorio: i.obligatorio,
                        unidad: i.unidad,
                        posicion: i.posicion,
                        ...(i.opciones ? { opciones: i.opciones } : {}),
                        formularioId: op.id,
                        valor: null,
                        marcadoPor: null,
                        ocurridoEn: null,
                    })),
                ];
                o.requiereFirma = o.requiereFirma || p.requiereFirma;
                break;
            }
            case "orden.quitar_formulario": {
                const f = (o.formularios ?? []).find((x) => x.id === d.formularioId);
                if (!f) {
                    rechazadas.add(ordenId);
                    return conOrden({ id: op.id, estado: "rechazada", error: problema("formulario_ya_quitado", 409, "Ya quitado") });
                }
                if (f.origen === "tipo") {
                    rechazadas.add(ordenId);
                    return conOrden({ id: op.id, estado: "rechazada", error: problema("formulario_del_tipo", 422, "Del tipo") });
                }
                o.formularios = (o.formularios ?? []).filter((x) => x.id !== f.id);
                o.checklist = (o.checklist ?? []).filter((i) => i.formularioId !== f.id);
                break;
            }
            case "firma.capturar":
                o.firma = { nombre: String(d.nombre), documento: String(d.documento ?? ""), adjuntoId: (d.adjuntoId as string | undefined) ?? null, ocurridoEn: cuando };
                break;
            case "adjunto.registrar":
                if (!api.subidas.has(String(d.adjuntoId))) {
                    rechazadas.add(ordenId);
                    return conOrden({ id: op.id, estado: "rechazada", error: problema("no_encontrado", 404, "Adjunto no encontrado") });
                }
                o.adjuntos = [
                    ...(o.adjuntos ?? []),
                    { id: String(d.adjuntoId), tipo: d.tipo as "foto", itemId: (d.itemId as number | undefined) ?? null, descripcion: "", ocurridoEn: cuando },
                ];
                break;
            default:
                break;
        }
        const destino = ESTADO_TRAS[op.tipo];
        if (destino) {
            o.estado = destino;
        }
        o.version += 1;
        return conOrden({ id: op.id, estado: "aplicada" });
    }

    const json = (route: Route, cuerpo: unknown, status = 200, cabeceras: Record<string, string> = {}) =>
        route.fulfill({ status, contentType: "application/json", headers: { Date: new Date().toUTCString(), ...cabeceras }, body: JSON.stringify(cuerpo) });
    const error = (route: Route, p: Problema, cabeceras: Record<string, string> = {}) =>
        route.fulfill({ status: p.status, contentType: "application/problem+json", headers: cabeceras, body: JSON.stringify(p) });

    await page.route("**/api/v1/**", async (route) => {
        const req = route.request();
        const url = new URL(req.url());
        const ruta = url.pathname;
        const metodo = req.method();
        const h = await req.allHeaders();
        const sesion = /(?:^|;\s*)iris_sesion=s:([^;]+)/.exec(h.cookie ?? "")?.[1];
        const correo = sesion ? decodeURIComponent(sesion) : undefined;
        api.peticiones.push({ metodo, ruta, canal: h["x-iris-canal"], csrf: h["x-csrf-token"], empresa: h["x-iris-empresa"], usuario: correo });
        if (api.sinRed) {
            return route.abort("internetdisconnected");
        }
        const usuario = correo ? usuarios.get(correo) : undefined;
        if (!usuario) {
            return error(route, problema("no_autenticado", 401, "Sin sesión"));
        }
        if (metodo !== "GET" && metodo !== "HEAD" && h["x-csrf-token"] !== CSRF) {
            return error(route, problema("csrf_invalido", 403, "CSRF"));
        }

        if (ruta === "/api/v1/yo" && metodo === "GET") {
            const empresa = Number(h["x-iris-empresa"]);
            const activa = empresas.find((e) => e.id === empresa) ?? (empresas.length === 1 ? empresas[0] : undefined);
            const yo: Schema<"Yo"> = {
                actor: "usuario",
                usuario: api.contrasenaTemporal ? { ...usuario, cambioContrasenaRequerido: true } : usuario,
                permisos: PERMISOS_TECNICO,
                empresas,
                modoEmpresa: empresas.length === 0 ? "sin_conector" : activa ? "activa" : "por_elegir",
                todasLasEmpresas: false,
                hayDatosSinEmpresa: false,
                ...(activa ? { empresaActiva: activa } : {}),
            };
            return json(route, yo);
        }
        if (ruta === "/api/v1/campo/paquete") {
            const fecha = url.searchParams.get("fecha") ?? new Date().toISOString().slice(0, 10);
            api.fechasPaquete.push(fecha);
            if (api.fallarPaquete) {
                return error(route, api.fallarPaquete);
            }
            const turno = opciones.turno?.(fecha);
            const paquete: Schema<"Paquete"> = {
                fecha,
                servidorEn: new Date().toISOString(),
                cursor: String(cursor),
                ordenes: [...api.ordenes.values()]
                    .filter((o) => o.tecnicoId === usuario.id && !["cerrada", "fallida", "cancelada"].includes(o.estado))
                    .map((o) => structuredClone(o)),
                existencias: existenciasDelTecnico(),
                ...(opciones.ruta ? { ruta: opciones.ruta } : {}),
                ...(turno ? { turno } : {}),
                plantillas: structuredClone(api.plantillas),
            };
            return json(route, paquete);
        }
        if (ruta === "/api/v1/campo/cambios") {
            const cambios = cambiosPendientes.splice(0);
            if (cambios.length > 0) {
                cursor += 1;
            }
            const r: Schema<"Cambios"> = { cambios, cursor: String(cursor), hayMas: false, plantillas: structuredClone(api.plantillas) };
            return json(route, r);
        }
        if (ruta === "/api/v1/sync/lote" && metodo === "POST") {
            const entrada = req.postDataJSON() as Schema<"EntradaLote">;
            const dueno = dispositivos.get(entrada.dispositivoId) ?? usuario.correo;
            dispositivos.set(entrada.dispositivoId, dueno);
            if (dueno !== usuario.correo) {
                return error(route, problema("dispositivo_ajeno", 409, "El dispositivo es de otro usuario"));
            }
            api.lotes.push(entrada);
            api.lotesDe.push(usuario.correo);
            const rechazadas = new Set<number>();
            const resultados = (entrada.operaciones ?? []).map((op) => aplicar(op, rechazadas, usuario));
            api.resultados.push(...resultados);
            const r: Schema<"RespuestaLote"> = { resultados, cursor: entrada.cursor ?? String(cursor), servidorEn: new Date().toISOString(), desfaseMs: 0 };
            return json(route, r);
        }
        if (ruta === "/api/v1/adjuntos" && metodo === "POST") {
            const e = req.postDataJSON() as Schema<"EntradaRegistrar">;
            if (e.entidad === "traslado") {
                if (api.firmaTrasladoNoLlega > 0 || !api.traslados.has(Number(e.entidadId))) {
                    api.firmaTrasladoNoLlega = Math.max(0, api.firmaTrasladoNoLlega - 1);
                    return error(route, problema("traslado_no_encontrado", 404, "Traslado no encontrado"));
                }
            } else if (api.ordenes.get(Number(e.entidadId))?.tecnicoId !== usuario.id) {
                return error(route, problema("no_encontrado", 404, "No existe o no puedes usar su entidad"));
            }
            const previa = api.subidas.get(e.id);
            if (!previa) {
                api.subidas.set(e.id, { entrada: e, recibido: 0 });
            }
            const s = api.subidas.get(e.id) as Subida;
            const adjunto: Schema<"Adjunto"> = {
                id: e.id,
                entidad: e.entidad,
                entidadId: e.entidadId,
                tipo: e.tipo,
                claveChecklist: e.claveChecklist ?? null,
                mime: e.mime,
                tamano: e.tamano,
                sha256: e.sha256,
                recibido: s.recibido,
                estado: s.recibido === e.tamano ? "completo" : s.recibido > 0 ? "subiendo" : "pendiente",
                motivoRechazo: null,
                lat: e.lat ?? null,
                lon: e.lon ?? null,
                tomadoEn: e.tomadoEn ?? null,
                subidoPor: TECNICO.id,
                creadoEn: new Date().toISOString(),
                completadoEn: null,
                urlSubida: `/api/v1/adjuntos/${e.id}/subida`,
                urlDescarga: `/api/v1/adjuntos/${e.id}`,
                version: 1,
            };
            return json(route, adjunto, previa ? 200 : 201, { Location: adjunto.urlSubida });
        }
        const subida = /^\/api\/v1\/adjuntos\/([^/]+)\/subida$/.exec(ruta);
        if (subida) {
            const s = api.subidas.get(subida[1] ?? "");
            if (!s || (s.entrada.entidad !== "traslado" && api.ordenes.get(Number(s.entrada.entidadId))?.tecnicoId !== usuario.id)) {
                return error(route, problema("no_encontrado", 404, "No existe"));
            }
            if (metodo === "HEAD") {
                return route.fulfill({ status: 200, headers: { "Upload-Offset": String(s.recibido), "Upload-Length": String(s.entrada.tamano), "Tus-Resumable": "1.0.0" } });
            }
            if (api.pausarSubidas) {
                return route.abort("connectionreset");
            }
            const desde = Number(h["upload-offset"]);
            if (desde !== s.recibido) {
                return error(route, problema("offset_distinto", 409, "Offset distinto"), { "Upload-Offset": String(s.recibido) });
            }
            const cuerpo = req.postDataBuffer();
            s.recibido = Math.min(s.entrada.tamano, s.recibido + (cuerpo?.byteLength ?? 0));
            return route.fulfill({ status: 204, headers: { "Upload-Offset": String(s.recibido), "Tus-Resumable": "1.0.0" } });
        }
        if (ruta === "/api/v1/campo/posiciones" && metodo === "POST") {
            // Como I2: solo la sesión de la app de campo; 6 lotes por minuto; misma hora = duplicada;
            // más de 24 h = descartada (vieja).
            if (h["x-iris-canal"] !== "campo") {
                return error(route, problema("requiere_sesion_campo", 403, "Requiere la sesión de la app de campo"));
            }
            const ahora = Date.now();
            api.intentosPosiciones.push(ahora);
            const enElMinuto = api.intentosPosiciones.filter((t) => t > ahora - 60_000).length;
            if (limite429.veces > 0 || enElMinuto > 6) {
                limite429.veces = Math.max(0, limite429.veces - 1);
                return error(route, problema("demasiadas_posiciones", 429, "Demasiados lotes de posiciones"), { "Retry-After": String(limite429.retryAfter) });
            }
            const entrada = req.postDataJSON() as Schema<"EntradaPosiciones">;
            const puntos = entrada.puntos ?? [];
            if (puntos.length < 1 || puntos.length > 500) {
                return error(route, problema("validacion", 422, "Entre 1 y 500 puntos"));
            }
            const suyas = guardadas.get(usuario.correo) ?? new Set<number>();
            guardadas.set(usuario.correo, suyas);
            const r: Schema<"RespuestaPosiciones"> = { aceptadas: 0, duplicadas: 0, descartadas: 0, motivos: {}, servidorEn: new Date(ahora).toISOString(), desfaseMs: 0 };
            for (const p of puntos) {
                const ms = Date.parse(p.registradaEn);
                if (ms < ahora - 24 * 60 * 60_000) {
                    r.descartadas += 1;
                    r.motivos.vieja = (r.motivos.vieja ?? 0) + 1;
                } else if (suyas.has(ms)) {
                    r.duplicadas += 1;
                } else {
                    suyas.add(ms);
                    r.aceptadas += 1;
                }
            }
            api.posiciones.push(entrada);
            api.respuestasPosiciones.push(r);
            return json(route, r);
        }
        if (ruta === "/api/v1/campo/inventario/traslados" && metodo === "GET") {
            const estado = url.searchParams.get("estado");
            const data = [...api.traslados.values()].filter((t) => !estado || t.estado === estado).map((t) => structuredClone(t));
            const r: Schema<"PaginaTraslados"> = { data, nextCursor: null };
            return json(route, r);
        }
        if (ruta === "/api/v1/campo/inventario/mi-bodega" && metodo === "GET") {
            if (api.fallarBodega) {
                return error(route, api.fallarBodega);
            }
            return json(route, bodegaDelTecnico());
        }
        if (ruta === "/api/v1/sesion" && metodo === "DELETE") {
            return route.fulfill({ status: 204 });
        }
        return error(route, problema("no_simulado", 501, `Sin simular: ${metodo} ${ruta}`));
    });
    return api;
}
