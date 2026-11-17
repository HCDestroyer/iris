import { describe, expect, it } from "vitest";
import { documentoDe } from "./contenido";
import { enlacesMeta, entradaLegal, esHttps, esquemaLegal, faltantesEnLlano, paginaResponde, valoresLegal, type DatosLegales } from "./datos";
import { CLAVES, IDIOMAS, RUTAS, RUTAS_PUBLICAS } from "./paginas";
import { TEXTO_REVISADO_EN, fechaISO, fechaLarga, titularDe, ultimaActualizacion } from "./titular";

const DATOS: DatosLegales = {
    configurado: true,
    razonSocial: "Redes del Sur, S. A.",
    nombreComercial: "RedSur",
    correoPrivacidad: "privacidad@redsur.example",
    telefono: "",
    direccion: "5a avenida 1-23",
    pais: "Guatemala",
    version: 3,
    actualizadoEn: "2026-11-17T18:00:00Z",
    completo: true,
    faltan: [],
};

describe("rutas públicas", () => {
    it("son seis, estables y sin repetir", () => {
        expect(RUTAS_PUBLICAS).toEqual(["/privacidad", "/en/privacy", "/terminos", "/en/terms", "/eliminacion-de-datos", "/en/data-deletion"]);
        expect(new Set(RUTAS_PUBLICAS).size).toBe(6);
    });
});

describe("titular", () => {
    it("usa el nombre comercial; la razón social solo si es distinta", () => {
        const t = titularDe({ nombreComercial: " RedSur ", razonSocial: "Redes del Sur, S. A." }, "es");
        expect(t).toMatchObject({ nombre: "RedSur", conNombre: true, razonSocial: "Redes del Sur, S. A.", correo: null });
        expect(titularDe({ nombreComercial: "RedSur", razonSocial: "RedSur" }, "es").razonSocial).toBeNull();
        expect(titularDe({ razonSocial: "Redes del Sur, S. A." }, "es")).toMatchObject({ nombre: "Redes del Sur, S. A.", razonSocial: null });
    });
    it("sin nombre: uno genérico en el idioma de la página", () => {
        expect(titularDe(null, "es")).toMatchObject({ nombre: "el proveedor de este servicio", conNombre: false });
        expect(titularDe({ nombreComercial: "  " }, "en")).toMatchObject({ nombre: "the provider of this service", conNombre: false });
    });
    it("la última actualización es lo más reciente entre el texto y los datos", () => {
        expect(ultimaActualizacion({ actualizadoEn: null }).toISOString()).toBe(new Date(TEXTO_REVISADO_EN).toISOString());
        expect(ultimaActualizacion({ actualizadoEn: "2020-01-01T00:00:00Z" }).toISOString()).toBe(new Date(TEXTO_REVISADO_EN).toISOString());
        expect(ultimaActualizacion({ actualizadoEn: "2027-03-02T10:00:00Z" }).toISOString()).toBe("2027-03-02T10:00:00.000Z");
        expect(ultimaActualizacion({ actualizadoEn: "no es fecha" }).toISOString()).toBe(new Date(TEXTO_REVISADO_EN).toISOString());
    });
    it("la fecha se escribe en la zona de Guatemala", () => {
        // 03:00 UTC del 2 de marzo todavía es 1 de marzo en Guatemala.
        expect(fechaLarga(new Date("2027-03-02T03:00:00Z"), "es")).toBe("1 de marzo de 2027");
        expect(fechaLarga(new Date("2027-03-02T03:00:00Z"), "en")).toBe("March 1, 2027");
        expect(fechaISO(new Date("2027-03-02T03:00:00Z"))).toBe("2027-03-01");
        expect(fechaISO(new Date(TEXTO_REVISADO_EN))).toBe(TEXTO_REVISADO_EN.slice(0, 10));
    });
});

