import type { Schema } from "@iris/contratos";
import type { ConversacionConEspera, Espera, MensajeAutomatico } from "../lib/conversaciones/contrato-11";

/*
 * Datos de la bandeja de conversaciones para las pruebas, tipados con packages/contratos
 * (contratos/conversaciones.md). Los mismos sirven a Vitest y a la API falsa de Playwright.
 */
type Conversacion = Schema<"ConversacionCliente">;
type Detalle = Schema<"ConversacionClienteDetalle">;
type Mensaje = Schema<"MensajeConversacion">;

/** Hora fija de las pruebas: miércoles 28 oct 2026, 10:45 en Guatemala. */
export const AHORA_CONV = Date.parse("2026-10-28T16:45:00Z");
const hace = (min: number) => new Date(AHORA_CONV - min * 60_000).toISOString();
const en = (min: number) => new Date(AHORA_CONV + min * 60_000).toISOString();

export const CANAL_SOPORTE = { id: 3, nombre: "Soporte", numero: "+50222000010", usos: ["soporte"] };
export const CANAL_COBROS = { id: 4, nombre: "Cobros", numero: "+50222000022", usos: ["cobros"] };
export const CANAL_VENTAS = { id: 5, nombre: "Ventas", numero: "+50222000031", usos: ["ventas"] };
export const CANAL_AVISOS = { id: 2, nombre: "Avisos", numero: "+50222000005", usos: ["avisos", "soporte"] };

export const ANDREA = { id: 1, nombre: "Ana López" };
export const ROSA = { id: 9, nombre: "Rosa Sánchez" };
export const LAURA = { id: 12, nombre: "Laura Méndez" };
export const MARIO = { id: 15, nombre: "Mario Ixcot" };

/** Espera de una conversación sin asignar (fase 1.1, §14.6). */
export function espera(datos: Partial<Espera> = {}): Espera {
    return { disponibles: [], totalDisponibles: 0, botContestaEn: null, botMotivo: null, botAtendio: false, ...datos };
}

export function conversacion(datos: Partial<Conversacion> & Pick<Conversacion, "id" | "titulo">): Conversacion {
    return {
        empresaId: 1,
        version: 1,
        canal: CANAL_SOPORTE,
        cola: { id: 2, nombre: "Soporte" },
        numero: "+50255120041",
        perfilNombre: null,
        contacto: { tipo: "desconocido", clienteId: null, servicioId: null, contactoIrisId: null, prospectoId: null, asociacion: null, verificado: false },
        estado: "abierta",
        reabiertaEn: null,
        etiquetas: [],
        ventana: { abierta: true, cierraEn: en(21 * 60) },
        sla: { estado: "en_tiempo", venceEn: en(30), esperaDesde: hace(4) },
        ultimoMensaje: { direccion: "entrante", tipo: "texto", texto: "Hola", evento: null, creadoEn: hace(4) },
        noLeidos: 0,
        ultimaActividad: hace(4),
        creadaEn: hace(600),
        ...datos,
    };
}

