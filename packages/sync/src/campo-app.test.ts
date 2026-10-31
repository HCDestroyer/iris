// Reglas de pantalla compartidas por la PWA y la app nativa (fase 4 y 5): la vista y sus pestañas,
// la barra de estado discreta, los contadores de la barra inferior, los textos de «Por recibir» y
// «Mi bodega», y el paso final «Firma del cliente y cierre».
import { describe, expect, it } from "vitest";
import {
    contadoresPestanas,
    encolarFirmaYCierre,
    etiquetaPunto,
    mismaVista,
    PASOS_ORDEN,
    partesEnvio,
    pestanaDe,
    PESTANAS,
    resumenEnvio,
    resumenTrabajo,
    SITUACIONES_TRASLADO,
    situacionLocal,
    textoErrorLectura,
    textoValorItem,
    urlDeVista,
    vecinosPaso,
    vistaDeUrl,
    type Vista,
} from "./campo";
import { ErrorTransporte, type EstadoSync, type Traslado } from "./index";
import { montar } from "./pruebas/montaje";
import { itemDePrueba, ordenDePrueba } from "./pruebas/fabricas";

describe("vista de la app de campo", () => {
    it("la vista va en la URL (?orden= / ?vista=pendientes)", () => {
        expect(vistaDeUrl("?orden=12")).toEqual({ tipo: "orden", id: 12 });
        expect(vistaDeUrl("?vista=pendientes")).toEqual({ tipo: "pendientes" });
        expect(vistaDeUrl("?orden=abc")).toEqual({ tipo: "lista" });
        expect(urlDeVista({ tipo: "orden", id: 3 })).toBe("/campo?orden=3");
    });

    it("cada paso, traslado y pestaña tiene su dirección (ida y vuelta)", () => {
        const vistas: Vista[] = [
            { tipo: "lista" },
            { tipo: "orden", id: 7 },
            ...PASOS_ORDEN.map((paso): Vista => ({ tipo: "orden", id: 7, paso })),
            { tipo: "pendientes" },
            { tipo: "recibir" },
            { tipo: "traslado", id: 31 },
            { tipo: "yo" },
            { tipo: "bodega" },
        ];
        for (const v of vistas) {
            expect(vistaDeUrl(urlDeVista(v).replace("/campo", ""))).toEqual(v);
        }
        expect(urlDeVista({ tipo: "orden", id: 7, paso: "checklist" })).toBe("/campo?orden=7&paso=checklist");
        // Un paso desconocido abre el resumen; un traslado inválido, la lista.
        expect(vistaDeUrl("?orden=7&paso=otro")).toEqual({ tipo: "orden", id: 7 });
        expect(vistaDeUrl("?traslado=-3")).toEqual({ tipo: "lista" });
        expect(pestanaDe({ tipo: "orden", id: 7, paso: "fotos" })).toBe("hoy");
        expect(pestanaDe({ tipo: "traslado", id: 31 })).toBe("recibir");
        expect(pestanaDe({ tipo: "bodega" })).toBe("yo");
    });

    it("pestañas en orden y cada una abre una vista de su pestaña", () => {
        expect(PESTANAS.map((p) => p.texto)).toEqual(["Hoy", "Por recibir", "Pendientes", "Yo"]);
        for (const p of PESTANAS) {
            expect(pestanaDe(p.vista)).toBe(p.id);
        }
    });

    it("misma vista y vecinos de cada paso", () => {
        expect(mismaVista({ tipo: "orden", id: 1, paso: "fotos" }, { tipo: "orden", id: 1, paso: "fotos" })).toBe(true);
        expect(mismaVista({ tipo: "orden", id: 1 }, { tipo: "orden", id: 1, paso: "fotos" })).toBe(false);
        expect(vecinosPaso("llegada")).toEqual({ anterior: null, siguiente: "checklist" });
        expect(vecinosPaso("fotos")).toEqual({ anterior: "materiales", siguiente: "cierre" });
        expect(vecinosPaso("cierre")).toEqual({ anterior: "fotos", siguiente: null });
    });
});

type EstadoEnvio = Parameters<typeof resumenEnvio>[0] & { recepciones: EstadoSync["recepciones"] };

function estado(parcial: Partial<EstadoEnvio> = {}): EstadoEnvio {
    return {
        conexion: "en_linea",
        sincronizando: false,
        pendientes: 0,
        rechazos: [],
        esperandoAdjuntos: [],
        subidas: { pendientes: 0, bytesPendientes: 0, conError: 0, rechazadas: 0 },
        recepciones: [],
        ...parcial,
    };
}

