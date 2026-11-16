import { describe, expect, it } from "vitest";
import { ErrorApi } from "../api/errores";
import { explicarErrorAbrir, urlArgosSegura } from "../integraciones/abrir-argos";
import { esquemaAprobar, esquemaRechazar, estadoBoleta, explicarErrorBoleta, montoLegible, quienDecidio } from "./boletas";

const error = (codigo: string, estado: number, detalle?: string) => ({ estado, codigo, titulo: "Título", detalle });

describe("boletas: aprobar y rechazar (B3)", () => {
    it("sin vínculo con Argos: ofrece vincular", () => {
        const e = explicarErrorBoleta(error("argos_sin_vinculo", 409), "aprobar");
        expect(e.tipo).toBe("vincular");
        expect(e.titulo).toBe("Vincula tu usuario de Argos para aprobar pagos");
    });

    it("permiso, empresa o usuario inactivo en Argos: se explican y no se reintenta", () => {
        for (const codigo of ["argos_sin_permiso", "argos_sin_empresa", "argos_usuario_inactivo", "solo_personas"]) {
            expect(explicarErrorBoleta(error(codigo, 403), "rechazar").tipo).toBe("explicar");
        }
        expect(explicarErrorBoleta(error("argos_sin_permiso", 403), "rechazar").texto).toContain("rechazar comprobantes");
        expect(explicarErrorBoleta(error("argos_sin_permiso", 403), "aprobar").texto).toContain("aprobar comprobantes");
    });

    it("ya la decidió otra persona o caja: se vuelve a leer", () => {
        for (const codigo of ["otra_persona_decidio", "comprobante_revisado", "boleta_ya_decidida"]) {
            expect(explicarErrorBoleta(error(codigo, 409), "aprobar").tipo).toBe("releer");
        }
    });

    it("Argos no respondió o no hay red: «Reintentar»", () => {
        expect(explicarErrorBoleta(error("argos_no_disponible", 502), "aprobar")).toMatchObject({ tipo: "reintentar", titulo: "Argos no respondió" });
        expect(explicarErrorBoleta(error("idempotencia_en_curso", 409), "aprobar").tipo).toBe("reintentar");
        expect(explicarErrorBoleta(error("sin_conexion", 0), "rechazar")).toMatchObject({ tipo: "reintentar", titulo: "No se pudo rechazar la boleta" });
        expect(explicarErrorBoleta(error("algo_nuevo", 500), "aprobar").tipo).toBe("reintentar");
    });

    it("Argos sin el contrato 0.7.0: pide actualizar, sin reintentar", () => {
        expect(explicarErrorBoleta(error("argos_sin_aprobacion", 422), "aprobar")).toMatchObject({ tipo: "explicar", titulo: "Actualiza Argos a 1.15.0 o posterior" });
    });

    it("un código que el panel no conoce muestra el detalle del servidor y no ofrece reintentar un 4xx", () => {
        expect(explicarErrorBoleta(error("regla_nueva", 422, "No se puede por la regla nueva"), "aprobar")).toEqual({ tipo: "explicar", titulo: "No se pudo aprobar la boleta", texto: "No se puede por la regla nueva" });
    });

    it("esquemas espejo: nota hasta 400; motivo de 3 a 500 y causa del catálogo", () => {
        expect(esquemaAprobar.safeParse({ nota: "" }).success).toBe(true);
        expect(esquemaAprobar.safeParse({ nota: "x".repeat(401) }).success).toBe(false);
        expect(esquemaRechazar.safeParse({ motivo: "no", causa: "otro" }).success).toBe(false);
        expect(esquemaRechazar.safeParse({ motivo: "  ab  ", causa: "otro" }).success).toBe(false);
        expect(esquemaRechazar.safeParse({ motivo: "No aparece el depósito", causa: "no_aparece" }).success).toBe(true);
        expect(esquemaRechazar.safeParse({ motivo: "x".repeat(501), causa: "otro" }).success).toBe(false);
        expect(esquemaRechazar.safeParse({ motivo: "No aparece", causa: "porque_si" }).success).toBe(false);
    });

    it("monto, estado y quién decidió", () => {
        expect(montoLegible("250.00")).toBe("Q 250.00");
        expect(montoLegible("1250.5")).toBe("Q 1,250.50");
        expect(montoLegible("no-es-numero")).toBe("Q no-es-numero");
        expect(estadoBoleta({ estado: "registrada" })).toEqual({ texto: "Pendiente de revisar", tono: "warning" });
        expect(estadoBoleta({ estado: "aprobada" }).tono).toBe("success");
        expect(estadoBoleta({ estado: "fallida" }).texto).toBe("Argos no la aceptó");
        expect(quienDecidio({ estado: "registrada", verificadoPorNombre: null })).toBeNull();
        expect(quienDecidio({ estado: "aprobada", verificadoPorNombre: "Marta Caal" })).toBe("La aprobó Marta Caal");
        expect(quienDecidio({ estado: "rechazada", verificadoPorNombre: null, decididaPor: { id: 1, nombre: "Ana López" } })).toBe("La rechazó Ana López");
        expect(quienDecidio({ estado: "aprobada", verificadoPorNombre: null })).toBe("La aprobó caja en Argos");
    });
});

describe("abrir Argos con autologin", () => {
    it("solo abre enlaces http(s) sin usuario ni contraseña", () => {
        expect(urlArgosSegura("https://argos.isp.gt/sso/entrar#abc")).toBe("https://argos.isp.gt/sso/entrar#abc");
        expect(urlArgosSegura("http://10.0.0.5:8080/sso/entrar#abc")).toBe("http://10.0.0.5:8080/sso/entrar#abc");
        expect(urlArgosSegura("javascript:alert(1)")).toBeNull();
        expect(urlArgosSegura("data:text/html,hola")).toBeNull();
        expect(urlArgosSegura("https://ana:clave@argos.isp.gt/")).toBeNull();
        expect(urlArgosSegura("/sso/entrar")).toBeNull();
        expect(urlArgosSegura("")).toBeNull();
    });

    it("explica por qué no abrió y si vale la pena reintentar", () => {
        const api = (codigo: string, estado: number, reintentarEn?: number) => new ErrorApi({ estado, codigo, titulo: "Título", reintentarEn });
        expect(explicarErrorAbrir(api("conector_apagado", 409))).toMatchObject({ reintentar: false });
        expect(explicarErrorAbrir(api("argos_sin_aprobacion", 422)).texto).toContain("Actualiza Argos a 1.15.0");
        expect(explicarErrorAbrir(api("demasiados_intentos", 429, 40))).toEqual({ texto: "Demasiados intentos. Vuelve a probar en 40 s.", reintentar: true });
        expect(explicarErrorAbrir(api("argos_no_disponible", 502))).toMatchObject({ reintentar: true });
        expect(explicarErrorAbrir(new Error("x"))).toEqual({ texto: "No se pudo abrir Argos.", reintentar: true });
    });
});
