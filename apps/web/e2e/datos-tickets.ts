import type { Schema } from "@iris/contratos";

/*
 * Fixtures de tickets para la API falsa, tipados con packages/contratos. Las fechas del SLA son
 * relativas a la hora real en que se arma el escenario (el navegador usa la hora real).
 */

type Ticket = Schema<"Ticket">;
type RelojSLA = Schema<"RelojSLA">;
type Mensaje = Schema<"Mensaje">;
type Evento = Schema<"EventoTicket">;

const MIN = 60_000;
const iso = (ms: number) => new Date(ms).toISOString();

export const CATEGORIAS: Schema<"Categoria">[] = [
    { id: 1, codigo: "sin_servicio", nombre: "Sin servicio", slaId: null, requiereOrden: true, activa: true, version: 1 },
    { id: 2, codigo: "intermitencia", nombre: "Intermitencia", slaId: null, requiereOrden: false, activa: true, version: 1 },
    { id: 3, codigo: "lentitud", nombre: "Lentitud", slaId: null, requiereOrden: false, activa: true, version: 1 },
    { id: 4, codigo: "cambio_plan", nombre: "Cambio de plan", slaId: null, requiereOrden: false, activa: true, version: 1 },
];

export const CLIENTES: Schema<"ClienteResumen">[] = [
    { id: 501, empresaId: 10, codigo: "CL-0501", nombre: "María Castillo", telefono: "5555 0101", direccion: "4a calle 3-21, zona 1", lat: 14.64, lon: -90.51, zonaId: 1, estado: "activo" },
    { id: 502, empresaId: 10, codigo: "CL-0502", nombre: "Ferretería El Tornillo", telefono: "5555 0202", direccion: "Km 18 carretera a El Salvador", lat: null, lon: null, zonaId: 2, estado: "activo" },
];

export function ficha(clienteId: number, stale: boolean): Schema<"Ficha"> | undefined {
    const cliente = CLIENTES.find((c) => c.id === clienteId);
    if (!cliente) return undefined;
    return {
        cliente,
        servicios: [
            {
                id: clienteId * 10,
                empresaId: cliente.empresaId,
                clienteId,
                codigo: `SV-${clienteId}`,
                planId: 3,
                planNombre: "Fibra 100 Mb/s",
                routerId: 7,
                routerNombre: "RB-Norte-01",
                zonaId: cliente.zonaId,
                zonaNombre: "Zona norte",
                ip: "10.20.30.40",
                direccion: cliente.direccion,
                lat: cliente.lat,
                lon: cliente.lon,
                estado: "activo",
            },
        ],
        estadoCuenta: stale ? undefined : { saldo: "150.00", moneda: "GTQ", facturasVencidas: 1, montoVencido: "150.00" },
        estadoCuentaError: stale ? "argos_no_disponible" : null,
        stale,
        sincronizadoEn: "2026-10-27T20:15:00Z",
    };
}

function reloj(estado: RelojSLA["estado"], ahora: number, venceEnMin?: number, objetivoMin = 240): RelojSLA {
    if (estado === "sin_sla") return { estado, objetivoMin: null, riesgoEn: null, venceEn: null, cumplidoEn: null };
    if (estado === "pausado") return { estado, objetivoMin, riesgoEn: null, venceEn: null, cumplidoEn: null };
    if (estado === "cumplido" || estado === "incumplido") {
        return { estado, objetivoMin, riesgoEn: iso(ahora - 120 * MIN), venceEn: iso(ahora - 60 * MIN), cumplidoEn: iso(ahora - 90 * MIN) };
    }
    const vence = ahora + (venceEnMin ?? 120) * MIN;
    return { estado, objetivoMin, riesgoEn: iso(vence - objetivoMin * 0.2 * MIN), venceEn: iso(vence), cumplidoEn: null };
}

