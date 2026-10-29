import { describe, expect, it } from "vitest";
import { consultaEncuestas, errorFechasEncuestas, filtrosEncuestasDesdeUrl, urlEncuestas, FILTROS_ENCUESTAS_VACIOS } from "./filtros";
import { textoCalificacion, textoPromedio } from "./presentacion";
import { cuerpoRespuesta, esquemaRespuestaEncuesta, pasoPorProblema, tokenDesdeFragmento } from "./publica";

const TOKEN = "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789_-abcde";

describe("token del fragmento", () => {
    it("lee el token de #, decodificado y sin espacios", () => {
        expect(tokenDesdeFragmento(`#${TOKEN}`)).toEqual({ token: TOKEN });
        expect(tokenDesdeFragmento(` ${TOKEN} `)).toEqual({ token: TOKEN });
    });

    it("sin fragmento: null; corto, con caracteres raros o mal codificado: inválido", () => {
        expect(tokenDesdeFragmento("")).toBeNull();
        expect(tokenDesdeFragmento("#")).toBeNull();
        expect(tokenDesdeFragmento("#corto")).toEqual({ invalido: true });
        expect(tokenDesdeFragmento(`#${TOKEN}<script>`)).toEqual({ invalido: true });
        expect(tokenDesdeFragmento("#%E0%A4%A")).toEqual({ invalido: true });
        expect(tokenDesdeFragmento(`#${"a".repeat(201)}`)).toEqual({ invalido: true });
    });
});

describe("formulario de la encuesta (espejo de la API)", () => {
    it("exige calificación 1–5 y comentario ≤ 1000", () => {
        expect(esquemaRespuestaEncuesta.safeParse({ calificacion: undefined, comentario: "" }).success).toBe(false);
        expect(esquemaRespuestaEncuesta.safeParse({ calificacion: 6, comentario: "" }).success).toBe(false);
        expect(esquemaRespuestaEncuesta.safeParse({ calificacion: 3, comentario: "x".repeat(1001) }).success).toBe(false);
        expect(esquemaRespuestaEncuesta.safeParse({ calificacion: 5, comentario: "x".repeat(1000) }).success).toBe(true);
    });

    it("sin comentario no lo manda", () => {
        expect(cuerpoRespuesta({ calificacion: 4, comentario: "  " })).toEqual({ calificacion: 4 });
        expect(cuerpoRespuesta({ calificacion: 2, comentario: " Tarde " })).toEqual({ calificacion: 2, comentario: "Tarde" });
    });

    it("cada problema de la API lleva a su pantalla", () => {
        expect(pasoPorProblema(404, "encuesta_no_encontrada")).toBe("invalido");
        expect(pasoPorProblema(409, "encuesta_respondida")).toBe("ya_respondida");
        expect(pasoPorProblema(410, "encuesta_vencida")).toBe("vencida");
        expect(pasoPorProblema(410, "encuesta_anulada")).toBe("anulada");
        expect(pasoPorProblema(429, "demasiados_intentos")).toBe("limite");
        expect(pasoPorProblema(500, "error_interno")).toBe("error");
    });
});

describe("filtros de /encuestas", () => {
    it("ida y vuelta por la URL y a la consulta", () => {
        const f = { desde: "2026-10-01", hasta: "2026-10-28", calificaciones: [1, 2] as (1 | 2)[], entidad: "orden" as const, estado: "respondida" as const };
        const url = urlEncuestas(f);
        expect(url).toBe("/encuestas?desde=2026-10-01&hasta=2026-10-28&calificacion=1%2C2&entidad=orden&estado=respondida");
        expect(filtrosEncuestasDesdeUrl(new URLSearchParams(url.split("?")[1]))).toEqual(f);
        expect(consultaEncuestas(f)).toEqual({ limit: 25, desde: "2026-10-01", hasta: "2026-10-28", calificacion: [1, 2], entidad: "orden", estado: "respondida" });
    });

    it("lo inválido se ignora; fechas al revés avisan", () => {
        expect(filtrosEncuestasDesdeUrl(new URLSearchParams("desde=ayer&calificacion=9,3&entidad=x&estado=borrada"))).toEqual({ ...FILTROS_ENCUESTAS_VACIOS, calificaciones: [3] });
        expect(urlEncuestas(FILTROS_ENCUESTAS_VACIOS)).toBe("/encuestas");
        expect(errorFechasEncuestas({ desde: "2026-10-10", hasta: "2026-10-01" })).toMatch(/antes/);
    });
});

describe("presentación", () => {
    it("calificación con texto y promedio sin base como «Sin datos»", () => {
        expect(textoCalificacion(4)).toBe("4 · Bien");
        expect(textoCalificacion(null)).toBe("Sin calificar");
        expect(textoPromedio(4.05)).toBe("4.1 de 5");
        expect(textoPromedio(null)).toBe("Sin datos");
    });
});
