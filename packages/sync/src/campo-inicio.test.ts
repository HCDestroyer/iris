// Diseño «combinada» de la app del técnico (nov 2026), lo que comparten la PWA y la app nativa:
// «Hoy» (siguiente orden, anillo, mosaicos y «Después»), la agenda de «Mañana» y «Semana», la
// píldora de estado encima de la isla, el encabezado que se va al bajar y vuelve compacto al subir,
// la vista y los chips, y la hoja de acciones rápidas del «+».
import { describe, expect, it } from "vitest";
import {
    accionesRapidas,
    accionPrincipal,
    anilloAvance,
    avisoIsla,
    buscarOrdenes,
    chipDe,
    citasDelDia,
    citasPorDia,
    DESPLAZAMIENTO_INICIAL,
    esquemaMaterial,
    desplazar,
    diaDeOrden,
    esVistaDeDia,
    fechaEncabezado,
    iniciales,
    mananaEnGuatemala,
    mosaicoPorRecibir,
    mosaicoRuta,
    nombreCliente,
    pasoCantidad,
    PERMISO_AGREGAR_FORMULARIO,
    primerNombre,
    resumenDia,
    resumenHoy,
    sumarDias,
    textoFormularios,
    tiraSemana,
    tituloAgenda,
    tonoCita,
    vistaDeUrl,
} from "./campo";
import type { EstadoSync, OrdenVista, PlantillaFormulario, Traslado } from "./index";
import { itemDePrueba, ordenDePrueba } from "./pruebas/fabricas";

function vista(id: number, parcial: Partial<OrdenVista> = {}): OrdenVista {
    return { ...ordenDePrueba(id), reasignada: false, pendientes: 0, rechazos: 0, bloqueada: false, esperandoAdjuntos: false, subidasPendientes: 0, ...parcial };
}

describe("encabezado", () => {
    it("fecha, saludo e iniciales", () => {
        // 5 nov 2026, 08:00 en Guatemala.
        expect(fechaEncabezado(Date.parse("2026-11-05T14:00:00Z"))).toBe("Jueves 5 de noviembre");
        // A las 23:30 de Guatemala sigue siendo jueves (en UTC ya es viernes).
        expect(fechaEncabezado(Date.parse("2026-11-06T05:30:00Z"))).toBe("Jueves 5 de noviembre");
        expect(primerNombre("  Tomás Herrera ")).toBe("Tomás");
        expect(iniciales("Tomás Andrés Herrera")).toBe("TH");
        expect(iniciales("Ana")).toBe("A");
        expect(iniciales("")).toBe("");
    });

    it("buscar entre las órdenes del teléfono, sin acentos", () => {
        const ordenes = [
            ordenDePrueba(1, { contacto: { nombre: "Jorge Méndez", telefono: "" }, direccion: "Calzada Roosevelt 22-43" }),
            ordenDePrueba(2, { contacto: { nombre: "Ana Pérez", telefono: "" } }),
        ];
        expect(buscarOrdenes(ordenes, "  ").length).toBe(0);
        expect(buscarOrdenes(ordenes, "mendez").map((o) => o.id)).toEqual([1]);
        expect(buscarOrdenes(ordenes, "ROOSEVELT").map((o) => o.id)).toEqual([1]);
        expect(buscarOrdenes(ordenes, "OT-000002").map((o) => o.id)).toEqual([2]);
        expect(buscarOrdenes(ordenes, "calle", 1)).toHaveLength(1);
        expect(nombreCliente({ contacto: { nombre: "", telefono: "" } })).toBe("");
        expect(nombreCliente({ contacto: { nombre: null, telefono: "" } } as never)).toBe("Cliente sin nombre");
    });

    it("chips Hoy · Mañana · Semana y vistas con encabezado completo", () => {
        const manana = "2026-11-06";
        expect(chipDe({ tipo: "lista" }, manana)).toBe("hoy");
        expect(chipDe({ tipo: "agenda" }, manana)).toBe("manana");
        expect(chipDe({ tipo: "agenda", dia: manana }, manana)).toBe("manana");
        // Otro día de la tira no marca ningún chip.
        expect(chipDe({ tipo: "agenda", dia: "2026-11-09" }, manana)).toBeNull();
        expect(chipDe({ tipo: "semana" }, manana)).toBe("semana");
        expect(chipDe({ tipo: "orden", id: 1 }, manana)).toBeNull();
        expect(esVistaDeDia({ tipo: "semana" })).toBe(true);
        expect(esVistaDeDia({ tipo: "orden", id: 1, paso: "fotos" })).toBe(false);
        // Un día mal escrito en la URL abre la agenda de mañana.
        expect(vistaDeUrl("?vista=agenda&dia=6-nov")).toEqual({ tipo: "agenda" });
    });
});

