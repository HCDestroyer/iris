// Inicio de la app de campo con el diseño «combinada» (nov 2026): «Hoy» con la siguiente orden en
// grande, los mosaicos y la lista «Después»; la agenda de «Mañana» y «Semana» con la tira de 7 días;
// el encabezado que se va al bajar y vuelve compacto al subir (y la isla que se encoge), y la hoja
// de acciones rápidas del «+». Sin DOM ni React: lo dibuja cada app. Compartido con la app nativa.
import type { OrdenCampo, PlantillaFormulario, RutaCampo, Traslado } from "../contrato";
import type { OperacionTrasladoLocal } from "../modelo";
import type { OrdenVista } from "../motor";
import { ZONA, hoyEnGuatemala } from "./fechas";
import { llego, progresoPasos, siguientePaso } from "./pasos";
import { accionesDe } from "./reglas";
import { porRecibir } from "./traslados";
import type { Vista } from "./vista";

// ---------------------------------------------------------------------------------------------
// Encabezado
// ---------------------------------------------------------------------------------------------

const encabezado = new Intl.DateTimeFormat("es-GT", { timeZone: ZONA, weekday: "long", day: "numeric", month: "long" });

/** «Jueves 5 de noviembre» (Guatemala), para el encabezado. */
export function fechaEncabezado(ms = Date.now()): string {
    const partes = encabezado.formatToParts(ms);
    const de = (t: Intl.DateTimeFormatPartTypes) => partes.find((p) => p.type === t)?.value ?? "";
    const dia = de("weekday");
    return `${dia.charAt(0).toUpperCase()}${dia.slice(1)} ${de("day")} de ${de("month")}`;
}

/** Primer nombre para «Hola, Tomás». */
export function primerNombre(nombre: string): string {
    return nombre.trim().split(/\s+/)[0] ?? "";
}

/** «TH»: iniciales del botón de la cuenta (dos como máximo). */
export function iniciales(nombre: string): string {
    const partes = nombre.trim().split(/\s+/).filter(Boolean);
    const letras = partes.length > 1 ? [partes[0], partes[partes.length - 1]] : partes;
    return letras
        .map((p) => p?.charAt(0) ?? "")
        .join("")
        .toUpperCase();
}

/** Nombre del cliente de una orden (el de Argos o el contacto de la orden). */
export function nombreCliente(o: Pick<OrdenCampo, "cliente" | "contacto">): string {
    return o.cliente?.nombre ?? o.contacto.nombre ?? "Cliente sin nombre";
}

const sinAcentos = (s: string) =>
    s
        .normalize("NFD")
        .replace(/[\u0300-\u036f]/g, "")
        .toLowerCase();

/**
 * Botón «Buscar» del encabezado: órdenes del teléfono por número, cliente o dirección (sin
 * acentos ni mayúsculas). Funciona sin señal. Vacío sin texto.
 */
export function buscarOrdenes<O extends Pick<OrdenCampo, "numero" | "direccion" | "cliente" | "contacto">>(ordenes: readonly O[], texto: string, maximo = 8): O[] {
    const q = sinAcentos(texto.trim());
    if (!q) {
        return [];
    }
    return ordenes.filter((o) => sinAcentos(`${o.numero} ${nombreCliente(o)} ${o.direccion}`).includes(q)).slice(0, maximo);
}

// ---------------------------------------------------------------------------------------------
// Hoy
// ---------------------------------------------------------------------------------------------

const TERMINADAS: ReadonlySet<string> = new Set(["cerrada", "fallida", "cancelada"]);
const EN_CURSO: ReadonlySet<string> = new Set(["en_camino", "en_sitio", "pausada"]);

type OrdenHoy = Pick<OrdenVista, "id" | "estado" | "reasignada">;

export interface ResumenHoy<O extends OrdenHoy> {
    /** La que va en grande: la primera en curso; si no hay, la primera por hacer. */
    siguiente: O | null;
    /** Las demás por hacer, en orden de ruta o de cita. */
    despues: O[];
    terminadas: O[];
    /** Por hacer (con la siguiente). */
    activas: number;
}

