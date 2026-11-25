// Piezas del rediseño (nov. 2026): marco del panel, listas, fichas, campos y el «?» del manual.
import { ClipboardList, LayoutDashboard, LifeBuoy, Plug, Router, Settings, Users } from "lucide-react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { AccionFija } from "./componentes/accion-fija";
import { Boton } from "./componentes/boton";
import { BotonManual, esClaveManual, rutaManual } from "./componentes/boton-manual";
import { CampoMoneda } from "./componentes/campo-moneda";
import { BotonBuscar, BotonFlotante, EncabezadoPanel, IndicadorEstado } from "./componentes/encabezado-panel";
import { FranjaFicha } from "./componentes/franja-ficha";
import { InsigniaEstado } from "./componentes/insignia-estado";
import { FilaLista, ListaFilas } from "./componentes/lista-filas";
import { gruposMenuAcciones, MenuAcciones, type AccionMenu } from "./componentes/menu-acciones";
import { MenuCuenta } from "./componentes/menu-cuenta";
import { IslaInferior, MenuIsla, textoContadorIsla, type GrupoIsla } from "./componentes/menu-isla";
import { PestanasSegmentadas } from "./componentes/pestanas-segmentadas";
import { VistaRapida } from "./componentes/vista-rapida";

const nada = () => undefined;

const GRUPOS: GrupoIsla[] = [
    {
        id: "operacion",
        etiqueta: "Operación",
        tituloOculto: true,
        entradas: [
            { href: "/", etiqueta: "Inicio", icono: LayoutDashboard, actual: true },
            { href: "/tickets", etiqueta: "Tickets", icono: LifeBuoy, contador: 42, tonoContador: "atencion" },
            { href: "/ordenes", etiqueta: "Órdenes y tareas", etiquetaCorta: "Órdenes", icono: ClipboardList },
        ],
    },
    {
        id: "administracion",
        etiqueta: "Administración",
        alPie: true,
        entradas: [
            { href: "/usuarios", etiqueta: "Usuarios", icono: Users },
            { href: "/administracion", etiqueta: "Administración", icono: Settings },
        ],
    },
];