describe("Hoy", () => {
    it("la siguiente es la primera en curso; si no hay, la primera por hacer", () => {
        const ordenes = [vista(1), vista(2, { estado: "en_sitio" }), vista(3, { estado: "cerrada" }), vista(4, { reasignada: true }), vista(5)];
        const r = resumenHoy(ordenes);
        expect(r.siguiente?.id).toBe(2);
        expect(r.despues.map((o) => o.id)).toEqual([1, 5]);
        expect(r.terminadas.map((o) => o.id)).toEqual([3, 4]);
        expect(r.activas).toBe(3);
        expect(resumenHoy([vista(7), vista(8)]).siguiente?.id).toBe(7);
        expect(resumenHoy([vista(9, { estado: "fallida" })])).toMatchObject({ siguiente: null, despues: [], activas: 0 });
    });

    it("botón principal: empezar (a la llegada), continuar (al paso que falta) o ver", () => {
        expect(accionPrincipal(vista(1))).toEqual({ texto: "Empezar trabajo", vista: { tipo: "orden", id: 1, paso: "llegada" } });
        expect(accionPrincipal(vista(1, { estado: "en_camino" }))).toEqual({ texto: "Continuar trabajo", vista: { tipo: "orden", id: 1, paso: "llegada" } });
        const enSitio = vista(1, { estado: "en_sitio", enSitioEn: "2026-10-28T15:10:00Z", checklist: [itemDePrueba(1, { obligatorio: true })] });
        expect(accionPrincipal(enSitio)).toEqual({ texto: "Continuar trabajo", vista: { tipo: "orden", id: 1, paso: "checklist" } });
        // Pausada en camino: reanudar es en la llegada.
        expect(accionPrincipal(vista(1, { estado: "pausada", pausadaDesde: "en_camino" })).vista).toEqual({ tipo: "orden", id: 1, paso: "llegada" });
        expect(accionPrincipal(vista(1, { estado: "pendiente" }))).toEqual({ texto: "Ver orden", vista: { tipo: "orden", id: 1 } });
        expect(accionPrincipal(vista(1, { estado: "cerrada" })).texto).toBe("Ver orden");
    });

    it("anillo de avance «2/4» con los pasos de trabajo listos", () => {
        const o = vista(1, { estado: "en_sitio", enSitioEn: "2026-10-28T15:10:00Z", tipo: "visita", checklist: [itemDePrueba(1, { obligatorio: true })] });
        expect(anilloAvance(o)).toEqual({ listos: 3, total: 4, texto: "3/4", etiqueta: "3 de 4 pasos listos", fraccion: 0.75 });
    });

    it("mosaico «Ruta de hoy» solo con el paquete en el teléfono y sin kilómetros inventados", () => {
        expect(mosaicoRuta({ fecha: null, ordenes: [], ruta: null })).toBeNull();
        expect(mosaicoRuta({ fecha: "2026-11-05", ordenes: [vista(1), vista(2), vista(3, { estado: "cerrada" })], ruta: null })).toEqual({
            valor: "2 órdenes",
            detalle: "Por hora de cita · 1 terminada",
        });
        expect(mosaicoRuta({ fecha: "2026-11-05", ordenes: [vista(1)], ruta: { ordenIds: [1] } })).toEqual({ valor: "1 orden", detalle: "En orden de ruta" });
    });

    it("mosaico «Por recibir»: sin leer no hay total", () => {
        const linea = { productoId: 1, cantidad: 2 } as unknown as NonNullable<Traslado["lineas"]>[number];
        const traslados: Traslado[] = [
            { id: 1, estado: "enviado", lineas: [linea, linea] },
            { id: 2, estado: "recibido", lineas: [linea] },
        ];
        expect(mosaicoPorRecibir(null, [])).toBeNull();
        expect(mosaicoPorRecibir(traslados, [])).toEqual({ cantidad: 1, valor: "1 traslado", detalle: "2 productos de bodega" });
        expect(mosaicoPorRecibir([], [])).toEqual({ cantidad: 0, valor: "Nada", detalle: "Todo recibido" });
    });
});