/**
 * Reparte las órdenes del teléfono (ya vienen en orden de ruta publicada o por hora de cita) en
 * la siguiente, «Después» y las terminadas. Una reasignada cuenta como terminada.
 */
export function resumenHoy<O extends OrdenHoy>(ordenes: readonly O[]): ResumenHoy<O> {
    const activas = ordenes.filter((o) => !TERMINADAS.has(o.estado) && !o.reasignada);
    const terminadas = ordenes.filter((o) => !activas.includes(o));
    const siguiente = activas.find((o) => EN_CURSO.has(o.estado)) ?? activas[0] ?? null;
    return { siguiente, despues: activas.filter((o) => o !== siguiente), terminadas, activas: activas.length };
}

type OrdenAccion = Parameters<typeof progresoPasos>[0] & Pick<OrdenVista, "id" | "reasignada" | "tipo">;

/**
 * Botón principal de la siguiente orden: «Continuar trabajo» (con el trabajo empezado, al primer
 * paso que falta), «Empezar trabajo» (a la llegada) o «Ver orden» (al resumen).
 */
export function accionPrincipal(orden: OrdenAccion): { texto: string; vista: Vista } {
    const acciones = accionesDe(orden);
    if (llego(orden) && (acciones.trabajo || acciones.cerrar)) {
        return { texto: "Continuar trabajo", vista: { tipo: "orden", id: orden.id, paso: siguientePaso(orden) } };
    }
    if (acciones.llegar || acciones.iniciarCamino || acciones.reanudar) {
        const empezado = orden.estado === "en_camino" || orden.estado === "pausada";
        return { texto: empezado ? "Continuar trabajo" : "Empezar trabajo", vista: { tipo: "orden", id: orden.id, paso: "llegada" } };
    }
    return { texto: "Ver orden", vista: { tipo: "orden", id: orden.id } };
}

/** Anillo de avance «2/4» de la siguiente orden (pasos de trabajo listos, sin la firma). */
export function anilloAvance(orden: Parameters<typeof progresoPasos>[0]): { listos: number; total: number; texto: string; etiqueta: string; fraccion: number } {
    const { listos, total } = progresoPasos(orden);
    return { listos, total, texto: `${listos}/${total}`, etiqueta: `${listos} de ${total} pasos listos`, fraccion: total > 0 ? listos / total : 0 };
}

/**
 * Mosaico «Ruta de hoy»: cuántas órdenes quedan y cómo van ordenadas. Solo con el paquete del día
 * en el teléfono (`fecha`): nunca un total sobre datos que no se bajaron. La ruta del teléfono no
 * trae distancia: no se muestran kilómetros.
 */
export function mosaicoRuta(estado: { fecha: string | null; ordenes: readonly OrdenHoy[]; ruta: Pick<RutaCampo, "ordenIds"> | null }): {
    valor: string;
    detalle: string;
} | null {
    if (estado.fecha === null) {
        return null;
    }
    const { activas, terminadas } = resumenHoy(estado.ordenes);
    const conRuta = (estado.ruta?.ordenIds?.length ?? 0) > 0;
    return {
        valor: `${activas} ${activas === 1 ? "orden" : "órdenes"}`,
        detalle: `${conRuta ? "En orden de ruta" : "Por hora de cita"}${terminadas.length > 0 ? ` · ${terminadas.length} ${terminadas.length === 1 ? "terminada" : "terminadas"}` : ""}`,
    };
}

/**
 * Mosaico «Por recibir»: traslados por recibir y sus productos. `null` mientras no se hayan
 * leído (sin señal o con error): el mosaico dice que no se pudo leer, sin inventar un total.
 */
