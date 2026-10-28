import { describe, expect, it } from "vitest";
import { ausenciasDelDia, citasDelDia, filasDeTecnicos, horarioDelDia, minutosEnFraccion, nombreDe, tramoEnHorario, type CitaAgenda, type Tecnico } from "./calendario";
import { diaDesdeIso, esDia, etiquetaDia, horaDe, hoyGuatemala, inicioSemana, isoDesdeDia, minutosEnDia, rangoUtc, sumarDias, textoAntiguedad } from "./fechas";
import { accionMover, arrastrePermitido, comprobarDestino, entradaAsignar, entradaProgramar, esquemaMover, movibleDeCita, sePuedeMover, valoresIniciales, type Asignable } from "./mover";
import { antiguedadAhora, frescura, iniciales, textoPosicion } from "./posiciones";
import { coordenadasDeGeometria, coordenadasReales, llegadasDeCitas, ordenTrasMover, presentacionDesvio, rutasDelDia, sePuedeReordenar, type ParadaRuta, type Ruta, type RutaDetalle } from "./rutas";

const cita = (p: Partial<CitaAgenda>): CitaAgenda => ({
    ordenId: 3,
    numero: "OT-000003",
    tipo: "retiro",
    estado: "programada",
    ordenVersion: 3,
    tecnicoId: 3,
    inicio: "2026-10-28T16:00:00Z",
    fin: "2026-10-28T17:00:00Z",
    ventanaInicio: null,
    ventanaFin: null,
    zonaId: 4,
    cuadrillaId: 1,
    direccion: "Colonia El Prado",
    lat: null,
    lon: null,
    clienteId: null,
    servicioId: null,
    ...p,
});

const tecnico = (p: Partial<Tecnico> & Pick<Tecnico, "id" | "usuarioId" | "nombre">): Tecnico => ({
    activo: true,
    actualizadoEn: "2026-10-01T00:00:00Z",
    creadoEn: "2026-10-01T00:00:00Z",
    baseLat: null,
    baseLon: null,
    bodegaId: null,
    correo: "x@iris.test",
    cuadrillaId: 1,
    habilidades: [],
    version: 1,
    ...p,
});

describe("fechas de Guatemala", () => {
    it("el día de Guatemala va de 06:00 UTC a 06:00 UTC", () => {
        expect(rangoUtc("2026-10-28", 1)).toEqual({ desde: "2026-10-28T06:00:00Z", hasta: "2026-10-29T06:00:00Z" });
        expect(rangoUtc("2026-10-26", 7)).toEqual({ desde: "2026-10-26T06:00:00Z", hasta: "2026-11-02T06:00:00Z" });
        expect(diaDesdeIso("2026-10-29T05:59:00Z")).toBe("2026-10-28");
        expect(diaDesdeIso("2026-10-29T06:00:00Z")).toBe("2026-10-29");
        expect(hoyGuatemala(Date.parse("2026-10-29T03:00:00Z"))).toBe("2026-10-28");
    });

    it("semana desde el lunes, sumar días entre meses y validar días", () => {
        expect(inicioSemana("2026-10-28")).toBe("2026-10-26");
        expect(inicioSemana("2026-11-01")).toBe("2026-10-26");
        expect(inicioSemana("2026-10-26")).toBe("2026-10-26");
        expect(sumarDias("2026-10-31", 1)).toBe("2026-11-01");
        expect(esDia("2026-02-30")).toBe(false);
        expect(esDia("2026-10-28")).toBe(true);
    });

    it("horas y minutos del día en Guatemala", () => {
        expect(horaDe("2026-10-28T16:30:00Z")).toBe("10:30");
        expect(minutosEnDia("2026-10-28T16:30:00Z", "2026-10-28")).toBe(630);
        expect(isoDesdeDia("2026-10-28", 630)).toBe("2026-10-28T16:30:00Z");
        expect(etiquetaDia("2026-10-28")).toBe("mié 28/10");
    });

    it("antigüedad legible", () => {
        expect(textoAntiguedad(42)).toBe("hace 42 s");
        expect(textoAntiguedad(185)).toBe("hace 3 min");
        expect(textoAntiguedad(2 * 3600 + 5 * 60)).toBe("hace 2 h 5 min");
        expect(textoAntiguedad(3 * 86400)).toBe("hace 3 días");
    });
});

