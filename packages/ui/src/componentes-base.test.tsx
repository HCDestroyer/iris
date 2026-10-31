import { CalendarDays, MessagesSquare } from "lucide-react";
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Autocompletar } from "./componentes/autocompletar";
import { avisar, cerrarAviso, colaAvisos, contenidoHeroui, duracionAviso, useAvisoEmergente } from "./componentes/aviso-emergente";
import { Boton } from "./componentes/boton";
import { BarraGuardar, FormularioPagina, rutaInterna, rutaQueSale, SeccionFormulario, type ClicEnlace } from "./componentes/formulario-pagina";
import { Globo } from "./componentes/globo";
import { Migas } from "./componentes/migas";
import { PanelEmergente } from "./componentes/panel-emergente";
import { Pestanas } from "./componentes/pestanas";
import { aFecha, hoyEnGuatemala, SelectorFecha, SelectorRangoFechas } from "./componentes/selector-fecha";

describe("Pestanas", () => {
    it("lista de pestañas con nombre, la elegida y su panel", () => {
        const html = renderToStaticMarkup(
            <Pestanas
                etiqueta="Secciones del ticket"
                seleccionada="conversacion"
                pestanas={[
                    { id: "resumen", etiqueta: "Resumen", contenido: <p>Datos</p> },
                    { id: "conversacion", etiqueta: "Conversación", icono: MessagesSquare, contenido: <p>Mensajes</p> },
                    { id: "historial", etiqueta: "Historial", deshabilitada: true, contenido: <p>Cambios</p> },
                ]}
            />,
        );
        expect(html).toMatch(/role="tablist"[^>]*aria-label="Secciones del ticket"|aria-label="Secciones del ticket"[^>]*role="tablist"/);
        expect(html.match(/role="tab"/g)).toHaveLength(3);
        expect(html).toMatch(/aria-selected="true"[^>]*>[\s\S]*?Conversación/);
        expect(html).toContain('role="tabpanel"');
        expect(html).toContain("Mensajes");
        expect(html).not.toContain("Datos");
        expect(html).toMatch(/aria-disabled="true"[^>]*>[\s\S]*?Historial/);
        expect(html).toContain("tabs__tab");
    });
});

describe("Aviso emergente", () => {
    afterEach(() => colaAvisos.clear());

    it("el tono decide la variante de HeroUI y el icono; error → danger", () => {
        const cerrar = vi.fn();
        expect(contenidoHeroui({ titulo: "Guardado" }, cerrar).variant).toBe("success");
        expect(contenidoHeroui({ tono: "error", titulo: "Falló" }, cerrar).variant).toBe("danger");
        expect(contenidoHeroui({ tono: "info", titulo: "Hay versión nueva" }, cerrar).variant).toBe("default");
    });

    it("la acción («Deshacer») corre y cierra el aviso", () => {
        const deshacer = vi.fn();
        const cerrar = vi.fn();
        const contenido = contenidoHeroui({ titulo: "Orden asignada", accion: { texto: "Deshacer", alPresionar: deshacer } }, cerrar);
        expect(contenido.actionProps?.children).toBe("Deshacer");
        contenido.actionProps?.onPress?.({} as never);
        expect(deshacer).toHaveBeenCalledOnce();
        expect(cerrar).toHaveBeenCalledOnce();
    });

    it("dura 5 s, 8 s con acción, o lo que se pida", () => {
        expect(duracionAviso({})).toBe(5000);
        expect(duracionAviso({ accion: { texto: "Deshacer", alPresionar: () => undefined } })).toBe(8000);
        expect(duracionAviso({ duracion: 0 })).toBe(0);
    });

    it("avisar agrega a la cola y cerrarAviso lo quita; el hook da las mismas funciones", () => {
        const id = avisar({ titulo: "Ticket TCK-311 escalado" });
        expect(colaAvisos.visibleToasts.map((t) => t.key)).toContain(id);
        cerrarAviso(id);
        expect(useAvisoEmergente().avisar).toBe(avisar);
    });
});

describe("Migas", () => {
    it("landmark de navegación; la última es la pantalla actual y no es enlace", () => {
        const html = renderToStaticMarkup(
            <Migas
                migas={[
                    { etiqueta: "Órdenes", href: "/ordenes" },
                    { etiqueta: "ORD-1042", href: "/ordenes/1042" },
                    { etiqueta: "Editar", href: "/ignorado" },
                ]}
            />,
        );
        expect(html).toMatch(/^<nav aria-label="Ruta de navegación"/);
        expect(html).toContain('href="/ordenes"');
        expect(html).toContain('href="/ordenes/1042"');
        expect(html).not.toContain('href="/ignorado"');
        expect(html).toMatch(/aria-current="page"[^>]*>[\s\S]*?Editar/);
    });
});