export function mosaicoPorRecibir(traslados: readonly Traslado[] | null, recepciones: readonly OperacionTrasladoLocal[]): { cantidad: number; valor: string; detalle: string } | null {
    if (!traslados) {
        return null;
    }
    const lista = porRecibir(traslados, recepciones);
    const productos = lista.reduce((n, t) => n + (t.lineas ?? []).length, 0);
    return {
        cantidad: lista.length,
        valor: lista.length === 0 ? "Nada" : `${lista.length} ${lista.length === 1 ? "traslado" : "traslados"}`,
        detalle: lista.length === 0 ? "Todo recibido" : `${productos} ${productos === 1 ? "producto" : "productos"} de bodega`,
    };
}

// ---------------------------------------------------------------------------------------------
// Agenda: Mañana y Semana
// ---------------------------------------------------------------------------------------------

const diaSemana = new Intl.DateTimeFormat("es-GT", { timeZone: "UTC", weekday: "short" });
const tituloDia = new Intl.DateTimeFormat("es-GT", { timeZone: "UTC", weekday: "long", day: "numeric", month: "long" });
const DIA_MS = 24 * 60 * 60 * 1000;

/** `YYYY-MM-DD` + n días (calendario, sin zona). */
export function sumarDias(fecha: string, n: number): string {
    const ms = Date.parse(`${fecha}T00:00:00Z`) + n * DIA_MS;
    return new Date(ms).toISOString().slice(0, 10);
}

/** Mañana en Guatemala. */
export function mananaEnGuatemala(ms = Date.now()): string {
    return sumarDias(hoyEnGuatemala(ms), 1);
}

export interface DiaTira {
    fecha: string;
    /** «JUE» */
    corto: string;
    /** 5 */
    numero: number;
    /** «jueves 5 de noviembre» (nombre accesible) */
    largo: string;
}

/** Los 7 días de la tira desde `desde` (hoy). */
export function tiraSemana(desde: string): DiaTira[] {
    return Array.from({ length: 7 }, (_, i) => {
        const fecha = sumarDias(desde, i);
        const ms = Date.parse(`${fecha}T12:00:00Z`);
        const corto = diaSemana.format(ms).replace(".", "").toUpperCase();
        return { fecha, corto, numero: Number(fecha.slice(8)), largo: tituloDia.format(ms) };
    });
}

/** «Mañana, vie 6», «Hoy, jue 5» o «Lunes 9 de noviembre»: título de la agenda de un día. */
export function tituloAgenda(fecha: string, hoy: string): string {
    const ms = Date.parse(`${fecha}T12:00:00Z`);
    const corto = `${diaSemana.format(ms).replace(".", "")} ${Number(fecha.slice(8))}`;
    if (fecha === hoy) {
        return `Hoy, ${corto}`;
    }
    if (fecha === sumarDias(hoy, 1)) {
        return `Mañana, ${corto}`;
    }
    const largo = tituloDia.format(ms);
    return `${largo.charAt(0).toUpperCase()}${largo.slice(1)}`;
}

type OrdenAgenda = Pick<OrdenCampo, "id" | "numero" | "estado" | "tipo" | "cita" | "programadaInicio" | "programadaFin" | "direccion" | "cliente" | "contacto" | "formularios">;

export interface CitaAgenda<O extends OrdenAgenda = OrdenAgenda> {
    orden: O;
    /** Inicio (ISO) de la cita de la agenda o, sin cita, la hora programada. */
    inicio: string;
    fin: string | null;
}

/** Día (Guatemala) en que cae una orden: el de su cita o, sin cita, el de su hora programada. */
export function diaDeOrden(orden: Pick<OrdenCampo, "cita" | "programadaInicio">): string | null {
    const inicio = orden.cita?.inicio ?? orden.programadaInicio;
    const ms = inicio ? Date.parse(inicio) : Number.NaN;
    return Number.isNaN(ms) ? null : hoyEnGuatemala(ms);
}

/**
 * Citas de un día, por hora. Solo las que tienen hora ese día; las canceladas no van (las
 * pendientes sin hora no son de ningún día: siguen en «Hoy»).
 */
