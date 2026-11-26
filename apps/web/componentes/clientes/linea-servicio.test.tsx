import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";
import { etiquetaServicio, lineaDeCopia, lineaDeServicioCampo, textoPrecio, type ServicioLinea } from "@/lib/datos/servicio";
import { LineaServicio, ListaServicios, NotaPermisosServicio } from "./linea-servicio";

const permisos = vi.hoisted(() => ({ lista: [] as string[] }));
vi.mock("@/lib/sesion", () => ({ useSesion: () => ({ puede: (ps: string[]) => ps.every((p) => permisos.lista.includes(p)) }) }));

const COMPLETO: ServicioLinea = {
    id: 4410,
    numero: 1,
    codigo: "S-4410",
    planNombre: "Chitá Fibra 50 Mbps residencial con nombre largo",
    zonaNombre: "Chitá",
    estado: "activo",
    precioMensual: "250.00",
    moneda: "GTQ",
    routerNombre: "Nodo Centro",
    ip: "100.64.12.34",
    direccion: "3a calle 4-56 zona 1",
};

/** Lo que manda el servidor a un rol sin servicios.ver_red ni servicios.ver_ubicacion. */
const RECORTADO: ServicioLinea = { id: 4518, numero: 2, codigo: "S-4518", planNombre: "Chitá Fibra 100 Mbps", zonaNombre: "Chitá", estado: "suspendido", precioMensual: "350.00", moneda: "GTQ" };

describe("precio y etiqueta del servicio", () => {
    it("el precio con la moneda de Argos; sin precio, nada (nunca «Q0»)", () => {
        expect(textoPrecio({ precioMensual: "250.00", moneda: "GTQ" })).toBe("Q250.00 al mes");
        expect(textoPrecio({ precioMensual: "10.00", moneda: "USD" })).toBe("USD 10.00 al mes");
        expect(textoPrecio({})).toBe("");
        expect(textoPrecio({ precioMensual: "" })).toBe("");
    });

    it("la etiqueta corta es la del asistente: «Servicio n · plan · zona»", () => {
        expect(etiquetaServicio(RECORTADO)).toBe("Servicio 2 · Chitá Fibra 100 Mbps · Chitá");
        expect(etiquetaServicio({ id: 1, estado: "activo" })).toBe("Sin plan");
    });

    it("adapta el servicio de campo y la copia de un ticket sin inventar campos", () => {
        const campo = lineaDeServicioCampo({ id: 1, codigo: "S-1", plan: "Fibra", zona: "Norte", estado: "activo", sincronizadoEn: "2026-11-19T00:00:00Z" });
        expect(campo).toEqual({ id: 1, codigo: "S-1", planNombre: "Fibra", zonaNombre: "Norte", estado: "activo" });
        expect(lineaDeCopia({ id: 2, planNombre: "Fibra", estado: "activo", zonaNombre: "Sur" })).toEqual({ id: 2, planNombre: "Fibra", estado: "activo", zonaNombre: "Sur" });
    });
});

