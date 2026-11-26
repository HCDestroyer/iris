import { FilaLista, ListaFilas } from "@iris/ui";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";
import { pieListaFilas } from "./pie-lista";

const nada = () => undefined;

describe("pieListaFilas", () => {
    it("con más páginas: «Mostrando n …» y «Cargar más»", () => {
        const cargarMas = vi.fn();
        const pie = pieListaFilas(
            {
                cuantos: 25,
                siguiente: "c2",
                cargarMas,
                cargandoMas: false,
                errorMas: null,
            },
            "tickets",
        );
        expect(pie.resumen).toBe("Mostrando 25 tickets");
        expect(pie.errorMas).toBeUndefined();
        pie.alCargarMas?.();
        expect(cargarMas).toHaveBeenCalledOnce();
    });

    it("en la última página: «No hay más …» y sin botón", () => {
        const pie = pieListaFilas(
            {
                cuantos: 3,
                siguiente: null,
                cargarMas: nada,
                cargandoMas: false,
                errorMas: null,
            },
            "órdenes",
        );
        expect(pie.resumen).toBe("No hay más órdenes.");
        expect(pie.alCargarMas).toBeUndefined();
    });

    it("con la lista vacía: solo el aviso de vacío, sin «No hay más …» debajo", () => {
        const pie = pieListaFilas(
            {
                cuantos: 0,
                siguiente: null,
                cargarMas: nada,
                cargandoMas: false,
                errorMas: null,
            },
            "tickets",
        );
        expect(pie.resumen).toBeUndefined();
        expect(pie.alCargarMas).toBeUndefined();
        const html = renderToStaticMarkup(
            <ListaFilas
                etiqueta="Tickets"
                {...pie}
                vacio={<p>No hay tickets</p>}
            >
                {[]}
            </ListaFilas>,
        );
        expect(html).toContain("No hay tickets");
        expect(html).not.toContain("No hay más");
    });

    it("si falla la página siguiente, el pie de ListaFilas muestra el error con «Reintentar» en lugar del botón", () => {
        const pie = pieListaFilas(
            {
                cuantos: 25,
                siguiente: "c2",
                cargarMas: nada,
                cargandoMas: false,
                errorMas: new Error("sin red"),
            },
            "usuarios",
        );
        const html = renderToStaticMarkup(
            <ListaFilas etiqueta="Usuarios" {...pie}>
                <FilaLista titulo="Ana López" />
            </ListaFilas>,
        );
        expect(html).toContain("No se pudieron cargar más usuarios");
        expect(html).toContain("Reintentar");
        expect(html).not.toContain(">Cargar más<");
    });
});
