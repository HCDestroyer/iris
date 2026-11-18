import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";
import { CLAVES, IDIOMAS, RUTAS, TITULOS } from "@/lib/legal/paginas";
import { titularDe } from "@/lib/legal/titular";
import { PaginaLegal } from "./pagina-legal";

// El enlace de Next y el selector de tema no cambian el texto: aquí se prueba el HTML del servidor.
vi.mock("next/link", () => ({ default: ({ href, children, ...resto }: { href: string; children: unknown }) => <a href={href} {...resto}>{children as never}</a> }));
vi.mock("@/componentes/selector-tema", () => ({ SelectorTema: () => null }));

const COMPLETO = {
    nombreComercial: "RedSur",
    razonSocial: "Redes del Sur, Sociedad Anónima",
    correo: "privacidad@redsur.example",
    telefono: "+502 2222 3333",
    direccion: "5a avenida 1-23, zona 1",
    pais: "Guatemala",
    actualizadoEn: "2026-12-24T18:00:00Z",
    completo: true,
};

const html = (clave: (typeof CLAVES)[number], idioma: (typeof IDIOMAS)[number], datos: Parameters<typeof titularDe>[0]) =>
    renderToStaticMarkup(<PaginaLegal clave={clave} idioma={idioma} titular={titularDe(datos, idioma)} />);

describe("PaginaLegal (HTML del servidor)", () => {
    it("con datos: el texto está en el HTML y nombra al titular, su contacto y la fecha", () => {
        const pagina = html("privacidad", "es", COMPLETO);
        expect(pagina).toContain('data-pagina-legal="privacidad"');
        expect(pagina).toContain("<h1");
        expect(pagina).toContain("Política de privacidad");
        expect(pagina).toContain("Redes del Sur, Sociedad Anónima («RedSur»)");
        expect(pagina).toContain('href="mailto:privacidad@redsur.example"');
        expect(pagina).toContain('href="tel:+50222223333"');
        expect(pagina).toContain("5a avenida 1-23, zona 1, Guatemala");
        // La fecha es la del último cambio de los datos (más reciente que la del texto).
        expect(pagina).toContain('dateTime="2026-12-24"');
        expect(pagina).toContain("24 de diciembre de 2026");
        expect(pagina).toContain("responde BAJA o ALTO");
    });

    it("en inglés: el contenido va marcado como inglés y enlaza a la versión en español", () => {
        const pagina = html("eliminacion", "en", COMPLETO);
        expect(pagina).toContain('lang="en"');
        expect(pagina).toContain("Data Deletion");
        expect(pagina).toContain(`href="${RUTAS.eliminacion.es}"`);
        expect(pagina).toContain("write to privacidad@redsur.example");
        expect(pagina).toContain("December 24, 2026");
    });

    it("las seis páginas enlazan a su otro idioma y a las otras dos del mismo idioma, con su índice", () => {
        for (const clave of CLAVES) {
            for (const idioma of IDIOMAS) {
                const pagina = html(clave, idioma, COMPLETO);
                const otro = idioma === "es" ? "en" : "es";
                expect(pagina, `${clave} ${idioma}`).toContain(`href="${RUTAS[clave][otro]}"`);
                for (const c of CLAVES) {
                    expect(pagina).toContain(`href="${RUTAS[c][idioma]}"`);
                    expect(pagina).toContain(TITULOS[c][idioma]);
                }
                // Cada entrada del índice apunta a una sección que existe.
                const anclas = [...pagina.matchAll(/href="#([a-z-]+)"/g)].map((m) => m[1]);
                expect(anclas.length).toBeGreaterThan(5);
                for (const ancla of anclas) {
                    expect(pagina, `${clave} ${idioma} #${ancla}`).toContain(`id="${ancla}"`);
                }
            }
        }
    });

    it("sin datos (o si la API no respondió): la página sale completa, sin huecos ni «undefined»", () => {
        for (const datos of [null, { nombreComercial: "", razonSocial: "", correo: "", telefono: "", direccion: "", pais: "", actualizadoEn: null, completo: false }]) {
            for (const clave of CLAVES) {
                for (const idioma of IDIOMAS) {
                    const pagina = html(clave, idioma, datos);
                    expect(pagina).toContain(`data-pagina-legal="${clave}"`);
                    expect(pagina).not.toMatch(/undefined|null|NaN|\[object/);
                    // Ni comillas, paréntesis o «de» colgando por un dato vacío.
                    expect(pagina).not.toMatch(/«»|\(\)|""|mailto:"|: <\/|\s,\s/);
                    expect(pagina).not.toContain("mailto:");
                    expect(pagina.toLowerCase()).toContain(idioma === "es" ? "el proveedor de este servicio" : "the provider of this service");
                    // La fecha es la del texto, y el atributo dice el mismo día que se lee.
                    expect(pagina).toContain('dateTime="2026-11-18"');
                    expect(pagina).toContain(idioma === "es" ? "18 de noviembre de 2026" : "November 18, 2026");
                    expect(pagina).toContain(idioma === "es" ? "escríbenos por el mismo canal" : "write to us through the same channel");
                }
            }
        }
    });

    it("un correo con caracteres de consulta (si llegara de la API) no agrega campos al enlace", () => {
        const pagina = html("privacidad", "es", { ...COMPLETO, correo: "ayuda?cc=x&body=texto@empresa.com" });
        const href = /href="(mailto:[^"]*)"/.exec(pagina)?.[1] ?? "";
        expect(href).toBe("mailto:ayuda%3Fcc%3Dx%26body%3Dtexto@empresa.com");
        expect(href).not.toMatch(/[?&=]/);
    });

    it("con solo el nombre comercial: lo usa y no inventa lo demás", () => {
        const pagina = html("terminos", "es", { nombreComercial: "RedSur" });
        expect(pagina).toContain("canales de atención de RedSur");
        expect(pagina).toContain("las leyes del país donde prestamos el servicio");
        expect(pagina).not.toContain("mailto:");
        expect(pagina).not.toMatch(/undefined|null/);
    });
});
