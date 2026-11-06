import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { Autocompletar } from "./componentes/autocompletar";
import { AvatarUsuario, inicialesDe } from "./componentes/avatar-usuario";
import { CampoBusqueda } from "./componentes/campo-busqueda";
import { aNumero, CampoNumero, deNumero } from "./componentes/campo-numero";
import { asignarRef, conAvisoValor } from "./componentes/control-nativo";
import { Interruptor } from "./componentes/interruptor";
import { ListaOpciones } from "./componentes/lista-opciones";
import { CampoSelector, leerOpciones, Selector } from "./componentes/selector";
import { aHora, CampoFecha, CampoHora, deHora } from "./componentes/selector-fecha";

/*
 * Envoltorios de HeroUI que reemplazaron a controles nativos o hechos a mano (Select, DatePicker,
 * TimeField, NumberField, SearchField, Switch, Avatar, ListBox y ComboBox con opciones ricas).
 */

describe("puente con react-hook-form (control-nativo)", () => {
    it("avisa cuando alguien escribe `value` y manda el foco al control visible", () => {
        const escritos: string[] = [];
        let enfocado = 0;
        const input = { value: "", name: "prioridad", focus: () => undefined };
        const envuelto = conAvisoValor(
            input,
            (v) => escritos.push(v),
            () => (enfocado += 1),
        );
        envuelto.value = "alta";
        (envuelto as { value: unknown }).value = 3;
        (envuelto as { value: unknown }).value = null;
        expect(escritos).toEqual(["alta", "3", ""]);
        expect(input.value).toBeNull();
        expect(envuelto.name).toBe("prioridad");
        envuelto.focus();
        expect(enfocado).toBe(1);
    });

    it("asignarRef sirve con función y con objeto", () => {
        let recibido: string | null = "x";
        asignarRef<string>((v) => {
            recibido = v;
        }, null);
        expect(recibido).toBeNull();
        const objeto = { current: null as string | null };
        asignarRef(objeto, "a");
        expect(objeto.current).toBe("a");
        asignarRef<string>(undefined, "b");
    });
});

describe("Selector (HeroUI Select)", () => {
    it("lee las <option> de fragmentos, listas y condiciones, con su texto y si están deshabilitadas", () => {
        const tecnicos = [
            { id: 7, nombre: "Ana", codigo: "A1" },
            { id: 8, nombre: "Luis", codigo: "L2" },
        ];
        const opciones = leerOpciones(
            <>
                <option value="">Todos</option>
                {tecnicos.map((t) => (
                    <option key={t.id} value={t.id}>
                        {t.nombre} ({t.codigo})
                    </option>
                ))}
                {tecnicos.length > 5 ? <option value="x">Nunca</option> : null}
                <optgroup label="Otros">
                    <option disabled value="z">
                        Zeta
                    </option>
                    <option>Sin valor</option>
                </optgroup>
            </>,
        );
        expect(opciones).toEqual([
            { valor: "", texto: "Todos", deshabilitada: false },
            { valor: "7", texto: "Ana (A1)", deshabilitada: false },
            { valor: "8", texto: "Luis (L2)", deshabilitada: false },
            { valor: "z", texto: "Zeta", deshabilitada: true },
            { valor: "Sin valor", texto: "Sin valor", deshabilitada: false },
        ]);
    });

    it("tamaño sm y md, deshabilitado y el valor vacío como opción", () => {
        const chico = renderToStaticMarkup(
            <Selector aria-label="Estado" tamano="sm" value="" disabled>
                <option value="">Todos</option>
                <option value="a">A</option>
            </Selector>,
        );
        expect(chico).toMatch(/<button[^>]*data-valor=""[^>]*class="[^"]*\bh-8\b/);
        expect(chico).toMatch(/<button[^>]*disabled=""/);
        expect(chico).toContain(">Todos</span>");
        const normal = renderToStaticMarkup(
            <Selector aria-label="Estado" value="zz" placeholder="Elige el estado">
                <option value="a">A</option>
            </Selector>,
        );
        expect(normal).toMatch(/<button[^>]*class="[^"]*\bh-10\b/);
        // Un valor que no está entre las opciones muestra el texto de ayuda.
        expect(normal).toContain("Elige el estado");
    });

    it("CampoSelector: ayuda enlazada y el input oculto con el name de register", () => {
        const html = renderToStaticMarkup(
            <CampoSelector id="c" etiqueta="Cuadrilla" ayuda="La que atiende la zona" name="cuadrillaId" defaultValue="2">
                <option value="1">Norte</option>
                <option value="2">Sur</option>
            </CampoSelector>,
        );
        expect(html).toMatch(/<button[^>]*aria-describedby="[^"]*c-ayuda/);
        expect(html).toContain('id="c-ayuda"');
        expect(html).toContain(">Sur</span>");
        expect(html).toMatch(/<input type="hidden" data-slot="valor-nativo" name="cuadrillaId" value="2"/);
    });
});