describe("MenuIsla", () => {
    it("expandida: barra lateral con el menú «Principal», la actual marcada, contador con tono y grupo que se abre", () => {
        const html = renderToStaticMarkup(<MenuIsla grupos={GRUPOS} compacta={false} alAlternar={nada} marca={<a href="/">Iris</a>} id="isla" />);
        expect(html).toMatch(/<aside[^>]*aria-label="Barra lateral"[^>]*data-slot="menu-isla"/);
        expect(html).not.toContain('data-contraida="true"');
        expect(html).toContain("w-62");
        expect(html).toMatch(/<nav aria-label="Principal"/);
        expect(html).toMatch(/<a[^>]*href="\/"[^>]*aria-current="page"/);
        expect(html).toMatch(/data-slot="contador" class="[^"]*bg-atencion-fondo[^"]*">42</);
        // La operación no muestra su título; Administración es un Disclosure de HeroUI, abierto.
        expect(html).toMatch(/class="sr-only">Operación</);
        expect(html).toMatch(/data-slot="grupo-isla" class="disclosure/);
        expect(html).toMatch(/aria-expanded="true"[^>]*>.*Administración/);
        // Botón ‹ › de 28 px sobre el borde derecho, con su nombre y a qué controla.
        expect(html).toMatch(/<button[^>]*aria-label="Contraer menú"[^>]*aria-expanded="true"[^>]*aria-controls="isla"/);
        expect(html).toContain("size-7");
        expect(html).toContain("-right-3.5");
    });

    it("compacta: 80 px, ícono y nombre corto, el contador es un punto y el número va en el nombre", () => {
        const html = renderToStaticMarkup(<MenuIsla grupos={GRUPOS} compacta alAlternar={nada} marca={<span>logo</span>} />);
        expect(html).toContain('data-contraida="true"');
        expect(html).toContain("w-20");
        expect(html).toMatch(/aria-label="Expandir menú"[^>]*aria-expanded="false"/);
        expect(html).toMatch(/<a[^>]*aria-label="Tickets, 42"[^>]*href="\/tickets"/);
        expect(html).toContain('data-slot="punto-contador"');
        expect(html).toMatch(/aria-hidden="true" class="max-w-full truncate">Órdenes</);
        // Administración (2 entradas) es un solo botón que abre su grupo al lado.
        expect(html).toMatch(/<button[^>]*aria-label="Administración: abrir el grupo"/);
    });

    it("contador: 99+ desde 100", () => {
        expect(textoContadorIsla(7)).toBe("7");
        expect(textoContadorIsla(120)).toBe("99+");
    });

    it("teléfono: isla inferior «Accesos rápidos» con «Más» y el botón redondo de la acción principal", () => {
        const html = renderToStaticMarkup(
            <IslaInferior
                entradas={GRUPOS[0]?.entradas ?? []}
                mas={<Boton variante="fantasma">Más</Boton>}
                accion={
                    <Boton aria-label="Crear" className="size-14">
                        +
                    </Boton>
                }
            />,
        );
        expect(html).toMatch(/<nav aria-label="Accesos rápidos" data-slot="barra-inferior"/);
        expect(html).toContain("rounded-full");
        expect(html).toContain("right-[4.75rem]");
        expect(html).toContain("env(safe-area-inset-bottom)");
        expect(html).toMatch(/>Órdenes<\/span>/);
        expect(html).toMatch(/aria-label="Crear"/);
    });
});

describe("EncabezadoPanel", () => {
    it("sin barra: dos grupos de islas sueltas (lo que haces a la izquierda, lo que vigilas a la derecha)", () => {
        const html = renderToStaticMarkup(<EncabezadoPanel izquierda={<span>izq</span>} derecha={<span>der</span>} />);
        expect(html).toMatch(/<header data-slot="encabezado-panel"/);
        expect(html).not.toMatch(/border-b|glass-material/);
        expect(html).toMatch(/izq.*ml-auto.*der/s);
    });

    it("Buscar: lupa, texto y Ctrl K; nombre «Buscador global» y el atajo para lectores", () => {
        const html = renderToStaticMarkup(<BotonBuscar alAbrir={nada} />);
        expect(html).toMatch(/<button[^>]*aria-label="Buscador global"/);
        expect(html).toMatch(/aria-keyshortcuts="Control\+K Meta\+K"/);
        expect(html).toContain("Buscar…");
        expect(html).toContain("Ctrl K");
        expect(html).toContain("lucide-search");
        expect(html).toContain("size-11");
        expect(html).toContain("sm:w-[15.625rem]");
    });

    it("BotonFlotante: isla de 44 px; redondo sin texto; acento en índigo tenue", () => {
        expect(renderToStaticMarkup(<BotonFlotante aria-label="Avisos" redondo />)).toContain("size-11");
        expect(renderToStaticMarkup(<BotonFlotante acento>Ticket</BotonFlotante>)).toContain("bg-primario-suave");
    });

    it("IndicadorEstado: un punto si todo está bien; crece con texto cuando hay que actuar", () => {
        const bien = renderToStaticMarkup(<IndicadorEstado icono={<Plug />} tono="bien" texto="Argos y Horus conectados" />);
        expect(bien).toMatch(/aria-label="Argos y Horus conectados"/);
        expect(bien).toContain('data-slot="punto-estado"');
        expect(bien).toContain("size-11");
        const mal = renderToStaticMarkup(<IndicadorEstado icono={<Plug />} tono="peligro" texto="Conexiones" accion="Argos sin conexión" />);
        expect(mal).toMatch(/aria-label="Conexiones: Argos sin conexión"/);
        expect(mal).toContain(">Argos sin conexión</span>");
        expect(mal).toContain("bg-peligro-fondo");
    });
});

describe("MenuCuenta", () => {
    it("isla con avatar, nombre y «rol · empresa»; el nombre accesible dice la disponibilidad", () => {
        const html = renderToStaticMarkup(
            <MenuCuenta nombre="Ana López" rol="Supervisora" empresa="Kns Telecom" disponible opciones={[{ id: "perfil", texto: "Mi cuenta", alElegir: nada }]} alCerrarSesion={nada} />,
        );
        expect(html).toMatch(/<button[^>]*aria-label="Tu cuenta: Ana López, disponible"/);
        expect(html).toContain("Supervisora · Kns Telecom");
        expect(html).toContain('data-slot="punto-disponible"');
        expect(html).toContain(">AL<");
        // Cerrado no dibuja el panel.
        expect(html).not.toContain("Cerrar sesión");
    });

    it("sin disponibilidad no hay punto", () => {
        const html = renderToStaticMarkup(<MenuCuenta nombre="Luis Pérez" opciones={[]} alCerrarSesion={nada} />);
        expect(html).toMatch(/aria-label="Tu cuenta: Luis Pérez"/);
        expect(html).not.toContain("punto-disponible");
    });
});

describe("MenuAcciones (⋯)", () => {
    const acciones: AccionMenu[] = [
        { id: "cerrar", texto: "Cerrar ticket…", peligro: true, alElegir: nada },
        { id: "abrir", texto: "Abrir ficha", atajo: "Enter", alElegir: nada },
        { id: "fusionar", texto: "Fusionar con otro…", peligro: true, permitida: false, alElegir: nada },
        { id: "asignar", texto: "Asignar…", alElegir: nada },
        { id: "copiar", texto: "Copiar número", seccion: "Más", alElegir: nada },
    ];

    it("las frecuentes primero (en su orden y por sección), las peligrosas al final; sin permiso no aparecen", () => {
        const { comunes, peligrosas } = gruposMenuAcciones(acciones);
        expect(comunes.map((g) => [g.titulo, g.acciones.map((a) => a.id)])).toEqual([
            [null, ["abrir", "asignar"]],
            ["Más", ["copiar"]],
        ]);
        expect(peligrosas.map((a) => a.id)).toEqual(["cerrar"]);
    });

    it("disparador redondo «⋯» con su nombre; sin acciones visibles no se dibuja", () => {
        const html = renderToStaticMarkup(<MenuAcciones etiqueta="Acciones de TCK-001236" acciones={acciones} />);
        expect(html).toMatch(/<button[^>]*aria-label="Acciones de TCK-001236"/);
        expect(html).toContain('data-slot="menu-acciones"');
        expect(html).toContain("lucide-ellipsis");
        expect(renderToStaticMarkup(<MenuAcciones etiqueta="x" acciones={[{ id: "a", texto: "A", permitida: false, alElegir: nada }]} />)).toBe("");
    });
});

describe("VistaRapida", () => {
    it("cerrada no pinta nada; abierta flota sin telón con «Vista rápida», código, título, estados y pie", () => {
        expect(renderToStaticMarkup(<VistaRapida abierto={false} alCerrar={nada} titulo="x" />)).toBe("");
        const html = renderToStaticMarkup(
            <VistaRapida abierto alCerrar={nada} titulo="Lentitud por las noches" codigo="TCK-001238" insignias={<InsigniaEstado tono="error" texto="Alta" />} pie={<Boton>Asignar</Boton>}>
                <p>Último mensaje</p>
            </VistaRapida>,
        );
        expect(html).toMatch(/role="dialog" aria-modal="false" aria-label="Vista rápida: Lentitud por las noches"/);
        expect(html).toContain('data-slot="vista-rapida"');
        expect(html).not.toMatch(/backdrop/);
        expect(html).toContain(">Vista rápida<");
        expect(html).toContain("TCK-001238");
        expect(html).toMatch(/<h2[^>]*>Lentitud por las noches<\/h2>/);
        expect(html).toMatch(/aria-label="Cerrar vista rápida"/);
        expect(html).toMatch(/<footer[^>]*>.*Asignar/s);
    });
});

describe("ListaFilas y FilaLista", () => {
    it("lista semántica con nombre, filas separadas, fila activa, menú y pie con «Cargar más»", () => {
        const html = renderToStaticMarkup(
            <ListaFilas etiqueta="Tickets" resumen="Mostrando 2 de 42" alCargarMas={nada} cabecera={<span>buscador</span>}>
                <FilaLista titulo="Sin internet en toda la cuadra" detalle="TCK-001236 · Kns Telecom" icono={Router} tonoIcono="peligro" href="/tickets/1" />
                <FilaLista titulo="Lentitud por las noches" detalle="TCK-001238" alAbrir={nada} activa menu={<span>menu</span>} extremo={<InsigniaEstado tono="neutral" texto="Abierto" />} />
            </ListaFilas>,
        );
        expect(html).toMatch(/<section data-slot="lista-filas" aria-label="Tickets"/);
        expect(html).toMatch(/<ul aria-label="Tickets" class="[^"]*divide-y/);
        expect(html.match(/data-slot="fila-lista"/g)).toHaveLength(2);
        expect(html).toMatch(/<a[^>]*href="\/tickets\/1"/);
        expect(html).toMatch(/data-activa="true" aria-current="true" class="[^"]*bg-primario-suave/);
        expect(html).toContain("bg-peligro-fondo");
        expect(html).toContain("Mostrando 2 de 42");
        expect(html).toContain("Cargar más");
    });

    it("extremo: oculto en el teléfono por omisión; con `debajo` baja a su línea bajo el título", () => {
        const oculto = renderToStaticMarkup(<FilaLista titulo="A" extremo={<span>estado</span>} />);
        expect(oculto).toMatch(/data-slot="extremo-fila" class="[^"]*hidden sm:flex/);
        const debajo = renderToStaticMarkup(<FilaLista titulo="A" extremo={<span>estado</span>} extremoTelefono="debajo" />);
        expect(debajo).toMatch(/data-slot="fila-lista" class="[^"]*flex-wrap sm:flex-nowrap/);
        expect(debajo).toMatch(/data-slot="extremo-fila" class="[^"]*order-last flex w-full/);
        expect(debajo).not.toMatch(/data-slot="extremo-fila" class="[^"]*hidden/);
    });

    it("pie: el error de «Cargar más» reemplaza al botón", () => {
        const html = renderToStaticMarkup(
            <ListaFilas etiqueta="Órdenes" alCargarMas={nada} errorMas={<p>No se pudieron cargar más órdenes</p>}>
                <FilaLista titulo="OT-1" />
            </ListaFilas>,
        );
        expect(html).toContain("No se pudieron cargar más órdenes");
        expect(html).not.toContain("Cargar más<");
    });

    it("sin filas muestra lo que diga `vacio`", () => {
        const html = renderToStaticMarkup(
            <ListaFilas etiqueta="Tickets" vacio={<p>No hay tickets</p>}>
                {[]}
            </ListaFilas>,
        );
        expect(html).toContain("No hay tickets");
        expect(html).not.toContain("<ul");
        // Un hueco y un `map` vacío tampoco son filas.
        const huecos = renderToStaticMarkup(
            <ListaFilas etiqueta="Tickets" vacio={<p>No hay tickets</p>}>
                {null}
                {[].map(() => null)}
            </ListaFilas>,
        );
        expect(huecos).toContain("No hay tickets");
    });
});