export function citasDelDia<O extends OrdenAgenda>(ordenes: readonly O[], fecha: string): CitaAgenda<O>[] {
    return ordenes
        .filter((o) => o.estado !== "cancelada" && diaDeOrden(o) === fecha)
        .map((o) => ({ orden: o, inicio: (o.cita?.inicio ?? o.programadaInicio) as string, fin: o.cita?.fin ?? o.programadaFin ?? null }))
        .sort((a, b) => Date.parse(a.inicio) - Date.parse(b.inicio) || a.orden.id - b.orden.id);
}

/** Cuántas citas hay en cada día de la tira (para el punto bajo el número). */
export function citasPorDia(ordenes: readonly OrdenAgenda[], dias: readonly string[]): Record<string, number> {
    return Object.fromEntries(dias.map((d) => [d, citasDelDia(ordenes, d).length]));
}

/** «4 citas · 7 h»: cuántas y cuánto suman (las que tienen fin). Sin citas, «Sin citas». */
export function resumenDia(citas: readonly CitaAgenda[]): string {
    if (citas.length === 0) {
        return "Sin citas";
    }
    const minutos = citas.reduce((n, c) => n + (c.fin ? Math.max(0, Date.parse(c.fin) - Date.parse(c.inicio)) / 60_000 : 0), 0);
    const h = Math.floor(minutos / 60);
    const m = Math.round(minutos % 60);
    const tiempo = minutos <= 0 ? "" : h > 0 ? ` · ${h} h${m > 0 ? ` ${m} min` : ""}` : ` · ${m} min`;
    return `${citas.length} ${citas.length === 1 ? "cita" : "citas"}${tiempo}`;
}

/** Tono del bloque de una cita en la agenda: en curso (marca), atrasada o sin formulario (aviso). */
export function tonoCita(orden: Pick<OrdenCampo, "estado" | "formularios">): "marca" | "aviso" | "neutral" {
    if (EN_CURSO.has(orden.estado)) {
        return "marca";
    }
    if ((orden.formularios ?? []).length === 0) {
        return "aviso";
    }
    return "neutral";
}

/** «2 formularios», «Sin formulario». */
export function textoFormularios(orden: Pick<OrdenCampo, "formularios">): string {
    const n = (orden.formularios ?? []).length;
    return n === 0 ? "Sin formulario" : `${n} ${n === 1 ? "formulario" : "formularios"}`;
}

// ---------------------------------------------------------------------------------------------
// Encabezado que se va al bajar y vuelve compacto al subir; isla que se encoge
// ---------------------------------------------------------------------------------------------

export interface EstadoDesplazamiento {
    /** Posición anterior (px desde arriba). */
    y: number;
    /** Barra compacta arriba (al subir, con el encabezado completo fuera de vista). */
    compacto: boolean;
    /** Isla encogida (al bajar). */
    islaEncogida: boolean;
}

export const DESPLAZAMIENTO_INICIAL: EstadoDesplazamiento = { y: 0, compacto: false, islaEncogida: false };

/**
 * Siguiente estado según la nueva posición del desplazamiento. El encabezado no es fijo: se va con
 * el contenido. Al bajar, la isla se encoge y no hay barra; al subir (más que `umbral` px) con el
 * encabezado fuera de vista (`y > alto`), vuelve la barra compacta y la isla crece. Arriba del todo,
 * todo como al principio. Movimientos menores que el umbral no cambian nada (sin parpadeo).
 */
export function desplazar(previo: EstadoDesplazamiento, y: number, alto: number, umbral = 8): EstadoDesplazamiento {
    const actual = Math.max(0, y);
    if (actual <= Math.max(alto, umbral) / 4) {
        return { y: actual, compacto: false, islaEncogida: false };
    }
    const delta = actual - previo.y;
    if (Math.abs(delta) < umbral) {
        return previo;
    }
    if (delta > 0) {
        return { y: actual, compacto: false, islaEncogida: true };
    }
    return { y: actual, compacto: actual > alto, islaEncogida: false };
}

// ---------------------------------------------------------------------------------------------
// «+»: acciones rápidas
// ---------------------------------------------------------------------------------------------

/** Permiso con el que la API acepta `orden.agregar_formulario` (sobre una orden propia). */
export const PERMISO_AGREGAR_FORMULARIO = "ordenes.registrar";