describe("agenda: Mañana y Semana", () => {
    it("días de calendario, mañana en Guatemala y la tira de 7 días", () => {
        expect(sumarDias("2026-12-31", 1)).toBe("2027-01-01");
        expect(sumarDias("2026-11-01", -1)).toBe("2026-10-31");
        expect(mananaEnGuatemala(Date.parse("2026-11-06T05:30:00Z"))).toBe("2026-11-06");
        const tira = tiraSemana("2026-11-05");
        expect(tira.map((d) => d.fecha)).toEqual(["2026-11-05", "2026-11-06", "2026-11-07", "2026-11-08", "2026-11-09", "2026-11-10", "2026-11-11"]);
        expect(tira.map((d) => d.corto)).toEqual(["JUE", "VIE", "SÁB", "DOM", "LUN", "MAR", "MIÉ"]);
        expect(tira[1]).toMatchObject({ numero: 6, largo: "viernes, 6 de noviembre" });
        expect(tituloAgenda("2026-11-06", "2026-11-05")).toBe("Mañana, vie 6");
        expect(tituloAgenda("2026-11-05", "2026-11-05")).toBe("Hoy, jue 5");
        expect(tituloAgenda("2026-11-09", "2026-11-05")).toBe("Lunes, 9 de noviembre");
    });

    it("citas de un día por hora (cita de la agenda o, sin cita, la hora programada)", () => {
        const ordenes = [
            ordenDePrueba(1, { programadaInicio: "2026-11-06T17:00:00Z", programadaFin: "2026-11-06T18:00:00Z" }),
            ordenDePrueba(2, { cita: { tecnicoId: 5, inicio: "2026-11-06T14:30:00Z", fin: "2026-11-06T16:30:00Z", ventanaInicio: null, ventanaFin: null } }),
            // 23:30 del 6 en Guatemala (ya es 7 en UTC).
            ordenDePrueba(3, { programadaInicio: "2026-11-07T05:30:00Z", programadaFin: null }),
            ordenDePrueba(4, { programadaInicio: "2026-11-06T15:00:00Z", estado: "cancelada" }),
            ordenDePrueba(5, { programadaInicio: null, estado: "pendiente" }),
        ];
        expect(diaDeOrden(ordenes[2] ?? ordenDePrueba(0))).toBe("2026-11-06");
        expect(diaDeOrden(ordenes[4] ?? ordenDePrueba(0))).toBeNull();
        const citas = citasDelDia(ordenes, "2026-11-06");
        expect(citas.map((c) => c.orden.id)).toEqual([2, 1, 3]);
        expect(resumenDia(citas)).toBe("3 citas · 3 h");
        expect(resumenDia([])).toBe("Sin citas");
        expect(resumenDia(citasDelDia([ordenes[2] ?? ordenDePrueba(0)], "2026-11-06"))).toBe("1 cita");
        expect(citasPorDia(ordenes, ["2026-11-05", "2026-11-06"])).toEqual({ "2026-11-05": 0, "2026-11-06": 3 });
    });

    it("tono del bloque y formularios", () => {
        expect(tonoCita({ estado: "en_camino", formularios: [] })).toBe("marca");
        expect(tonoCita({ estado: "programada", formularios: [] })).toBe("aviso");
        const f = { id: "f", plantillaId: 1 } as unknown as NonNullable<OrdenVista["formularios"]>[number];
        expect(tonoCita({ estado: "programada", formularios: [f] })).toBe("neutral");
        expect(textoFormularios({ formularios: [f, f] })).toBe("2 formularios");
        expect(textoFormularios({ formularios: null })).toBe("Sin formulario");
    });
});

type EstadoEnvio = Parameters<typeof avisoIsla>[0];

function estadoEnvio(parcial: Partial<EstadoEnvio> = {}): EstadoEnvio {
    return {
        conexion: "en_linea",
        sincronizando: false,
        pendientes: 0,
        rechazos: [],
        esperandoAdjuntos: [],
        subidas: { pendientes: 0, bytesPendientes: 0, conError: 0, rechazadas: 0 },
        ...parcial,
    };
}

describe("píldora de estado encima de la isla", () => {
    it("con todo enviado no sale (queda el punto)", () => {
        expect(avisoIsla(estadoEnvio())).toBeNull();
        expect(avisoIsla(estadoEnvio({ conexion: "sin_conexion" }))).toBeNull();
    });

    it("sin señal con cosas por enviar, rechazos, sesión vencida y empresa no válida", () => {
        expect(avisoIsla(estadoEnvio({ conexion: "sin_conexion", pendientes: 2 }))).toEqual({
            tono: "warning",
            estado: "Sin conexión",
            detalle: "2 pendientes por enviar",
            rechazo: false,
            accion: "pendientes",
            etiqueta: "Sin conexión: 2 pendientes por enviar. Ver detalle",
        });
        const rechazo = { id: "r" } as unknown as EstadoSync["rechazos"][number];
        expect(avisoIsla(estadoEnvio({ rechazos: [rechazo], pendientes: 1 }))).toMatchObject({ tono: "error", rechazo: true, detalle: "1 pendiente por enviar · 1 rechazado" });
        expect(avisoIsla(estadoEnvio({ sincronizando: true, pendientes: 1 }))).toMatchObject({ tono: "info", estado: "Sincronizando…" });
        expect(avisoIsla(estadoEnvio({ conexion: "sin_sesion" }))).toMatchObject({ tono: "error", estado: "Sesión vencida", detalle: "", accion: "entrar", etiqueta: "Sesión vencida" });
        expect(avisoIsla(estadoEnvio({ conexion: "sin_empresa", pendientes: 1 }))).toMatchObject({ accion: "pendientes", estado: "Empresa no válida" });
        expect(avisoIsla(estadoEnvio({ conexion: "sin_empresa" }))?.accion).toBe("empresa");
    });
});

