import type { Schema } from "@iris/contratos";

/*
 * Fixtures de órdenes, adjuntos y pendientes hacia Argos para la API falsa, tipados con
 * packages/contratos. Las fechas son relativas a la hora real en que se arma el escenario.
 */

type Orden = Schema<"Orden">;
type OrdenDetalle = Schema<"OrdenDetalle">;
type Evento = Schema<"EventoOrden">;
type Adjunto = Schema<"Adjunto">;
type Pendiente = Schema<"Pendiente">;

const MIN = 60_000;
const iso = (ms: number) => new Date(ms).toISOString();

/** PNG pequeños de prueba (generados): dos fotos y una firma con tinta oscura sobre transparente. */
export const PNG: Record<"fotoA" | "fotoB" | "firma", string> = {
    fotoA: "iVBORw0KGgoAAAANSUhEUgAAAKAAAAB4CAIAAAD6wG44AAAEPklEQVR42u3dV1YbQRBA0dkBjijvxQkHHMBBONs4B2wEQsC2vQSj1vRMl/qvq37mneK8HdwDSDPdVdXG2eHGqWwhO5HN6y6tOpYdrZq1zWSHs8tpf2V/ZAdtVw6Olv2W/ZL9lP1YdrXpu+zbRceir6JrX2SfZZ9k+3XX9+d1H2UfZO9l7+abaW/XOtl8I3vd1rvolexlW+W6YN0M2HVZur29FNh1cboJsOsSdSOw60J1A7DrcnUzYNdl6famKbDr4nQTYNcl6vani8p1wboB2HW5uhmw67J0JbDr4nT7Lxpg1yXqRmDXheoGYNfl6rbArovUrYFdl6rbf76oXBesG4FdF6obgIm6/0p/YLoZMOV3txgYpiuBQX+ZtcAU3f6zBpj1f1cFDNKNwLhPVeXALN0ATPzMbAPcfd0GmPaNyAAYobsCBn7f1QJTdAdPU2DQ0wwVMEg3AWY9qyoHZulGYNyTSBvg7usGYOJzZgNghG4GTHmLoAWm6A52U2DQOyIVMEg3AWa9ASwHZulGYNz7XRvg7usGYOLbewNghO5g97RCns3QAlN0JTDo5I0KGKQ72GmAWeeqyoFZuhEYd2quGBimG4CJZyINgBG6GTDlxKsWmKIrgUHnmVXAIN3BkwaYdVq9HJilG4FxdxGMgDuvG4CJN00sgAm6w3Vgyj0iNTBEVwKDbonpgDm6w8cNMOsOoAIYpRuBcTc8y4FZugGYeH/XBrj7ugKYdDvbABih2wLD7t5rgSm6w0cBmDdZQQUM0l0CI+dmlAOzdCMwbiqKHpihG4CJM2+UwBjdDJgy0UgDTNIdPkyBQfOqioFhugkwaxpZOTBLNwLjZs3ZAHdfNwATJwkaACN0M2DKnEgtMEV3uJ0Cg6aAqoBBuqPtswo547UcmKUbgXETfG2Au68bgInzmQ2AEboZMGX6thaYoiuBQbPVVcAg3dGDBpg1Ob8cmKUbgXF7EWyAu68bgIlbLwyAEbo1MG+niRaYorsERm6sUQGDdEf3G2DWPqJyYJZuBMZtm9IAk3QDMHGXWDEwTDcDpmyKMwBG6Epg0B5ALTBFd3SvAWZteVQBg3QjMG6HZzkwSzcAEze02gB3X3cFDNy/awCM0B2vA1O2K2uBKbrjuykwaHe2ChikmwCzNqOXA7N0IzBu770NcPd1AzBO1wYYobsOzNA1AKbojrcSYIxub08HDNJtgUm6KmCWbg0M07UCBuiOt84rnq4JMEM3A0bo6oExuhKYotubqoBJuuM7DTBIVwMM043ALN3+dGED3H3dAIzTtQFG6GbACF3euapiXQnsujjd8e0G2HWJuhHYdaG6kyWw63J1M2DXZelKYNfF6U5uNcCuS9SNwK4L1Q3ArsvVbYFdF6lbA7suVXdy87xyXbBuAuy6RN0I7LpQ3QDsulzdDNh1WbqTGymw6+J0E2DXJepGYNeF6l70H8Zw2eyopREiAAAAAElFTkSuQmCC",
    fotoB: "iVBORw0KGgoAAAANSUhEUgAAAKAAAAB4CAIAAAD6wG44AAAEeklEQVR42u3dV1YbQRBA0VmEbQy2yVnkDBLgnAM5iuycs71sL8Gqrm7Nf9ffO8V5O7gHkGa6q4q/a+1/Qr9Tv1I/pY5GP7T1ju+hb7HOr6kvqc/aRten0MfUh9R7qbvRO22z+23oTazndepV6qW21XsROk+dpU6lvkYn2nbfcego1l9PHaYOtJ2B/dBeaje1Iw022tZ2B7dCm7GhjdR6ak3bG34Rep56lnoqVRo90fYrj0OPYiMPUw9S97WD0Xuhu6k7qdvSWKNb2uHYzdBqbLxwXbDuigK7LlVXgF0XrFsCuy5Sd1mBXZeqK8CuC9YtgV0XqVtTYNel6gqw64J1a/WJwnXBulUFdl2qrgC7Llh3KQK7LlQ3ALsuV1eAXResu6jArkvVFWDXBes2gV2XqbsQgF0Xq7tQnyxcF6xbAsN0/+X+wHTnFZj3u5sPzNIVYORf5mxgmO78UQKG/d+1AJN05xSY96kqGximK8DIz8xGYIzubATGfSOyAJN0AzDx+242MEx39miqQD7NMAJjdGcUmPesygJM0hVg5JPIbGCYbgkMe85sAEbpTisw7y2CBZikK8DId0TZwDDdEhj2BtAEDNKdUmDe+918YJauACPf3mcDw3SnjqcL5NkMCzBJd1KBeSdvsoFhugKMPFdlA+boTkRg3Kk5AzBKNwATz0RmA8N0BRh54tUIjNEdV2DeeWYLMElXgJGn1bOBYbpNYNpdBCMwRncsAANvmliASbpjxzMF8h5RNjBMtwSG3RIzAYN0RxWYdwcwH5ilK8DIG57ZwDDd0ZMEDLu/awEm6Y4oMO92djYwTFeAkXfvjcAY3UoExk1WsACTdAMwcW5GNjBMt3IyWyCnohiBMbrDCsybeWMBJukKMHKiUTYwTLcEhs2rMgCjdIcUmDeNzAJM0hVg5Ky5bGCYbgkMmyRoAgbpDiowb05kPjBLV4CRU0CzgWG6A6dzBXLGqwWYpBuBeRN8s4FhugKMnM9sA+bo9kdg3PRtAzBKNwATZ6tnA8N0BRg5Od8IjNHtU2DeXgQLMElXgJFbL7KBYbpNYNpOEyMwRrc3AAM31liASbq9p/MFch9RNjBMtwSGbZsyAYN0exSYt0ssH5ilK8DITXHZwDDdnrMEDNsDaAEm6XYrMG/LYzYwTFeAkTs8jcAY3a4IjNvQagEm6QZg4v7dbGCYbtfZQoHcrmwExuh2KjBvd7YFmKQrwMjN6NnAMN0SGLb33gCM0u1QYJju6uG4BZikK8A83RUDMEy3BCbpWoFBuu0KDNM1AbN0BZinu2wAhuneOF8seLpGYJJuBIbpWoBhugLM061ZgTm61yMwS9cGjNINwDjdWn0iGximK8A83aoZGKN7TYFhurxzVdm6Auy6YN0msOsyddsCsOtiddvOlwrXBeuWwK6L1G1VYNel6gqw64J1Wy8SsOsida8qsOtSdQXYdcG6LRHYdaG6Adh1ubotF9XCdcG6VxTYdam6Auy6YN0S2HWRupcV2HWpugLsumDdEth1kbqXLqr/ATAiikZjpk5uAAAAAElFTkSuQmCC",
    firma: "iVBORw0KGgoAAAANSUhEUgAAAPAAAABQCAYAAAAnSfh8AAACRklEQVR42u3dO1LEQAwFQF+BjBtw/xNCyGbU4pFkSd1V5DsPPc94v9cFAAAAAAAAAAAAAAAAAADv+fj8+n79kwg0La8SQ/PyKjHHBksaNeWVP450De97ZY8j3YATjtxxpHt41m5lSC2vYcp/fkHu3B4SJc7ffRWY4wOixLnlVWKOD4YS5xdYiQnbEQxVfHkVmNAd4T/30NuG8cR6lZiwgbrzJ2slpmiolLiuwEqsvFfEUClxXNZ2Ya6of/67Bd1Q4oysTbTyPuIxTRvGyDV5JUCBLyXunbUS/+ZgsUrc8mLpOYZFF66nL3bSsGWuY/MThgqsxCOy3lbidbcOnRba/Z9S+finl3jtO/oUeN8JonuJvQmocSG67xDddrFOhV33rLsCu39/+n2k8v4RjguPx/vUi47iDr+fVIS5j11pBxe44z1l95yrX/q6mFPeDmuY+Nqq7Az/+Hv4ibtH5loUd0l5s9ey/bPM2e/f1lQFLi3u1I9AZn2KSlOHlzdjTcpbk7WmLgqo6ltENr60EbXG8dmdGBA7w5nybso1a47GZnjqau9ody9z57e4WRqZ86ljm69oVd4uF8zV5fW2M79w0HEXHpV3ZHE3/cqBHwnrd8EcW9xTpTdU7+Woni6Yt4uXdRHYPlTKW7ODts5b4Z5RYtnX7MLt81be+qGSfc0uPCJzw1Obr/zrTzyS4/bzDIap7tQjNY4UWFLxJZY5ISWWkBMPTQdLMjUllhK3hksSdSWWDjQtsVTAiQcAAAAAAAAAAAAAAGCGH+4G7wiz/7dxAAAAAElFTkSuQmCC",
};

