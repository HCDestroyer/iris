import type { Schema } from "@iris/contratos";
import { describe, expect, it } from "vitest";
import {
    entradaAusencia,
    entradaCrearTecnico,
    entradaEditarTecnico,
    entradaTurnos,
    erroresDe,
    errorTelefono,
    esquemaAusencia,
    esquemaCuadrilla,
    esquemaTecnico,
    esquemaTurnos,
    habilidadesDeTexto,
    textoTurnos,
    valoresAusencia,
    valoresTecnico,
    valoresTurnos,
} from "./esquemas";
import { conflictosDe, cuadriculaMes, etiquetaMes, inicioMes, resumenPorDia, sumarMeses, textoConflicto, trozos, unirCalendarios, vistaDeUrl } from "./mes";
import { MENU, menuVisible } from "@/lib/menu";
import { esDia } from "@/lib/despacho/fechas";

type Cita = Schema<"CitaAgenda">;
type Ausencia = Schema<"AusenciaAgenda">;
type Tecnico = Schema<"Tecnico">;

/** Día y hora de Guatemala (UTC−6) → ISO UTC. */
const utc = (dia: string, hora: string) => new Date(Date.parse(`${dia}T${hora}:00Z`) + 6 * 3_600_000).toISOString().replace(".000Z", "Z");

const cita = (p: Partial<Cita> & Pick<Cita, "ordenId" | "tecnicoId" | "inicio" | "fin">): Cita => ({
    numero: `OT-${String(p.ordenId).padStart(6, "0")}`,
    tipo: "visita",
    estado: "programada",
    ordenVersion: 1,
    ventanaInicio: null,
    ventanaFin: null,
    zonaId: 4,
    cuadrillaId: 1,
    direccion: "",
    lat: null,
    lon: null,
    clienteId: null,
    servicioId: null,
    conflictos: [],
    ...p,
});

const ausencia = (p: Partial<Ausencia> & Pick<Ausencia, "id" | "tecnicoId" | "inicio" | "fin">): Ausencia => ({ fichaId: 1, motivo: "vacaciones", ...p });

const tecnico: Tecnico = {
    id: 21,
    usuarioId: 3,
    nombre: "Lucía Ramírez",
    correo: "lucia@iris.test",
    cuadrillaId: 1,
    bodegaId: 30,
    habilidades: ["fibra"],
    baseLat: 14.6,
    baseLon: -90.5,
    telefono: "+502 5555-0000",
    activo: true,
    version: 4,
    creadoEn: "2026-10-01T00:00:00Z",
    actualizadoEn: "2026-10-01T00:00:00Z",
};