describe("calendario", () => {
    it("filas: técnicos activos por nombre y, al final, los que tienen citas sin ficha en el filtro", () => {
        const filas = filasDeTecnicos(
            [tecnico({ id: 2, usuarioId: 11, nombre: "Jorge Méndez" }), tecnico({ id: 1, usuarioId: 3, nombre: "Lucía Ramírez" }), tecnico({ id: 3, usuarioId: 12, nombre: "Inactivo", activo: false })],
            [cita({ tecnicoId: 3 }), cita({ ordenId: 9, tecnicoId: 40, cuadrillaId: 7 })],
            [],
        );
        expect(filas.map((f) => [f.usuarioId, f.nombre, f.sinFicha])).toEqual([
            [11, "Jorge Méndez", false],
            [3, "Lucía Ramírez", false],
            [40, "Técnico 40", true],
        ]);
    });

    it("citas y ausencias del día del técnico", () => {
        const citas = [cita({ ordenId: 5, inicio: "2026-10-28T19:00:00Z", fin: "2026-10-28T20:00:00Z" }), cita({}), cita({ ordenId: 6, inicio: "2026-10-29T16:00:00Z", fin: "2026-10-29T17:00:00Z" })];
        expect(citasDelDia(citas, 3, "2026-10-28").map((c) => c.ordenId)).toEqual([3, 5]);
        const aus = [{ id: 1, tecnicoId: 3, fichaId: 1, motivo: "vacaciones", inicio: "2026-10-27T06:00:00Z", fin: "2026-10-29T06:00:00Z" }];
        expect(ausenciasDelDia(aus, 3, "2026-10-28")).toHaveLength(1);
        expect(ausenciasDelDia(aus, 3, "2026-10-29")).toHaveLength(0);
        expect(ausenciasDelDia(aus, 11, "2026-10-28")).toHaveLength(0);
    });

    it("horario base 06–20 que se amplía con citas y ventanas, y tramos en %", () => {
        expect(horarioDelDia([], "2026-10-28")).toEqual({ desde: 360, hasta: 1200 });
        const h = horarioDelDia([cita({ inicio: "2026-10-28T11:30:00Z", fin: "2026-10-28T12:30:00Z", ventanaFin: "2026-10-29T03:10:00Z" })], "2026-10-28");
        expect(h).toEqual({ desde: 300, hasta: 1320 });
        const t = tramoEnHorario("2026-10-28T13:00:00Z", "2026-10-28T14:00:00Z", "2026-10-28", { desde: 360, hasta: 1200 });
        expect(t?.izquierda).toBeCloseTo(((420 - 360) / 840) * 100);
        expect(t?.ancho).toBeCloseTo((60 / 840) * 100);
        expect(tramoEnHorario("2026-10-28T08:00:00Z", "2026-10-28T09:00:00Z", "2026-10-28", { desde: 360, hasta: 1200 })).toBeNull();
    });

    it("soltar en la línea redondea a 15 minutos dentro del horario", () => {
        const h = { desde: 360, hasta: 1200 };
        expect(minutosEnFraccion(0.5, h)).toBe(780);
        expect(minutosEnFraccion(0.506, h)).toBe(780);
        expect(minutosEnFraccion(-1, h)).toBe(360);
        expect(minutosEnFraccion(2, h)).toBe(1185);
    });

    it("nombres del catálogo o «Zona N»", () => {
        expect(nombreDe([{ id: 4, nombre: "Centro" }], 4, "Zona")).toBe("Centro");
        expect(nombreDe([], 9, "Zona")).toBe("Zona 9");
        expect(nombreDe(undefined, null, "Zona")).toBeUndefined();
    });
});

