// Días de «Hoy», «Mañana» y «Semana» en hora de Guatemala (UTC−6), también en la franja en que el
// día UTC ya cambió y en Guatemala no (18:00–24:00 GT). Y la lectura con señal que no se pierde si
// la piden mientras otra corre (al volver la señal).
import { describe, expect, it } from "vitest";
import { citasDelDia, diaDeOrden, fechaEncabezado, hoyEnGuatemala, lecturaUnica, mananaEnGuatemala, tiraSemana, tituloAgenda } from "./campo";
import { ordenDePrueba } from "./pruebas/fabricas";
import { vaciarPromesas } from "./pruebas/reloj-manual";

/** 19:30 del jueves 5 en Guatemala: en UTC ya es el viernes 6. */
const NOCHE_GT = Date.parse("2026-11-06T01:30:00Z");
/** 17:30 del jueves 5 en Guatemala (23:30 UTC, mismo día). */
const TARDE_GT = Date.parse("2026-11-05T23:30:00Z");

describe("días en hora de Guatemala", () => {
    it.each([
        ["01:30 UTC (19:30 GT)", NOCHE_GT],
        ["23:30 UTC (17:30 GT)", TARDE_GT],
    ])("a las %s hoy es el jueves 5 y mañana el viernes 6", (_, ahora) => {
        expect(hoyEnGuatemala(ahora)).toBe("2026-11-05");
        expect(mananaEnGuatemala(ahora)).toBe("2026-11-06");
        expect(fechaEncabezado(ahora)).toBe("Jueves 5 de noviembre");
        const hoy = hoyEnGuatemala(ahora);
        const tira = tiraSemana(hoy);
        expect(tira.map((d) => d.fecha)).toEqual(["2026-11-05", "2026-11-06", "2026-11-07", "2026-11-08", "2026-11-09", "2026-11-10", "2026-11-11"]);
        expect(tira.map((d) => d.corto)).toEqual(["JUE", "VIE", "SÁB", "DOM", "LUN", "MAR", "MIÉ"]);
        expect(tituloAgenda(mananaEnGuatemala(ahora), hoy)).toBe("Mañana, vie 6");
        expect(tituloAgenda(hoy, hoy)).toBe("Hoy, jue 5");
    });

    it("una cita a las 19:00 GT del jueves es del jueves aunque en UTC sea viernes; la de las 00:30 GT del viernes, del viernes", () => {
        const jueves = ordenDePrueba(1, { cita: { tecnicoId: 5, inicio: "2026-11-06T01:00:00Z", fin: "2026-11-06T02:00:00Z", ventanaInicio: null, ventanaFin: null } });
        const viernes = ordenDePrueba(2, { programadaInicio: "2026-11-06T06:30:00Z", programadaFin: "2026-11-06T07:30:00Z" });
        expect(diaDeOrden(jueves)).toBe("2026-11-05");
        expect(diaDeOrden(viernes)).toBe("2026-11-06");
        const manana = mananaEnGuatemala(NOCHE_GT);
        expect(citasDelDia([jueves, viernes], manana).map((c) => c.orden.id)).toEqual([2]);
        expect(citasDelDia([jueves, viernes], hoyEnGuatemala(NOCHE_GT)).map((c) => c.orden.id)).toEqual([1]);
    });
});

describe("lecturaUnica", () => {
    function diferida() {
        let resolver: () => void = () => undefined;
        let rechazar: (e: unknown) => void = () => undefined;
        const promesa = new Promise<void>((ok, mal) => {
            resolver = ok;
            rechazar = mal;
        });
        return { promesa, resolver, rechazar };
    }

    it("una a la vez: sin `otraVez` devuelve la que corre; con `otraVez` (volvió la señal) lee una vez más al terminar, aunque la primera falle", async () => {
        const llamadas: ReturnType<typeof diferida>[] = [];
        const leer = lecturaUnica(() => {
            const d = diferida();
            llamadas.push(d);
            return d.promesa;
        });
        const primera = leer();
        void primera.catch(() => undefined);
        // Abrir la vista otra vez mientras corre: la misma lectura.
        expect(leer()).toBe(primera);
        // Vuelve la señal con la primera (condenada) en vuelo: se pide de nuevo, dos veces.
        const segunda = leer({ otraVez: true });
        const tercera = leer({ otraVez: true });
        expect(llamadas).toHaveLength(1);
        llamadas[0]?.rechazar(new Error("sin señal"));
        await vaciarPromesas(10);
        expect(llamadas).toHaveLength(2);
        llamadas[1]?.resolver();
        await expect(segunda).resolves.toBeUndefined();
        await expect(tercera).resolves.toBeUndefined();
        expect(llamadas).toHaveLength(2);
        // Sin otra en curso, se lee enseguida.
        void leer();
        expect(llamadas).toHaveLength(3);
    });
});
