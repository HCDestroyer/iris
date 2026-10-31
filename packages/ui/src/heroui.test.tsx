import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { Aviso } from "./componentes/aviso";
import { Boton, variantesBoton } from "./componentes/boton";
import { Campo } from "./componentes/campo";
import { Cargando } from "./componentes/cargando";
import { Esqueleto } from "./componentes/esqueleto";
import { EstadoVacio } from "./componentes/estado-vacio";
import { InsigniaEstado } from "./componentes/insignia-estado";
import { unirProps } from "./componentes/ranura";
import { Selector } from "./componentes/selector";
import { Tabla, TablaCelda, TablaCuerpo, TablaFila } from "./componentes/tabla";
import { Tarjeta, TarjetaTitulo } from "./componentes/tarjeta";
import { CircleCheck } from "lucide-react";

const leer = (ruta: string) => readFileSync(fileURLToPath(new URL(ruta, import.meta.url)), "utf8");

describe("tema Horus para HeroUI", () => {
    const tema = leer("./estilos/tema-heroui.css");

    it("cada color que usan los componentes de HeroUI apunta a un token de Horus", () => {
        const requeridas = [
            "accent",
            "accent-foreground",
            "accent-hover",
            "focus",
            "background",
            "foreground",
            "muted",
            "default",
            "default-foreground",
            "surface",
            "overlay",
            "border",
            "separator",
            "success",
            "warning",
            "danger",
            "danger-foreground",
            "field-background",
            "field-border",
        ];
        for (const nombre of requeridas) {
            const declaracion = new RegExp(`--${nombre}:\\s*([^;]+);`).exec(tema);
            expect(declaracion, `falta --${nombre}`).not.toBeNull();
            expect(declaracion?.[1], `--${nombre} no usa un token --hx-*`).toContain("--hx-");
        }
    });

    it("la marca es un solo color: accent y focus son el teal de Horus", () => {
        expect(tema).toMatch(/--accent:\s*var\(--hx-primary\);/);
        expect(tema).toMatch(/--focus:\s*var\(--hx-primary\);/);
    });

    it("sin colores crudos: ningún hexadecimal en el tema", () => {
        expect(tema).not.toMatch(/#[0-9a-f]{3,8}\b/i);
    });

    it("estilos.css importa HeroUI, luego los tokens y luego el tema (en ese orden)", () => {
        const estilos = leer("./estilos/estilos.css");
        const heroui = estilos.indexOf('@import "@heroui/styles"');
        const tokens = estilos.indexOf('@import "./horus-tokens.css"');
        const temaHeroui = estilos.indexOf('@import "./tema-heroui.css"');
        expect(heroui).toBeGreaterThanOrEqual(0);
        expect(tokens).toBeGreaterThan(heroui);
        expect(temaHeroui).toBeGreaterThan(tokens);
        // `muted` es de HeroUI (texto atenuado): Iris no lo redefine como fondo.
        expect(estilos).not.toMatch(/--color-muted:/);
    });

    it("la versión instalada de HeroUI es la fijada (v3)", () => {
        const requerir = createRequire(import.meta.url);
        const paquete = JSON.parse(readFileSync(requerir.resolve("@heroui/react/package.json"), "utf8")) as { version: string };
        expect(paquete.version).toMatch(/^3\./);
    });
});

describe("componentes de siempre, por dentro HeroUI", () => {
    it("Boton es un <button> nativo con las clases de HeroUI y el radio de Horus", () => {
        const html = renderToStaticMarkup(
            <Boton title="Guardar cambios" data-testid="g">
                Guardar
            </Boton>,
        );
        expect(html).toMatch(/^<button[^>]*type="button"/);
        expect(html).toContain("button--primary");
        expect(html).toContain("rounded-md");
        expect(html).toContain('title="Guardar cambios"');
        expect(html).toContain('data-testid="g"');
        expect(html).toContain('data-slot="boton"');
    });

    it("variantes de Iris → variantes de HeroUI", () => {
        expect(variantesBoton({ variante: "contorno" })).toContain("button--outline");
        expect(variantesBoton({ variante: "fantasma", tamano: "icono" })).toContain("button--icon-only");
        expect(variantesBoton({ variante: "peligro" })).toContain("text-error");
        expect(variantesBoton({ variante: "secundario", tamano: "sm" })).toContain("button--sm");
    });

    it("Boton asChild pasa estilo y props al hijo (p. ej. un enlace)", () => {
        const html = renderToStaticMarkup(
            <Boton asChild variante="contorno">
                <a href="/tickets">Tickets</a>
            </Boton>,
        );
        expect(html).toMatch(/^<a[^>]*href="\/tickets"/);
        expect(html).toContain("button--outline");
        expect(html).not.toContain("<button");
    });

    it("Ranura encadena manejadores (primero el del hijo) y respeta preventDefault", () => {
        const llamadas: string[] = [];
        const unidas = unirProps(
            { onClick: () => llamadas.push("ranura"), className: "a" },
            { onClick: (e: { defaultPrevented: boolean }) => llamadas.push(e.defaultPrevented ? "hijo-previene" : "hijo"), className: "b" },
        );
        (unidas.onClick as (e: { defaultPrevented: boolean }) => void)({ defaultPrevented: false });
        (unidas.onClick as (e: { defaultPrevented: boolean }) => void)({ defaultPrevented: true });
        expect(llamadas).toEqual(["hijo", "ranura", "hijo-previene"]);
        expect(unidas.className).toBe("a b");
    });

    it("Campo usa el Input de HeroUI y conserva etiqueta, ayuda y error enlazados", () => {
        const html = renderToStaticMarkup(<Campo id="c" etiqueta="Correo" error="Escribe un correo" ayuda="El de trabajo" />);
        expect(html).toMatch(/<label[^>]*for="c"/);
        expect(html).toMatch(/<input[^>]*class="[^"]*\binput\b/);
        expect(html).toMatch(/<input[^>]*aria-invalid="true"/);
        expect(html).toMatch(/<input[^>]*aria-describedby="c-ayuda c-error"/);
        expect(html).toMatch(/<input[^>]*data-slot="entrada"/);
    });

    it("Selector sigue nativo, con el estilo de campo de HeroUI", () => {
        const html = renderToStaticMarkup(
            <Selector aria-label="Estado">
                <option>Todos</option>
            </Selector>,
        );
        expect(html).toMatch(/<select[^>]*class="[^"]*\binput\b/);
        expect(html).toContain("<option>Todos</option>");
    });

    it("Tarjeta, Tabla, InsigniaEstado, Aviso, Esqueleto y EstadoVacio llevan las piezas de HeroUI", () => {
        expect(renderToStaticMarkup(<Tarjeta>x</Tarjeta>)).toMatch(/class="[^"]*\bcard\b[^"]*surface-card/);
        expect(renderToStaticMarkup(<TarjetaTitulo>T</TarjetaTitulo>)).toMatch(/^<h2[^>]*card__title/);
        const tabla = renderToStaticMarkup(
            <Tabla aria-label="Órdenes">
                <TablaCuerpo>
                    <TablaFila>
                        <TablaCelda>1</TablaCelda>
                    </TablaFila>
                </TablaCuerpo>
            </Tabla>,
        );
        expect(tabla).toMatch(/<table[^>]*aria-label="Órdenes"/);
        expect(tabla).toContain("table__row");
        expect(tabla).toContain("table__cell");
        const insignia = renderToStaticMarkup(<InsigniaEstado tono="error" texto="Vencido" icono={CircleCheck} />);
        expect(insignia).toContain("chip--danger");
        expect(insignia).toContain('data-slot="insignia-estado"');
        const info = renderToStaticMarkup(<InsigniaEstado tono="info" texto="Comercial" />);
        expect(info).toContain("chip--default");
        expect(info).toContain("text-info");
        const aviso = renderToStaticMarkup(<Aviso tono="warning" titulo="Datos viejos" />);
        expect(aviso).toContain("alert--warning");
        expect(aviso).toContain('role="status"');
        expect(renderToStaticMarkup(<Esqueleto />)).toContain("skeleton--pulse");
        expect(renderToStaticMarkup(<EstadoVacio icono={CircleCheck} titulo="Nada pendiente" />)).toContain("empty-state");
        expect(renderToStaticMarkup(<Cargando />)).toMatch(/role="status"[\s\S]*spinner/);
    });
});