describe("línea de servicio", () => {
    it("completa: plan entero, estado con texto, precio, zona, router, IP, dirección y código", () => {
        const html = renderToStaticMarkup(<LineaServicio servicio={COMPLETO} numerado />);
        for (const texto of ["Chitá Fibra 50 Mbps residencial con nombre largo", "Activo", "Q250.00 al mes", "Chitá", "Nodo Centro", "100.64.12.34", "3a calle 4-56 zona 1", "S-4410", 'aria-label="Servicio 1"']) {
            expect(html).toContain(texto);
        }
        // El plan envuelve: nunca se recorta con puntos suspensivos.
        expect(html).not.toContain("truncate");
    });

    it("sin red ni ubicación muestra solo lo que llegó, sin «sin IP» ni huecos", () => {
        const html = renderToStaticMarkup(<LineaServicio servicio={RECORTADO} />);
        expect(html).toContain("Q350.00 al mes");
        expect(html).toContain("Suspendido");
        expect(html).not.toContain("linea-servicio-ip");
        expect(html).not.toContain("linea-servicio-router");
        expect(html).not.toContain("linea-servicio-direccion");
        expect(html).not.toContain("Sin IP");
    });

    it("sin precio (Argos anterior a 1.18.0) no aparece ningún monto", () => {
        const sinPrecio: ServicioLinea = { id: 7, planNombre: "Fibra 20", zonaNombre: "Norte", estado: "activo", ip: "100.64.1.1" };
        const html = renderToStaticMarkup(<LineaServicio servicio={sinPrecio} />);
        expect(html).not.toMatch(/Q\d/);
        expect(html).not.toContain("al mes");
    });

    it("con varios servicios los numera y marca el de la conversación", () => {
        const html = renderToStaticMarkup(<ListaServicios servicios={[COMPLETO, RECORTADO]} marcadoId={4518} />);
        expect(html.match(/data-testid="linea-servicio-marca"/g)).toHaveLength(1);
        expect(html).toContain("De esta conversación");
        expect(html).toContain('aria-label="Servicio 2"');
        // Con un solo servicio no hace falta marca ni número.
        const uno = renderToStaticMarkup(<ListaServicios servicios={[COMPLETO]} marcadoId={4410} />);
        expect(uno).not.toContain("De esta conversación");
        expect(uno).not.toContain('aria-label="Servicio 1"');
    });

    it("compacta: una línea con plan, estado, precio y zona", () => {
        const html = renderToStaticMarkup(<LineaServicio servicio={RECORTADO} variante="compacta" numerado />);
        expect(html).toContain('data-variante="compacta"');
        expect(html).toContain("2.");
        expect(html).toContain("Q350.00 al mes");
    });
});

describe("nota de permisos", () => {
    it("solo aparece si el rol no ve la red o la ubicación, y dice qué no ve", () => {
        permisos.lista = ["servicios.ver_red", "servicios.ver_ubicacion"];
        expect(renderToStaticMarkup(<NotaPermisosServicio />)).toBe("");
        permisos.lista = [];
        expect(renderToStaticMarkup(<NotaPermisosServicio />)).toContain("la dirección ni los datos de red");
        permisos.lista = ["servicios.ver_ubicacion"];
        expect(renderToStaticMarkup(<NotaPermisosServicio />)).toContain("los datos de red (IP y router)");
    });
});

describe("tipo de servicio (contrato 0.15.0 §13.1)", () => {
    it("muestra el tipo con ícono y texto; un servicio de cable no muestra router", () => {
        const cable: ServicioLinea = { id: 7010, planNombre: "TV Básica", estado: "activo", tipoServicio: "cable", sinRouter: true, routerNombre: "No debía venir" };
        const html = renderToStaticMarkup(<LineaServicio servicio={cable} />);
        expect(html).toContain('data-tipo="cable"');
        expect(html).toContain("Cable (TV)");
        expect(html).not.toContain("linea-servicio-router");
        const compacta = renderToStaticMarkup(<LineaServicio servicio={{ ...cable, tipoServicio: "mixto", sinRouter: false }} variante="compacta" />);
        expect(compacta).toContain("Internet y cable");
    });

    it("sin tipo (Argos anterior a 0.15.0) no inventa nada", () => {
        const html = renderToStaticMarkup(<LineaServicio servicio={COMPLETO} />);
        expect(html).not.toContain("linea-servicio-tipo");
        expect(html).toContain("Nodo Centro");
    });

    it("el servicio de campo lleva su tipo", () => {
        const campo = lineaDeServicioCampo({ id: 1, codigo: "S-1", plan: "TV", estado: "activo", sincronizadoEn: "2026-11-19T00:00:00Z", tipoServicio: "cable", sinRouter: true });
        expect(campo).toEqual({ id: 1, codigo: "S-1", planNombre: "TV", estado: "activo", tipoServicio: "cable", sinRouter: true });
    });
});
