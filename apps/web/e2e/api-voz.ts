import type { Schema } from "@iris/contratos";
import type { Route } from "@playwright/test";
import type { Conversacion, Mensaje } from "../lib/conversaciones/tipos";

/*
 * Voz E1 en la API falsa (e2e/api-falsa.ts), tipada con packages/contratos y fiel a
 * contratos/voz.md §3: «Recibir llamadas», reclamar (atómico, reserva de 12 s) → contestar (SDP) →
 * confirmar-medio, soltar, colgar, rechazar y anotar; devoluciones; ajustes por número y de la
 * empresa. Las llamadas llegan por el feed de conversaciones (`llamada_entrante`…).
 */

type Problema = Schema<"Problema">;
type Permiso = Schema<"Permiso">;
type Llamada = Schema<"Llamada">;
type Evento = Schema<"EventoConversacion">;
type Devolucion = Schema<"DevolucionLlamada">;
type LlamadasCanal = Schema<"LlamadasCanal">;
type AjustesVoz = Schema<"AjustesVoz">;
type Requisito = Schema<"RequisitoLlamadas">;

const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
const problema = (route: Route, p: Problema) => route.fulfill({ status: p.status, contentType: "application/problem+json", body: JSON.stringify(p) });
const P = (codigo: string, status: number, title: string, extra: Partial<Problema> = {}): Problema => ({ type: `urn:iris:problema:${codigo}`, title, status, ...extra });

/** Quien atiende llamadas en la bandeja. */
export const PERMISOS_LLAMADAS: Permiso[] = ["llamadas.ver", "llamadas.contestar", "llamadas.rechazar", "llamadas.colgar", "llamadas.anotar", "llamadas.descartar_devolucion"];
/** Además, quien administra. */
export const PERMISOS_VOZ_ADMIN: Permiso[] = ["llamadas.configurar", "voz.configurar"];

export type OpcionesVoz = {
    /** «Recibir llamadas» ya encendido en este navegador (por defecto, apagado). */
    recibe?: boolean;
    /** Devoluciones pendientes al empezar: ids de conversación. */
    devoluciones?: number[];
    /** El número 3 (Soporte) aún no cumple los requisitos de Meta. */
    sinRequisitos?: boolean;
    /** Solo quien atiende: sin `llamadas.configurar` ni `voz.configurar`. */
    soloAtender?: boolean;
    /** Conversaciones cuyo cliente tiene saldo vencido (solo viaja en `GET /llamadas/{id}`). */
    saldoVencido?: number[];
    /** Cómo están en WhatsApp los ajustes del número 3 (por defecto, aplicados). */
    whatsapp?: "pendiente" | "error";
};

/** Una oferta SDP de mentira con forma de SDP (el doble de `RTCPeerConnection` no la interpreta). */
export const SDP_OFERTA = "v=0\r\no=- 1 1 IN IP4 157.240.0.1\r\ns=-\r\nt=0 0\r\nm=audio 3480 UDP/TLS/RTP/SAVPF 111 126\r\na=rtpmap:111 opus/48000/2\r\na=setup:actpass\r\n";

const BASE = "/api/v1/mensajeria";
const OTRA = { id: 9, nombre: "Rosa Sánchez" };

const REQUISITOS = (cumple: boolean): Requisito[] => [
    { codigo: "conexion_api", cumple: true, aviso: false, detalle: "" },
    { codigo: "limite_mensajeria", cumple, aviso: false, detalle: cumple ? "" : "Hoy puede escribirle a 250 personas al día. Sube de nivel en Meta enviando mensajes de calidad." },
    // Iris no puede leer la suscripción al campo `calls`: sin comprobar hasta la primera llamada.
    { codigo: "webhook_calls", cumple: false, aviso: true, sinComprobar: true, detalle: "" },
    { codigo: "calling_habilitado", cumple, aviso: false, detalle: cumple ? "" : "Enciende las llamadas del número en WhatsApp Manager." },
    { codigo: "plantilla_permiso", cumple: false, aviso: true, detalle: "Sin ella no se le puede pedir permiso de llamar a un cliente fuera de las 24 horas." },
];

