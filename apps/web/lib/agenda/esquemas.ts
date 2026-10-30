import type { Schema } from "@iris/contratos";
import { z } from "zod";
import { esDia, isoDesdeDia, minutosDeTexto, sumarDias } from "@/lib/despacho/fechas";

/*
 * Formularios de técnicos, cuadrillas, zonas, turnos y ausencias: espejo de las validaciones de
 * apps/api/internal/agenda (largos, teléfono, turnos sin cruces, ausencias de hasta 366 días).
 * Las selecciones obligatorias dejan el botón deshabilitado con su mensaje junto al campo.
 */

export type Tecnico = Schema<"Tecnico">;
export type TecnicoDetalle = Schema<"TecnicoDetalle">;
export type Ausencia = Schema<"Ausencia">;
export type Turno = Schema<"Turno">;
export type CandidatoTecnico = Schema<"CandidatoTecnico">;
export type MotivoAusencia = Ausencia["motivo"];

const ID = /^\d{1,15}$/;

/** Primer mensaje de cada campo de un resultado de zod (`turnos.0.fin`). */
export function erroresDe(r: z.ZodSafeParseResult<unknown>): Record<string, string> {
    const salida: Record<string, string> = {};
    if (r.success) return salida;
    for (const i of r.error.issues) {
        const clave = i.path.map(String).join(".");
        salida[clave] ??= i.message;
    }
    return salida;
}

// --- Técnicos -------------------------------------------------------------------------------

/** Teléfono como lo valida la API: hasta 30 caracteres, al menos 7 dígitos; vacío = sin teléfono. */
export function errorTelefono(valor: string): string | undefined {
    const t = valor.trim();
    if (t === "") return undefined;
    if (t.length > 30) return "Admite hasta 30 caracteres";
    if (!/^[\d +\-().]+$/.test(t)) return "Usa solo dígitos, espacios, +, -, paréntesis y puntos";
    if ((t.match(/\d/g) ?? []).length < 7) return "El teléfono necesita al menos 7 dígitos";
    return undefined;
}

/** Habilidades separadas por comas, en minúsculas y sin repetir (como las guarda la API). */
export function habilidadesDeTexto(texto: string): string[] {
    const salida: string[] = [];
    for (const h of texto.split(",")) {
        const v = h.trim().toLowerCase();
        if (v !== "" && !salida.includes(v)) salida.push(v);
    }
    return salida.sort();
}

export type ValoresTecnico = { usuarioId: string; cuadrillaId: string; telefono: string; habilidades: string; bodegaId: string };

/** Alta (`conUsuario`) o edición de la ficha. La cuadrilla es obligatoria: define las zonas que atiende. */
export function esquemaTecnico(conUsuario: boolean) {
    return z
        .object({ usuarioId: z.string(), cuadrillaId: z.string(), telefono: z.string(), habilidades: z.string(), bodegaId: z.string() })
        .superRefine((d, ctx) => {
            if (conUsuario && !ID.test(d.usuarioId)) ctx.addIssue({ code: "custom", path: ["usuarioId"], message: "Elige el usuario del técnico" });
            if (!ID.test(d.cuadrillaId)) ctx.addIssue({ code: "custom", path: ["cuadrillaId"], message: "Elige la cuadrilla: define las zonas que atiende" });
            const tel = errorTelefono(d.telefono);
            if (tel) ctx.addIssue({ code: "custom", path: ["telefono"], message: tel });
            const hs = habilidadesDeTexto(d.habilidades);
            if (hs.length > 30) ctx.addIssue({ code: "custom", path: ["habilidades"], message: "Hasta 30 habilidades" });
            if (hs.some((h) => h.length > 40)) ctx.addIssue({ code: "custom", path: ["habilidades"], message: "Cada habilidad admite hasta 40 caracteres" });
            if (d.bodegaId.trim() !== "" && !ID.test(d.bodegaId.trim())) ctx.addIssue({ code: "custom", path: ["bodegaId"], message: "Escribe el número de bodega de Argos" });
        });
}

export function valoresTecnico(t?: Tecnico): ValoresTecnico {
    return {
        usuarioId: t ? String(t.usuarioId) : "",
        cuadrillaId: t?.cuadrillaId ? String(t.cuadrillaId) : "",
        telefono: t?.telefono ?? "",
        habilidades: (t?.habilidades ?? []).join(", "),
        bodegaId: t?.bodegaId ? String(t.bodegaId) : "",
    };
}

function comunesTecnico(v: ValoresTecnico) {
    const bodega = v.bodegaId.trim();
    const telefono = v.telefono.trim();
    return {
        cuadrillaId: Number(v.cuadrillaId),
        habilidades: habilidadesDeTexto(v.habilidades),
        ...(bodega !== "" ? { bodegaId: Number(bodega) } : {}),
        ...(telefono !== "" ? { telefono } : {}),
    };
}