describe("FranjaFicha", () => {
    it("identidad (h1), estados, contadores, una acción principal y «Más», en una fila", () => {
        const html = renderToStaticMarkup(
            <FranjaFicha
                titulo="José López"
                detalle="CLI-00412 · Zona Norte"
                icono={Router}
                insignias={<InsigniaEstado tono="success" texto="Activo" />}
                contadores={[
                    { etiqueta: "Tickets abiertos", valor: 2 },
                    { etiqueta: "Órdenes", valor: 1 },
                ]}
                manual={<BotonManual clave="clientes/ficha" />}
                accion={<Boton>Crear orden</Boton>}
                mas={<span>más</span>}
            />,
        );
        expect(html).toMatch(/<section data-slot="franja-ficha" class="surface-card flex/);
        expect(html).toMatch(/<h1[^>]*title="José López"[^>]*>José López<\/h1>/);
        // Título y detalle cortados: el texto completo queda en `title`.
        expect(html).toMatch(/<p class="truncate[^"]*" title="CLI-00412 · Zona Norte">/);
        expect(html).toMatch(/<dt[^>]*>Tickets abiertos<\/dt><dd[^>]*>2<\/dd>/);
        expect(html).toContain('data-clave="clientes/ficha"');
        expect(html).toMatch(/ml-auto.*Crear orden.*más/s);
        // Las acciones bajan y se acomodan en el teléfono: nunca desbordan la franja.
        expect(html).toMatch(/data-slot="acciones-ficha" class="[^"]*max-w-full min-w-0 flex-wrap/);
    });
});