export const ADJ = {
    fotoAntena: "01936f3e-8a4b-7c2d-9e1f-000000000001",
    fotoOnt: "01936f3e-8a4b-7c2d-9e1f-000000000002",
    fotoSubiendo: "01936f3e-8a4b-7c2d-9e1f-000000000003",
    firma: "01936f3e-8a4b-7c2d-9e1f-000000000004",
} as const;

export const TIPOS_ORDEN: Schema<"TipoOrden">[] = [
    {
        id: 1,
        codigo: "instalacion",
        nombre: "Instalación",
        duracionMin: 120,
        exigeInstalar: true,
        exigeRetirar: false,
        plantillaId: 1,
        plantillas: [
            { id: 1, codigo: "instalacion_fibra", nombre: "Instalación de fibra", requiereFirma: true, items: null },
            { id: 2, codigo: "instalacion_inalambrica", nombre: "Instalación inalámbrica", requiereFirma: true, items: null },
        ],
    },
    { id: 2, codigo: "visita", nombre: "Visita técnica", duracionMin: 60, exigeInstalar: false, exigeRetirar: false, plantillaId: 3, plantillas: [{ id: 3, codigo: "visita", nombre: "Visita técnica", requiereFirma: false, items: null }] },
    { id: 3, codigo: "retiro", nombre: "Retiro", duracionMin: 60, exigeInstalar: false, exigeRetirar: true, plantillaId: 4, plantillas: [{ id: 4, codigo: "retiro", nombre: "Retiro", requiereFirma: true, items: null }] },
    { id: 4, codigo: "cambio_equipo", nombre: "Cambio de equipo", duracionMin: 90, exigeInstalar: true, exigeRetirar: true, plantillaId: 5, plantillas: [{ id: 5, codigo: "cambio_equipo", nombre: "Cambio de equipo", requiereFirma: true, items: null }] },
    { id: 5, codigo: "mantenimiento", nombre: "Mantenimiento", duracionMin: 90, exigeInstalar: false, exigeRetirar: false, plantillaId: 6, plantillas: [{ id: 6, codigo: "mantenimiento", nombre: "Mantenimiento", requiereFirma: false, items: null }] },
];