export function entradaCrearTecnico(v: ValoresTecnico): Schema<"EntradaCrearTecnico"> {
    return { usuarioId: Number(v.usuarioId), ...comunesTecnico(v) };
}

/** La edición reemplaza la ficha: la base (lat/lon) se conserva tal como se leyó. */
export function entradaEditarTecnico(v: ValoresTecnico, t: Tecnico): Schema<"EntradaEditarTecnico"> {
    return {
        version: t.version,
        ...comunesTecnico(v),
        ...(t.baseLat !== null && t.baseLon !== null ? { baseLat: t.baseLat, baseLon: t.baseLon } : {}),
    };
}

// --- Cuadrillas y zonas -----------------------------------------------------------------------

const nombre = (que: string) =>
    z
        .string()
        .trim()
        .min(1, `Escribe el nombre de la ${que}`)
        .max(120, "Admite hasta 120 caracteres");

export const esquemaCuadrilla = z.object({ nombre: nombre("cuadrilla"), zonaIds: z.array(z.string()) });
export type ValoresCuadrilla = z.input<typeof esquemaCuadrilla>;

export const esquemaZona = z.object({ nombre: nombre("zona") });
export type ValoresZona = z.input<typeof esquemaZona>;

// --- Turnos ---------------------------------------------------------------------------------

export const DIAS_SEMANA: readonly { dia: number; texto: string; corto: string }[] = [
    { dia: 1, texto: "Lunes", corto: "Lun" },
    { dia: 2, texto: "Martes", corto: "Mar" },
    { dia: 3, texto: "Miércoles", corto: "Mié" },
    { dia: 4, texto: "Jueves", corto: "Jue" },
    { dia: 5, texto: "Viernes", corto: "Vie" },
    { dia: 6, texto: "Sábado", corto: "Sáb" },
    { dia: 7, texto: "Domingo", corto: "Dom" },
];

export type FilaTurno = { dia: string; inicio: string; fin: string };
export type ValoresTurnos = { turnos: FilaTurno[] };

/** `HH:MM` → minutos; «24:00» = 1440 (solo como fin). */
function minutosFin(hora: string): number | undefined {
    return hora === "24:00" ? 1440 : minutosDeTexto(hora);
}

export const esquemaTurnos = z
    .object({ turnos: z.array(z.object({ dia: z.string(), inicio: z.string(), fin: z.string() })).max(28, "Hasta 28 turnos por semana") })
    .superRefine((d, ctx) => {
        const validos: { i: number; dia: number; a: number; b: number }[] = [];
        d.turnos.forEach((t, i) => {
            const dia = Number(t.dia);
            if (!(dia >= 1 && dia <= 7)) ctx.addIssue({ code: "custom", path: ["turnos", i, "dia"], message: "Elige el día" });
            const a = minutosDeTexto(t.inicio);
            const b = minutosFin(t.fin);
            if (a === undefined) ctx.addIssue({ code: "custom", path: ["turnos", i, "inicio"], message: "Hora de inicio (HH:MM)" });
            if (b === undefined) ctx.addIssue({ code: "custom", path: ["turnos", i, "fin"], message: "Hora de fin (HH:MM)" });
            if (a !== undefined && b !== undefined) {
                if (b <= a) ctx.addIssue({ code: "custom", path: ["turnos", i, "fin"], message: "El fin debe ser posterior al inicio (un turno nocturno se parte en dos)" });
                else if (dia >= 1 && dia <= 7) validos.push({ i, dia, a, b });
            }
        });
        validos.sort((x, y) => x.dia - y.dia || x.a - y.a);
        for (let k = 1; k < validos.length; k++) {
            const p = validos[k - 1];
            const c = validos[k];
            if (p && c && p.dia === c.dia && c.a < p.b) {
                ctx.addIssue({ code: "custom", path: ["turnos", c.i, "inicio"], message: `Se cruza con otro turno del ${DIAS_SEMANA[c.dia - 1]?.texto.toLowerCase()}` });
            }
        }
    });

export function valoresTurnos(turnos: readonly Turno[] | null): ValoresTurnos {
    return { turnos: [...(turnos ?? [])].sort((a, b) => a.dia - b.dia || a.inicio.localeCompare(b.inicio)).map((t) => ({ dia: String(t.dia), inicio: t.inicio, fin: t.fin })) };
}

/** Lunes a viernes de 08:00 a 17:00 (atajo del formulario). */
export const TURNOS_HABILES: readonly FilaTurno[] = [1, 2, 3, 4, 5].map((d) => ({ dia: String(d), inicio: "08:00", fin: "17:00" }));

