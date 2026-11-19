import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { AlbumFotos, FotoMensaje, medidasFoto } from "./componentes/fotos";
import { VisorFotos } from "./componentes/visor-fotos";

describe("medidasFoto", () => {
    it("toma la proporción de la foto con máximo de ancho y alto", () => {
        expect(medidasFoto(1600, 900)).toEqual({ ancho: 320, proporcion: 1.778, contener: false, supuesta: false });
        // De pie: el alto manda (340 px) y el ancho baja.
        expect(medidasFoto(900, 1600)).toEqual({ ancho: 191, proporcion: 0.563, contener: false, supuesta: false });
        expect(medidasFoto(1200, 1200).ancho).toBe(320);
    });

    it("acota las proporciones extremas entre 1:2 y 2:1 (recorte centrado; el visor la muestra entera)", () => {
        expect(medidasFoto(2000, 300)).toMatchObject({ ancho: 320, proporcion: 2 });
        expect(medidasFoto(300, 2000)).toMatchObject({ ancho: 170, proporcion: 0.5 });
    });

    it("una foto chica no se agranda; una diminuta se centra en el mínimo sin estirarse", () => {
        expect(medidasFoto(240, 180)).toMatchObject({ ancho: 240, contener: false });
        expect(medidasFoto(80, 80)).toEqual({ ancho: 150, proporcion: 1, contener: true, supuesta: false });
    });

    it("sin medidas (WebP, mensaje antiguo) reserva 4:3", () => {
        for (const [a, h] of [
            [null, null],
            [undefined, 300],
            [0, 0],
            [Number.NaN, 10],
        ] as const) {
            expect(medidasFoto(a, h)).toEqual({ ancho: 320, proporcion: 1.333, contener: false, supuesta: true });
        }
    });
});

describe("FotoMensaje", () => {
    it("reserva la proporción antes de cargar: aspect-ratio y ancho acotado al 100 % de la burbuja", () => {
        const html = renderToStaticMarkup(<FotoMensaje src="/api/v1/adjuntos/x" ancho={900} alto={1600} estado="lista" etiqueta="Ver foto de Carlos, 06:49" superpuesto="06:49" />);
        expect(html).toContain('aria-label="Ver foto de Carlos, 06:49"');
        expect(html).toContain("width:min(191px, 100%)");
        expect(html).toContain("aspect-ratio:0.563");
        expect(html).toContain('data-estado-foto="cargando"');
        expect(html).toContain('width="900"');
        expect(html).toContain('loading="lazy"');
        expect(html).toContain('data-slot="hora-foto"');
    });

    it("pendiente y rechazada: marcador con su texto, del mismo tamaño", () => {
        const pendiente = renderToStaticMarkup(<FotoMensaje estado="pendiente" etiqueta="x" textoPendiente="Descargando de WhatsApp…" />);
        expect(pendiente).toContain('role="img"');
        expect(pendiente).toContain("Descargando de WhatsApp…");
        expect(pendiente).toContain("aspect-ratio:1.333");
        expect(pendiente).not.toContain("<img");
        const rechazada = renderToStaticMarkup(<FotoMensaje estado="rechazada" etiqueta="x" textoRechazada="No se pudo descargar de WhatsApp" />);
        expect(rechazada).toContain('data-estado-foto="rechazada"');
        expect(rechazada).toContain("No se pudo descargar de WhatsApp");
    });
});

describe("AlbumFotos", () => {
    const foto = (id: string) => ({ id, src: `/f/${id}`, ancho: 800, alto: 600, estado: "lista" as const, etiqueta: `Ver foto ${id}` });

    it("cuatro celdas como máximo; la última lleva «+N» y la hora", () => {
        const html = renderToStaticMarkup(<AlbumFotos etiqueta="6 fotos de Carlos" fotos={["a", "b", "c", "d", "e", "f"].map(foto)} superpuesto="06:49" />);
        expect(html).toContain('aria-label="6 fotos de Carlos"');
        expect(html).toContain('data-fotos="6"');
        expect(html.match(/<img /g)).toHaveLength(4);
        expect(html).toContain("+2");
        expect(html).toContain("Ver foto d (y 2 más)");
        expect(html.match(/data-slot="hora-foto"/g)).toHaveLength(1);
    });

    it("con tres, la tercera ocupa el ancho", () => {
        const html = renderToStaticMarkup(<AlbumFotos etiqueta="3 fotos" fotos={["a", "b", "c"].map(foto)} />);
        expect(html).toContain("col-span-2");
    });
});

describe("VisorFotos", () => {
    it("cerrado no pinta nada", () => {
        expect(renderToStaticMarkup(<VisorFotos fotos={[]} indice={null} alCambiar={() => undefined} />)).toBe("");
    });
});
