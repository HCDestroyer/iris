import type { Miga } from "@iris/ui";

/*
 * Migas del panel a partir de la ruta: cada pantalla con página tiene su etiqueta (la de su
 * título). Las fichas (`/ordenes/[id]`…) muestran el nombre del registro cuando la pantalla ya lo
 * leyó (`useNombreEnMigas`); mientras tanto, el tipo («Orden»).
 */

const ETIQUETAS: Readonly<Record<string, string>> = {
    "/": "Inicio",
    "/tickets": "Tickets",
    "/conversaciones": "Conversaciones",
    "/conversaciones/nueva": "Nueva conversación",
    "/equipo": "Equipo",
    "/ordenes": "Órdenes",
    "/agenda": "Agenda",
    "/despacho": "Despacho",
    "/mapa": "Mapa",
    "/tecnicos": "Técnicos",
    "/usuarios": "Usuarios",
    "/comercial": "Comercial",
    "/encuestas": "Encuestas",
    "/reportes": "Reportes",
    "/cuenta": "Mi perfil",
    "/cuenta/mfa": "Verificación en dos pasos",
    "/administracion": "Administración",
    "/administracion/accesos": "Accesos",
    "/administracion/actividad-app": "Actividad de la app",
    "/administracion/buzones": "Buzones de correo",
    "/administracion/cuadrillas": "Cuadrillas",
    "/administracion/zonas": "Zonas",
    "/administracion/datos-sin-empresa": "Datos sin empresa",
    "/administracion/integraciones": "Integraciones",
    "/administracion/integraciones/argos/vuelta": "Conectar con Argos",
    "/administracion/plantillas": "Plantillas de formulario",
    "/administracion/plantillas/nueva": "Nueva plantilla",
    "/administracion/roles": "Roles y permisos",
    "/administracion/roles/nuevo": "Nuevo rol",
    "/administracion/buzones/nuevo": "Nuevo buzón",
    "/administracion/numeros": "Números de WhatsApp",
    "/administracion/numeros/nuevo": "Agregar número",
    "/administracion/numeros/nuevo/api": "Con datos de la API",
    "/administracion/numeros/nuevo/qr": "Con QR",
    "/administracion/conexion-meta": "Conexión con Meta",
    "/administracion/numeros/guia": "Cómo conectar WhatsApp",
    "/administracion/colas": "Colas",
    "/administracion/colas/nueva": "Nueva cola",
    "/administracion/bandeja": "Ajustes de la bandeja",
    "/administracion/asistente": "Asistente de IA",
    "/administracion/horarios": "Horarios",
    "/administracion/horarios/nuevo": "Nuevo horario",
    "/administracion/manuales": "Biblioteca de manuales",
    "/administracion/manuales/nuevo": "Nuevo manual",
    "/administracion/respuestas-rapidas": "Respuestas rápidas",
    "/administracion/respuestas-rapidas/nueva": "Nueva respuesta",
    // Formularios en pantalla propia (fase 2).
    "/tickets/nuevo": "Nuevo ticket",
    "/ordenes/nueva": "Nueva orden",
    "/usuarios/nuevo": "Nuevo usuario",
    "/tecnicos/nuevo": "Nuevo técnico",
    "/tecnicos/argos": "Técnicos de Argos sin ficha",
    "/comercial/nuevo": "Nuevo prospecto",
};