export function entradaTurnos(v: ValoresTurnos, version: number): Schema<"EntradaFijarTurnos"> {
    return { version, turnos: v.turnos.map((t) => ({ dia: Number(t.dia), inicio: t.inicio, fin: t.fin })) };
}

/** «Lun 08:00–17:00 · Mar …» (resumen de la lista). */
export function textoTurnos(turnos: readonly Turno[] | null): string {
    const ts = [...(turnos ?? [])].sort((a, b) => a.dia - b.dia || a.inicio.localeCompare(b.inicio));
    if (ts.length === 0) return "Sin turnos";
    return ts.map((t) => `${DIAS_SEMANA[t.dia - 1]?.corto ?? t.dia} ${t.inicio}–${t.fin}`).join(" · ");
}

// --- Ausencias ------------------------------------------------------------------------------

export const MOTIVOS: readonly { valor: MotivoAusencia; texto: string }[] = [
    { valor: "vacaciones", texto: "Vacaciones" },
    { valor: "enfermedad", texto: "Enfermedad" },
    { valor: "capacitacion", texto: "Capacitación" },
    { valor: "permiso", texto: "Permiso" },
    { valor: "otro", texto: "Otro motivo" },
];

export type ValoresAusencia = { motivo: string; todoElDia: boolean; desdeDia: string; desdeHora: string; hastaDia: string; hastaHora: string; detalle: string };

const MS_DIA = 86_400_000;

/** Inicio y fin UTC de la ausencia (hora de Guatemala); «todo el día» va de 00:00 a 00:00 del día siguiente al último. */
function rangoAusencia(d: ValoresAusencia): { inicio: string; fin: string } | undefined {
    if (!esDia(d.desdeDia) || !esDia(d.hastaDia)) return undefined;
    if (d.todoElDia) return { inicio: isoDesdeDia(d.desdeDia, 0), fin: isoDesdeDia(sumarDias(d.hastaDia, 1), 0) };
    const a = minutosDeTexto(d.desdeHora);
    const b = minutosDeTexto(d.hastaHora);
    if (a === undefined || b === undefined) return undefined;
    return { inicio: isoDesdeDia(d.desdeDia, a), fin: isoDesdeDia(d.hastaDia, b) };
}

/** Ausencia en hora de Guatemala, del día (y hora) de inicio al de fin; hasta 366 días. */
export const esquemaAusencia = z
    .object({
        motivo: z.string(),
        todoElDia: z.boolean(),
        desdeDia: z.string(),
        desdeHora: z.string(),
        hastaDia: z.string(),
        hastaHora: z.string(),
        detalle: z.string().max(500, "Admite hasta 500 caracteres"),
    })
    .superRefine((d, ctx) => {
        if (!MOTIVOS.some((m) => m.valor === d.motivo)) ctx.addIssue({ code: "custom", path: ["motivo"], message: "Elige el motivo de la ausencia" });
        if (!esDia(d.desdeDia)) ctx.addIssue({ code: "custom", path: ["desdeDia"], message: "Elige el día en que empieza" });
        if (!esDia(d.hastaDia)) ctx.addIssue({ code: "custom", path: ["hastaDia"], message: "Elige el día en que termina" });
        if (!d.todoElDia) {
            if (minutosDeTexto(d.desdeHora) === undefined) ctx.addIssue({ code: "custom", path: ["desdeHora"], message: "Elige la hora de inicio" });
            if (minutosDeTexto(d.hastaHora) === undefined) ctx.addIssue({ code: "custom", path: ["hastaHora"], message: "Elige la hora de fin" });
        }
        const r = rangoAusencia(d);
        if (r) {
            const largo = Date.parse(r.fin) - Date.parse(r.inicio);
            if (largo <= 0) ctx.addIssue({ code: "custom", path: [d.todoElDia ? "hastaDia" : "hastaHora"], message: "El fin debe ser posterior al inicio" });
            else if (largo > 366 * MS_DIA) ctx.addIssue({ code: "custom", path: ["hastaDia"], message: "Una ausencia dura hasta 366 días" });
        }
    });

export function valoresAusencia(dia: string): ValoresAusencia {
    return { motivo: "", todoElDia: true, desdeDia: dia, desdeHora: "08:00", hastaDia: dia, hastaHora: "17:00", detalle: "" };
}

export function entradaAusencia(v: ValoresAusencia): Schema<"EntradaRegistrarAusencia"> {
    const detalle = v.detalle.trim();
    const r = rangoAusencia(v) ?? { inicio: "", fin: "" };
    return { motivo: v.motivo as MotivoAusencia, inicio: r.inicio, fin: r.fin, ...(detalle !== "" ? { detalle } : {}) };
}

export function textoMotivo(m: string): string {
    return MOTIVOS.find((x) => x.valor === m)?.texto ?? "Ausencia";
}
