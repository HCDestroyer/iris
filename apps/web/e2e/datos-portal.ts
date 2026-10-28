import type { Schema } from "@iris/contratos";

/* Fixtures del portal de soporte, tipados con packages/contratos. */

type TicketPortal = Schema<"TicketPortal">;
type MensajePortal = Schema<"MensajePortal">;

export const CLIENTE: Schema<"ClientePortal"> = { clienteId: 77, empresaId: 10, nombre: "María Castillo" };
export const CSRF_PORTAL = "pcsrf-e2e";

export const TICKET_VALIDO = "tkt_valido_e2e_0000000000001";
export const TICKET_VENCIDO = "tkt_vencido_e2e_000000000001";
export const TICKET_LIMITE = "tkt_limite_e2e_0000000000001";
export const TICKET_RED = "tkt_red_e2e_000000000000001";

export const CATEGORIAS: Schema<"CategoriaPortal">[] = [
    { id: 1, codigo: "sin_servicio", nombre: "Sin internet" },
    { id: 2, codigo: "lentitud", nombre: "Internet lento" },
    { id: 3, codigo: "facturacion", nombre: "Cobros y facturas" },
];

function ticket(datos: Partial<TicketPortal> & Pick<TicketPortal, "id" | "asunto" | "estado">): TicketPortal {
    const hora = String(10 + (datos.id % 10)).padStart(2, "0");
    return {
        numero: `TK-${String(datos.id).padStart(6, "0")}`,
        categoriaId: 1,
        categoria: "Sin internet",
        servicioId: null,
        origen: "portal",
        afectadoPorIncidente: false,
        puedeConfirmar: false,
        resueltoEn: null,
        cerradoEn: null,
        version: 1,
        creadoEn: `2026-10-27T${hora}:00:00Z`,
        actualizadoEn: `2026-10-28T${hora}:30:00Z`,
        ...datos,
    };
}

function mensaje(id: number, autorTipo: MensajePortal["autorTipo"], cuerpo: string, autorNombre: string, minuto: number): MensajePortal {
    return { id, autorTipo, autorNombre, cuerpo, adjuntos: [], creadoEn: `2026-10-28T14:${String(minuto).padStart(2, "0")}:00Z` };
}

/** Escenario nuevo por prueba (el estado cambia con las acciones). */
export function escenarioPortal() {
    const tickets: TicketPortal[] = [
        ticket({ id: 501, asunto: "Sin internet desde la mañana", estado: "abierto", afectadoPorIncidente: true }),
        ticket({ id: 502, asunto: "El módem se reinicia solo", estado: "resuelto", puedeConfirmar: true, categoria: "Internet lento", categoriaId: 2, resueltoEn: "2026-10-28T14:20:00Z", version: 4 }),
        ticket({ id: 503, asunto: "Cambio de contraseña del WiFi", estado: "cerrado", categoria: null, categoriaId: null, cerradoEn: "2026-10-26T12:00:00Z" }),
    ];
    // Relleno para «Cargar más» (la primera página del portal es de 20).
    for (let i = 0; i < 20; i++) {
        tickets.push(ticket({ id: 400 + i, asunto: `Consulta anterior ${i + 1}`, estado: i % 2 === 0 ? "en_espera" : "nuevo", categoria: "Cobros y facturas", categoriaId: 3 }));
    }
    const mensajes = new Map<number, MensajePortal[]>([
        [
            501,
            [
                mensaje(1, "cliente", "Desde las 7 no tengo internet. La luz LOS del módem está en rojo.", "María Castillo", 0),
                mensaje(2, "agente", "Hola, María. Estamos revisando tu conexión.", "Sofía Morales", 5),
                mensaje(3, "sistema", "Detectamos una falla general en tu zona y ya hay un equipo trabajando.", "Soporte", 10),
            ],
        ],
        [
            502,
            [
                mensaje(4, "cliente", "El módem se reinicia varias veces al día.", "María Castillo", 1),
                mensaje(5, "agente", "Actualizamos el firmware de tu módem. ¿Sigue reiniciándose?", "Sofía Morales", 20),
            ],
        ],
        [503, [mensaje(6, "cliente", "¿Cómo cambio la contraseña del WiFi?", "María Castillo", 2)]],
    ]);
    return { tickets, mensajes, siguienteTicket: 900, siguienteMensaje: 100 };
}
