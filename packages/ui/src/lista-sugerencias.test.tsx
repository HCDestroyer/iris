import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { idOpcion, ListaSugerencias, teclasSugerencias } from "./componentes/lista-sugerencias";

describe("ListaSugerencias", () => {
    it("listbox con su nombre, opciones con id y la activa marcada", () => {
        const html = renderToStaticMarkup(
            <ListaSugerencias
                id="menciones"
                etiqueta="Mencionar a…"
                activa={1}
                alElegir={() => undefined}
                opciones={[
                    { valor: "5", etiqueta: "Mario Ixcot", detalle: "Técnico" },
                    { valor: "3", etiqueta: "Laura Méndez" },
                ]}
            />,
        );
        expect(html).toMatch(/role="listbox"[^>]*aria-label="Mencionar a…"|aria-label="Mencionar a…"[^>]*role="listbox"/);
        expect(html.match(/role="option"/g)).toHaveLength(2);
        expect(html).toContain(`id="${idOpcion("menciones", 1)}"`);
        expect(html).toMatch(/aria-selected="true"[^>]*>[\s\S]*?Laura Méndez/);
        expect(renderToStaticMarkup(<ListaSugerencias id="x" etiqueta="x" activa={0} alElegir={() => undefined} opciones={[]} />)).toBe("");
    });

    it("teclas: flechas mueven en círculo, Enter elige, Escape cierra y lo demás pasa", () => {
        const hecho: string[] = [];
        const evento = (key: string) => ({ key, shiftKey: false, preventDefault: () => undefined, stopPropagation: () => undefined }) as never;
        const estado = { total: 3, activa: 2, alResaltar: (i: number) => hecho.push(`r${i}`), alElegir: (i: number) => hecho.push(`e${i}`), alCerrar: () => hecho.push("c") };
        expect(teclasSugerencias(evento("ArrowDown"), estado)).toBe(true);
        expect(teclasSugerencias(evento("ArrowUp"), estado)).toBe(true);
        expect(teclasSugerencias(evento("Enter"), estado)).toBe(true);
        expect(teclasSugerencias(evento("Escape"), estado)).toBe(true);
        expect(teclasSugerencias(evento("a"), estado)).toBe(false);
        expect(teclasSugerencias(evento("ArrowDown"), { ...estado, total: 0 })).toBe(false);
        expect(hecho).toEqual(["r0", "r1", "e2", "c"]);
    });
});
