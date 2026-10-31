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
};

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

function etiqueta(ruta: string, nombre: string | null | undefined, ultima: boolean): string | undefined {
    const fija = ETIQUETAS[ruta];
    if (fija) return fija;
    const ficha = FICHAS.find(([patron]) => patron.test(ruta));
    if (!ficha) return undefined;
    return ultima && nombre ? nombre : ficha[1];
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
    tramos.forEach((_, i) => {
        const prefijo = `/${tramos.slice(0, i + 1).join("/")}`;
        const ultima = i === tramos.length - 1;
        const texto = etiqueta(prefijo, nombre, ultima);
        if (texto) migas.push(ultima ? { etiqueta: texto } : { etiqueta: texto, href: prefijo });
    });
    return migas;
}