describe("mover una orden", () => {
    it("elige la acción explícita por estado y por si lleva hora", () => {
        expect(accionMover("pendiente", true)).toBe("programar");
        expect(accionMover("programada", true)).toBe("reprogramar");
        expect(accionMover("en_camino", true)).toBe("reprogramar");
        expect(accionMover("fallida", true)).toBe("reprogramar");
        expect(accionMover("pendiente", false)).toBe("asignar");
        expect(accionMover("fallida", false)).toBeNull();
        expect(accionMover("en_sitio", true)).toBeNull();
        expect(accionMover("cerrada", true)).toBeNull();
        expect(sePuedeMover("fallida", (p) => p.includes("ordenes.asignar"))).toBe(false);
        expect(sePuedeMover("fallida", (p) => p.includes("ordenes.reprogramar"))).toBe(true);
        expect(sePuedeMover("en_camino", (p) => p.includes("ordenes.asignar"))).toBe(true);
        expect(sePuedeMover("en_sitio", () => true)).toBe(false);
    });

    it("las franjas con hora solo aceptan el arrastre con el permiso de programar o reprogramar", () => {
        const soloAsignar = (p: readonly string[]) => p.includes("ordenes.asignar");
        const programada = movibleDeCita(cita({}));
        const pendiente = { ...programada, estado: "pendiente" as const, inicio: null, fin: null };
        const fallida = { ...programada, estado: "fallida" as const };
        // Solo `ordenes.asignar`: ninguna franja con hora acepta; el nombre del técnico sí.
        expect(arrastrePermitido(programada, true, soloAsignar)).toBeNull();
        expect(arrastrePermitido(pendiente, true, soloAsignar)).toBeNull();
        expect(arrastrePermitido(programada, false, soloAsignar)).toBe(programada);
        expect(arrastrePermitido(pendiente, false, soloAsignar)).toBe(pendiente);
        // Cada acción con su permiso exacto.
        expect(arrastrePermitido(programada, true, (p) => p.includes("ordenes.reprogramar"))).toBe(programada);
        expect(arrastrePermitido(pendiente, true, (p) => p.includes("ordenes.reprogramar"))).toBeNull();
        expect(arrastrePermitido(pendiente, true, (p) => p.includes("ordenes.programar"))).toBe(pendiente);
        // Una fallida no se asigna sin hora; y sin arrastre, nada.
        expect(arrastrePermitido(fallida, false, () => true)).toBeNull();
        expect(arrastrePermitido(null, true, () => true)).toBeNull();
    });

    it("reprogramar exige motivo y conserva la duración de la cita", () => {
        const m = movibleDeCita(cita({ inicio: "2026-10-28T16:00:00Z", fin: "2026-10-28T17:30:00Z" }));
        const esquema = esquemaMover(m);
        const sinMotivo = esquema.safeParse({ tecnicoId: "11", soloAsignar: false, fecha: "2026-10-29", hora: "09:00", motivo: "" });
        expect(sinMotivo.success).toBe(false);
        expect(sinMotivo.error?.issues.map((i) => i.path.join("."))).toEqual(["motivo"]);
        const ok = esquema.parse({ tecnicoId: "11", soloAsignar: false, fecha: "2026-10-29", hora: "09:00", motivo: " Cliente pidió mañana " });
        expect(ok).toEqual({ accion: "reprogramar", tecnicoId: 11, inicio: "2026-10-29T15:00:00Z", fin: "2026-10-29T16:30:00Z", motivo: "Cliente pidió mañana" });
        if (ok.accion !== "asignar") {
            expect(entradaProgramar(ok, 3)).toEqual({ version: 3, inicio: "2026-10-29T15:00:00Z", fin: "2026-10-29T16:30:00Z", tecnicoId: 11, motivo: "Cliente pidió mañana" });
        }
    });

    it("programar una pendiente sin fin (lo calcula la API) y validar día, hora y técnico", () => {
        const m = { ...movibleDeCita(cita({})), estado: "pendiente" as const, inicio: null, fin: null };
        const esquema = esquemaMover(m);
        expect(esquema.parse({ tecnicoId: "3", soloAsignar: false, fecha: "2026-10-28", hora: "14:15", motivo: "" })).toEqual({
            accion: "programar",
            tecnicoId: 3,
            inicio: "2026-10-28T20:15:00Z",
            fin: undefined,
            motivo: undefined,
        });
        const mal = esquema.safeParse({ tecnicoId: "", soloAsignar: false, fecha: "2026-13-01", hora: "25:00", motivo: "" });
        expect(mal.error?.issues.map((i) => i.path.join(".")).sort()).toEqual(["fecha", "hora", "tecnicoId"]);
        expect(esquema.parse({ tecnicoId: "3", soloAsignar: true, fecha: "", hora: "", motivo: "" })).toEqual({ accion: "asignar", tecnicoId: 3 });
        expect(entradaAsignar(3, 7, null)).toEqual({ version: 7, tecnicoId: 3 });
        expect(entradaAsignar(3, 7, 2)).toEqual({ version: 7, tecnicoId: 3, cuadrillaId: 2 });
    });

    it("valores iniciales: del destino del arrastre o de la cita actual", () => {
        const m = movibleDeCita(cita({}));
        expect(valoresIniciales(m, undefined, "2026-10-30")).toEqual({ tecnicoId: "3", soloAsignar: false, fecha: "2026-10-28", hora: "10:00", motivo: "" });
        expect(valoresIniciales(m, { tecnicoId: 11, dia: "2026-10-29", minutos: 13 * 60 + 45 }, "2026-10-30")).toMatchObject({ tecnicoId: "11", fecha: "2026-10-29", hora: "13:45" });
        expect(valoresIniciales(m, { tecnicoId: 11, dia: null, minutos: null }, "2026-10-30")).toMatchObject({ tecnicoId: "11", soloAsignar: true });
        // Quien solo puede asignar empieza en «solo asignar» (nunca en una acción que no puede hacer).
        expect(valoresIniciales(m, undefined, "2026-10-30", (conHora) => !conHora)).toMatchObject({ soloAsignar: true });
    });

    const asignable = (p: Partial<Asignable>): Asignable => ({
        tecnicoId: 1,
        usuarioId: 11,
        nombre: "Jorge Méndez",
        cuadrillaId: 1,
        habilidades: [],
        disponible: true,
        motivo: null,
        minutosLibres: 240,
        turnos: [{ inicio: "2026-10-28T14:00:00Z", fin: "2026-10-29T00:00:00Z" }],
        ausencias: [],
        ocupado: [],
        libres: [
            { inicio: "2026-10-28T14:00:00Z", fin: "2026-10-28T16:00:00Z" },
            { inicio: "2026-10-28T20:00:00Z", fin: "2026-10-29T00:00:00Z" },
        ],
        capacidad: [],
        ...p,
    });

    it("comprueba el destino contra asignables: fuera de lista, no disponible, hueco y ventana", () => {
        const m = movibleDeCita(cita({ inicio: "2026-10-28T16:00:00Z", fin: "2026-10-28T17:00:00Z", ventanaInicio: "2026-10-28T14:00:00Z", ventanaFin: "2026-10-28T16:00:00Z" }));
        expect(comprobarDestino(m, undefined, { inicio: "2026-10-28T14:00:00Z", duracionTipo: 60 })[0]?.tono).toBe("error");
        const ausente = comprobarDestino(m, asignable({ disponible: false, motivo: "ausente" }), { inicio: "2026-10-28T14:00:00Z", duracionTipo: 60 });
        expect(ausente).toEqual([{ tono: "error", texto: "Jorge Méndez: Está ausente ese día." }]);
        expect(comprobarDestino(m, asignable({}), { inicio: "2026-10-28T14:30:00Z", duracionTipo: 60 })).toEqual([{ tono: "success", texto: "Jorge Méndez tiene libre ese horario (60 min)." }]);
        const choca = comprobarDestino(m, asignable({}), { inicio: "2026-10-28T15:30:00Z", duracionTipo: 60 });
        expect(choca[0]).toEqual({ tono: "error", texto: "Ese horario no está libre para Jorge Méndez (60 min). Libre: 08:00–10:00 y 14:00–18:00." });
        const fueraVentana = comprobarDestino(m, asignable({}), { inicio: "2026-10-28T20:30:00Z", duracionTipo: 60 });
        expect(fueraVentana.map((c) => c.tono)).toEqual(["success", "warning"]);
        expect(fueraVentana[1]?.texto).toContain("08:00–10:00");
        expect(comprobarDestino(m, asignable({ disponible: false, motivo: "ausente" }), { inicio: null, duracionTipo: 60 })[0]?.tono).toBe("success");
    });
});