describe("encabezado que se va al bajar y vuelve compacto al subir", () => {
    const ALTO = 200;
    it("al bajar se encoge la isla y no hay barra; al subir vuelve la barra compacta", () => {
        let e = desplazar(DESPLAZAMIENTO_INICIAL, 120, ALTO);
        expect(e).toEqual({ y: 120, compacto: false, islaEncogida: true });
        e = desplazar(e, 600, ALTO);
        expect(e).toMatchObject({ compacto: false, islaEncogida: true });
        // Un temblor del dedo no cambia nada.
        expect(desplazar(e, 596, ALTO)).toBe(e);
        e = desplazar(e, 560, ALTO);
        expect(e).toEqual({ y: 560, compacto: true, islaEncogida: false });
        // Al subir con el encabezado completo a la vista no hace falta la barra compacta.
        expect(desplazar({ y: 180, compacto: false, islaEncogida: true }, 150, ALTO)).toEqual({ y: 150, compacto: false, islaEncogida: false });
        // Arriba del todo, como al principio.
        expect(desplazar(e, 10, ALTO)).toEqual({ y: 10, compacto: false, islaEncogida: false });
        expect(desplazar(e, -40, ALTO)).toEqual({ y: 0, compacto: false, islaEncogida: false });
    });
});

describe("− / + en materiales", () => {
    it("suma y resta de a uno sin bajar de 0 y con los decimales y la coma del técnico", () => {
        expect(pasoCantidad("", 1)).toBe("1");
        expect(pasoCantidad("35", 1)).toBe("36");
        expect(pasoCantidad("35", -1)).toBe("34");
        expect(pasoCantidad("1", -1)).toBe("");
        expect(pasoCantidad("0,5", -1)).toBe("");
        expect(pasoCantidad("2,5", 1)).toBe("3,5");
        expect(pasoCantidad("2.125", 1)).toBe("3.125");
        expect(pasoCantidad("abc", 1)).toBe("1");
        expect(pasoCantidad("1000000", 1)).toBe("1000000");
        // Lo que da el botón siempre pasa el validador de la API.
        expect(esquemaMaterial.safeParse({ productoId: "4", cantidad: pasoCantidad("2,5", 1) }).success).toBe(true);
    });
});

describe("«+»: acciones rápidas", () => {
    const plantilla = (id: number, activa = true): PlantillaFormulario => ({ id, versionId: id * 100, codigo: `p${id}`, nombre: `Plantilla ${id}`, version: 1, activa, items: [] }) as unknown as PlantillaFormulario;
    const f = { id: "f", plantillaId: 1 } as unknown as NonNullable<OrdenVista["formularios"]>[number];

    it("agregar formulario solo con el permiso y en órdenes con el trabajo empezado", () => {
        const ordenes = [vista(1, { estado: "en_sitio", formularios: [f] }), vista(2), vista(3, { estado: "pausada" }), vista(4, { estado: "en_camino", reasignada: true })];
        const plantillas = [plantilla(1), plantilla(2), plantilla(3, false)];
        const r = accionesRapidas(ordenes, plantillas, [PERMISO_AGREGAR_FORMULARIO, "ordenes.cerrar"]);
        expect(r.formulario?.ordenes.map((o) => [o.orden.id, o.plantillas.map((p) => p.id)])).toEqual([
            [1, [2]],
            [3, [1, 2]],
        ]);
        expect(r.tarea).toEqual({ disponible: false, motivo: "Próximamente" });
        // Sin el permiso (o sin saberlo: sesión guardada de antes), la opción no aparece.
        expect(accionesRapidas(ordenes, plantillas, ["ordenes.cerrar"]).formulario).toBeNull();
        expect(accionesRapidas(ordenes, plantillas, undefined).formulario).toBeNull();
    });
});