export const CONVERSACIONES: Conversacion[] = [
    conversacion({
        id: 41,
        titulo: "Carlos Ruiz",
        version: 7,
        contacto: { tipo: "cliente", clienteId: 4101, servicioId: 41010, contactoIrisId: null, prospectoId: null, asociacion: "automatica", verificado: true },
        asignado: ANDREA,
        etiquetas: [{ id: 3, nombre: "Sin servicio", color: "peligro" }],
        sla: { estado: "por_vencer", venceEn: en(6), esperaDesde: hace(4) },
        ultimoMensaje: { direccion: "entrante", tipo: "texto", texto: "Ya lo reinicié y sigue igual.", evento: null, creadoEn: hace(4) },
        noLeidos: 2,
        ultimaActividad: hace(4),
    }),
    conversacion({
        id: 42,
        titulo: "María López",
        canal: CANAL_COBROS,
        cola: { id: 3, nombre: "Caja" },
        numero: "+50255330017",
        contacto: { tipo: "cliente", clienteId: 4102, servicioId: 41020, contactoIrisId: null, prospectoId: null, asociacion: "automatica", verificado: true },
        sla: { estado: "vencido", venceEn: hace(5), esperaDesde: hace(40) },
        ultimoMensaje: { direccion: "entrante", tipo: "imagen", texto: null, evento: null, creadoEn: hace(7) },
        noLeidos: 1,
        ultimaActividad: hace(7),
        espera: espera({ botMotivo: "sin_disponibles", botContestaEn: en(1) }),
    } as Partial<ConversacionConEspera> & Pick<Conversacion, "id" | "titulo">),
    conversacion({
        id: 43,
        titulo: "+50255440018",
        canal: CANAL_VENTAS,
        cola: { id: 4, nombre: "Ventas" },
        numero: "+50255440018",
        perfilNombre: "Luis H.",
        ventana: { abierta: true, cierraEn: en(23 * 60) },
        sla: { estado: "vencido", venceEn: hace(1), esperaDesde: hace(25) },
        ultimoMensaje: { direccion: "entrante", tipo: "texto", texto: "¿Tienen fibra en San Lucas?", evento: null, creadoEn: hace(25) },
        noLeidos: 1,
        ultimaActividad: hace(25),
        espera: espera({ disponibles: [LAURA], totalDisponibles: 1, botAtendio: true }),
    } as Partial<ConversacionConEspera> & Pick<Conversacion, "id" | "titulo">),
    conversacion({
        id: 44,
        titulo: "Ferretería El Tornillo, Sociedad Anónima de Capital Variable",
        numero: "+50255660044",
        contacto: { tipo: "cliente", clienteId: 4104, servicioId: 41040, contactoIrisId: null, prospectoId: null, asociacion: "manual", verificado: false },
        asignado: { id: 9, nombre: "Rosa Sánchez" },
        estado: "esperando_cliente",
        ventana: { abierta: false, cierraEn: hace(60) },
        sla: { estado: "sin", venceEn: null, esperaDesde: null },
        ultimoMensaje: {
            direccion: "saliente",
            tipo: "texto",
            texto: "Gracias por su paciencia, ya quedó resuelto el problema de la conexión en la sucursal central y la bodega.",
            evento: null,
            creadoEn: hace(26 * 60),
        },
        ultimaActividad: hace(26 * 60),
    }),
    // Fase 1.1: sin asignar, se avisó a los disponibles de Soporte y el bot contesta en 12 min.
    conversacion({
        id: 45,
        titulo: "Julio Coy",
        numero: "+50255300045",
        contacto: { tipo: "cliente", clienteId: 5530, servicioId: 55300, contactoIrisId: null, prospectoId: null, asociacion: "automatica", verificado: true },
        sla: { estado: "en_tiempo", venceEn: en(12), esperaDesde: hace(18) },
        ultimoMensaje: { direccion: "entrante", tipo: "texto", texto: "Se me fue el internet otra vez", evento: null, creadoEn: hace(11) },
        noLeidos: 3,
        ultimaActividad: hace(11),
        espera: espera({ disponibles: [ROSA, LAURA], totalDisponibles: 2, botContestaEn: en(12), botMotivo: "sin_confirmacion" }),
    } as Partial<ConversacionConEspera> & Pick<Conversacion, "id" | "titulo">),
    // Fase 1.1: nadie la tomó y contestó el bot.
    conversacion({
        id: 46,
        titulo: "Diana García",
        numero: "+50255600046",
        contacto: { tipo: "cliente", clienteId: 6012, servicioId: 60120, contactoIrisId: null, prospectoId: null, asociacion: "automatica", verificado: true },
        sla: { estado: "en_tiempo", venceEn: en(20), esperaDesde: hace(84) },
        ultimoMensaje: { direccion: "entrante", tipo: "texto", texto: "Ok, gracias", evento: null, creadoEn: hace(53) },
        ultimaActividad: hace(53),
        espera: espera({ disponibles: [ROSA, LAURA], totalDisponibles: 2, botMotivo: "sin_confirmacion", botAtendio: true }),
    } as Partial<ConversacionConEspera> & Pick<Conversacion, "id" | "titulo">),
];

export function detalle(c: Conversacion, extra: Partial<Detalle> = {}): Detalle {
    return {
        ...c,
        vinculos: c.id === 41 ? [{ tipo: "ticket", id: "4812", referencia: "TK-004812", creadoEn: hace(200) }] : [],
        participantes: [],
        clienteResumen:
            c.contacto.tipo === "cliente"
                ? {
                      id: c.contacto.clienteId ?? 0,
                      nombre: c.titulo,
                      codigo: `C-${c.contacto.clienteId ?? 0}`,
                      servicios: [{ id: c.contacto.servicioId ?? 0, codigo: "S-4101", plan: "Fibra 100 Mb", estado: "activo", zona: "Villa Nueva · Norte" }],
                      sincronizadoEn: hace(2),
                  }
                : undefined,
        ...extra,
    };
}