describe("barra de estado discreta", () => {
    it("con señal y todo enviado es solo el punto («En línea, todo enviado»)", () => {
        const r = resumenEnvio(estado());
        expect(r.abierta).toBe(false);
        expect(etiquetaPunto("en_linea", r)).toBe("En línea, todo enviado");
        // Sin señal y sin nada por enviar tampoco se abre.
        const sinSenal = resumenEnvio(estado({ conexion: "sin_conexion" }));
        expect(sinSenal.abierta).toBe(false);
        expect(etiquetaPunto("sin_conexion", sinSenal)).toBe("Sin conexión, todo enviado");
    });

    it("crece con algo por enviar, un rechazo, la sesión vencida o la empresa no válida", () => {
        const conPendientes = resumenEnvio(estado({ conexion: "sin_conexion", pendientes: 2, subidas: { pendientes: 1, bytesPendientes: 9, conError: 0, rechazadas: 0 } }));
        expect(conPendientes).toMatchObject({ abierta: true, porEnviar: 2, archivos: 1, texto: "Sin conexión" });
        expect(partesEnvio(conPendientes)).toEqual({ pendientes: ["2 pendientes por enviar", "1 archivo por subir"], rechazos: null });
        expect(resumenEnvio(estado({ subidas: { pendientes: 1, bytesPendientes: 0, conError: 1, rechazadas: 1 } }))).toMatchObject({ abierta: true, rechazos: 1, archivos: 0 });
        expect(resumenEnvio(estado({ conexion: "sin_sesion" }))).toMatchObject({ abierta: true, texto: "Sesión vencida" });
        expect(resumenEnvio(estado({ conexion: "sin_empresa" }))).toMatchObject({ abierta: true, texto: "Empresa no válida" });
        expect(resumenEnvio(estado({ sincronizando: true, pendientes: 1 })).texto).toBe("Sincronizando…");
        expect(etiquetaPunto("en_linea", resumenEnvio(estado({ pendientes: 1 })))).toBe("En línea");
    });

    it("contadores de la barra inferior: lo pendiente y lo que falta por recibir (0 si no se leyó)", () => {
        const traslados: Traslado[] = [
            { id: 1, estado: "enviado", lineas: [] },
            { id: 2, estado: "enviado", lineas: [], recepcion: { estado: "recibida" } } as Traslado,
        ];
        const e = estado({ pendientes: 1, subidas: { pendientes: 2, bytesPendientes: 0, conError: 0, rechazadas: 1 } });
        expect(contadoresPestanas(e, null)).toEqual({ hoy: 0, recibir: 0, pendientes: 3, yo: 0 });
        expect(contadoresPestanas(e, traslados).recibir).toBe(1);
    });
});

describe("«Por recibir» y «Mi bodega»: textos", () => {
    it("situación local y su texto", () => {
        expect(situacionLocal({ estado: "rechazada" })).toBe("rechazado_telefono");
        expect(situacionLocal({ estado: "pendiente" })).toBe("en_telefono");
        expect(SITUACIONES_TRASLADO.rechazado_argos).toEqual({ tono: "error", texto: "Argos lo rechazó" });
    });

    it("error de lectura: sin señal, un problema conocido de Argos o el genérico", () => {
        expect(textoErrorLectura(new ErrorTransporte("red", "x"), "tu bodega")).toEqual({ sinSenal: true, texto: "Sin señal: conéctate para ver tu bodega." });
        const sinBodega = new ErrorTransporte("http", "x", 422, { type: "urn:iris:problema:tecnico_sin_bodega", title: "x", status: 422 });
        expect(textoErrorLectura(sinBodega, "tu bodega").texto).toMatch(/No tienes bodega en Argos/);
        expect(textoErrorLectura(new Error("x"), "tus traslados")).toEqual({ sinSenal: false, texto: "No se pudo leer tus traslados." });
    });
});

describe("«Firma del cliente y cierre»", () => {
    it("resumen del trabajo en palabras del cliente", () => {
        const orden = ordenDePrueba(1, {
            tipo: "visita",
            checklist: [itemDePrueba(1, { etiqueta: "Señal", valor: true }), itemDePrueba(2, { etiqueta: "Potencia", tipo: "numero", unidad: "dBm", valor: -18.5 }), itemDePrueba(3)],
        });
        const r = resumenTrabajo(orden);
        expect(r.descripcion).toBe("Visita técnica · checklist 2/3 · 0 fotos · 0 materiales · 0 equipos");
        expect(r.checklist.map((i) => i.etiqueta)).toEqual(["Señal", "Potencia"]);
        expect(r.checklist[0]?.valor).toBe("Sí");
        expect(r.checklist[1]?.valor).toMatch(/^-18[.,]5 dBm$/);
        expect(textoValorItem(itemDePrueba(9, { tipo: "si_no", valor: false }))).toBe("No");
    });

    it("encola la firma y el cierre juntos, en orden; sin firma, solo el cierre", async () => {
        const { motor } = await montar();
        await motor.registrar("orden.iniciar_camino", 1, {});
        await motor.registrar("orden.llegar", 1, {});
        await encolarFirmaYCierre(motor, {
            orden: { id: 1, requiereFirma: true },
            firma: { nombre: "Juan Pérez", documento: "1234", svg: "<svg/>", png: new Uint8Array([1, 2, 3]), tomadoEn: "2026-10-28T16:00:00Z" },
            resultado: "exitosa",
            posicion: { lat: 14.6, lon: -90.5 },
        });
        const cola = motor.obtenerEstado().cola;
        expect(cola.map((op) => op.tipo)).toEqual(["orden.iniciar_camino", "orden.llegar", "adjunto.registrar", "firma.capturar", "orden.cerrar"]);
        expect(cola.at(-1)?.datos).toMatchObject({ resultado: "exitosa", lat: 14.6, lon: -90.5 });
        expect(motor.obtenerEstado().adjuntos.find((a) => a.tipo === "firma")).toMatchObject({ obligatorio: true, mime: "image/png" });
        expect(motor.obtenerOrden(1)?.estado).toBe("cerrada");

        await motor.registrar("orden.iniciar_camino", 2, {});
        await motor.registrar("orden.llegar", 2, {});
        await encolarFirmaYCierre(motor, { orden: { id: 2, requiereFirma: false }, resultado: "parcial" });
        expect(motor.obtenerEstado().cola.filter((op) => op.ordenId === 2).map((op) => op.tipo)).toEqual(["orden.iniciar_camino", "orden.llegar", "orden.cerrar"]);
    });
});