describe("mes de la agenda", () => {
    it("cuadrícula de semanas completas de lunes a domingo", () => {
        const oct = cuadriculaMes("2026-10-15");
        expect(oct.desde).toBe("2026-09-28");
        expect(oct.dias.at(-1)).toBe("2026-11-01");
        expect(oct.semanas).toHaveLength(5);
        expect(oct.semanas.every((s) => s.length === 7)).toBe(true);
        // Marzo de 2026 empieza en domingo: seis semanas.
        expect(cuadriculaMes("2026-03-10").semanas).toHaveLength(6);
        // Febrero de 2027 empieza en lunes y termina en domingo: exactamente cuatro.
        expect(cuadriculaMes("2027-02-01").dias).toHaveLength(28);
    });

    it("meses: suma y recorta al último día; inicio y etiqueta", () => {
        expect(sumarMeses("2026-01-31", 1)).toBe("2026-02-28");
        expect(sumarMeses("2026-12-15", 1)).toBe("2027-01-15");
        expect(sumarMeses("2026-01-10", -1)).toBe("2025-12-10");
        expect(inicioMes("2026-10-28")).toBe("2026-10-01");
        expect(etiquetaMes("2026-10-28")).toBe("octubre de 2026");
    });

    it("parte el rango en trozos de hasta 31 días (lo que acepta GET /agenda)", () => {
        expect(trozos("2026-09-28", 42)).toEqual([
            { desde: "2026-09-28", dias: 31 },
            { desde: "2026-10-29", dias: 11 },
        ]);
        expect(trozos("2026-09-28", 31)).toEqual([{ desde: "2026-09-28", dias: 31 }]);
    });

    it("une los trozos sin repetir citas ni ausencias", () => {
        const a = cita({ ordenId: 1, tecnicoId: 3, inicio: utc("2026-10-28", "09:00"), fin: utc("2026-10-28", "10:00") });
        const au = ausencia({ id: 7, tecnicoId: 12, inicio: utc("2026-10-28", "00:00"), fin: utc("2026-11-02", "00:00") });
        const u = unirCalendarios([
            { citas: [a], ausencias: [au] },
            { citas: [a], ausencias: [au] },
            { citas: null, ausencias: null },
        ]);
        expect(u.citas).toHaveLength(1);
        expect(u.ausencias).toHaveLength(1);
    });

    it("conflictos: los marca la API; el detalle sale de lo leído y, si no está, se dice", () => {
        const dia = "2026-10-28";
        const enAusencia = cita({ ordenId: 1, tecnicoId: 12, inicio: utc(dia, "10:00"), fin: utc(dia, "11:00"), conflictos: ["ausencia"] });
        const a = cita({ ordenId: 2, tecnicoId: 3, inicio: utc(dia, "09:00"), fin: utc(dia, "10:30"), conflictos: ["solape"] });
        const b = cita({ ordenId: 3, tecnicoId: 3, inicio: utc(dia, "10:00"), fin: utc(dia, "11:00"), estado: "en_camino", conflictos: ["solape"] });
        const cerrada = cita({ ordenId: 4, tecnicoId: 3, inicio: utc(dia, "09:30"), fin: utc(dia, "10:00"), estado: "cerrada" });
        // Se cruza con una cita que el filtro no trajo: la API lo marca igual.
        const fuera = cita({ ordenId: 5, tecnicoId: 11, inicio: utc(dia, "09:00"), fin: utc(dia, "10:30"), conflictos: ["solape"] });
        const sinConflicto = cita({ ordenId: 6, tecnicoId: 3, inicio: utc(dia, "11:00"), fin: utc(dia, "12:00") });
        const aus = ausencia({ id: 9, tecnicoId: 12, inicio: utc(dia, "00:00"), fin: utc("2026-10-29", "00:00") });
        const citas = [enAusencia, a, b, cerrada, fuera, sinConflicto];
        const c = conflictosDe(citas, [aus]);
        expect([...c.keys()].sort()).toEqual([1, 2, 3, 5]);
        const uno = (id: number) => c.get(id)?.[0] ?? { tipo: "solape" as const, ordenId: 0, con: undefined };
        expect(textoConflicto(uno(1))).toContain("Vacaciones");
        expect(textoConflicto(uno(2))).toBe("se cruza con OT-000003 (10:00–11:00)");
        expect(textoConflicto(uno(5))).toBe("se cruza con otra cita del técnico (fuera de este filtro)");
        expect(textoConflicto({ tipo: "ausencia", ordenId: 7, ausencia: undefined })).toBe("cae en una ausencia del técnico");

        const resumen = resumenPorDia([dia, "2026-10-29"], citas, [aus], c);
        const r = resumen.get(dia);
        expect(r?.total).toBe(6);
        expect(r?.conflictos).toBe(4);
        expect(r?.ausentes).toBe(1);
        expect(r?.porEstado).toEqual([
            { estado: "programada", cuantas: 4 },
            { estado: "en_camino", cuantas: 1 },
            { estado: "cerrada", cuantas: 1 },
        ]);
        // La ausencia termina a las 00:00 del 29: ese día no cuenta.
        expect(resumen.get("2026-10-29")).toEqual({ total: 0, porEstado: [], ausentes: 0, conflictos: 0 });
    });

    it("unir trozos: truncado si alguno lo está", () => {
        expect(unirCalendarios([{ citas: [], ausencias: [], truncado: false }, { citas: [], ausencias: [], truncado: true }]).truncado).toBe(true);
        expect(unirCalendarios([{ citas: [], ausencias: [], truncado: false }]).truncado).toBe(false);
    });

    it("la vista y el día salen de la URL; lo inválido cae en el mes de hoy", () => {
        expect(vistaDeUrl(null, null, "2026-10-30", esDia)).toEqual({ vista: "mes", dia: "2026-10-30" });
        expect(vistaDeUrl("dia", "2026-11-04", "2026-10-30", esDia)).toEqual({ vista: "dia", dia: "2026-11-04" });
        expect(vistaDeUrl("año", "2026-02-30", "2026-10-30", esDia)).toEqual({ vista: "mes", dia: "2026-10-30" });
    });
});

describe("formulario de técnico", () => {
    it("sin usuario o sin cuadrilla no se puede guardar, con el mensaje en el campo", () => {
        const e = erroresDe(esquemaTecnico(true).safeParse(valoresTecnico()));
        expect(e.usuarioId).toBe("Elige el usuario del técnico");
        expect(e.cuadrillaId).toBe("Elige la cuadrilla: define las zonas que atiende");
        expect(erroresDe(esquemaTecnico(false).safeParse(valoresTecnico(tecnico)))).toEqual({});
    });

    it("teléfono como lo valida la API", () => {
        expect(errorTelefono("")).toBeUndefined();
        expect(errorTelefono("+502 (5555) 12.34")).toBeUndefined();
        expect(errorTelefono("llámame")).toBe("Usa solo dígitos, espacios, +, -, paréntesis y puntos");
        expect(errorTelefono("123")).toBe("El teléfono necesita al menos 7 dígitos");
        expect(errorTelefono("1".repeat(31))).toBe("Admite hasta 30 caracteres");
    });

    it("habilidades en minúsculas, sin repetir y ordenadas", () => {
        expect(habilidadesDeTexto(" Fibra, altura,fibra, ,Inalambrico")).toEqual(["altura", "fibra", "inalambrico"]);
    });

    it("cuerpos de crear y editar (la edición conserva la base leída)", () => {
        const v = { usuarioId: "3", cuadrillaId: "2", telefono: " 5555-1234 ", habilidades: "fibra, Altura", bodegaId: "" };
        expect(entradaCrearTecnico(v)).toEqual({ usuarioId: 3, cuadrillaId: 2, habilidades: ["altura", "fibra"], telefono: "5555-1234" });
        expect(entradaEditarTecnico({ ...v, telefono: "", bodegaId: "30" }, tecnico)).toEqual({
            version: 4,
            cuadrillaId: 2,
            habilidades: ["altura", "fibra"],
            bodegaId: 30,
            baseLat: 14.6,
            baseLon: -90.5,
        });
    });

    it("cuadrilla: nombre obligatorio, zonas opcionales", () => {
        expect(erroresDe(esquemaCuadrilla.safeParse({ nombre: "  ", zonaIds: [] })).nombre).toBe("Escribe el nombre de la cuadrilla");
        expect(esquemaCuadrilla.safeParse({ nombre: "Norte", zonaIds: [] }).success).toBe(true);
    });
});

