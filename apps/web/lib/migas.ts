import type { Miga } from "@iris/ui";

/*
 * Migas del panel a partir de la ruta: cada pantalla con página tiene su etiqueta (la de su
 * título). Las fichas (`/ordenes/[id]`…) muestran el nombre del registro cuando la pantalla ya lo
 * leyó (`useNombreEnMigas`); mientras tanto, el tipo («Orden»).
 */

const ETIQUETAS: Readonly<Record<string, string>> = {
    "/": "Inicio",
    "/tickets": "Tickets",
    "/ordenes": "Órdenes",
    "/agenda": "Agenda",
    "/despacho": "Despacho",
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
    // Formularios en pantalla propia (fase 2).
    "/tickets/nuevo": "Nuevo ticket",
    "/ordenes/nueva": "Nueva orden",
    "/usuarios/nuevo": "Nuevo usuario",
    "/tecnicos/nuevo": "Nuevo técnico",
    "/comercial/nuevo": "Nuevo prospecto",
};

/** Pantallas dentro de una ficha (editar, cotización…): la ficha de arriba lleva el nombre del registro. */
const SUBPANTALLAS: readonly (readonly [RegExp, string])[] = [
    [/^\/usuarios\/[^/]+\/editar$/, "Editar"],
    [/^\/tecnicos\/[^/]+\/editar$/, "Editar"],
    [/^\/tecnicos\/[^/]+\/ausencias\/nueva$/, "Nueva ausencia"],
    [/^\/comercial\/[^/]+\/cotizacion$/, "Cotización"],
    [/^\/administracion\/buzones\/[^/]+\/editar$/, "Editar buzón"],
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
];

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
        if (texto) migas.push(ultima ? { etiqueta: texto } : { etiqueta: texto, href: prefijo });
    });
    return migas;
}