let seq = 0;
export function mensaje(conversacionId: number, datos: Partial<Mensaje> & Pick<Mensaje, "direccion">): Mensaje {
    seq += 1;
    return {
        id: `0193a000-0000-7000-8000-${String(seq).padStart(12, "0")}`,
        conversacionId,
        seq,
        tipo: datos.direccion === "sistema" ? "sistema" : "texto",
        texto: null,
        estadoEntrega: null,
        motivoFalla: null,
        menciones: [],
        creadoEn: hace(300 - seq),
        enviadoEn: null,
        entregadoEn: null,
        leidoEn: null,
        ...datos,
    };
}

export function mensajesDe(id: number): Mensaje[] {
    if (id === 45) {
        return [
            mensaje(45, { direccion: "sistema", evento: { tipo: "conversacion_creada", colaNombre: "Soporte" }, creadoEn: hace(18) }),
            mensaje(45, { direccion: "entrante", texto: "Buenas, se me fue el internet otra vez", creadoEn: hace(18) }),
            mensaje(45, { direccion: "sistema", evento: { tipo: "aviso_sin_atender", colaNombre: "Soporte", disponibles: 2 } as Schema<"EventoSistema">, creadoEn: hace(18) }),
            mensaje(45, { direccion: "entrante", texto: "¿Me pueden ayudar?", creadoEn: hace(11) }),
        ];
    }
    if (id === 46) {
        const bot: MensajeAutomatico = mensaje(46, {
            direccion: "saliente",
            texto: "Gracias por escribir. En este momento todos nuestros agentes están ocupados; te respondemos en cuanto se libere uno.",
            estadoEntrega: "entregado",
            creadoEn: hace(54),
        });
        bot.automatico = "bot";
        return [
            mensaje(46, { direccion: "sistema", evento: { tipo: "conversacion_creada", colaNombre: "Soporte" }, creadoEn: hace(84) }),
            mensaje(46, { direccion: "entrante", texto: "Hola, quiero cambiar mi plan a 200 Mb, ¿cuánto cuesta?", creadoEn: hace(84) }),
            mensaje(46, { direccion: "sistema", evento: { tipo: "aviso_sin_atender", colaNombre: "Soporte", disponibles: 3 } as Schema<"EventoSistema">, creadoEn: hace(84) }),
            mensaje(46, { direccion: "sistema", evento: { tipo: "bot_atendio", detalle: "sin_confirmacion", minutos: 30 } as Schema<"EventoSistema">, creadoEn: hace(54) }),
            bot,
            mensaje(46, { direccion: "entrante", texto: "Ok, gracias", creadoEn: hace(53) }),
        ];
    }
    if (id !== 41) return [mensaje(id, { direccion: "entrante", texto: "Hola, ¿me ayudan?", creadoEn: hace(10) })];
    return [
        mensaje(41, { direccion: "sistema", evento: { tipo: "conversacion_creada", colaNombre: "Soporte" }, creadoEn: hace(220) }),
        mensaje(41, { direccion: "entrante", texto: "Buenos días, no tengo internet desde las 7.", creadoEn: hace(213) }),
        mensaje(41, { direccion: "sistema", evento: { tipo: "ticket_creado", actorNombre: "Ana", referencia: "TK-004812" }, creadoEn: hace(208) }),
        mensaje(41, {
            direccion: "saliente",
            texto: "Hola Carlos, hay una falla en tu zona y ya la estamos atendiendo. Te avisamos por aquí.",
            autor: ANDREA,
            estadoEntrega: "leido",
            creadoEn: hace(205),
        }),
        mensaje(41, { direccion: "nota", texto: "NOC confirma que la caja NAP 14 ya está arriba. Si sigue sin servicio, agendar visita.", autor: ANDREA, creadoEn: hace(30) }),
        mensaje(41, { direccion: "entrante", texto: "Ya lo reinicié y sigue igual.", creadoEn: hace(4) }),
    ];
}