describe("turnos y ausencias", () => {
    it("turnos: día obligatorio, fin posterior (hasta 24:00) y sin cruces en el día", () => {
        const e = erroresDe(
            esquemaTurnos.safeParse({
                turnos: [
                    { dia: "", inicio: "08:00", fin: "12:00" },
                    { dia: "1", inicio: "13:00", fin: "12:00" },
                    { dia: "2", inicio: "08:00", fin: "12:00" },
                    { dia: "2", inicio: "11:00", fin: "24:00" },
                ],
            }),
        );
        expect(e["turnos.0.dia"]).toBe("Elige el día");
        expect(e["turnos.1.fin"]).toContain("posterior al inicio");
        expect(e["turnos.3.inicio"]).toBe("Se cruza con otro turno del martes");
        expect(esquemaTurnos.safeParse({ turnos: [{ dia: "1", inicio: "13:00", fin: "24:00" }] }).success).toBe(true);
    });

    it("turnos: lectura ordenada, cuerpo y resumen", () => {
        const v = valoresTurnos([
            { dia: 2, inicio: "08:00", fin: "17:00" },
            { dia: 1, inicio: "13:00", fin: "17:00" },
            { dia: 1, inicio: "08:00", fin: "12:00" },
        ]);
        expect(v.turnos.map((t) => `${t.dia} ${t.inicio}`)).toEqual(["1 08:00", "1 13:00", "2 08:00"]);
        expect(entradaTurnos(v, 3)).toEqual({
            version: 3,
            turnos: [
                { dia: 1, inicio: "08:00", fin: "12:00" },
                { dia: 1, inicio: "13:00", fin: "17:00" },
                { dia: 2, inicio: "08:00", fin: "17:00" },
            ],
        });
        expect(textoTurnos([])).toBe("Sin turnos");
        expect(textoTurnos([{ dia: 1, inicio: "08:00", fin: "12:00" }])).toBe("Lun 08:00–12:00");
    });

    it("ausencia: motivo obligatorio; todo el día va de 00:00 al día siguiente del último", () => {
        const base = valoresAusencia("2026-10-28");
        expect(erroresDe(esquemaAusencia.safeParse(base)).motivo).toBe("Elige el motivo de la ausencia");
        const v = { ...base, motivo: "vacaciones", hastaDia: "2026-10-30", detalle: " Viaje " };
        expect(esquemaAusencia.safeParse(v).success).toBe(true);
        expect(entradaAusencia(v)).toEqual({ motivo: "vacaciones", inicio: utc("2026-10-28", "00:00"), fin: utc("2026-10-31", "00:00"), detalle: "Viaje" });
        const parcial = { ...v, todoElDia: false, hastaDia: "2026-10-28", desdeHora: "13:00", hastaHora: "12:00" };
        expect(erroresDe(esquemaAusencia.safeParse(parcial)).hastaHora).toBe("El fin debe ser posterior al inicio");
        expect(erroresDe(esquemaAusencia.safeParse({ ...v, hastaDia: "2027-12-01" })).hastaDia).toBe("Una ausencia dura hasta 366 días");
    });
});

describe("menú de técnicos", () => {
    it("Técnicos con permisos de fichas (no basta agenda.ver); cuadrillas y zonas en Administración", () => {
        expect(menuVisible(["agenda.ver"]).map((e) => e.href)).not.toContain("/tecnicos");
        for (const p of ["agenda.crear_tecnico", "agenda.fijar_turnos", "agenda.registrar_ausencia"] as const) {
            expect(menuVisible([p]).map((e) => e.href)).toContain("/tecnicos");
        }
        expect(menuVisible(["agenda.crear_cuadrilla"]).map((e) => e.href)).toContain("/administracion");
        expect(menuVisible(["agenda.crear_zona"]).map((e) => e.href)).toContain("/administracion");
        expect(MENU.find((e) => e.href === "/tecnicos")?.etiqueta).toBe("Técnicos");
    });
});