/** Pantallas dentro de una ficha (editar, cotización…): la ficha de arriba lleva el nombre del registro. */
const SUBPANTALLAS: readonly (readonly [RegExp, string])[] = [
    [/^\/usuarios\/[^/]+\/editar$/, "Editar"],
    [/^\/tecnicos\/[^/]+\/editar$/, "Editar"],
    [/^\/tecnicos\/[^/]+\/ausencias\/nueva$/, "Nueva ausencia"],
    [/^\/comercial\/[^/]+\/cotizacion$/, "Cotización"],
    [/^\/administracion\/buzones\/[^/]+\/editar$/, "Editar buzón"],
    [/^\/administracion\/respuestas-rapidas\/[^/]+\/editar$/, "Editar respuesta"],
    [/^\/conversaciones\/[^/]+\/prospecto$/, "Nuevo prospecto"],
    [/^\/conversaciones\/[^/]+\/orden$/, "Nueva orden"],
    [/^\/conversaciones\/[^/]+\/boleta$/, "Boleta de pago"],
    [/^\/administracion\/numeros\/[^/]+\/configurar$/, "Configurar número"],
    [/^\/administracion\/numeros\/[^/]+\/conectar$/, "Conectar"],
    [/^\/administracion\/numeros\/[^/]+\/credenciales$/, "Credenciales"],
    [/^\/administracion\/numeros\/[^/]+\/conectado$/, "Conectado"],
    [/^\/administracion\/numeros\/[^/]+\/compartir$/, "Compartir"],
    [/^\/administracion\/numeros\/[^/]+\/prestamo$/, "Préstamo"],
    [/^\/administracion\/colas\/[^/]+\/editar$/, "Editar cola"],
    [/^\/administracion\/horarios\/[^/]+\/editar$/, "Editar horario"],
    [/^\/administracion\/manuales\/[^/]+\/editar$/, "Editar manual"],
];

/** Fichas: la etiqueta mientras no se conoce el nombre del registro. */
const FICHAS: readonly (readonly [RegExp, string])[] = [
    [/^\/tickets\/[^/]+$/, "Ticket"],
    [/^\/ordenes\/[^/]+$/, "Orden"],
    [/^\/tecnicos\/[^/]+$/, "Técnico"],
    [/^\/usuarios\/[^/]+$/, "Usuario"],
    [/^\/comercial\/[^/]+$/, "Prospecto"],
    [/^\/administracion\/plantillas\/[^/]+$/, "Plantilla"],
    [/^\/administracion\/roles\/[^/]+$/, "Rol"],
    [/^\/administracion\/numeros\/[^/]+$/, "Número"],
];

/**
 * Fichas sin pantalla propia: su miga lleva el nombre del registro pero no es enlace (y «‹» en el
 * teléfono lleva a la lista). Un número de WhatsApp solo tiene sus subpantallas.
 */
const FICHAS_SIN_PAGINA: readonly RegExp[] = [/^\/administracion\/numeros\/[^/]+$/];

/** `conNombre`: este tramo es la ficha del registro cuyo nombre se conoce (la última ficha de la ruta). */
function etiqueta(ruta: string, nombre: string | null | undefined, conNombre: boolean): string | undefined {
    const fija = ETIQUETAS[ruta];
    if (fija) return fija;
    const sub = SUBPANTALLAS.find(([patron]) => patron.test(ruta));
    if (sub) return sub[1];
    const ficha = FICHAS.find(([patron]) => patron.test(ruta));
    if (!ficha) return undefined;
    return conNombre && nombre ? nombre : ficha[1];
}

/**
 * «Órdenes › ORD-1042», «Administración › Roles y permisos › Supervisor». La última es la
 * pantalla actual (sin enlace). Los tramos sin pantalla propia (p. ej. `/integraciones/argos`) se
 * saltan.
 */
export function migasDeRuta(ruta: string, nombre?: string | null): Miga[] {
    const limpia = ruta.length > 1 ? ruta.replace(/\/+$/, "") : ruta;
    if (limpia === "/") return [{ etiqueta: "Inicio" }];
    const tramos = limpia.split("/").filter(Boolean);
    const migas: Miga[] = [];
    const prefijos = tramos.map((_, i) => `/${tramos.slice(0, i + 1).join("/")}`);
    // El nombre va en la ficha más profunda («Usuarios › Ana López › Editar»).
    let ultimaFicha = -1;
    prefijos.forEach((p, i) => {
        if (!ETIQUETAS[p] && FICHAS.some(([patron]) => patron.test(p))) ultimaFicha = i;
    });
    prefijos.forEach((prefijo, i) => {
        const ultima = i === tramos.length - 1;
        const texto = etiqueta(prefijo, nombre, i === ultimaFicha);
        const sinPagina = !ETIQUETAS[prefijo] && FICHAS_SIN_PAGINA.some((patron) => patron.test(prefijo));
        if (texto) migas.push(ultima || sinPagina ? { etiqueta: texto } : { etiqueta: texto, href: prefijo });
    });
    return migas;
}
