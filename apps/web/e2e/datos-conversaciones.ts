import type { Schema } from "@iris/contratos";

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

export function conversacion(datos: Partial<Conversacion> & Pick<Conversacion, "id" | "titulo">): Conversacion {
    return {
        empresaId: 1,
        version: 1,
        canal: CANAL_SOPORTE,
        cola: { id: 2, nombre: "Soporte" },
        numero: "+50255120041",
        perfilNombre: null,
        contacto: { tipo: "desconocido", clienteId: null, servicioId: null, contactoIrisId: null, prospectoId: null, asociacion: null },
        estado: "abierta",
        reabiertaEn: null,
        asignado: null,
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
        contacto: { tipo: "cliente", clienteId: 4101, servicioId: 41010, contactoIrisId: null, prospectoId: null, asociacion: "automatica" },
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
        contacto: { tipo: "cliente", clienteId: 4102, servicioId: 41020, contactoIrisId: null, prospectoId: null, asociacion: "automatica" },
        sla: { estado: "vencido", venceEn: hace(5), esperaDesde: hace(40) },
        ultimoMensaje: { direccion: "entrante", tipo: "imagen", texto: null, evento: null, creadoEn: hace(7) },
        noLeidos: 1,
        ultimaActividad: hace(7),
    }),
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
    }),
    conversacion({
        id: 44,
        titulo: "Ferretería El Tornillo, Sociedad Anónima de Capital Variable",
        numero: "+50255660044",
        contacto: { tipo: "cliente", clienteId: 4104, servicioId: 41040, contactoIrisId: null, prospectoId: null, asociacion: "manual" },
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
                : null,
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
        autor: null,
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
