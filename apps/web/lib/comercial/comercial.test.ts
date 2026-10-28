import type { Schema } from "@iris/contratos";
import { describe, expect, it } from "vitest";
import {
    ACTIVIDAD_VACIA,
    cuerpoActividad,
    cuerpoCotizacion,
    cuerpoCrearProspecto,
    cuerpoFactibilidadManual,
    cuerpoFirma,
    esquemaActividad,
    esquemaCotizacion,
    esquemaFactibilidadManual,
    esquemaPerder,
    esquemaProspecto,
    FACTIBILIDAD_MANUAL_VACIA,
    lineaVacia,
    PROSPECTO_VACIO,
} from "./esquemas";
import { accionesVisibles } from "./acciones";
import { FILTROS_VACIOS, filtrosDesdeUrl, hayFiltros, queryDe, textoConteo, urlDe, vistaDesdeUrl } from "./filtros";
import { circulo, geojsonCobertura } from "./mapa";
import { requisitosGanar, textoDistancia, textoEvento, textoMonto, textoMotivo, type Cobertura } from "./presentacion";

const mensajes = (r: { success: boolean; error?: { issues: { path: PropertyKey[]; message: string }[] } }) =>
    Object.fromEntries((r.error?.issues ?? []).map((i) => [i.path.join("."), i.message]));

const PLANES: Schema<"PlanComercial">[] = [
    { id: 7, nombre: "Fibra 100", precio: "250.00", activo: true },
    { id: 8, nombre: "Fibra 300", precio: null, activo: true },
];

describe("prospecto (espejo de EntradaCrearProspecto)", () => {
    it("exige nombre y dirección; lat y lon van juntas y en rango", () => {
        expect(mensajes(esquemaProspecto.safeParse(PROSPECTO_VACIO))).toEqual({ nombre: "Escribe el nombre", direccion: "Escribe la dirección" });
        const base = { ...PROSPECTO_VACIO, nombre: "Ana", direccion: "Zona 1" };
        expect(mensajes(esquemaProspecto.safeParse({ ...base, lat: "14.6" }))).toEqual({ lon: "Indica latitud y longitud juntas" });
        expect(mensajes(esquemaProspecto.safeParse({ ...base, lat: "95", lon: "-90" })).lat).toBe("Latitud entre -90 y 90");
        expect(mensajes(esquemaProspecto.safeParse({ ...base, correo: "ana" })).correo).toBe("Escribe un correo válido");
        expect(mensajes(esquemaProspecto.safeParse({ ...base, nombre: "A" })).nombre).toBe("Al menos 2 caracteres");
        expect(mensajes(esquemaProspecto.safeParse({ ...base, telefono: "1".repeat(31) })).telefono).toBe("Admite hasta 30 caracteres");
    });

    it("arma el cuerpo sin vacíos, con números y coma decimal", () => {
        const d = esquemaProspecto.parse({ ...PROSPECTO_VACIO, nombre: " Ana ", direccion: "Zona 1", telefono: "5555-1234", zonaId: "4", lat: "14,6349", lon: "-90.5133" });
        expect(cuerpoCrearProspecto(d)).toEqual({ nombre: "Ana", direccion: "Zona 1", origen: "panel", telefono: "5555-1234", zonaId: 4, lat: 14.6349, lon: -90.5133 });
    });
});

describe("actividad, perder y factibilidad manual", () => {
    it("la próxima acción pide qué hacer y una fecha válida; sin hora, las 9:00 de Guatemala", () => {
        const r = esquemaActividad.safeParse({ ...ACTIVIDAD_VACIA, descripcion: "Llamé", proximaDia: "2026-11-02" });
        expect(mensajes(r)).toEqual({ proximaAccion: "¿Qué hay que hacer ese día?" });
        const d = esquemaActividad.parse({ ...ACTIVIDAD_VACIA, descripcion: "Llamé", proximaAccion: "Volver a llamar", proximaDia: "2026-11-02" });
        expect(cuerpoActividad(d)).toEqual({ tipo: "llamada", descripcion: "Llamé", proximaAccion: "Volver a llamar", proximaFecha: "2026-11-02T15:00:00Z" });
        expect(cuerpoActividad(esquemaActividad.parse({ ...ACTIVIDAD_VACIA, descripcion: "x", proximaAccion: "y", proximaDia: "2026-11-02", proximaHora: "16:30" })).proximaFecha).toBe("2026-11-02T22:30:00Z");
    });

    it("perder exige motivo; la visita técnica, notas de 5+ caracteres", () => {
        expect(mensajes(esquemaPerder.safeParse({ motivo: " " }))).toEqual({ motivo: "Escribe por qué se perdió" });
        expect(mensajes(esquemaFactibilidadManual(false).safeParse({ ...FACTIBILIDAD_MANUAL_VACIA, notas: "Poste a 400 m" })).notas).toMatch(/al menos 20/);
        const notas = "Poste a 400 m, hay que tender fibra";
        const d = esquemaFactibilidadManual(false).parse({ ...FACTIBILIDAD_MANUAL_VACIA, resultado: "requiere_obra", caja: "CTO-12", routerId: "501", notas });
        expect(cuerpoFactibilidadManual(d, 3)).toEqual({ version: 3, resultado: "requiere_obra", notas, caja: "CTO-12", routerId: 501 });
    });

    it("una visita que contradice una automática no_viable exige el motivo de la anulación", () => {
        const notas = "Hay una caja nueva a 120 m del domicilio";
        expect(mensajes(esquemaFactibilidadManual(true).safeParse({ ...FACTIBILIDAD_MANUAL_VACIA, notas })).motivoAnulacion).toMatch(/contradice/);
        expect(esquemaFactibilidadManual(true).safeParse({ ...FACTIBILIDAD_MANUAL_VACIA, resultado: "no_viable", notas }).success).toBe(true);
        const d = esquemaFactibilidadManual(true).parse({ ...FACTIBILIDAD_MANUAL_VACIA, notas, motivoAnulacion: "Caja instalada la semana pasada" });
        expect(cuerpoFactibilidadManual(d, 1)).toEqual({ version: 1, resultado: "viable", notas, motivoAnulacion: "Caja instalada la semana pasada" });
    });
});