describe("Selector de fecha", () => {
    it("convierte AAAA-MM-DD y hoy es el de Guatemala", () => {
        expect(aFecha("2026-11-04")?.toString()).toBe("2026-11-04");
        expect(aFecha("4/11/2026")).toBeNull();
        expect(aFecha(null)).toBeNull();
        expect(hoyEnGuatemala()).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    });

    it("fecha: etiqueta, segmentos en formato de Guatemala y error enlazado", () => {
        const html = renderToStaticMarkup(<SelectorFecha id="f" etiqueta="Fecha de la visita" valor="2026-11-04" alCambiar={() => undefined} error="Elige una fecha futura" />);
        expect(html).toContain("Fecha de la visita");
        expect(html).toContain('role="group"');
        // es-GT: día / mes / año.
        expect(html.indexOf('data-type="day"')).toBeLessThan(html.indexOf('data-type="month"'));
        expect(html.indexOf('data-type="month"')).toBeLessThan(html.indexOf('data-type="year"'));
        expect(html).toContain('value="2026-11-04"');
        expect(html).toContain('aria-label="Abrir calendario"');
        expect(html).toContain('aria-invalid="true"');
        expect(html).toContain("Elige una fecha futura");
    });

    it("rango: inicio y fin", () => {
        const html = renderToStaticMarkup(<SelectorRangoFechas etiqueta="Periodo" valor={{ inicio: "2026-11-01", fin: "2026-11-30" }} alCambiar={() => undefined} />);
        expect(html).toContain("Periodo");
        expect(html).toContain("30");
        expect(html).toContain('aria-label="Abrir calendario"');
    });
});

describe("Autocompletar", () => {
    it("combobox con su etiqueta, ayuda enlazada y botón para ver opciones", () => {
        const html = renderToStaticMarkup(
            <Autocompletar
                id="t"
                etiqueta="Técnico"
                ayuda="Escribe el nombre"
                opciones={[
                    { valor: "1", etiqueta: "Luis Pérez", detalle: "3 órdenes hoy" },
                    { valor: "2", etiqueta: "Ana Cux" },
                ]}
                valor={null}
                alCambiar={() => undefined}
            />,
        );
        expect(html).toMatch(/<input[^>]*role="combobox"/);
        expect(html).toMatch(/<input[^>]*id="t"/);
        expect(html).toMatch(/aria-describedby="[^"]*t-ayuda/);
        expect(html).toContain("Técnico");
        expect(html).toContain('aria-label="Ver opciones"');
    });
});

describe("Globo y PanelEmergente", () => {
    it("Globo no agrega otro elemento enfocable y oculta el texto hasta que se pide", () => {
        const html = renderToStaticMarkup(
            <Globo texto="Agenda del técnico">
                <Boton tamano="icono" aria-label="Agenda">
                    <CalendarDays />
                </Boton>
            </Globo>,
        );
        expect(html.match(/<button/g)).toHaveLength(1);
        expect(html).toContain('aria-label="Agenda"');
        expect(html).not.toContain("Agenda del técnico");
    });

    it("PanelEmergente: el disparador es el propio botón, cerrado al inicio", () => {
        const html = renderToStaticMarkup(
            <PanelEmergente titulo="Filtros" disparador={<Boton variante="contorno">Filtros</Boton>}>
                <p>Contenido</p>
            </PanelEmergente>,
        );
        expect(html.match(/<button/g)).toHaveLength(1);
        expect(html).toMatch(/<button[^>]*aria-expanded="false"/);
        expect(html).not.toContain("Contenido");
    });
});

