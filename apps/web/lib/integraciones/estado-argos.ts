import type { components } from "@iris/contratos";
import type { Tono } from "@iris/ui";

/**
 * Presentación de la pantalla de estado de la conexión con Argos (contrato 0.8.0 §2.5 d). Solo lo
 * que Iris sabe; nunca un total calculado sobre datos que no se leyeron (las listas por cursor no
 * dicen cuántas filas hay: «1 200 leídos hasta ahora», no «1 200 de 3 000»).
 */
export type EstadoConexionArgos = components["schemas"]["EstadoConexionArgos"];
export type SincronizacionArgos = components["schemas"]["Sincronizacion"];
export type EtapaSincronizacion = components["schemas"]["EtapaSincronizacion"];
export type Recurso = EtapaSincronizacion["recurso"];

export const NOMBRE_RECURSO: Record<Recurso, string> = {
    empresas: "Empresas",
    zonas: "Zonas",
    planes: "Planes",
    routers: "Routers",
    cuentas_bancarias: "Cuentas bancarias",
    bancos: "Bancos",
    clientes: "Clientes",
    servicios: "Servicios",
};

const UNIDAD: Record<Recurso, [string, string]> = {
    empresas: ["empresa", "empresas"],
    zonas: ["zona", "zonas"],
    planes: ["plan", "planes"],
    routers: ["router", "routers"],
    cuentas_bancarias: ["cuenta", "cuentas"],
    bancos: ["banco", "bancos"],
    clientes: ["cliente", "clientes"],
    servicios: ["servicio", "servicios"],
};

const numero = new Intl.NumberFormat("es-GT");

export function cantidad(n: number, recurso: Recurso): string {
    const [uno, varios] = UNIDAD[recurso];
    return `${numero.format(n)} ${n === 1 ? uno : varios}`;
}

export const TONO_ESTADO: Record<EtapaSincronizacion["estado"], Tono> = {
    pendiente: "neutral",
    en_curso: "info",
    completa: "success",
    con_errores: "warning",
};

export const TEXTO_ESTADO: Record<EtapaSincronizacion["estado"], string> = {
    pendiente: "Pendiente",
    en_curso: "En curso",
    completa: "Completa",
    con_errores: "Con errores",
};

/** La línea de detalle de una etapa: lo leído (nunca «x de y» sin total), lo omitido y por qué espera. */
export function detalleEtapa(e: EtapaSincronizacion, anterior: EtapaSincronizacion | undefined): string {
    if (e.estado === "pendiente") {
        if (e.recurso === "servicios" && anterior && anterior.estado !== "completa" && anterior.estado !== "con_errores") return "Espera a los clientes";
        return "Todavía no empieza";
    }
    const total = e.total ?? null;
    let texto: string;
    if (e.estado === "en_curso") {
        texto = total !== null ? `${numero.format(e.procesados)} de ${cantidad(total, e.recurso)}` : `${cantidad(e.procesados, e.recurso)} leídos hasta ahora`;
    } else if (e.estado === "con_errores" && e.procesados === 0) {
        texto = "No se cargó";
    } else {
        texto = cantidad(e.procesados, e.recurso);
    }
    if (e.omitidos > 0) {
        texto += ` · ${numero.format(e.omitidos)} sin guardar (eliminados en Argos o sin cliente en Iris)`;
    }
    return texto;
}

export const TONO_SINCRONIZACION: Record<SincronizacionArgos["estado"], Tono> = { en_curso: "info", completa: "success", con_errores: "warning" };
export const TEXTO_SINCRONIZACION: Record<SincronizacionArgos["estado"], string> = { en_curso: "En curso", completa: "Completa", con_errores: "Con errores" };

export function textoInforme(s: SincronizacionArgos, fecha: (iso: string) => string): string {
    switch (s.informeEstado) {
        case "informado":
            return s.informadoEn ? `Avance informado a Argos el ${fecha(s.informadoEn)}.` : "Avance informado a Argos.";
        case "pendiente":
            // excluirRetirados va con el informe: los dos solo con Argos ≥ 0.8.0.
            return s.excluirRetirados ?"El avance se informa a Argos en un momento." : "Argos no recibe el avance con su versión.";
        case "descartado":
            return "Argos ya tenía un informe más nuevo; este se descartó.";
        case "no_soportado":
            return "Este Argos no recibe el avance (anterior a 0.8.0).";
        case "error":
            return "No se pudo informar el avance a Argos; se reintenta solo.";
    }
}

/** El botón: «Reanudar» con una en curso o con errores, «Sincronizar de nuevo» si ya terminó, «Sincronizar» sin ninguna. */
export function textoBotonSincronizar(s: SincronizacionArgos | undefined): string {
    if (!s) return "Sincronizar";
    return s.estado === "completa" ? "Sincronizar de nuevo" : "Reanudar";
}

/** El error más reciente de una etapa en curso que sigue sola tras un corte. */
export function corteEnCurso(s: SincronizacionArgos): EtapaSincronizacion | undefined {
    if (s.estado !== "en_curso") return undefined;
    return s.etapas?.find((e) => e.estado === "en_curso" && e.error);
}

/** Cuántas cosas hay por revisar en «Qué falló» (solo con lo leído). */
export function porRevisar(f: EstadoConexionArgos["fallos"]): number {
    const pendientes = (f.pendientes ?? []).reduce((n, p) => n + p.cuantos, 0);
    return f.webhooksConError + f.webhooksRechazados + pendientes;
}

/** Huella SHA-256 en pares separados por «:» (como la muestran los navegadores). */
export function huellaLegible(hex: string): string {
    return (hex.match(/.{1,2}/g) ?? []).join(":").toUpperCase();
}