describe("posiciones en vivo", () => {
    it("antigüedad = la de la API + lo que pasó desde la lectura; frescura y texto", () => {
        expect(antiguedadAhora({ antiguedadSeg: 40 }, 1_000_000, 1_030_000)).toBe(70);
        expect(antiguedadAhora({ antiguedadSeg: 40 }, 1_000_000, 900_000)).toBe(40);
        expect(frescura(180)).toBe("en_vivo");
        expect(frescura(181)).toBe("retrasada");
        expect(frescura(901)).toBe("sin_senal");
        expect(textoPosicion(70)).toBe("En vivo · hace 1 min");
        expect(textoPosicion(2 * 3600)).toBe("Sin señal reciente · hace 2 h");
        expect(iniciales("Lucía  Ramírez Pérez")).toBe("LP");
        expect(iniciales("Jorge")).toBe("J");
    });
});

describe("ruta propuesta frente a la real", () => {
    const parada = (p: Partial<ParadaRuta> & Pick<ParadaRuta, "posicion" | "ordenId">): ParadaRuta => ({
        numero: `OT-${p.ordenId}`,
        tipo: "visita",
        estado: "programada",
        oculta: false,
        vigente: true,
        direccion: "",
        lat: null,
        lon: null,
        sinCoordenadas: false,
        duracionMin: 60,
        ventanaInicio: null,
        ventanaFin: null,
        citaInicio: null,
        fija: false,
        llegadaEstimada: null,
        salidaEstimada: null,
        viajeSeg: 0,
        distanciaM: 0,
        esperaSeg: 0,
        retrasoSeg: 0,
        fueraDeVentana: false,
        enCaminoEn: null,
        llegadaReal: null,
        finalizadaEn: null,
        desvioSeg: null,
        ...p,
    });

    it("borrador y publicada del día", () => {
        const r = (id: number, estado: Ruta["estado"]) => ({ id, estado }) as Ruta;
        expect(rutasDelDia([r(1, "publicada"), r(2, "borrador")])).toEqual({ borrador: r(2, "borrador"), publicada: r(1, "publicada") });
        expect(rutasDelDia([])).toEqual({ borrador: null, publicada: null });
    });

    it("desvío: a tiempo (±5 min), tarde o antes; sin dato, nada", () => {
        expect(presentacionDesvio(null)).toBeNull();
        expect(presentacionDesvio(300)?.texto).toBe("A tiempo");
        expect(presentacionDesvio(-120)?.tono).toBe("success");
        expect(presentacionDesvio(720)).toMatchObject({ tono: "warning", texto: "12 min tarde" });
        expect(presentacionDesvio(-900)).toMatchObject({ tono: "info", texto: "15 min antes" });
    });

    it("reordenar: solo en borrador, sin ocultas, y sin mover ni saltar las fijas", () => {
        const paradas = [parada({ posicion: 1, ordenId: 1, fija: true }), parada({ posicion: 2, ordenId: 2 }), parada({ posicion: 3, ordenId: 3 })];
        expect(sePuedeReordenar({ estado: "borrador", paradas })).toBe(true);
        expect(sePuedeReordenar({ estado: "publicada", paradas })).toBe(false);
        expect(sePuedeReordenar({ estado: "borrador", paradas: [...paradas, parada({ posicion: 4, ordenId: 0, oculta: true })] })).toBe(false);
        expect(ordenTrasMover(paradas, 1, 1)).toEqual([1, 3, 2]);
        expect(ordenTrasMover(paradas, 2, -1)).toEqual([1, 3, 2]);
        expect(ordenTrasMover(paradas, 1, -1)).toBeNull();
        expect(ordenTrasMover(paradas, 2, 1)).toBeNull();
    });

    it("geometría solo si es un LineString válido; recorrido real solo si está disponible", () => {
        expect(coordenadasDeGeometria(null)).toBeNull();
        expect(coordenadasDeGeometria({ type: "Point", coordinates: [1, 2] })).toBeNull();
        expect(coordenadasDeGeometria({ type: "LineString", coordinates: [[-90.5, 14.6], ["x", 1]] })).toBeNull();
        expect(coordenadasDeGeometria({ type: "LineString", coordinates: [[-90.5, 14.6], [-90.4, 14.7]] })).toEqual([
            [-90.5, 14.6],
            [-90.4, 14.7],
        ]);
        const real = (p: Partial<RutaDetalle["real"]>): RutaDetalle["real"] => ({ disponible: true, motivo: null, puntos: [], totalPuntos: 0, distanciaM: 0, primeraEn: null, ultimaEn: null, ordenVisitado: [], ...p });
        expect(coordenadasReales(real({ disponible: false, motivo: "sin_posiciones" }))).toBeNull();
        expect(
            coordenadasReales(
                real({
                    puntos: [
                        { lat: 14.6, lon: -90.5, registradaEn: "x" },
                        { lat: 14.7, lon: -90.4, registradaEn: "y" },
                    ],
                }),
            ),
        ).toEqual([
            [-90.5, 14.6],
            [-90.4, 14.7],
        ]);
    });

    it("llegadas reales desde la agenda (enSitioEn), en orden", () => {
        const citas = [cita({ ordenId: 5, enSitioEn: "2026-10-28T19:06:00Z" }), cita({ ordenId: 1, enSitioEn: "2026-10-28T14:30:00Z" }), cita({ ordenId: 3 }), cita({ ordenId: 9, tecnicoId: 11, enSitioEn: "2026-10-28T15:00:00Z" })];
        expect(llegadasDeCitas(citas, 3).map((c) => c.ordenId)).toEqual([1, 5]);
    });
});