export function apiVoz(
    contexto: {
        actor: () => { id: number; nombre: string };
        conversaciones: Conversacion[];
        mensajes: Map<number, Mensaje[]>;
        /** Pone un evento en el feed de conversaciones. */
        emitir: (evento: Omit<Evento, "id" | "ocurridoEn" | "noLeidos">) => void;
        /** Tipo de conexión del número (por defecto, API). */
        tipoConexion: (canalId: number) => "api" | "qr";
    },
    opciones: OpcionesVoz = {},
) {
    const { actor, conversaciones, mensajes, emitir } = contexto;
    const recibe = new Map<string, boolean>();
    const llamadas = new Map<string, Llamada>();
    /** La reserva queda atada al navegador de `X-Iris-Navegador` que la reclamó. */
    const reservas = new Map<string, { usuarioId: number; hasta: number; navegador: string }>();
    let n = 0;
    let idDevolucion = 70;
    const devoluciones: Devolucion[] = [];
    const canales = new Map<number, LlamadasCanal>();
    let ajustes: AjustesVoz = { version: 3, grabar: "nunca", duracionMaxPersonaMin: 60, segundosAsignado: 10 };

    /** Lo que recibió el servidor, para comprobarlo en la prueba. */
    const recibido = {
        acciones: [] as { accion: string; llamadaId: string; cuerpo: Record<string, unknown>; usuarioId: number; navegador: string }[],
        configurados: [] as { canalId: number; cuerpo: Record<string, unknown> }[],
        ajustes: [] as Record<string, unknown>[],
        descartadas: [] as { id: number; motivo: string }[],
        recibir: [] as { accion: "encender" | "apagar"; navegador: string }[],
    };
    /** Para forzar respuestas: la prueba lo cambia antes de actuar. */
    const forzar: {
        reclamar?: "otro" | "misma_persona" | "no_recibe" | "sin_red" | "otra_llamada";
        /** Cuántas veces más `reclamar` / `soltar` responden 409 `accion_en_curso` (WhatsApp aún no contesta). */
        enCurso?: { reclamar?: number; soltar?: number };
        /** El servidor apagó «Recibir» en este navegador (pasó del máximo por persona). */
        desplazado?: "tope" | "inactivo";
        /** Qué pasa al reintentar la escritura en WhatsApp (por defecto, se aplica). */
        aplicar?: "error";
        /** `otro_navegador`: la reserva es de otro navegador de la misma persona (409, no se toca). */
        contestar?: "sdp_invalido" | "reserva_vencida" | "meta_rechazo" | "otro_navegador";
        /** `perdida`: el servidor acepta la llamada pero la respuesta no llega al navegador. */
        confirmar?: "reserva_vencida" | "perdida";
        /** Cuántas veces más `colgar` responde 500. */
        colgar?: number;
        /** `soltar` responde 500 (no es `accion_en_curso`). */
        soltar?: "error";
        anotar?: "error";
        descartar?: "cerrada";
    } = {};

    /** Peticiones que el «servidor» deja esperando hasta que la prueba las suelte (para las carreras). */
    const retenidas = new Map<string, Promise<void>>();
    /** Retiene la próxima petición de esa acción (ya recibida y anotada) hasta llamar a lo que devuelve. */
    function retener(accion: "reclamar" | "confirmar-medio" | "contestar"): () => void {
        let liberar: () => void = () => undefined;
        retenidas.set(accion, new Promise<void>((resolver) => (liberar = resolver)));
        return () => {
            retenidas.delete(accion);
            liberar();
        };
    }

    const puedePara = (l: Llamada): Llamada["puede"] => {
        const yo = actor().id;
        const mia = l.usuario?.id === yo;
        const final = ["terminada", "perdida", "rechazada", "fuera_de_horario", "fallida"].includes(l.estado);
        return {
            contestar: l.estado === "sonando",
            rechazar: l.estado === "sonando",
            colgar: mia && (l.estado === "en_curso" || l.estado === "conectando"),
            anotar: mia && (final || l.estado === "en_curso"),
            tomar: false,
            transferir: false,
            escucharGrabacion: false,
        };
    };
    const vista = (l: Llamada): Llamada => ({ ...structuredClone(l), puede: puedePara(l) });

    function mensajeDe(l: Llamada): Mensaje | undefined {
        return l.conversacionId === undefined ? undefined : mensajes.get(l.conversacionId)?.find((m) => m.id === l.mensajeId);
    }
    /** Refleja la llamada en su mensaje del hilo (`tipo: "llamada"`). */
    function alHilo(l: Llamada) {
        const m = mensajeDe(l);
        if (!m) return;
        m.llamada = {
            id: l.id,
            direccion: l.direccion,
            estado: l.estado,
            tieneNota: Boolean(l.nota),
            ...(l.atendidaPor ? { atendidaPor: l.atendidaPor } : {}),
            ...(l.duracionSeg !== undefined ? { duracionSeg: l.duracionSeg } : {}),
            ...(l.motivoFin ? { motivoFin: l.motivoFin } : {}),
            ...(l.usuario ? { usuario: l.usuario } : {}),
        };
    }
    function avisar(tipo: "llamada_entrante" | "llamada_actualizada" | "llamada_terminada", l: Llamada) {
        alHilo(l);
        const m = mensajeDe(l);
        emitir({ tipo, conversacionId: l.conversacionId ?? 0, llamada: vista(l), ...(m && tipo !== "llamada_entrante" ? { mensaje: structuredClone(m) } : {}) });
    }

    /** Entra una llamada de WhatsApp a la conversación: suena a los candidatos. */
    function entrante(conversacionId: number, datos: Partial<Llamada> = {}): Llamada {
        const c = conversaciones.find((x) => x.id === conversacionId);
        if (!c) throw new Error(`No hay conversación ${conversacionId}`);
        n += 1;
        const id = `0199c0de-0000-7000-8000-${String(n).padStart(12, "0")}`;
        const mensajeId = `0199c0de-1111-7000-8000-${String(n).padStart(12, "0")}`;
        const ahora = new Date().toISOString();
        const l: Llamada = {
            id,
            empresaId: 1,
            origen: "whatsapp",
            direccion: "entrante",
            conversacionId,
            mensajeId,
            canal: { id: c.canal.id, nombre: c.canal.nombre, numero: c.canal.numero },
            numero: c.numero,
            titulo: c.titulo,
            contacto: c.contacto.clienteId ? { tipo: "cliente", clienteId: c.contacto.clienteId, ...(c.contacto.servicioId ? { servicioId: c.contacto.servicioId } : {}), verificacion: "numero" } : { tipo: "desconocido" },
            ...(c.cola ? { cola: c.cola } : {}),
            estado: "sonando",
            tramos: [],
            sonoEn: ahora,
            suenaHasta: new Date(Date.now() + 30_000).toISOString(),
            // Señales del cliente: solo en llamadas vivas con cliente. `saldoVencido` no viaja en el feed.
            ...(c.contacto.clienteId
                ? { senales: { servicio: { id: c.contacto.servicioId ?? 0, codigo: `S-${c.contacto.clienteId}`, plan: "Fibra 100 Mb", estado: "activo" }, ...(conversacionId === 41 ? { ticketAbierto: { id: 4812, numero: "TK-004812", asunto: "Sin internet desde las 7" } } : {}) } }
                : {}),
            puede: { contestar: true, rechazar: true, colgar: false, anotar: false, tomar: false, transferir: false, escucharGrabacion: false },
            ...datos,
        };
        llamadas.set(id, l);
        const lista = mensajes.get(conversacionId) ?? [];
        const ultimo = lista.at(-1);
        lista.push({
            id: mensajeId,
            conversacionId,
            seq: (ultimo?.seq ?? 0) + 1,
            direccion: "entrante",
            tipo: "llamada",
            texto: null,
            automatico: null,
            creadoEn: ahora,
            enviadoEn: null,
            entregadoEn: null,
            leidoEn: null,
            estadoEntrega: null,
            motivoFalla: null,
            menciones: [],
        });
        mensajes.set(conversacionId, lista);
        c.ultimoMensaje = { tipo: "llamada", direccion: "entrante", texto: null, evento: null, automatico: null, creadoEn: ahora };
        alHilo(l);
        if (l.estado === "sonando") avisar("llamada_entrante", l);
        return l;
    }

    function terminar(l: Llamada, estado: Llamada["estado"], motivoFin: string) {
        l.estado = estado;
        l.motivoFin = motivoFin;
        l.terminadaEn = new Date().toISOString();
        if (l.contestadaEn) l.duracionSeg = Math.max(1, Math.round((Date.now() - Date.parse(l.contestadaEn)) / 1000));
        reservas.delete(l.id);
        avisar("llamada_terminada", l);
    }

    /** Nadie contestó: queda perdida, con su devolución pendiente. */
    function perder(id: string, sonoSeg = 25) {
        const l = llamadas.get(id);
        if (!l) return;
        l.duracionSeg = sonoSeg;
        terminar(l, "perdida", "sin_respuesta");
        l.duracionSeg = sonoSeg;
        alHilo(l);
        pendiente(l.conversacionId ?? 0, l.id);
        emitir({ tipo: "devolucion_actualizada", conversacionId: l.conversacionId ?? 0 });
    }
    function pendiente(conversacionId: number, llamadaId: string, pedidaPor: Devolucion["pedidaPor"] = "sistema"): Devolucion {
        idDevolucion += 1;
        const conv = conversaciones.find((x) => x.id === conversacionId);
        const d: Devolucion = { id: idDevolucion, conversacionId, llamadaId, titulo: conv?.titulo ?? "", numero: conv?.numero ?? "", estado: "pendiente", pedidaPor, creadaEn: new Date(Date.now() - 6 * 60_000).toISOString(), venceEn: new Date(Date.now() + 86_400_000).toISOString() };
        devoluciones.push(d);
        return d;
    }
    /** Una llamada vieja en el hilo, ya en un estado final (para ver cómo queda cada una). */
    function historica(conversacionId: number, estado: Llamada["estado"], datos: Partial<Llamada> = {}): Llamada {
        const l = entrante(conversacionId, { estado, terminadaEn: new Date().toISOString(), ...datos });
        alHilo(l);
        return l;
    }
    /** Otra persona la reclama: a los demás les deja de sonar. */
    function contestaOtro(id: string) {
        const l = llamadas.get(id);
        if (!l) return;
        l.estado = "conectando";
        l.usuario = OTRA;
        l.atendidaPor = "persona";
        reservas.set(id, { usuarioId: OTRA.id, hasta: Date.now() + 12_000, navegador: "navegador-de-rosa" });
        avisar("llamada_actualizada", l);
    }
    /** El cliente cuelga. */
    function cuelgaCliente(id: string) {
        const l = llamadas.get(id);
        if (l) terminar(l, l.contestadaEn ? "terminada" : "perdida", l.contestadaEn ? "colgo_cliente" : "sin_respuesta");
    }

    for (const conversacionId of opciones.devoluciones ?? []) {
        const l = historica(conversacionId, "perdida", { duracionSeg: 25, motivoFin: "sin_respuesta", sonoEn: new Date(Date.now() - 6 * 60_000).toISOString() });
        pendiente(conversacionId, l.id);
    }

    function canal(id: number): LlamadasCanal {
        let c = canales.get(id);
        if (!c) {
            const qr = contexto.tipoConexion(id) === "qr";
            const cumple = !qr && !(opciones.sinRequisitos && id === 3);
            c = {
                canalId: id,
                tipoConexion: qr ? "qr" : "api",
                disponible: !qr,
                llamadasActivas: false,
                segundosSonando: 30,
                fueraDeHorario: "rechazar_con_mensaje",
                iconoVisible: true,
                devolucion: false,
                mensajePerdida: { activo: true, texto: "No pudimos contestarte. Escríbenos por aquí y te atendemos." },
                requisitos: { cumple, comprobadoEn: new Date(Date.now() - 3_600_000).toISOString(), requisitos: qr ? [{ codigo: "conexion_api", cumple: false, aviso: false, detalle: "El número está conectado por QR." }] : REQUISITOS(cumple) },
                whatsapp:
                    id === 3 && opciones.whatsapp === "pendiente"
                        ? { estado: "pendiente", intentos: 2, proximoIntentoEn: new Date(Date.now() + 4 * 60_000).toISOString(), detalle: "WhatsApp no respondió." }
                        : id === 3 && opciones.whatsapp === "error"
                          ? { estado: "error", intentos: 6, detalle: "WhatsApp rechazó los ajustes del número." }
                          : { estado: "aplicado", intentos: 1 },
                version: 1,
            };
            canales.set(id, c);
        }
        return c;
    }

    async function manejar(x: { route: Route; metodo: string; ruta: string; query: URLSearchParams; cuerpo: unknown; idempotencia: string | undefined; puede: (p: Permiso) => boolean }): Promise<boolean> {
        const { route, metodo, ruta, query, puede } = x;
        const cuerpo = (x.cuerpo ?? {}) as Record<string, unknown>;
        const cabeceras = route.request().headers();
        const prohibido = async () => (await problema(route, P("permiso_denegado", 403, "Sin permiso")), true);
        const noExiste = async () => (await problema(route, P("no_encontrado", 404, "No encontrado")), true);

        // --- Ajustes de la empresa ---
        if (ruta === `${BASE}/voz/ajustes` && metodo === "GET") {
            if (!puede("voz.configurar")) return prohibido();
            return (await json(route, ajustes), true);
        }
        if (ruta === `${BASE}/voz/ajustes/actualizar` && metodo === "POST") {
            if (!puede("voz.configurar")) return prohibido();
            recibido.ajustes.push(cuerpo);
            if (cuerpo.version !== ajustes.version) return (await problema(route, P("conflicto_version", 409, "Alguien más lo cambió")), true);
            const min = Number(cuerpo.duracionMaxPersonaMin);
            if (!(min >= 10 && min <= 120)) return (await problema(route, P("validacion", 422, "Datos inválidos", { errors: [{ field: "body.duracionMaxPersonaMin", message: "Entre 10 y 120 minutos", rule: "rango" }] })), true);
            if (cuerpo.grabar !== undefined && cuerpo.grabar !== "nunca") return (await problema(route, P("grabacion_no_disponible", 422, "La grabación todavía no está disponible")), true);
            ajustes = { grabar: "nunca", duracionMaxPersonaMin: min, segundosAsignado: Number(cuerpo.segundosAsignado), version: ajustes.version + 1 };
            return (await json(route, ajustes), true);
        }

        // --- Ajustes por número ---
        const porCanal = /^\/api\/v1\/mensajeria\/canales\/(\d+)\/llamadas(?:\/(configurar|comprobar|sincronizar|aplicar))?$/.exec(ruta);
        if (porCanal) {
            if (!puede("llamadas.configurar")) return prohibido();
            const c = canal(Number(porCanal[1]));
            if (!porCanal[2] && metodo === "GET") return (await json(route, c), true);
            if (metodo !== "POST") return false;
            if (porCanal[2] === "comprobar") {
                // Meta ya la dejó lista: la segunda comprobación pasa.
                if (c.tipoConexion === "api") c.requisitos = { cumple: true, comprobadoEn: new Date().toISOString(), requisitos: REQUISITOS(true) };
                c.version += 1;
                return (await json(route, c), true);
            }
            if (porCanal[2] === "aplicar") {
                c.whatsapp = forzar.aplicar === "error" ? { estado: "error", intentos: c.whatsapp.intentos + 1, detalle: "WhatsApp rechazó los ajustes del número." } : { estado: "aplicado", intentos: c.whatsapp.intentos + 1 };
                c.version += 1;
                return (await json(route, c), true);
            }
            if (porCanal[2] === "sincronizar") {
                c.sincronizadoEn = new Date().toISOString();
                c.iconoVisible = false;
                c.version += 1;
                return (await json(route, c), true);
            }
            recibido.configurados.push({ canalId: c.canalId, cuerpo });
            if (cuerpo.version !== c.version) return (await problema(route, P("conflicto_version", 409, "Alguien más lo cambió")), true);
            if (cuerpo.llamadasActivas && c.tipoConexion === "qr") return (await problema(route, P("llamadas_no_disponibles_qr", 422, "Un número por QR no puede recibir llamadas")), true);
            if (cuerpo.llamadasActivas && !c.requisitos.cumple) {
                const faltan = (c.requisitos.requisitos ?? []).filter((r) => !r.cumple && !r.aviso);
                return (await problema(route, P("requisitos_sin_cumplir", 422, "Faltan requisitos de Meta", { errors: faltan.map((r) => ({ field: "body.llamadasActivas", message: r.detalle, rule: r.codigo })) })), true);
            }
            const seg = Number(cuerpo.segundosSonando);
            if (!(seg >= 15 && seg <= 45)) return (await problema(route, P("validacion", 422, "Datos inválidos", { errors: [{ field: "body.segundosSonando", message: "Entre 15 y 45 segundos", rule: "rango" }] })), true);
            const { version: _version, colaId, horarioId, ...resto } = cuerpo as unknown as Schema<"EntradaLlamadasCanal">;
            void _version;
            const nuevo: LlamadasCanal = { ...c, ...resto, version: c.version + 1 };
            if (colaId !== undefined) nuevo.colaId = colaId;
            else delete nuevo.colaId;
            if (horarioId !== undefined) nuevo.horarioId = horarioId;
            else delete nuevo.horarioId;
            canales.set(c.canalId, nuevo);
            return (await json(route, nuevo), true);
        }

        if (!ruta.startsWith(`${BASE}/llamadas`)) return false;

        // --- «Recibir llamadas» ---
        if (ruta === `${BASE}/llamadas/recibir` && metodo === "GET") {
            if (!puede("llamadas.contestar")) return prohibido();
            // El id va en la cabecera (no queda en los registros de acceso); la query sigue valiendo.
            const nav = cabeceras["x-iris-navegador"] ?? query.get("navegador") ?? "";
            if (!nav) return (await problema(route, P("navegador_requerido", 422, "Falta el id del navegador")), true);
            if (forzar.desplazado) return (await json(route, { recibe: false, vivo: true, motivo: forzar.desplazado }), true);
            return (await json(route, { recibe: recibe.get(nav) ?? opciones.recibe ?? false, vivo: true }), true);
        }
        const interruptor = /^\/api\/v1\/mensajeria\/llamadas\/recibir\/(encender|apagar)$/.exec(ruta);
        if (interruptor && metodo === "POST") {
            if (!puede("llamadas.contestar")) return prohibido();
            const nav = String(cuerpo.navegador ?? "");
            if (!/^[A-Za-z0-9_-]{8,64}$/.test(nav)) return (await problema(route, P("validacion", 422, "Datos inválidos", { errors: [{ field: "body.navegador", message: "De 8 a 64 caracteres", rule: "formato" }] })), true);
            const accion = interruptor[1] as "encender" | "apagar";
            recibido.recibir.push({ accion, navegador: nav });
            recibe.set(nav, accion === "encender");
            if (accion === "encender") delete forzar.desplazado;
            return (await json(route, { recibe: accion === "encender", vivo: true }), true);
        }

        // --- Devoluciones ---
        if (ruta === `${BASE}/llamadas/devoluciones` && metodo === "GET") {
            if (!puede("llamadas.ver")) return prohibido();
            return (await json(route, { data: devoluciones.filter((d) => d.estado === "pendiente"), nextCursor: null }), true);
        }
        const descartar = /^\/api\/v1\/mensajeria\/llamadas\/devoluciones\/(\d+)\/descartar$/.exec(ruta);
        if (descartar && metodo === "POST") {
            if (!puede("llamadas.descartar_devolucion")) return prohibido();
            const d = devoluciones.find((v) => v.id === Number(descartar[1]));
            if (!d) return noExiste();
            if (forzar.descartar === "cerrada" || d.estado !== "pendiente") {
                d.estado = "hecha";
                return (await problema(route, P("devolucion_cerrada", 409, "La devolución ya está cerrada")), true);
            }
            const motivo = String(cuerpo.motivo ?? "").trim();
            if (!motivo) return (await problema(route, P("validacion", 422, "Datos inválidos", { errors: [{ field: "body.motivo", message: "Escribe el motivo", rule: "requerido" }] })), true);
            d.estado = "descartada";
            recibido.descartadas.push({ id: d.id, motivo });
            emitir({ tipo: "devolucion_actualizada", conversacionId: d.conversacionId });
            return (await json(route, d), true);
        }

        // --- La llamada ---
        if (ruta === `${BASE}/llamadas/activa` && metodo === "GET") {
            if (!puede("llamadas.ver")) return prohibido();
            const yo = actor().id;
            const viva = [...llamadas.values()].find((l) => (l.estado === "sonando" && puede("llamadas.contestar")) || ((l.estado === "en_curso" || l.estado === "conectando") && l.usuario?.id === yo));
            if (!viva) return (await route.fulfill({ status: 204 }), true);
            return (await json(route, vista(viva)), true);
        }
        const una = /^\/api\/v1\/mensajeria\/llamadas\/([0-9a-f-]{36})(?:\/(reclamar|contestar|confirmar-medio|soltar|colgar|rechazar|anotar))?$/.exec(ruta);
        if (!una) return false;
        const l = llamadas.get(una[1] ?? "");
        if (!l) return noExiste();
        if (!una[2]) {
            if (metodo !== "GET") return false;
            if (!puede("llamadas.ver")) return prohibido();
            // El detalle es el único que dice si hay saldo vencido (y solo cuando es `true`).
            const detalle = vista(l);
            if (detalle.senales && (opciones.saldoVencido ?? []).includes(l.conversacionId ?? 0)) detalle.senales.saldoVencido = true;
            return (await json(route, detalle), true);
        }
        if (metodo !== "POST") return false;
        const accion = una[2];
        const yo = actor();
        const nav = cabeceras["x-iris-navegador"] ?? "";
        recibido.acciones.push({ accion, llamadaId: l.id, cuerpo, usuarioId: yo.id, navegador: nav });
        // La petición llegó; el «servidor» tarda en atenderla lo que diga la prueba.
        await retenidas.get(accion);
        const reserva = reservas.get(l.id);
        const final = ["terminada", "perdida", "rechazada", "fuera_de_horario", "fallida"].includes(l.estado);
        const yaContestada = (detalle: string, regla?: string) =>
            problema(route, P("llamada_ya_contestada", 409, "La llamada ya fue contestada", { detail: detalle, ...(regla ? { errors: [{ field: "", message: detalle, rule: regla }] } : {}) }));

        if (accion === "reclamar") {
            if (!puede("llamadas.contestar")) return prohibido();
            if (!nav) return (await problema(route, P("navegador_requerido", 422, "Falta el id del navegador")), true);
            if (forzar.enCurso?.reclamar) {
                forzar.enCurso.reclamar -= 1;
                return (await problema(route, P("accion_en_curso", 409, "WhatsApp aún no responde")), true);
            }
            if (forzar.reclamar === "otra_llamada") return (await problema(route, P("llamada_en_curso", 409, "Ya tienes otra llamada")), true);
            if (forzar.reclamar === "sin_red") return (await route.abort("internetdisconnected"), true);
            if (forzar.reclamar === "no_recibe") return (await problema(route, P("no_recibe_llamadas", 422, "Este navegador no recibe llamadas")), true);
            if (forzar.reclamar === "misma_persona") return (await yaContestada("Ya la contestaste en otra pestaña.", "misma_persona"), true);
            if (forzar.reclamar === "otro") {
                contestaOtro(l.id);
                return (await yaContestada(`La contestó ${OTRA.nombre}.`), true);
            }
            if (l.estado === "conectando" && reserva?.usuarioId === yo.id && reserva.navegador !== nav) return (await yaContestada("La estás contestando en otro navegador.", "misma_persona"), true);
            if (l.estado === "conectando" && reserva?.usuarioId === yo.id && reserva.hasta > Date.now()) {
                // El mismo navegador dentro de la reserva (otra pestaña): la misma respuesta.
                return (await json(route, { llamada: vista(l), sdpOferta: SDP_OFERTA, ice: { servidores: [] }, venceEn: new Date(reserva.hasta).toISOString() }), true);
            }
            if (l.estado !== "sonando") return (await yaContestada(l.usuario ? `La contestó ${l.usuario.nombre}.` : "La llamada ya no está sonando.", l.usuario?.id === yo.id ? "misma_persona" : undefined), true);
            l.estado = "conectando";
            l.usuario = { id: yo.id, nombre: yo.nombre };
            const hasta = Date.now() + 12_000;
            reservas.set(l.id, { usuarioId: yo.id, hasta, navegador: nav });
            avisar("llamada_actualizada", l);
            return (await json(route, { llamada: vista(l), sdpOferta: SDP_OFERTA, ice: { servidores: [] }, venceEn: new Date(hasta).toISOString() }), true);
        }
        if (accion === "rechazar") {
            if (!puede("llamadas.rechazar")) return prohibido();
            if (final) return (await problema(route, P("llamada_terminada", 409, "La llamada ya terminó")), true);
            if (cuerpo.paraTodos === true) {
                // Solo quien supervisa: deja de sonarles a todos y queda rechazada.
                if (!puede("conversaciones.supervisar")) return prohibido();
                if (l.estado !== "sonando") return (await yaContestada("La llamada ya no está sonando."), true);
                l.usuario = { id: yo.id, nombre: yo.nombre };
                terminar(l, "rechazada", "rechazada_supervisor");
            }
            return (await json(route, vista(l)), true);
        }
        if (accion === "contestar" || accion === "confirmar-medio" || accion === "soltar") {
            // Después de reclamar, la llamada es de quien la reclamó: cualquier otro, 404.
            if (reserva?.usuarioId !== yo.id) return noExiste();
            // …y de ese navegador: desde otro de la misma persona, 409 sin tocar la reserva.
            if (reserva.navegador !== nav || forzar.contestar === "otro_navegador") return (await yaContestada("La estás contestando en otro navegador.", "otro_navegador"), true);
            if (final) return (await problema(route, P("llamada_terminada", 409, "La llamada ya terminó")), true);
            if (accion === "soltar") {
                if (forzar.enCurso?.soltar) {
                    forzar.enCurso.soltar -= 1;
                    return (await problema(route, P("accion_en_curso", 409, "WhatsApp aún no responde")), true);
                }
                if (forzar.soltar === "error") return (await problema(route, P("error_interno", 500, "Error del servidor")), true);
                // Ya aceptada (confirmar-medio ganó la carrera): no se puede soltar.
                if (l.estado === "en_curso") return (await problema(route, P("llamada_en_curso", 409, "La llamada ya está en curso")), true);
                l.estado = "sonando";
                delete l.usuario;
                reservas.delete(l.id);
                avisar("llamada_actualizada", l);
                return (await json(route, vista(l)), true);
            }
            const vencida = () => problema(route, P("reserva_vencida", 409, "Pasó el tiempo para contestar"));
            if (accion === "contestar") {
                if (forzar.contestar === "reserva_vencida" || reserva.hasta < Date.now()) return (await vencida(), true);
                if (forzar.contestar === "meta_rechazo") return (await problema(route, P("meta_rechazo", 502, "WhatsApp no aceptó la llamada")), true);
                const sdp = String(cuerpo.sdpRespuesta ?? "");
                // Como el servidor (§6.2-11): audio, `a=setup` y huella DTLS. Las direcciones locales
                // del navegador no se rechazan: el servidor las sanea antes de mandar el SDP a Meta.
                if (forzar.contestar === "sdp_invalido" || !sdp.includes("m=audio") || !sdp.includes("a=setup:") || !sdp.includes("a=fingerprint:")) {
                    return (await problema(route, P("sdp_invalido", 422, "SDP inválido", { errors: [{ field: "body.sdpRespuesta", message: "Solo audio Opus", rule: "sdp" }] })), true);
                }
                // Desde aquí corren los 5 s para `confirmar-medio`.
                reserva.hasta = Date.now() + 5_000;
                return (await json(route, vista(l)), true);
            }
            if (forzar.confirmar === "reserva_vencida" || reserva.hasta < Date.now()) return (await vencida(), true);
            l.estado = "en_curso";
            l.atendidaPor = "persona";
            l.contestadaEn = new Date().toISOString();
            l.tramos = [{ tipo: "persona", usuario: { id: yo.id, nombre: yo.nombre }, inicio: l.contestadaEn }] as Llamada["tramos"];
            avisar("llamada_actualizada", l);
            // Aceptada en el servidor, pero la respuesta se pierde por el camino.
            if (forzar.confirmar === "perdida") return (await route.abort("connectionreset"), true);
            return (await json(route, vista(l)), true);
        }
        if (accion === "colgar") {
            if (!puede("llamadas.colgar")) return prohibido();
            if (forzar.colgar) {
                forzar.colgar -= 1;
                return (await problema(route, P("error_interno", 500, "Error del servidor")), true);
            }
            if (final) return (await problema(route, P("llamada_terminada", 409, "La llamada ya terminó")), true);
            terminar(l, "terminada", "colgo_agente");
            return (await json(route, vista(l)), true);
        }
        // anotar
        if (!puede("llamadas.anotar")) return prohibido();
        if (forzar.anotar === "error") {
            delete forzar.anotar;
            return (await problema(route, P("error_interno", 500, "Error del servidor")), true);
        }
        const nota = String(cuerpo.nota ?? "").trim();
        if (!nota || nota.length > 2000) return (await problema(route, P("validacion", 422, "Datos inválidos", { errors: [{ field: "body.nota", message: "Entre 1 y 2000 caracteres", rule: "longitud" }] })), true);
        l.nota = nota;
        if (typeof cuerpo.resultado === "string") l.resultado = cuerpo.resultado as NonNullable<Llamada["resultado"]>;
        alHilo(l);
        return (await json(route, vista(l)), true);
    }

    return { manejar, llamadas, devoluciones, recibido, forzar, retener, entrante, historica, perder, contestaOtro, cuelgaCliente, canal, ajustes: () => ajustes };
}

export type ApiVoz = ReturnType<typeof apiVoz>;