describe("Formulario en pantalla propia", () => {
    const ubicacion = { origin: "https://iris.test", pathname: "/ordenes/nueva", search: "?ticket=311" };
    const enlace = (href: string, atributos: Record<string, string> = {}) => ({
        closest: () => ({
            getAttribute: (n: string) => (n === "href" ? href : (atributos[n] ?? null)),
            hasAttribute: (n: string) => n in atributos,
        }),
    });
    const clic = (target: unknown, cambios: Partial<ClicEnlace> = {}): ClicEnlace => ({
        button: 0,
        metaKey: false,
        ctrlKey: false,
        shiftKey: false,
        altKey: false,
        defaultPrevented: false,
        target: target as EventTarget,
        ...cambios,
    });

    it("rutaQueSale detecta los enlaces que dejan la pantalla", () => {
        expect(rutaQueSale(clic(enlace("/ordenes")), ubicacion)).toBe("/ordenes");
        expect(rutaQueSale(clic(enlace("/tickets/311#hilo")), ubicacion)).toBe("/tickets/311#hilo");
        expect(rutaQueSale(clic(enlace("https://iris.test/agenda")), ubicacion)).toBe("/agenda");
    });

    it("rutaQueSale ignora pestaña nueva, otro sitio, la misma pantalla, teclas y clics ya atendidos", () => {
        expect(rutaQueSale(clic(enlace("/ordenes", { target: "_blank" })), ubicacion)).toBeNull();
        expect(rutaQueSale(clic(enlace("/reporte.csv", { download: "" })), ubicacion)).toBeNull();
        expect(rutaQueSale(clic(enlace("https://argos.test/clientes/1")), ubicacion)).toBeNull();
        expect(rutaQueSale(clic(enlace("#cliente")), ubicacion)).toBeNull();
        expect(rutaQueSale(clic(enlace("/ordenes"), { ctrlKey: true }), ubicacion)).toBeNull();
        expect(rutaQueSale(clic(enlace("/ordenes"), { button: 1 }), ubicacion)).toBeNull();
        expect(rutaQueSale(clic(enlace("/ordenes"), { defaultPrevented: true }), ubicacion)).toBeNull();
        expect(rutaQueSale(clic({ closest: () => null }), ubicacion)).toBeNull();
    });

    it("sin redirección abierta: lo que se normaliza a «//host» no se intercepta ni se navega", () => {
        const base = `${ubicacion.origin}${ubicacion.pathname}${ubicacion.search}`;
        for (const malo of ["/.//evil.com/x", "/..//evil.com/x", "/a/..//evil.com/x", "https://iris.test//evil.com/x", "/\\evil.com", "/\\/evil.com", "\\\\evil.com", "/\t/evil.com", "//evil.com/x", "https://evil.com/x"]) {
            expect(rutaQueSale(clic(enlace(malo)), ubicacion), malo).toBeNull();
            expect(rutaInterna(malo, base), malo).toBeNull();
        }
        // Lo que sí es interno se devuelve ya resuelto, nunca el texto recibido.
        expect(rutaInterna("/tickets/../ordenes?x=1#a", base)).toBe("/ordenes?x=1#a");
        expect(rutaInterna("ordenes", "https://iris.test/tickets/")).toBe("/tickets/ordenes");
    });

    it("BarraGuardar: Cancelar y Guardar (submit), deshabilitado mientras falte algo", () => {
        const html = renderToStaticMarkup(<BarraGuardar textoGuardar="Crear orden" faltan={["el técnico"]} alCancelar={() => undefined} />);
        expect(html).toContain('data-slot="barra-guardar"');
        expect(html).toContain("sticky");
        expect(html).toContain("safe-area-inset-bottom");
        expect(html).toContain(">Cancelar<");
        expect(html).toMatch(/<button[^>]*type="submit"[^>]*disabled=""[^>]*>Crear orden/);
        expect(html).toContain("Falta elegir el técnico para guardar.");
    });

    it("FormularioPagina: migas, título, secciones y barra dentro del formulario", () => {
        const html = renderToStaticMarkup(
            <FormularioPagina
                titulo="Nueva orden"
                migas={[{ etiqueta: "Órdenes", href: "/ordenes" }, { etiqueta: "Nueva orden" }]}
                alEnviar={() => undefined}
                barra={{ textoGuardar: "Crear orden" }}
            >
                <SeccionFormulario titulo="1. Cliente y servicio">
                    <p>Campos</p>
                </SeccionFormulario>
            </FormularioPagina>,
        );
        expect(html).toContain('aria-label="Ruta de navegación"');
        expect(html).toMatch(/<h1[^>]*>Nueva orden<\/h1>/);
        expect(html).toMatch(/<form[^>]*novalidate=""/i);
        expect(html).toMatch(/<h2[^>]*>1\. Cliente y servicio<\/h2>/);
        // «Cancelar» vuelve a la miga anterior.
        expect(html).toContain(">Cancelar<");
        expect(html.indexOf("barra-guardar")).toBeLessThan(html.indexOf("</form>"));
    });
});