describe("CampoFecha y CampoHora (DatePicker y TimeField)", () => {
    it("convierte HH:MM de ida y vuelta (24 h)", () => {
        expect(aHora("08:30")?.hour).toBe(8);
        expect(aHora("17:05:00")?.minute).toBe(5);
        expect(aHora("24:00")).toBeNull();
        expect(aHora("8.30")).toBeNull();
        expect(aHora("")).toBeNull();
        expect(deHora(aHora("7:05"))).toBe("07:05");
        expect(deHora(null)).toBe("");
    });

    it("fecha con register: segmentos día/mes/año, error enlazado y el valor AAAA-MM-DD en el input oculto", () => {
        const html = renderToStaticMarkup(<CampoFecha id="f" etiqueta="Fecha" name="fecha" defaultValue="2026-11-20" error="Elige una fecha futura" />);
        expect(html).toContain('data-valor="2026-11-20"');
        expect(html).toMatch(/<input type="hidden" data-slot="valor-nativo" name="fecha" value="2026-11-20"/);
        expect(html.indexOf('data-type="day"')).toBeLessThan(html.indexOf('data-type="month"'));
        expect(html).toContain('aria-label="Abrir calendario"');
        expect(html).toMatch(/aria-describedby="[^"]*f-error/);
        expect(html).toContain("Elige una fecha futura");
    });

    it("hora: hora y minutos sin AM/PM, y vacía si no hay valor", () => {
        const html = renderToStaticMarkup(<CampoHora etiqueta="Hora" value="15:30" onChange={() => undefined} />);
        expect(html).toContain('data-type="hour"');
        expect(html).toContain('data-type="minute"');
        expect(html).not.toContain('data-type="dayPeriod"');
        expect(html).toContain(">15</span>");
        const vacia = renderToStaticMarkup(<CampoHora etiqueta="Hora" name="hora" />);
        expect(vacia).toMatch(/name="hora" value=""/);
    });
});

describe("CampoNumero (NumberField)", () => {
    it("texto del formulario ↔ número", () => {
        expect(aNumero("250.5")).toBe(250.5);
        expect(aNumero("")).toBeNull();
        expect(aNumero("abc")).toBeNull();
        expect(deNumero(199.99)).toBe("199.99");
        expect(deNumero(Number.NaN)).toBe("");
        expect(deNumero(null)).toBe("");
    });

    it("con pasos: botones − y + con íconos de Lucide; el monto sin pasos", () => {
        const cantidad = renderToStaticMarkup(<CampoNumero etiqueta="Cantidad" conPasos name="cantidad" defaultValue="2" />);
        expect(cantidad).toContain("lucide-minus");
        expect(cantidad).toContain("lucide-plus");
        expect(cantidad).toContain('data-slot="campo-numero"');
        expect(cantidad).toMatch(/<label[^>]*>Cantidad<\/label>/);
        expect(cantidad).toMatch(/name="cantidad" value="2"/);
        const monto = renderToStaticMarkup(<CampoNumero etiqueta="Precio unitario" name="precio" />);
        expect(monto).not.toContain("lucide-plus");
    });
});

