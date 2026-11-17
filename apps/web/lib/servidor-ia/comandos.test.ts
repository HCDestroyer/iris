import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { COMANDO, lineaEstado } from "./presentacion";

/*
 * Los comandos que el panel deja copiar tienen que existir tal cual en el instalador del servidor
 * (`infra/voz/install-voz.sh`): esta prueba lo lee y falla si alguno de `COMANDO` no está entre
 * los que acepta, para que panel e instalador no se vuelvan a separar.
 */

const instalador = readFileSync(path.resolve(process.cwd(), "../../infra/voz/install-voz.sh"), "utf8");

/** Las etiquetas (`nombre)`) de todos los `case … in … esac` que empiezan con `inicio` (hasta el `esac` de su misma sangría). */
function ordenes(inicio: RegExp): Set<string> {
    const lineas = instalador.split("\n");
    const nombres = new Set<string>();
    lineas.forEach((linea, desde) => {
        if (!inicio.test(linea)) return;
        const sangria = /^\s*/.exec(linea)?.[0] ?? "";
        const hasta = lineas.findIndex((l, i) => i > desde && l === `${sangria}esac`);
        const etiqueta = new RegExp(`^${sangria}    ([a-z][a-z-]*)\\)`);
        for (const l of lineas.slice(desde + 1, hasta)) {
            const m = etiqueta.exec(l);
            if (m?.[1]) nombres.add(m[1]);
        }
    });
    return nombres;
}

describe("los comandos del panel existen en el instalador de iris-voz", () => {
    const principales = ordenes(/^\s*case "\$CMD" in$/);
    const deModelo = ordenes(/^\s*case "\$sub" in$/);

    it("el instalador se pudo leer y trae sus órdenes", () => {
        expect(principales.size).toBeGreaterThan(5);
        expect(deModelo.size).toBeGreaterThan(2);
        expect(principales.has("model")).toBe(true);
    });

    it.each(Object.entries(COMANDO))("%s: «%s» es una orden que el instalador acepta", (_nombre, comando) => {
        const [sudo, binario, orden, sub] = comando.split(" ");
        expect([sudo, binario]).toEqual(["sudo", "iris-voz"]);
        expect(principales, `«${orden}» no está entre las órdenes de install-voz.sh`).toContain(orden);
        if (orden === "model") expect(deModelo, `«model ${sub}» no está entre las de install-voz.sh`).toContain(sub);
    });

    it("una orden en español no pasaría", () => {
        for (const vieja of ["enlazar", "desenlazar", "estado", "actualizar", "modelo"]) expect(principales.has(vieja)).toBe(false);
        for (const vieja of ["descargar", "permitir", "quitar"]) expect(deModelo.has(vieja)).toBe(false);
    });
});

describe("lo que llega nulo al admin de una empresa", () => {
    it("sin `encendidoSeg` la línea de estado no inventa cuánto lleva encendido", () => {
        const linea = lineaEstado(
            { motorId: null, estado: "listo", detalle: null, version: "0.1.0", compatible: true, motivoIncompatible: null, encendidoSeg: null, tomadoEn: "2026-11-17T17:01:56Z", lecturaOk: true, modelosDisponibles: [] },
            Date.parse("2026-11-17T17:02:00Z"),
        );
        expect(linea).toEqual({ tono: "ok", titulo: "En línea", detalle: "versión 0.1.0 · compatible con esta versión de Iris" });
        expect(linea.detalle).not.toMatch(/encendido|null|NaN/);
    });
});