describe("contenido", () => {
    it("los dos idiomas tienen las mismas secciones, en el mismo orden", () => {
        const t = titularDe(null, "es");
        for (const clave of CLAVES) {
            const [es, en] = IDIOMAS.map((i) => documentoDe(clave, i, t).secciones.map((s) => s.id));
            expect(en, clave).toEqual(es);
            expect(new Set(es).size, `${clave}: ids repetidos`).toBe(es?.length);
            expect(es).not.toContain("contacto");
        }
    });
    it("cada página cubre lo que Meta revisa", () => {
        const t = titularDe({ nombreComercial: "RedSur", correo: "privacidad@redsur.example", pais: "Guatemala" }, "en");
        const texto = (clave: (typeof CLAVES)[number]) => JSON.stringify(documentoDe(clave, "en", t));
        // Privacidad: qué datos, para qué, con quién (Meta), y cómo pedir que se borren.
        for (const frase of ["phone number", "WhatsApp Business Platform", "Meta Platforms", "artificial intelligence provider", "delete", "privacidad@redsur.example", "BAJA or ALTO"]) {
            expect(texto("privacidad"), frase).toContain(frase);
        }
        expect(texto("terminos")).toContain("the laws of Guatemala");
        expect(texto("terminos")).toContain("virtual assistant");
        for (const frase of ["write to privacidad@redsur.example", "What we delete", "What we must keep", "30 days"]) {
            expect(texto("eliminacion"), frase).toContain(frase);
        }
    });
    it("no trae datos de ningún titular escritos en el texto", () => {
        const t = titularDe(null, "es");
        for (const clave of CLAVES) {
            for (const idioma of IDIOMAS) {
                expect(JSON.stringify(documentoDe(clave, idioma, t))).not.toMatch(/kns|@[a-z0-9-]+\.(gt|com)\b/i);
            }
        }
    });
});

describe("formulario del panel", () => {
    it("acepta todo vacío (se guarda lo que haya) y recorta espacios", () => {
        const r = esquemaLegal.parse({ razonSocial: "  Redes del Sur  ", nombreComercial: "", correoPrivacidad: "", telefono: "", direccion: "", pais: "" });
        expect(r.razonSocial).toBe("Redes del Sur");
    });
    it("valida el correo, el teléfono y la línea única, como el servidor", () => {
        const base = valoresLegal(DATOS);
        const error = (cambio: Partial<typeof base>) => esquemaLegal.safeParse({ ...base, ...cambio }).error?.issues[0]?.path[0];
        expect(error({ correoPrivacidad: "a@b.example, c@d.example" })).toBe("correoPrivacidad");
        expect(error({ correoPrivacidad: "sin-arroba" })).toBe("correoPrivacidad");
        expect(error({ telefono: "llámame" })).toBe("telefono");
        expect(error({ telefono: "+502 2222-3333" })).toBeUndefined();
        expect(error({ direccion: "línea 1\nlínea 2" })).toBe("direccion");
        expect(error({ pais: "x".repeat(81) })).toBe("pais");
    });
    it("la entrada lleva la versión leída", () => {
        expect(entradaLegal(DATOS, valoresLegal(DATOS))).toEqual({ version: 3, razonSocial: "Redes del Sur, S. A.", nombreComercial: "RedSur", correoPrivacidad: "privacidad@redsur.example", telefono: "", direccion: "5a avenida 1-23", pais: "Guatemala" });
    });
    it("lo que falta se dice con las palabras del formulario", () => {
        expect(faltantesEnLlano(["razonSocial", "correoPrivacidad", "pais"])).toEqual(["La razón social", "El correo de privacidad", "El país"]);
        expect(faltantesEnLlano(["algoNuevo"])).toEqual(["algoNuevo"]);
        expect(faltantesEnLlano(null)).toEqual([]);
    });
});

describe("enlaces para Meta", () => {
    it("arma las seis URL con el dominio del panel", () => {
        const enlaces = enlacesMeta("https://iris.isp.example/");
        expect(enlaces.map((e) => e.url)).toEqual(RUTAS_PUBLICAS.map((r) => `https://iris.isp.example${r}`));
        expect(enlaces.find((e) => e.clave === "eliminacion" && e.idioma === "en")).toMatchObject({ titulo: "Data Deletion", ruta: RUTAS.eliminacion.en });
    });
    it("avisa si no es https", () => {
        expect(esHttps("https://iris.isp.example")).toBe(true);
        expect(esHttps("http://localhost:3000")).toBe(false);
    });
    it("una página responde bien solo con 200 y su marca", () => {
        expect(paginaResponde(200, '<div data-pagina-legal="privacidad">', "privacidad")).toBe(true);
        // El inicio de sesión (200 sin la marca), otra página o un error no cuentan.
        expect(paginaResponde(200, "<h1>Iniciar sesión</h1>", "privacidad")).toBe(false);
        expect(paginaResponde(200, '<div data-pagina-legal="terminos">', "privacidad")).toBe(false);
        expect(paginaResponde(502, '<div data-pagina-legal="privacidad">', "privacidad")).toBe(false);
    });
});