function base(id: number, ahora: number, parcial: Partial<Ticket>): Ticket {
    return {
        id,
        numero: `TK-${String(100 + id).padStart(6, "0")}`,
        empresaId: null,
        clienteId: null,
        servicioId: null,
        zonaId: null,
        contacto: { nombre: null, telefono: null, correo: null, verificado: true },
        categoriaId: 2,
        prioridad: "media",
        estado: "abierto",
        origen: "panel",
        asunto: `Consulta de servicio ${id}`,
        asignadoA: null,
        cuadrillaId: null,
        nivelEscalamiento: 0,
        slaId: 1,
        slaRespuesta: reloj("cumplido", ahora),
        slaResolucion: reloj("en_tiempo", ahora, 600),
        enEsperaDesde: null,
        cerradoEn: null,
        ticketPadreId: null,
        fusionadoEnId: null,
        version: 1,
        creadoEn: iso(ahora - (200 - id) * MIN),
        actualizadoEn: iso(ahora - (60 - Math.min(id, 59)) * MIN),
        ...parcial,
    };
}

export type Escenario = {
    tickets: Ticket[];
    mensajes: Mensaje[];
    eventos: Evento[];
};

/** Tickets 1–5 con casos de SLA, 6–30 de relleno (dos páginas de 25), 31 cerrado. */
export function escenarioTickets(ahora: number, empresas: readonly number[]): Escenario {
    const empresa = empresas[0] ?? null;
    const tickets: Ticket[] = [
        base(1, ahora, {
            asunto: "Sin internet desde la mañana",
            prioridad: "critica",
            estado: "nuevo",
            categoriaId: 1,
            clienteId: 501,
            servicioId: 5010,
            slaRespuesta: reloj("en_riesgo", ahora, 20, 60),
            slaResolucion: reloj("en_tiempo", ahora, 300),
        }),
        base(2, ahora, {
            asunto: "Intermitencia por las tardes",
            prioridad: "alta",
            estado: "abierto",
            clienteId: 502,
            asignadoA: 1,
            slaResolucion: reloj("vencido", ahora, -45),
        }),
        base(3, ahora, {
            asunto: "Lentitud en horas pico",
            prioridad: "media",
            estado: "en_espera",
            categoriaId: 3,
            asignadoA: 6,
            enEsperaDesde: iso(ahora - 30 * MIN),
            slaResolucion: reloj("pausado", ahora),
            contacto: { nombre: "Jorge Méndez", telefono: "5555 0303", correo: "jorge@correo.test", verificado: true },
        }),
        base(4, ahora, {
            asunto: "Solicitud de cambio de plan",
            prioridad: "baja",
            estado: "resuelto",
            categoriaId: 4,
            slaResolucion: reloj("cumplido", ahora),
        }),
        base(5, ahora, { asunto: "Router reinicia solo", prioridad: "media", estado: "abierto", slaResolucion: reloj("en_tiempo", ahora, 135) }),
    ];
    for (let id = 6; id <= 30; id++) {
        tickets.push(base(id, ahora, { prioridad: id % 3 === 0 ? "baja" : "media", estado: id % 2 === 0 ? "abierto" : "nuevo" }));
    }
    tickets.push(base(31, ahora, { asunto: "Consulta ya atendida", estado: "cerrado", cerradoEn: iso(ahora - 300 * MIN), slaResolucion: reloj("cumplido", ahora) }));
    // 32: nació de un correo sin verificar; 33: incidente de Horus (maestro de 34). Cerrados para no
    // cambiar las cuentas de la bandeja (que muestra los activos).
    const cerrado = { estado: "cerrado" as const, cerradoEn: iso(ahora - 20 * MIN), slaResolucion: reloj("cumplido", ahora) };
    tickets.push(
        base(32, ahora, {
            ...cerrado,
            asunto: "No me llega la factura",
            origen: "correo",
            contacto: { nombre: "Juan Pérez", telefono: null, correo: "juan@correo.test", verificado: false },
        }),
    );
    tickets.push(base(33, ahora, { ...cerrado, asunto: "Router RB-Norte-01 caído", origen: "horus", clienteId: 501, servicioId: 5010, prioridad: "critica" }));
    tickets.push(base(34, ahora, { ...cerrado, asunto: "Sin servicio en zona norte", origen: "telegram", ticketPadreId: 33 }));
    for (const t of tickets) {
        t.empresaId = empresa;
    }
    if (empresas[1] !== undefined) {
        tickets.push(base(40, ahora, { asunto: "Antena desalineada (Fibra Sur)", empresaId: empresas[1], prioridad: "alta" }));
    }

    const mensajes: Mensaje[] = [];
    const eventos: Evento[] = [];
    let idM = 1;
    let idE = 1;
    for (const t of tickets) {
        mensajes.push({
            id: idM++,
            ticketId: t.id,
            publico: true,
            autorTipo: "cliente",
            autorId: null,
            autorNombre: "Cliente",
            canal: "telefono",
            cuerpo: `Hola, ${t.asunto.toLowerCase()}.`,
            adjuntos: null,
            creadoEn: t.creadoEn,
        });
        eventos.push({ id: idE++, ticketId: t.id, tipo: "creado", de: null, a: "nuevo", motivo: null, datos: {}, actorTipo: "usuario", actorId: 1, actorNombre: "Ana López", creadoEn: t.creadoEn });
    }
    mensajes.push({
        id: idM++,
        ticketId: 1,
        publico: false,
        autorTipo: "usuario",
        autorId: 1,
        autorNombre: "Ana López",
        canal: "panel",
        cuerpo: "Revisar la ONT antes de mandar cuadrilla.",
        adjuntos: null,
        creadoEn: iso(ahora - 10 * MIN),
    });
    mensajes.push({
        id: idM++,
        ticketId: 32,
        publico: false,
        autorTipo: "sistema",
        autorId: null,
        autorNombre: "Juan Pérez <juan@correo.test>",
        canal: "correo",
        cuerpo:
            "Correo de juan@correo.test no verificado (el servidor de correo no autenticó el remitente): se guarda como nota interna, no cambia el estado y no recibe las respuestas.\n\n¿Ya pueden enviarme la factura de octubre?",
        adjuntos: null,
        creadoEn: iso(ahora - 15 * MIN),
    });
    eventos.push({ id: idE++, ticketId: 2, tipo: "estado", de: "nuevo", a: "abierto", motivo: null, datos: {}, actorTipo: "usuario", actorId: 1, actorNombre: "Ana López", creadoEn: iso(ahora - 50 * MIN) });
    eventos.push({ id: idE++, ticketId: 2, tipo: "sla_vencido", de: null, a: null, motivo: null, datos: { sla: "resolucion" }, actorTipo: "sistema", actorId: null, actorNombre: "Sistema", creadoEn: iso(ahora - 45 * MIN) });
    return { tickets, mensajes, eventos };
}

