import { describe, expect, it } from "vitest";
import { enlaceTexto, tituloTextosConCuentas } from "@/componentes/administracion/textos-con-cuentas";
import { textoErrorCuentas } from "@/componentes/conversaciones/enviar-cuentas";
import { ErrorApi } from "@/lib/api/errores";
import { aliasCuenta, textoSinTitular, titularCuenta, tituloCuenta } from "./cuentas";

describe("cuentas bancarias de Argos (contrato 0.8.0 §7.8)", () => {
    it("título con banco y tipo; sin tipo, solo el banco", () => {
        expect(tituloCuenta({ banco: "Banco Industrial", tipo: "monetaria" })).toBe("Banco Industrial · Monetaria");
        expect(tituloCuenta({ banco: "Banrural", tipo: "ahorro" })).toBe("Banrural · Ahorro");
        expect(tituloCuenta({ banco: "BAM", tipo: null })).toBe("BAM");
    });

    it("el alias solo si no repite el banco", () => {
        expect(aliasCuenta({ alias: "Principal", banco: "Banco Industrial" })).toBe("Principal");
        expect(aliasCuenta({ alias: " banrural ", banco: "Banrural" })).toBeNull();
        expect(aliasCuenta({ alias: "", banco: "BAM" })).toBeNull();
    });

    it("el titular o el aviso de que falta en Argos", () => {
        expect(titularCuenta({ titular: "Internet del Norte, S.A." })).toBe("A nombre de Internet del Norte, S.A.");
        expect(titularCuenta({ titular: null })).toBe("Sin titular en Argos");
        expect(textoSinTitular(1)).toBe("1 cuenta no va porque no tiene titular en Argos.");
        expect(textoSinTitular(2)).toBe("2 cuentas no van porque no tienen titular en Argos.");
    });

    it("errores de «Enviar cuentas» en llano", () => {
        const e = (codigo: string, estado = 422) => new ErrorApi({ estado, codigo, titulo: "x" });
        expect(textoErrorCuentas(e("sin_cuentas_para_enviar"))).toContain("falta el titular");
        expect(textoErrorCuentas(e("fuera_de_ventana"))).toContain("24 h");
        expect(textoErrorCuentas(e("argos_no_disponible", 503))).toContain("Argos no responde");
    });

    it("aviso al admin de textos con números de cuenta", () => {
        expect(tituloTextosConCuentas(1)).toBe("1 texto parece tener un número de cuenta escrito a mano");
        expect(tituloTextosConCuentas(3)).toBe("3 textos parecen tener números de cuenta escritos a mano");
        expect(enlaceTexto({ tipo: "manual", id: 4 })).toBe("/administracion/manuales/4/editar");
        expect(enlaceTexto({ tipo: "respuesta_rapida", id: 7 })).toBe("/administracion/respuestas-rapidas/7/editar");
        expect(enlaceTexto({ tipo: "fuera_de_horario", id: 2 })).toBe("/administracion/numeros/2/configurar");
    });
});
