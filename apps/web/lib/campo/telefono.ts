import type { Schema } from "@iris/contratos";
import type { Conexion, OrdenVista, TelefonoOrden } from "@iris/sync";
import type { MotivoTelefono, TelefonoMio } from "@iris/telefono";
import { principal, telefonos, type Telefono } from "@/lib/datos/telefono";

/*
 * Llamar desde la app de campo (E4c fase 3, contratos/voz.md §3.12; maqueta aprobada: pantallas 23 a
 * 36). Dos formas:
 *  - «Llamar por la empresa»: la extensión del técnico por el puente de Iris (el cliente ve el número
 *    de la empresa y queda en la orden). Solo con señal y con el teléfono disponible para la app.
 *  - «Llamar desde mi celular»: `tel:` con el número de la orden guardada (también sin señal); se
 *    anota en la orden con `llamada.directa` del lote (`contratos/sync.md` §2).
 * Aquí, lo que no depende de React: qué números ofrece la orden, qué forma se ofrece y los textos.
 */

export type Llamada = Schema<"Llamada">;

/** Un número de la orden que se puede elegir: su clave para el servidor y cómo se ve. */
export type NumeroOrden = { clave: TelefonoOrden; etiqueta: string; telefono: Telefono };

const ETIQUETA: Record<TelefonoOrden, string> = { contacto: "Contacto en sitio", movil: "Móvil", principal: "Principal" };

/**
 * Los números de la orden, en el orden en que el servidor los prefiere (contacto en sitio, móvil,
 * principal), uno por clave y solo los que se pueden marcar. El servidor pone el número con la misma
 * clave (de la orden o de la instantánea de Argos): nunca viaja el que tiene el teléfono.
 */
export function numerosDeOrden(orden: Pick<OrdenVista, "contacto" | "cliente">): NumeroOrden[] {
    const deArgos = orden.cliente ? (orden.cliente.telefonos ?? telefonos("telefono", orden.cliente.telefono)) : [];
    const candidatos: [TelefonoOrden, Telefono | null][] = [
        ["contacto", telefonos("telefono", orden.contacto.telefono).find((t) => t.e164 !== null) ?? null],
        ["movil", deArgos.find((t) => t.tipo === "movil" && t.e164 !== null) ?? null],
        ["principal", deArgos.find((t) => t.tipo !== "movil" && t.e164 !== null) ?? null],
    ];
    const vistos = new Set<string>();
    const salida: NumeroOrden[] = [];
    for (const [clave, t] of candidatos) {
        if (!t?.e164 || vistos.has(t.e164)) continue;
        vistos.add(t.e164);
        const tipo = clave === "contacto" && t.tipo === "movil" ? " · móvil" : "";
        salida.push({ clave, etiqueta: ETIQUETA[clave] + tipo, telefono: t });
    }
    // Sin ninguno interpretable, el primero que haya (para «Desde mi celular»; el servidor dirá sin_contacto).
    if (salida.length === 0) {
        const primero = principal([...telefonos("telefono", orden.contacto.telefono), ...deArgos]);
        if (primero) salida.push({ clave: orden.contacto.telefono ? "contacto" : "principal", etiqueta: ETIQUETA[orden.contacto.telefono ? "contacto" : "principal"], telefono: primero });
    }
    return salida;
}

/** `tel:` con el número en E.164 (o como vino si no se pudo interpretar). */
export function enlaceTelefono(t: Telefono): string {
    return `tel:${(t.e164 ?? t.original).replace(/[^\d+]/g, "")}`;
}

/** Por qué no se ofrece «Llamar por la empresa». */
export type SinEmpresa = "sin_senal" | "sin_extension" | "no_disponible" | "revisando";

/**
 * ¿Se ofrece «Llamar por la empresa»? Solo con señal y con el teléfono disponible para la app
 * (`GET /telefono`: `disponible` y `dispositivos` con `app`, y permiso de llamar).
 */
export function formaEmpresa(conexion: Conexion, mio: TelefonoMio | null): { ofrecer: true } | { ofrecer: false; motivo: SinEmpresa } {
    if (conexion === "sin_conexion") return { ofrecer: false, motivo: "sin_senal" };
    if (!mio) return { ofrecer: false, motivo: "revisando" };
    if (mio.disponible && (mio.dispositivos ?? []).includes("app") && mio.puede.llamar) return { ofrecer: true };
    return { ofrecer: false, motivo: sinExtension(mio.motivo) ? "sin_extension" : "no_disponible" };
}

function sinExtension(m: MotivoTelefono | null | undefined): boolean {
    return m === "sin_extension" || m === "extension_escritorio" || m === "sin_credencial";
}