describe("PestanasSegmentadas", () => {
    it("Tabs de HeroUI con riel segmentado, la elegida marcada y contadores con tono", () => {
        const html = renderToStaticMarkup(
            <PestanasSegmentadas
                etiqueta="Vistas de tickets"
                seleccionada="activos"
                pestanas={[
                    { id: "activos", etiqueta: "Activos", contador: 42 },
                    { id: "sin", etiqueta: "Sin asignar", contador: 6, tonoContador: "peligro" },
                    { id: "todos", etiqueta: "Todos" },
                ]}
            />,
        );
        expect(html).toMatch(/aria-label="Vistas de tickets" role="tablist"/);
        expect(html).toContain("bg-relleno");
        expect(html).toContain("rounded-[14px]");
        expect(html).toMatch(/aria-selected="true"[^>]*role="tab"[^>]*data-selected="true"><div data-slot="tabs-indicator"[^>]*><\/div><span[^>]*>Activos/);
        expect(html).toMatch(/data-slot="contador" class="[^"]*bg-peligro-fondo[^"]*">6</);
    });

    it("a lo ancho reparte las opciones (Hoy · Mañana · Semana)", () => {
        const html = renderToStaticMarkup(<PestanasSegmentadas etiqueta="Días" ancho pestanas={[{ id: "hoy", etiqueta: "Hoy" }]} />);
        expect(html).toContain("[&amp;&gt;*]:flex-1");
        expect(html).toContain("w-full");
    });
});

