import { FileText, Image as IconoImagen } from "lucide-react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { ListaRecortada } from "./componentes/lista-recortada";
import { MenuArchivos } from "./componentes/menu-archivos";
import { VistaPreviaArchivos, type ArchivoEnVista } from "./componentes/vista-previa-archivos";

const nada = () => undefined;

describe("VistaPreviaArchivos", () => {
    const base: ArchivoEnVista = { id: "a", nombre: "foto.png", detalle: "PNG · 12 KB", clase: "imagen", url: "blob:http://x/1", estado: "listo" };

    it("sin archivos no pinta nada", () => {
        expect(renderToStaticMarkup(<VistaPreviaArchivos etiqueta="Archivos por enviar" archivos={[]} alQuitar={nada} />)).toBe("");
    });

    it("una foto: miniatura ampliable, nombre, detalle y quitar", () => {
        const html = renderToStaticMarkup(<VistaPreviaArchivos etiqueta="Archivos por enviar" archivos={[base]} alQuitar={nada} />);
        expect(html).toContain('role="group"');
        expect(html).toContain('aria-label="Archivos por enviar"');
        expect(html).toContain('src="blob:http://x/1"');
        expect(html).toContain('aria-label="Ampliar foto.png"');
        expect(html).toContain("PNG · 12 KB");
        expect(html).toContain('aria-label="Quitar foto.png"');
        expect(html).toContain('data-estado="listo"');
    });

    it("un documento, un video o un SVG (sin URL) nunca se incrustan: icono y nombre como texto", () => {
        const html = renderToStaticMarkup(
            <VistaPreviaArchivos
                etiqueta="Archivos"
                archivos={[
                    { id: "d", nombre: 'logo<img src=x onerror="alert(1)">.svg', detalle: "SVG · 1 KB", clase: "documento", estado: "invalido", mensaje: "Solo fotos (JPG, PNG, WebP) o PDF." },
                    { id: "v", nombre: "poste.mp4", detalle: "Video · 3 MB", clase: "video", estado: "invalido", mensaje: "Los videos no se pueden enviar." },
                ]}
                alQuitar={nada}
            />,
        );
        expect(html).not.toContain("<img");
        expect(html).toContain("logo&lt;img src=x onerror=&quot;alert(1)&quot;&gt;.svg");
        expect(html).toContain("Solo fotos (JPG, PNG, WebP) o PDF.");
        expect(html).toContain("Los videos no se pueden enviar.");
        expect(html.match(/data-estado="invalido"/g)).toHaveLength(2);
    });

    it("subiendo: barra de avance con su valor, y no se puede quitar a medias", () => {
        const html = renderToStaticMarkup(<VistaPreviaArchivos etiqueta="Archivos" archivos={[{ ...base, estado: "subiendo", progreso: 0.42 }]} alQuitar={nada} />);
        expect(html).toContain('role="progressbar"');
        expect(html).toContain('aria-valuenow="42"');
        expect(html).toContain("Subiendo… 42 %");
        expect(html).toMatch(/aria-label="Quitar foto\.png"[^>]*disabled/);
    });

    it("error: lo dice como alerta y ofrece «Reintentar» solo si se puede", () => {
        const fallo: ArchivoEnVista = { ...base, estado: "error", mensaje: "No se pudo subir: La API no responde" };
        const con = renderToStaticMarkup(<VistaPreviaArchivos etiqueta="Archivos" archivos={[fallo]} alQuitar={nada} alReintentar={nada} />);
        expect(con).toContain('role="alert"');
        expect(con).toContain("No se pudo subir: La API no responde");
        expect(con).toContain("Reintentar");
        expect(renderToStaticMarkup(<VistaPreviaArchivos etiqueta="Archivos" archivos={[fallo]} alQuitar={nada} />)).not.toContain("Reintentar");
    });
});

describe("MenuArchivos", () => {
    it("un selector de archivos oculto por opción, con sus tipos, varios y cámara", () => {
        const html = renderToStaticMarkup(
            <MenuArchivos
                etiqueta="Adjuntar"
                alElegir={nada}
                opciones={[
                    { clave: "fotos", etiqueta: "Fotos", icono: IconoImagen, accept: "image/jpeg,image/png", varios: true },
                    { clave: "documento", etiqueta: "Documento", icono: FileText, accept: "application/pdf" },
                    { clave: "camara", etiqueta: "Cámara", icono: IconoImagen, accept: "image/jpeg", camara: true },
                ]}
            />,
        );
        expect(html).toContain('aria-label="Adjuntar"');
        expect(html).toMatch(/<input[^>]*data-archivos="fotos"[^>]*accept="image\/jpeg,image\/png"[^>]*multiple/);
        expect(html).toMatch(/<input[^>]*data-archivos="documento"[^>]*accept="application\/pdf"/);
        expect(html).not.toMatch(/<input[^>]*data-archivos="documento"[^>]*multiple/);
        expect(html).toMatch(/<input[^>]*data-archivos="camara"[^>]*capture="environment"/);
        expect(html.match(/<input[^>]*type="file"[^>]*hidden/g)).toHaveLength(3);
    });
});

describe("ListaRecortada", () => {
    it("sin medir (servidor) pinta todos, sin «+N»", () => {
        const html = renderToStaticMarkup(
            <ListaRecortada
                title="Uno, Dos"
                elementos={[
                    { clave: 1, nodo: <b>Uno</b> },
                    { clave: 2, nodo: <b>Dos</b> },
                ]}
            />,
        );
        expect(html).toContain("<b>Uno</b>");
        expect(html).toContain("<b>Dos</b>");
        expect(html).toContain('title="Uno, Dos"');
        expect(html).not.toContain("lista-mas");
        expect(html).not.toContain("data-oculto");
    });
});