describe("cotización (espejo de calcularLineas)", () => {
    it("exactamente una línea de plan", () => {
        const r = esquemaCotizacion(PLANES).safeParse({ lineas: [lineaVacia("instalacion")], vigenteDia: "", notas: "" });
        expect(mensajes(r).lineas).toBe("La cotización lleva exactamente una línea de plan");
    });

    it("con conector: el plan es de Argos y su precio no se pisa; si Argos no lo publica, va a mano", () => {
        const sinPlan = esquemaCotizacion(PLANES).safeParse({ lineas: [lineaVacia("plan")], vigenteDia: "", notas: "" });
        expect(mensajes(sinPlan)["lineas.0.planId"]).toBe("Elige el plan");
        const sinPrecio = esquemaCotizacion(PLANES).safeParse({ lineas: [{ ...lineaVacia("plan"), planId: "8" }], vigenteDia: "", notas: "" });
        expect(mensajes(sinPrecio)["lineas.0.precioUnitario"]).toMatch(/Argos no publica/);
        const d = esquemaCotizacion(PLANES).parse({
            lineas: [{ ...lineaVacia("plan"), planId: "7", precioUnitario: "999" }, { ...lineaVacia("descuento"), descripcion: "Promo", precioUnitario: "50", recurrente: true }, { ...lineaVacia("instalacion"), descripcion: "Instalación", precioUnitario: "150.5" }],
            vigenteDia: "2026-11-15",
            notas: "",
        });
        expect(cuerpoCotizacion(d, 4, PLANES)).toEqual({
            version: 4,
            lineas: [
                { tipo: "plan", planId: 7 },
                { tipo: "descuento", descripcion: "Promo", precioUnitario: "50", recurrente: true },
                { tipo: "instalacion", descripcion: "Instalación", precioUnitario: "150.5" },
            ],
            vigenteHasta: "2026-11-16T05:59:00Z",
        });
    });

    it("sin conector: el plan con descripción y precio a mano; montos como texto decimal", () => {
        const r = esquemaCotizacion(null).safeParse({ lineas: [{ ...lineaVacia("plan"), precioUnitario: "25.123" }], vigenteDia: "", notas: "" });
        expect(mensajes(r)).toEqual({ "lineas.0.descripcion": "Es obligatoria", "lineas.0.precioUnitario": "Usa un monto como 250 o 250.50" });
        const d = esquemaCotizacion(null).parse({ lineas: [{ ...lineaVacia("plan"), descripcion: "Fibra 50", precioUnitario: "199.99", cantidad: "1" }], vigenteDia: "", notas: "" });
        expect(cuerpoCotizacion(d, 1, null).lineas).toEqual([{ tipo: "plan", descripcion: "Fibra 50", precioUnitario: "199.99" }]);
    });
});

describe("firma", () => {
    it("manda la versión del contrato, el nombre y el SVG", () => {
        expect(cuerpoFirma({ firmanteNombre: " Ana ", firmanteDocumento: "" }, 2, "<svg/>")).toEqual({ version: 2, firmanteNombre: "Ana", svg: "<svg/>" });
    });
});

