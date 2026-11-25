import { describe, expect, it } from "vitest";
import { avisoError, clasificarMarcado, motivoFin, numeroLegible, puntoTelefono, reloj, textoErrorLlamar, textoMotivo } from "./presentacion";

/** Un error de la API con la forma del `ErrorApi` del panel (el motor lo distingue por su forma). */
class ErrorApiPrueba extends Error {
    readonly estado: number;
    readonly codigo: string;
    readonly titulo: string;
    readonly detalle: string | undefined;
    readonly reintentarEn: number | undefined;
    constructor(d: { estado: number; codigo: string; titulo: string; detalle?: string; reintentarEn?: number | undefined }) {
        super(d.titulo);
        this.estado = d.estado;
        this.codigo = d.codigo;
        this.titulo = d.titulo;
        this.detalle = d.detalle;
        this.reintentarEn = d.reintentarEn;
    }
}

describe("teclado: lo mismo que acepta el servidor", () => {
    it("Guatemala: 8 dígitos que empiezan por 2 a 7, con o sin +502", () => {
        expect(clasificarMarcado("5512 3480")).toEqual({ tipo: "externo", nacional: "55123480" });
        expect(clasificarMarcado("+502 2433-1020")).toEqual({ tipo: "externo", nacional: "24331020" });
        expect(clasificarMarcado("50277001122")).toEqual({ tipo: "externo", nacional: "77001122" });
    });
    it("extensiones y grupos: 2 a 6 cifras", () => {
        expect(clasificarMarcado("205")).toEqual({ tipo: "interno", numero: "205" });
        expect(clasificarMarcado("600")).toEqual({ tipo: "interno", numero: "600" });
        expect(clasificarMarcado("123456")).toEqual({ tipo: "interno", numero: "123456" });
    });
    it("lo demás no", () => {
        expect(clasificarMarcado("")).toEqual({ tipo: "incompleto" });
        expect(clasificarMarcado("1")).toEqual({ tipo: "incompleto" });
        expect(clasificarMarcado("5512348")).toEqual({ tipo: "incompleto" });
        expect(clasificarMarcado("+1 555 123 4567").tipo).toBe("invalido");
        expect(clasificarMarcado("0019005551234").tipo).toBe("invalido");
        expect(clasificarMarcado("85123480").tipo).toBe("invalido");
        expect(clasificarMarcado("1234abcd").tipo).toBe("invalido");
        expect(clasificarMarcado("+205").tipo).not.toBe("interno");
    });
    it("se muestran legibles", () => {
        expect(numeroLegible("+50255123480")).toBe("+502 5512 3480");
        expect(numeroLegible("55123480")).toBe("+502 5512 3480");
        expect(numeroLegible("205")).toBe("205");
        expect(reloj(252)).toBe("04:12");
        expect(reloj(3723)).toBe("1:02:03");
    });
});

describe("el punto del teléfono y los avisos", () => {
    it("un punto si todo va bien; un aviso si hay que actuar", () => {
        expect(puntoTelefono({ fase: "listo" })).toMatchObject({ forma: "punto", tono: "success" });
        expect(puntoTelefono({ fase: "apagado" })).toMatchObject({ forma: "punto", tono: "neutral" });
        expect(puntoTelefono({ fase: "no_disponible", motivo: "sin_extension" })).toBeNull();
        expect(puntoTelefono({ fase: "error", error: { tipo: "sin_conexion", desde: 0 } })).toMatchObject({ forma: "aviso", tono: "danger", corto: "Sin conexión" });
        expect(puntoTelefono({ fase: "error", error: { tipo: "pestana" } })).toMatchObject({ forma: "aviso", tono: "warning", corto: "En otra pestaña" });
    });
    it("cada error dice qué hacer, en llano", () => {
        const sin = avisoError({ tipo: "sin_conexion", desde: Date.parse("2026-11-21T16:42:00Z") }, "204");
        expect(sin.titulo).toBe("No hay conexión con la centralita");
        expect(sin.detalle).toContain("Desde las 10:42");
        expect(sin.detalle).toContain("extensión 204");
        expect(sin.accion).toBe("reintentar");
        expect(avisoError({ tipo: "clave" }, "204").detalle).toContain("Administración › Centralita");
        expect(avisoError({ tipo: "microfono", estado: "denegado" }, null).pasos).toHaveLength(3);
        expect(avisoError({ tipo: "pestana" }, null).accion).toBe("usar_aqui");
        expect(textoMotivo("extension_escritorio")).toContain("teléfono de escritorio");
    });
    it("los errores de llamar", () => {
        const e = (codigo: string, estado = 422) => new ErrorApiPrueba({ estado, codigo, titulo: "x" });
        expect(textoErrorLlamar(e("destino_no_permitido")).titulo).toBe("Ese número no se puede marcar");
        expect(textoErrorLlamar(e("limite_llamadas")).titulo).toContain("límite");
        expect(textoErrorLlamar(e("llamadas_simultaneas", 409)).titulo).toContain("muchas llamadas");
        expect(textoErrorLlamar(new Error("red")).titulo).toBe("No se pudo llamar");
    });
    it("por qué terminó", () => {
        expect(motivoFin("Terminated", "saliente")).toBeNull();
        expect(motivoFin("Busy", "saliente")).toBe("Ocupado");
        expect(motivoFin("No Answer", "saliente")).toBe("No contestó");
        expect(motivoFin("No Answer", "entrante")).toBeNull();
        expect(motivoFin("User Denied Media Access", "entrante")).toContain("micrófono");
    });
});