describe("CampoMoneda", () => {
    it("NumberField de HeroUI en quetzales, con − y + que dicen su paso y el valor para el formulario", () => {
        const html = renderToStaticMarkup(<CampoMoneda etiqueta="Monto" name="monto" defaultValue="150" paso={25} />);
        expect(html).toMatch(/class="number-field/);
        expect(html).toMatch(/value="Q\s?150\.00"/);
        expect(html).toMatch(/aria-label="Restar Q\s?25\.00"/);
        expect(html).toMatch(/aria-label="Sumar Q\s?25\.00"/);
        expect(html).toMatch(/<input type="hidden" data-slot="valor-nativo" name="monto" value="150"/);
    });
});

describe("AccionFija", () => {
    it("grupo con nombre, fijo abajo en el teléfono con el área segura; en su lugar en escritorio", () => {
        const html = renderToStaticMarkup(
            <AccionFija etiqueta="Acciones de la orden">
                <Boton className="flex-1">Continuar: Fotos</Boton>
            </AccionFija>,
        );
        expect(html).toMatch(/role="group" aria-label="Acciones de la orden" data-slot="accion-fija" data-modo="telefono"/);
        expect(html).toContain("fixed inset-x-0 bottom-0 lg:static");
        expect(html).toContain("env(safe-area-inset-bottom)");
    });
});

describe("BotonManual («?»)", () => {
    it("enlaza a /manual/<clave> con nombre accesible y cursor de ayuda", () => {
        const html = renderToStaticMarkup(<BotonManual clave="tickets/lista" />);
        expect(html).toMatch(/<a[^>]*href="\/manual\/tickets\/lista"/);
        expect(html).toMatch(/aria-label="Manual de esta pantalla"/);
        expect(html).toContain('data-clave="tickets/lista"');
        expect(html).toContain("cursor-help");
        expect(html).toContain("lucide-circle-question-mark");
    });

    it("la clave es modulo/pantalla: minúsculas, números y guiones", () => {
        for (const bien of ["tickets/lista", "campo/orden", "administracion/integraciones/argos", "agenda/dia-tecnico"]) expect(esClaveManual(bien)).toBe(true);
        for (const mal of ["tickets", "/tickets/lista", "Tickets/Lista", "tickets/", "tickets//lista", "tickets/lista?x=1", "tíckets/lista"]) expect(esClaveManual(mal)).toBe(false);
        expect(rutaManual("inicio/tablero")).toBe("/manual/inicio/tablero");
        // Dentro de /campo, el manual que guarda el service worker (abre sin señal).
        expect(rutaManual("campo/hoy", "/campo/manual")).toBe("/campo/manual/hoy");
        expect(renderToStaticMarkup(<BotonManual clave="campo/orden" base="/campo/manual" />)).toMatch(/href="\/campo\/manual\/orden"/);
        expect(() => rutaManual("mal")).toThrow(/modulo\/pantalla/);
    });
});