export const TECNICO_ID = 3;

export type EscenarioOrdenes = {
    ordenes: OrdenDetalle[];
    eventos: (Evento & { ordenId: number })[];
    adjuntos: Adjunto[];
    pendientes: Pendiente[];
};

function base(id: number, ahora: number, empresa: number | null, p: Partial<OrdenDetalle>): OrdenDetalle {
    const tipo = p.tipo ?? "visita";
    return {
        id,
        numero: `OT-${String(id).padStart(6, "0")}`,
        tipo,
        tipoId: TIPOS_ORDEN.find((t) => t.codigo === tipo)?.id ?? 2,
        plantillaId: null,
        estado: "pendiente",
        resultado: null,
        motivo: null,
        motivoFalla: null,
        empresaId: empresa,
        ticketId: null,
        prospectoId: null,
        clienteId: null,
        servicioId: null,
        zonaId: 4,
        cuadrillaId: null,
        bodegaId: null,
        tecnicoId: null,
        direccion: `Colonia El Prado, casa ${id}`,
        lat: null,
        lon: null,
        contacto: { nombre: null, telefono: null },
        descripcion: "",
        programadaInicio: null,
        programadaFin: null,
        asignadaEn: null,
        enCaminoEn: null,
        enSitioEn: null,
        finalizadaEn: null,
        version: 1,
        creadoEn: iso(ahora - (200 - id) * MIN),
        actualizadoEn: iso(ahora - (100 - id) * MIN),
        checklist: [],
        notas: [],
        materiales: [],
        equipos: [],
        adjuntos: [],
        requiereFirma: false,
        ...p,
    };
}