describe("filtros", () => {
    it("arman la query: míos, sin vendedor, otro vendedor, zona, origen, texto y columna", () => {
        expect(queryDe({ ...FILTROS_VACIOS, responsable: "mios", q: " fibra " }, 5)).toEqual({ vendedorId: 5, q: "fibra" });
        expect(queryDe({ ...FILTROS_VACIOS, responsable: "sin", zonaId: 4, origen: "web" }, 5)).toEqual({ sinVendedor: true, zonaId: 4, origen: "web" });
        expect(queryDe({ ...FILTROS_VACIOS, responsable: "u9", estado: "nuevo" }, 5, { estado: "cotizado", conCoordenadas: true })).toEqual({ vendedorId: 9, estado: "cotizado", conCoordenadas: true });
    });

    it("van y vuelven de la URL, ignorando valores inválidos", () => {
        const f = { estado: "cotizado", responsable: "u9", zonaId: 4, origen: "web", q: "ana" } as const;
        const url = urlDe(f, "lista");
        expect(url).toBe("/comercial?vista=lista&estado=cotizado&responsable=u9&zona=4&origen=web&q=ana");
        const p = new URL(url, "http://x").searchParams;
        expect(filtrosDesdeUrl(p)).toEqual(f);
        expect(vistaDesdeUrl(p)).toBe("lista");
        expect(filtrosDesdeUrl(new URLSearchParams("estado=raro&responsable=u0&zona=-1&origen=argos2"))).toEqual(FILTROS_VACIOS);
        expect(urlDe(FILTROS_VACIOS, "embudo")).toBe("/comercial");
        expect(hayFiltros(FILTROS_VACIOS)).toBe(false);
    });

    it("el conteo de una columna dice «+» si hay más páginas", () => {
        expect(textoConteo(20, true)).toBe("20+");
        expect(textoConteo(3, false)).toBe("3");
    });
});

describe("cobertura", () => {
    const cobertura: Cobertura = {
        disponible: true,
        motivo: null,
        motor: "distancia",
        umbrales: { empresaId: 10, viableM: 300, obraM: 1000, porDefecto: true, version: 0 },
        routers: [{ id: 1, nombre: "R1", zonaId: null, lat: 14.6, lon: -90.5, sincronizadoEn: "2026-10-28T00:00:00Z" }],
    };

    it("un círculo cerrado del radio pedido", () => {
        const anillo = circulo(14.6, -90.5, 1000, 16);
        expect(anillo).toHaveLength(17);
        expect(anillo[0]).toEqual(anillo[16]);
        const [lon, lat] = anillo[4] ?? [0, 0];
        expect(lon).toBeCloseTo(-90.5, 6);
        expect((lat - 14.6) * 111_320).toBeCloseTo(1000, 0);
    });

    it("obra debajo y viable encima; nada si no está disponible", () => {
        const g = geojsonCobertura(cobertura);
        expect(g.features.map((f) => f.properties.capa)).toEqual(["obra", "viable"]);
        expect(geojsonCobertura({ ...cobertura, disponible: false, motivo: "sin_conector" }).features).toEqual([]);
        expect(geojsonCobertura(null).features).toEqual([]);
    });
});

describe("presentación", () => {
    it("motivos, distancias, montos y eventos legibles", () => {
        expect(textoMotivo("sin_conector")).toMatch(/no está conectado con Argos/);
        expect(textoMotivo("algo_nuevo")).toBe("algo nuevo");
        expect(textoDistancia(850.4)).toBe("850 m");
        expect(textoDistancia(1234)).toBe("1.2 km");
        expect(textoDistancia(null)).toBeUndefined();
        expect(textoMonto("250.00", "GTQ")).toMatch(/250\.00/);
        expect(textoEvento("cotizacion_aceptada")).toBe("Cotización aceptada");
        expect(textoEvento("otra_cosa")).toBe("Otra cosa");
    });

    it("requisitos para ganar: qué falta", () => {
        const r = requisitosGanar({});
        expect(r.map((x) => [x.clave, x.cumplido, x.detalle])).toEqual([
            ["factibilidad", false, "Sin evaluar"],
            ["cotizacion", false, "Sin cotización"],
            ["contrato", false, "Sin contrato"],
        ]);
    });
});

describe("botones del prospecto", () => {
    const acciones = { contactar: true, evaluar: true, cotizar: false, aceptar: false, contratar: false, firmar: false, ganar: false, confirmaObra: false, perder: true, reabrir: false };
    const todos = () => true;

    it("salen de `acciones` cruzadas con los permisos de /yo", () => {
        expect(accionesVisibles({ acciones, estado: "nuevo" }, todos)).toEqual(["contactar", "asignar", "perder"]);
        expect(accionesVisibles({ acciones, estado: "nuevo" }, (p) => p.includes("prospectos.perder"))).toEqual(["perder"]);
    });

    it("«Asignar vendedor» no se ofrece en un prospecto ganado ni perdido", () => {
        const cerrado = { ...acciones, contactar: false, perder: false };
        expect(accionesVisibles({ acciones: cerrado, estado: "ganado" }, todos)).toEqual([]);
        expect(accionesVisibles({ acciones: { ...cerrado, reabrir: true }, estado: "perdido" }, todos)).toEqual(["reabrir"]);
        expect(accionesVisibles({ acciones: cerrado, estado: "contrato" }, todos)).toEqual(["asignar"]);
    });
});