export type ResultadoAccion = { estado: number; cuerpo: unknown };

const conflicto = (): ResultadoAccion => ({
    estado: 409,
    cuerpo: { type: "urn:iris:problema:conflicto_version", title: "Conflicto de versión", status: 409, detail: "El ticket cambió desde que lo leíste; vuelve a leerlo" },
});
const transicion = (detalle: string): ResultadoAccion => ({
    estado: 422,
    cuerpo: { type: "urn:iris:problema:transicion_invalida", title: "Transición inválida", status: 422, detail: detalle },
});

/** Aplica una acción sobre el escenario como lo haría la API (sin SLA real). */
export function aplicarAccion(
    esc: Escenario,
    t: Ticket,
    accion: string,
    cuerpo: Record<string, unknown>,
    actor: { id: number; nombre: string },
): ResultadoAccion {
    if (typeof cuerpo.version === "number" && cuerpo.version !== t.version) {
        return conflicto();
    }
    const ahora = new Date().toISOString();
    const evento = (tipo: Evento["tipo"], de: string | null, a: string | null, motivo: string | null) =>
        esc.eventos.push({ id: esc.eventos.length + 1000, ticketId: t.id, tipo, de, a, motivo, datos: {}, actorTipo: "usuario", actorId: actor.id, actorNombre: actor.nombre, creadoEn: ahora });
    const estado = (a: Ticket["estado"], motivo: string | null = null) => {
        evento("estado", t.estado, a, motivo);
        t.estado = a;
    };
    const mensaje = (publico: boolean, texto: string): Mensaje => {
        const m: Mensaje = { id: esc.mensajes.length + 1000, ticketId: t.id, publico, autorTipo: "usuario", autorId: actor.id, autorNombre: actor.nombre, canal: "panel", cuerpo: texto, adjuntos: null, creadoEn: ahora };
        esc.mensajes.push(m);
        return m;
    };
    const motivo = typeof cuerpo.motivo === "string" ? cuerpo.motivo : null;
    let respuesta: unknown;
    switch (accion) {
        case "responder": {
            if (t.estado === "cerrado") return transicion("Un ticket cerrado no se responde");
            const m = mensaje(true, String(cuerpo.cuerpo));
            if (t.estado === "nuevo") estado("abierto");
            t.slaRespuesta = { ...t.slaRespuesta, estado: "cumplido", cumplidoEn: ahora };
            t.version++;
            respuesta = { ticket: t, mensaje: m };
            break;
        }
        case "nota-interna": {
            const m = mensaje(false, String(cuerpo.cuerpo));
            respuesta = { ticket: t, mensaje: m };
            break;
        }
        case "asignar": {
            const nuevo = typeof cuerpo.usuarioId === "number" ? cuerpo.usuarioId : null;
            if (nuevo === t.asignadoA) {
                return { estado: 422, cuerpo: { type: "urn:iris:problema:sin_cambios", title: "Sin cambios", status: 422, detail: "El ticket ya tiene ese responsable" } };
            }
            t.asignadoA = nuevo;
            evento("asignado", null, null, null);
            if (t.estado === "nuevo") estado("abierto");
            t.version++;
            respuesta = t;
            break;
        }
        case "escalar": {
            const p = cuerpo.prioridad as Ticket["prioridad"] | undefined;
            evento("escalado", t.prioridad, p ?? t.prioridad, motivo);
            if (p) t.prioridad = p;
            t.nivelEscalamiento++;
            t.version++;
            respuesta = t;
            break;
        }
        case "poner-en-espera":
            if (t.estado !== "abierto") return transicion("Solo un ticket abierto se pone en espera");
            estado("en_espera", motivo);
            t.slaResolucion = { ...t.slaResolucion, estado: "pausado", venceEn: null };
            t.version++;
            respuesta = t;
            break;
        case "reanudar":
            if (t.estado !== "en_espera") return transicion("Solo un ticket en espera se reanuda");
            estado("abierto", motivo);
            t.version++;
            respuesta = t;
            break;
        case "resolver":
            if (t.estado === "resuelto" || t.estado === "cerrado") return transicion("El ticket ya está resuelto o cerrado");
            mensaje(true, String(cuerpo.solucion));
            estado("resuelto");
            t.slaResolucion = { ...t.slaResolucion, estado: "cumplido", cumplidoEn: ahora };
            t.version++;
            respuesta = t;
            break;
        case "reabrir":
            if (t.estado !== "resuelto") return transicion("Solo un ticket resuelto se reabre");
            estado("abierto", motivo);
            t.version++;
            respuesta = t;
            break;
        case "cerrar":
            if (t.estado === "cerrado") return transicion("El ticket ya está cerrado");
            estado("cerrado", motivo);
            t.cerradoEn = ahora;
            t.version++;
            respuesta = t;
            break;
        case "fusionar": {
            const destino = esc.tickets.find((x) => x.id === cuerpo.destinoId && x.empresaId === t.empresaId);
            if (!destino || destino.id === t.id) {
                return { estado: 422, cuerpo: { type: "urn:iris:problema:fusion_invalida", title: "Fusión inválida", status: 422, detail: "El destino no es válido" } };
            }
            evento("fusionado", t.numero, destino.numero, motivo);
            t.estado = "cerrado";
            t.fusionadoEnId = destino.id;
            t.version++;
            destino.version++;
            respuesta = { origen: t, destino };
            break;
        }
        default:
            return { estado: 404, cuerpo: { type: "urn:iris:problema:no_encontrado", title: "No encontrado", status: 404 } };
    }
    t.actualizadoEn = ahora;
    return { estado: 200, cuerpo: respuesta };
}