function pendiente(id: number, ordenId: number, p: Partial<Pendiente>): Pendiente {
    return {
        id,
        sistema: "argosisp",
        entidad: "orden",
        entidadId: String(ordenId),
        operacion: "consumo",
        metodo: "POST",
        ruta: "/inventario/consumos",
        payload: {},
        idempotencyKey: `iris:${p.operacion ?? "consumo"}:${ordenId}`,
        estado: "pendiente",
        intentos: 0,
        proximoIntento: null,
        ultimoError: null,
        ultimoStatus: null,
        dependeDe: null,
        datosIncompletos: false,
        conflicto: null,
        conflictoEn: null,
        payloadConflicto: null,
        hechoEn: null,
        version: 1,
        creadoEn: "2026-10-28T15:00:00Z",
        ...p,
    };
}

/** Hoy a las `hora` de Guatemala (UTC−6), en ms. */
function hoyA(ahora: number, hora: number): number {
    const local = new Date(ahora - 6 * 3_600_000);
    return Date.UTC(local.getUTCFullYear(), local.getUTCMonth(), local.getUTCDate(), 6) + hora * 3_600_000;
}

/**
 * Órdenes 1–8 con casos (1 cerrada con evidencias y pendientes en Argos, 2 pendiente desde el
 * ticket 2, 3 programada, 4 en camino, 5 en sitio, 6 fallida, 7 cancelada, 8 cerrada con un
 * conflicto) y 9–35 de relleno (dos páginas de 30).
 */
