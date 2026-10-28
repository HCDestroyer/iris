import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { Aviso } from "./componentes/aviso";
import { GrupoSegmentado } from "./componentes/grupo-segmentado";
import { InsigniaEstado } from "./componentes/insignia-estado";
import { LogoIris } from "./componentes/marca";

const leer = (ruta: string) => readFileSync(fileURLToPath(new URL(ruta, import.meta.url)), "utf8");

describe("identidad Horus", () => {
    it("los tokens son copia exacta de docs/marca/horus-tokens.css", () => {
        expect(leer("./estilos/horus-tokens.css")).toBe(leer("../../../docs/marca/horus-tokens.css"));
    });

    it("el logo es copia exacta de docs/marca/logo-mark.svg", () => {
        expect(leer("./marca/logo-mark.svg")).toBe(leer("../../../docs/marca/logo-mark.svg"));
    });

    it("LogoIris dibuja los mismos trazos que el SVG de la marca", () => {
        const svg = leer("./marca/logo-mark.svg");
        const html = renderToStaticMarkup(<LogoIris />);
        for (const d of svg.matchAll(/ d="([^"]+)"/g)) {
            expect(html).toContain(`d="${d[1]}"`);
        }
        expect(html).toContain('fill="#0f766e"');
    });
});

describe("estados: icono + texto + color", () => {
    it("InsigniaEstado lleva icono, texto y tono", () => {
        const html = renderToStaticMarkup(<InsigniaEstado tono="success" texto="MFA activo" />);
        expect(html).toContain("<svg");
        expect(html).toContain("MFA activo");
        expect(html).toContain("text-success");
    });

    it("un Aviso de error se anuncia como alerta y muestra su acción", () => {
        const html = renderToStaticMarkup(
            <Aviso tono="error" titulo="No se pudo cargar" accion={<span>Reintentar</span>}>
                Detalle
            </Aviso>,
        );
        expect(html).toContain('role="alert"');
        expect(html).toContain("Reintentar");
    });
});

describe("GrupoSegmentado", () => {
    it("la opción elegida se distingue con el color de marca (no solo con el fondo)", () => {
        const html = renderToStaticMarkup(
            <GrupoSegmentado etiqueta="Mostrar" opciones={[{ valor: "a", etiqueta: "En curso" }, { valor: "b", etiqueta: "Todos" }]} valor="b" alCambiar={() => undefined} />,
        );
        expect(html).toMatch(/aria-checked="true"[^>]*aria-checked:text-primary[^>]*>Todos</);
        expect(html).toMatch(/aria-checked="false"[^>]*>En curso</);
        expect(html).toContain("aria-checked:shadow-[0_0_0_1px_var(--hx-primary)]");
    });
});
