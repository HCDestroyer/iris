import { describe, expect, it } from "vitest";
import type { AusenciaAgenda, CitaAgenda, Tecnico } from "../despacho/calendario";
import { citasPorDia, equipoDeHoy, puntosTendencia } from "./equipo";

const DIA = "2026-11-05";
/** Hora de Guatemala del día → ISO UTC. */
const h = (hora: string, dia = DIA) => new Date(Date.parse(`${dia}T${hora}:00Z`) + 6 * 3_600_000).toISOString();
const AHORA = Date.parse(h("12:00"));

const tecnico = (usuarioId: number, nombre: string, activo = true) => ({ id: usuarioId + 100, usuarioId, nombre, activo }) as unknown as Tecnico;
const cita = (ordenId: number, tecnicoId: number, inicio: string, estado: CitaAgenda["estado"]) =>
    ({ ordenId, numero: `OT-${ordenId}`, tecnicoId, inicio: h(inicio), fin: h(inicio.replace(/^(\d\d)/, (x) => String(Number(x) + 1).padStart(2, "0"))), estado }) as unknown as CitaAgenda;
const pos = (tecnicoId: number, antiguedadSeg: number) => ({ tecnicoId, antiguedadSeg, nombre: "", lat: 0, lon: 0 }) as never;

describe("equipo de hoy", () => {
    const tecnicos = [tecnico(1, "Ana"), tecnico(2, "Beto"), tecnico(3, "Carla"), tecnico(4, "Dora"), tecnico(5, "Eli"), tecnico(9, "Inactivo", false)];
    const citas = [
        cita(10, 1, "08:00", "cerrada"),
        cita(11, 1, "11:00", "en_sitio"),
        cita(12, 1, "15:00", "programada"),
        cita(20, 2, "09:00", "programada"),
        cita(30, 3, "08:00", "cerrada"),
        cita(31, 3, "10:00", "cancelada"),
        cita(40, 4, "13:00", "programada"),
    ];
    const ausencias = [{ id: 1, tecnicoId: 4, inicio: h("07:00"), fin: h("18:00") } as unknown as AusenciaAgenda];

    it("avance (sin canceladas), orden actual y siguiente, y el estado de cada uno", () => {
        const filas = equipoDeHoy(tecnicos, citas, ausencias, [pos(1, 30), pos(2, 3600), pos(3, 30)], DIA, AHORA);
        const de = (id: number) => filas.find((f) => f.usuarioId === id);
        expect(de(1)).toMatchObject({ estado: "en_sitio", total: 3, terminadas: 1 });
        expect(de(1)?.actual?.ordenId).toBe(11);
        expect(de(1)?.siguiente?.ordenId).toBe(12);
        expect(de(2)?.estado).toBe("sin_senal");
        expect(de(3)).toMatchObject({ estado: "termino", total: 1, terminadas: 1 });
        expect(de(4)?.estado).toBe("ausente");
        expect(de(5)?.estado).toBe("sin_ordenes");
        expect(de(9)).toBeUndefined();
        // Los que piden atención van primero.
        expect(filas[0]?.usuarioId).toBe(2);
    });

    it("sin posiciones leídas (sin permiso) nadie queda «sin señal»", () => {
        const filas = equipoDeHoy(tecnicos, citas, ausencias, null, DIA, AHORA);
        expect(filas.find((f) => f.usuarioId === 2)?.estado).toBe("sin_iniciar");
    });

    it("quien tiene citas hoy pero no vino en la lista también aparece", () => {
        const filas = equipoDeHoy([], [cita(50, 77, "09:00", "en_camino")], [], null, DIA, AHORA);
        expect(filas).toMatchObject([{ usuarioId: 77, nombre: "Técnico 77", estado: "en_camino" }]);
    });

    it("citas por día (tendencia) sin las canceladas", () => {
        expect(citasPorDia(citas, ["2026-11-04", DIA])).toEqual([0, 6]);
    });
});

describe("línea de tendencia", () => {
    it("escala los valores a la caja y deja plana una serie constante", () => {
        expect(puntosTendencia([0, 10], 96, 32)).toBe("0,30 96,2");
        expect(puntosTendencia([3, 3, 3], 96, 32)).toBe("0,16 48,16 96,16");
        expect(puntosTendencia([])).toBe("");
    });
});