/** El aviso de la hoja cuando no se ofrece por la empresa (pantallas 24b y 31). */
export function textoSinEmpresa(motivo: SinEmpresa): { titulo: string; detalle: string } | null {
    switch (motivo) {
        case "sin_senal":
            return { titulo: "Sin señal", detalle: "«Llamar por la empresa» vuelve cuando haya señal. Desde tu celular sí puedes llamar ahora." };
        case "sin_extension":
            return { titulo: "Todavía no tienes extensión de la empresa", detalle: "La asigna un administrador en Iris. Mientras tanto puedes llamar desde tu celular." };
        case "no_disponible":
            return { titulo: "Llamar por la empresa no está disponible", detalle: "El teléfono de la empresa está apagado en Iris. Mientras tanto puedes llamar desde tu celular." };
        case "revisando":
            return null;
    }
}

/**
 * ¿La señal alcanza para el audio? Con la Network Information API (Chrome de Android): en 2G o con
 * más de 1,5 s de ida y vuelta, no (el cliente no te oiría bien). Sin la API, sí (no se sabe).
 */
export function senalDebil(red: { effectiveType?: string; rtt?: number } | undefined): boolean {
    if (!red) return false;
    if (red.effectiveType === "slow-2g" || red.effectiveType === "2g") return true;
    return typeof red.rtt === "number" && red.rtt > 1500;
}

/** Cómo terminó la llamada por la empresa, para el aviso de la orden (pantallas 34 y 36). */
export type FinLlamada = "terminada" | "no_contesto" | "ocupado" | "fallo";

/** La causa de JsSIP (https://jssip.net/documentation/3.x/api/causes/) en lo que ve el técnico. */
export function finDeLlamada(causa: string | undefined, contestada: boolean): FinLlamada {
    if (contestada) return "terminada";
    switch (causa) {
        case "Busy":
            return "ocupado";
        case "No Answer":
        case "Unavailable":
        case "Rejected":
        case "Canceled":
        case "Not Found":
            return "no_contesto";
        default:
            return "fallo";
    }
}

/** Duración en llano: «3 min 12 s», «45 s». */
export function duracionLlana(segundos: number): string {
    const s = Math.max(0, Math.round(segundos));
    if (s < 60) return `${s} s`;
    const m = Math.floor(s / 60);
    const r = s % 60;
    return r === 0 ? `${m} min` : `${m} min ${r} s`;
}

/** Una fila de la tarjeta «Llamadas» de la orden (pantallas 34 y 35). */
export type FilaLlamada = { id: string; tipo: "empresa" | "directa"; titulo: string; detalle: string; tono: "success" | "warning" | "neutral"; pendiente: boolean };

const hora = (iso: string) => new Intl.DateTimeFormat("es-GT", { hour: "2-digit", minute: "2-digit", hour12: false, timeZone: "America/Guatemala" }).format(Date.parse(iso));

/** Las llamadas de la orden (del servidor y las directas que siguen en la cola), de la más nueva a la más vieja. */
export function filasLlamadas(servidor: readonly Llamada[], pendientes: readonly { id: string; ocurridoEn: string }[], extension: string | null): FilaLlamada[] {
    const filas: (FilaLlamada & { en: string })[] = [];
    const ids = new Set(servidor.map((l) => l.id));
    for (const l of servidor) {
        const en = l.sonoEn ?? l.terminadaEn ?? new Date(0).toISOString();
        if (l.origen === "celular") {
            filas.push({ id: l.id, tipo: "directa", titulo: "Llamada directa desde tu celular", detalle: `${hora(en)} · sin duración`, tono: "neutral", pendiente: false, en });
            continue;
        }
        const quien = extension ? `Por la empresa · tú (ext. ${extension})` : "Por la empresa";
        const contesto = l.contestadaEn !== null && l.contestadaEn !== undefined;
        const resultado = contesto ? `${duracionLlana(l.duracionSeg ?? 0)} · Contestó` : l.estado === "en_curso" || l.estado === "marcando" || l.estado === "conectando" ? "En curso" : "No contestó";
        filas.push({ id: l.id, tipo: "empresa", titulo: quien, detalle: `${hora(en)} · ${resultado}`, tono: contesto ? "success" : "warning", pendiente: false, en });
    }
    for (const p of pendientes) {
        if (ids.has(p.id)) continue;
        filas.push({ id: p.id, tipo: "directa", titulo: "Llamada directa desde tu celular", detalle: hora(p.ocurridoEn), tono: "neutral", pendiente: true, en: p.ocurridoEn });
    }
    return filas.sort((a, b) => Date.parse(b.en) - Date.parse(a.en)).map((f) => ({ id: f.id, tipo: f.tipo, titulo: f.titulo, detalle: f.detalle, tono: f.tono, pendiente: f.pendiente }));
}

export { hora as horaLlamada };