export function escenarioOrdenes(ahora: number, empresas: readonly number[]): EscenarioOrdenes {
    const empresa = empresas[0] ?? null;
    const t = (h: number) => iso(hoyA(ahora, h));
    const ordenes: OrdenDetalle[] = [
        base(1, ahora, empresa, {
            tipo: "instalacion",
            plantillaId: 1,
            estado: "cerrada",
            resultado: "exitosa",
            ticketId: 1,
            clienteId: 501,
            servicioId: 5010,
            tecnicoId: TECNICO_ID,
            bodegaId: 30,
            direccion: "4a calle 3-21, zona 1",
            lat: 14.634915,
            lon: -90.506882,
            contacto: { nombre: "María Castillo", telefono: "5555 0101" },
            descripcion: "Instalar fibra 100 Mb/s y dejar la ONT configurada.",
            programadaInicio: t(8),
            programadaFin: t(10),
            asignadaEn: t(7),
            enCaminoEn: t(8),
            enSitioEn: t(8.5),
            finalizadaEn: t(9.75),
            version: 9,
            requiereFirma: true,
            checklist: [
                { itemId: 11, codigo: "potencia", etiqueta: "Potencia óptica", tipo: "medicion", obligatorio: true, unidad: "dBm", posicion: 1, valor: -19.4 as never, marcadoPor: TECNICO_ID, ocurridoEn: t(9) },
                { itemId: 12, codigo: "antena", etiqueta: "Foto de la roseta", tipo: "foto", obligatorio: true, unidad: "", posicion: 2, valor: ADJ.fotoAntena as never, marcadoPor: TECNICO_ID, ocurridoEn: t(9.1) },
                { itemId: 13, codigo: "navega", etiqueta: "El cliente navega", tipo: "si_no", obligatorio: true, unidad: "", posicion: 3, valor: true as never, marcadoPor: TECNICO_ID, ocurridoEn: t(9.5) },
                { itemId: 14, codigo: "comentario", etiqueta: "Comentario del cliente", tipo: "texto", obligatorio: false, unidad: "", posicion: 4, valor: null, marcadoPor: null, ocurridoEn: null },
            ],
            notas: [{ id: "n1", texto: "El cliente pidió el router en la sala.", autorId: TECNICO_ID, autorNombre: "Lucía Ramírez", canal: "campo", ocurridoEn: t(9.2) }],
            materiales: [
                { id: "m1", productoId: 801, descripcion: "Cable drop 1 hilo (m)", cantidad: 85, bodegaId: 30, excedeExistencia: false, ocurridoEn: t(9.3) },
                { id: "m2", productoId: 802, descripcion: "Conector SC/APC", cantidad: 2, bodegaId: 30, excedeExistencia: true, ocurridoEn: t(9.3) },
            ],
            equipos: [{ id: "e1", accion: "instalar", unidadId: 9001, serie: "ZTEG12345678", mac: "AA:BB:CC:DD:EE:01", modelo: "ZTE F670L", condicion: null, validado: false, ocurridoEn: t(9.4) }],
            adjuntos: [{ id: ADJ.fotoOnt, tipo: "foto", itemId: null, descripcion: "ONT instalada", ocurridoEn: t(9.45) }],
            firma: { nombre: "María Castillo", documento: "2456 78901 0101", ocurridoEn: t(9.7), adjuntoId: ADJ.firma },
        }),
        base(2, ahora, empresa, { tipo: "visita", ticketId: 2, clienteId: 502, direccion: "Km 18 carretera a El Salvador", contacto: { nombre: "Ferretería El Tornillo", telefono: "5555 0202" }, descripcion: "Intermitencia por las tardes" }),
        base(3, ahora, empresa, { tipo: "retiro", estado: "programada", tecnicoId: TECNICO_ID, programadaInicio: t(10), programadaFin: t(11), asignadaEn: t(7), version: 3 }),
        base(4, ahora, empresa, { tipo: "cambio_equipo", estado: "en_camino", tecnicoId: TECNICO_ID, programadaInicio: t(11), programadaFin: t(12.5), enCaminoEn: t(10.8), version: 4 }),
        base(5, ahora, empresa, { tipo: "mantenimiento", estado: "en_sitio", tecnicoId: TECNICO_ID, programadaInicio: t(13), programadaFin: t(14.5), enSitioEn: t(13.1), version: 5 }),
        base(6, ahora, empresa, { tipo: "instalacion", estado: "fallida", resultado: "fallida", motivoFalla: "cliente_ausente", motivo: "Nadie abrió; se llamó dos veces.", tecnicoId: TECNICO_ID, programadaInicio: t(7), programadaFin: t(9), finalizadaEn: t(7.5), version: 6 }),
        base(7, ahora, empresa, { tipo: "visita", estado: "cancelada", resultado: "cancelada", motivo: "El cliente resolvió reiniciando el router.", finalizadaEn: t(6), version: 2 }),
        base(8, ahora, empresa, { tipo: "retiro", estado: "cerrada", resultado: "exitosa", tecnicoId: TECNICO_ID, finalizadaEn: t(6.5), version: 7 }),
    ];
    for (let id = 9; id <= 35; id++) {
        const programada = id % 3 === 0;
        ordenes.push(
            base(id, ahora, empresa, {
                tipo: (["visita", "instalacion", "mantenimiento"] as const)[id % 3],
                estado: programada ? "programada" : "pendiente",
                ...(programada ? { tecnicoId: TECNICO_ID, programadaInicio: iso(hoyA(ahora, 8) + 86_400_000), programadaFin: iso(hoyA(ahora, 9) + 86_400_000) } : {}),
            }),
        );
    }
    const ev = (id: number, ordenId: number, e: Partial<Evento>): Evento & { ordenId: number } => ({
        id,
        ordenId,
        tipo: "estado",
        de: null,
        a: null,
        motivo: null,
        datos: {},
        actorTipo: "usuario",
        actorId: TECNICO_ID,
        actorNombre: "Lucía Ramírez",
        canal: "campo",
        ocurridoEn: t(8),
        creadoEn: t(8),
        ...e,
    });
    const eventos = [
        ev(1, 1, { tipo: "creada", a: "pendiente", actorId: 1, actorNombre: "Ana López", canal: "panel", ocurridoEn: t(6) }),
        ev(2, 1, { tipo: "programada", actorId: 1, actorNombre: "Ana López", canal: "panel", datos: { tecnico_id: TECNICO_ID, inicio: t(8), fin: t(10), reprogramada: false }, ocurridoEn: t(7) }),
        ev(3, 1, { de: "programada", a: "en_camino", datos: { lat: 14.6201, lon: -90.5302, precision: 12, tomado_en: t(8) }, ocurridoEn: t(8) }),
        ev(4, 1, { de: "en_camino", a: "en_sitio", datos: { lat: 14.634902, lon: -90.506871, precision: 6, tomado_en: t(8.5) }, ocurridoEn: t(8.5) }),
        ev(5, 1, { de: "en_sitio", a: "cerrada", datos: { lat: 14.63491, lon: -90.50688, precision: 5, tomado_en: t(9.75) }, ocurridoEn: t(9.75) }),
        ev(6, 6, { de: "programada", a: "fallida", motivo: "Nadie abrió; se llamó dos veces.", ocurridoEn: t(7.5) }),
    ];
    const adjunto = (id: string, ordenId: number, a: Partial<Adjunto>): Adjunto => ({
        id,
        entidad: "orden",
        entidadId: String(ordenId),
        tipo: "foto",
        mime: "image/png",
        tamano: 1200,
        sha256: "0".repeat(64),
        recibido: 1200,
        estado: "completo",
        claveChecklist: null,
        lat: null,
        lon: null,
        tomadoEn: t(9.1),
        subidoPor: TECNICO_ID,
        motivoRechazo: null,
        urlSubida: `/api/v1/adjuntos/${id}/subida`,
        urlDescarga: `/api/v1/adjuntos/${id}`,
        version: 1,
        creadoEn: t(9.1),
        completadoEn: t(9.2),
        ...a,
    });
    const adjuntos = [
        adjunto(ADJ.fotoAntena, 1, { claveChecklist: "antena", lat: 14.634915, lon: -90.506882 }),
        adjunto(ADJ.fotoOnt, 1, { tomadoEn: t(9.45) }),
        adjunto(ADJ.fotoSubiendo, 1, { estado: "subiendo", recibido: 300, completadoEn: null }),
        adjunto(ADJ.firma, 1, { tipo: "firma", tomadoEn: t(9.7) }),
    ];
    const pendientes = [
        pendiente(101, 1, { operacion: "activar_instalacion", ruta: "/servicios/5010/activar", estado: "hecho", intentos: 1, hechoEn: t(9.8) }),
        pendiente(102, 1, {
            operacion: "consumo",
            estado: "fallido_negocio",
            intentos: 1,
            ultimoStatus: 422,
            ultimoError: "Argos rechazó el consumo: el producto 802 no tiene existencia suficiente en la bodega 30 (pide 2, hay 0). Revisa la bodega del técnico o registra una entrada antes de reintentar.",
            version: 2,
        }),
        pendiente(103, 1, { operacion: "asignar_comodato", estado: "bloqueado", dependeDe: [102], ultimoError: "depende_de_102_fallido", version: 2 }),
        pendiente(104, 8, { operacion: "retiro", estado: "hecho", intentos: 1, hechoEn: t(7), conflicto: "cierre_repetido_con_otros_datos", conflictoEn: t(7.2), payloadConflicto: {} }),
    ];
    return { ordenes, eventos, adjuntos, pendientes };
}

/** `Orden` (respuesta de las acciones) a partir del detalle. */
export function soloOrden(d: OrdenDetalle): Orden {
    const o: Partial<OrdenDetalle> = { ...d };
    delete o.checklist;
    delete o.notas;
    delete o.materiales;
    delete o.equipos;
    delete o.adjuntos;
    delete o.firma;
    delete o.requiereFirma;
    return o as Orden;
}