describe("CampoBusqueda (SearchField)", () => {
    it("input de búsqueda con su etiqueta (visible u oculta) y la lupa de Lucide", () => {
        const html = renderToStaticMarkup(<CampoBusqueda etiqueta="Buscar" etiquetaVisible={false} valor="fibra" alCambiar={() => undefined} placeholder="Número o asunto" />);
        expect(html).toMatch(/<input[^>]*type="search"/);
        expect(html).toMatch(/class="[^"]*sr-only[^"]*"[^>]*>Buscar|>Buscar<\/span>/);
        expect(html).toContain("sr-only");
        expect(html).toContain("lucide-search");
        expect(html).toContain('value="fibra"');
    });
});

describe("Interruptor (Switch)", () => {
    it("un checkbox real con role switch, marcado por defecto y con ayuda", () => {
        const html = renderToStaticMarkup(<Interruptor id="s" etiqueta="Todo el día" ayuda="Sin hora de inicio ni fin" defaultChecked name="todoElDia" />);
        expect(html).toMatch(/<input[^>]*id="s"[^>]*role="switch"|<input[^>]*role="switch"[^>]*id="s"/);
        expect(html).toMatch(/<input[^>]*checked=""/);
        expect(html).toMatch(/name="todoElDia"/);
        expect(html).toContain("Todo el día");
        expect(html).toContain('id="s-ayuda"');
        expect(html).toMatch(/<input[^>]*aria-describedby="[^"]*s-ayuda/);
        // El input cubre la etiqueta (clic en cualquier parte, como la casilla).
        expect(html).toContain("iris-entrada-cubierta");
    });
});

describe("AvatarUsuario (Avatar)", () => {
    it("iniciales y el Avatar de HeroUI, decorativo", () => {
        expect(inicialesDe("Ana López")).toBe("AL");
        expect(inicialesDe("María José Pérez Cux")).toBe("MC");
        expect(inicialesDe("iris")).toBe("I");
        expect(inicialesDe("  ")).toBe("?");
        const html = renderToStaticMarkup(<AvatarUsuario nombre="Luis Pérez" />);
        expect(html).toMatch(/class="avatar\b/);
        expect(html).toContain('aria-hidden="true"');
        expect(html).toContain("LP");
    });
});

describe("ListaOpciones (ListBox) y Autocompletar con opciones ricas", () => {
    it("ListaOpciones es un listbox con su nombre y opciones con contenido propio", () => {
        const html = renderToStaticMarkup(
            <ListaOpciones
                etiqueta="Servicios del cliente"
                opciones={[
                    { valor: "1", texto: "Fibra 100", contenido: <span>Fibra 100 · Activo</span> },
                    { valor: "2", texto: "Radio 10", contenido: <span>Radio 10 · Suspendido</span>, deshabilitada: true },
                ]}
                alElegir={() => undefined}
            />,
        );
        expect(html).toMatch(/role="listbox"[^>]*aria-label="Servicios del cliente"|aria-label="Servicios del cliente"[^>]*role="listbox"/);
        expect(html.match(/role="option"/g)).toHaveLength(2);
        expect(html).toContain("Radio 10 · Suspendido");
        expect(html).toMatch(/aria-disabled="true"/);
    });

    it("Autocompletar con lupa, texto controlado y límite de caracteres", () => {
        const html = renderToStaticMarkup(
            <Autocompletar
                etiqueta="Cliente en Argos"
                opciones={[{ valor: "1", etiqueta: "Ana Morales", contenido: <span>Ana Morales · CL-0503</span> }]}
                valor={null}
                alCambiar={() => undefined}
                alBuscar={() => undefined}
                texto="Ana"
                conLupa
                maxLength={100}
                cargando
            />,
        );
        expect(html).toMatch(/<input[^>]*role="combobox"/);
        expect(html).toContain('value="Ana"');
        expect(html).toMatch(/maxLength="100"|maxlength="100"/);
        expect(html).toContain("lucide-search");
    });
});