type OrdenFormulario = Pick<OrdenVista, "id" | "numero" | "estado" | "tipo" | "reasignada" | "formularios" | "cliente" | "contacto">;

export interface AccionesRapidas<O extends OrdenFormulario> {
    /**
     * «Agregar formulario a una orden»: `null` si el técnico no tiene el permiso (la opción no
     * aparece). Con permiso, las órdenes donde se puede (trabajo empezado) y, de cada una, las
     * plantillas activas que todavía no tiene.
     */
    formulario: { ordenes: { orden: O; plantillas: PlantillaFormulario[] }[] } | null;
    /** «Agendar tarea propia»: la API todavía no la tiene; se muestra deshabilitada. */
    tarea: { disponible: false; motivo: string };
}

/**
 * Qué ofrece el «+». `permisos` es la lista de `GET /yo`; `undefined` (sesión guardada de antes
 * de esta versión) cuenta como sin permiso hasta el próximo `GET /yo` con señal.
 */
export function accionesRapidas<O extends OrdenFormulario>(
    ordenes: readonly O[],
    plantillas: readonly PlantillaFormulario[],
    permisos: readonly string[] | undefined,
): AccionesRapidas<O> {
    const tarea = { disponible: false as const, motivo: "Próximamente" };
    if (!permisos?.includes(PERMISO_AGREGAR_FORMULARIO)) {
        return { formulario: null, tarea };
    }
    const lista = ordenes
        .filter((o) => accionesDe(o).agregarFormulario)
        .map((orden) => ({
            orden,
            plantillas: plantillas.filter((p) => p.activa && !(orden.formularios ?? []).some((f) => f.plantillaId === p.id)),
        }));
    return { formulario: { ordenes: lista }, tarea };
}

// ---------------------------------------------------------------------------------------------
// Lecturas con señal (agenda, «Por recibir», «Mi bodega»)
// ---------------------------------------------------------------------------------------------

/** Pide la lectura; `otraVez` al volver la señal (ver `lecturaUnica`). */
export type CargarLectura = (opciones?: { otraVez?: boolean }) => Promise<void>;

export interface LecturaUnica {
    cargar: CargarLectura;
    /**
     * Al desmontar o cerrar sesión: no lanza nada más (ni la repetición pendiente) y `vigente()`
     * pasa a `false` para que lo que llegue tarde no se aplique.
     */
    detener: () => void;
}

/**
 * Una lectura a la vez: pedirla mientras otra corre devuelve la que corre (o la repetición ya
 * pedida). Con `otraVez` (al volver la señal) no se conforma con la que corre, que pudo empezar un
 * instante antes de que el teléfono supiera que no había señal y está condenada: al terminar esa,
 * se lee una vez más. `correr` recibe `vigente`: falso tras `detener()`.
 */
export function lecturaUnica(correr: (vigente: () => boolean) => Promise<void>): LecturaUnica {
    let enCurso: Promise<void> | null = null;
    let repetir: Promise<void> | null = null;
    let detenida = false;
    const vigente = () => !detenida;
    const lanzar = (): Promise<void> => {
        const p = correr(vigente).finally(() => {
            if (enCurso === p) {
                enCurso = null;
            }
        });
        enCurso = p;
        return p;
    };
    const cargar: CargarLectura = (opciones) => {
        if (detenida) {
            return Promise.resolve();
        }
        if (repetir) {
            // Ya hay una repetición esperando: es la que deja todo al día.
            return repetir;
        }
        if (!enCurso) {
            return lanzar();
        }
        if (!opciones?.otraVez) {
            return enCurso;
        }
        // Una sola repetición, sin importar cuántas veces la pidan mientras tanto.
        const r: Promise<void> = enCurso
            .catch(() => undefined)
            .then(() => {
                repetir = null;
                return detenida ? undefined : lanzar();
            });
        repetir = r;
        return r;
    };
    return {
        cargar,
        detener: () => {
            detenida = true;
        },
    };
}
