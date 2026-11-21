// Datos del titular en la cola local (contrato argosisp.md 0.10.0 §3.4.8): en IndexedDB solo cifrados.
import Dexie from "dexie";
import { IDBKeyRange, indexedDB } from "fake-indexeddb";
import { describe, expect, it } from "vitest";
import { AlmacenDexie, TIPOS_CIFRADOS } from "./almacen-dexie";
import type { OperacionLocal } from "./modelo";

const DPI = "1234567890101";
const FOTO = "SEFZIFVOQSBGT1RPIERFTCBET0NVTUVOVE8gREUgUFJVRUJB";

let n = 0;

function identidad(id: string, secuencia: number): OperacionLocal {
    return {
        id,
        tipo: "orden.registrar_identidad",
        ordenId: 7,
        versionBase: 1,
        ocurridoEn: "2026-11-21T15:00:00.000Z",
        datos: {
            documentoTipo: "dpi",
            documentoNumero: DPI,
            nombre: "Ana Pérez",
            mayorDeEdad: true,
            coincide: true,
            frente: { mime: "image/jpeg", base64: FOTO },
        },
        secuencia,
        estado: "pendiente",
        error: null,
        intentos: 0,
    } as OperacionLocal;
}

/** Todo el texto de un valor de IndexedDB, también el de los bytes (para buscar algo en claro). */
function textos(v: unknown, salida: string[] = []): string[] {
    if (typeof v === "string") {
        salida.push(v);
    } else if (v instanceof ArrayBuffer || ArrayBuffer.isView(v)) {
        const b = v instanceof ArrayBuffer ? new Uint8Array(v) : new Uint8Array(v.buffer, v.byteOffset, v.byteLength);
        salida.push(new TextDecoder().decode(b));
    } else if (v && typeof v === "object") {
        for (const h of Object.values(v)) {
            textos(h, salida);
        }
    }
    return salida;
}

async function filasCrudas(nombre: string): Promise<{ operaciones: unknown[]; llaves: { llave: CryptoKey }[] }> {
    const db = new Dexie(nombre, { indexedDB, IDBKeyRange });
    await db.open();
    const operaciones = await db.table("operaciones").toArray();
    const llaves = await db.table("llaves").toArray();
    db.close();
    return { operaciones, llaves };
}

describe("AlmacenDexie: datos sensibles cifrados", () => {
    it("las cuatro operaciones del titular son las cifradas", () => {
        expect([...TIPOS_CIFRADOS].sort()).toEqual(
            ["orden.cambiar_plan", "orden.cambiar_titular", "orden.completar_datos", "orden.registrar_identidad"].sort(),
        );
    });

    it("la fila de Dexie no tiene el documento ni la foto en claro, y se leen igual", async () => {
        const nombre = `iris-campo-cifrado-${++n}`;
        const almacen = new AlmacenDexie({ nombre, dexie: { indexedDB, IDBKeyRange } });
        const nota: OperacionLocal = { ...identidad("0190f000-0000-7000-8000-0000000000a2", 2), tipo: "nota.agregar", datos: { texto: "hola" } } as OperacionLocal;
        await almacen.escribir({ operaciones: { poner: [identidad("0190f000-0000-7000-8000-0000000000a1", 1), nota] } });

        const crudo = await filasCrudas(nombre);
        const todo = textos(crudo.operaciones).join("\n");
        expect(todo).not.toContain(DPI);
        expect(todo).not.toContain(FOTO);
        expect(todo).not.toContain("documentoNumero");
        expect(todo).not.toContain("base64");
        expect(todo).toContain("hola"); // lo que no es sensible queda como siempre
        // La llave no se puede exportar.
        expect(crudo.llaves).toHaveLength(1);
        expect(crudo.llaves[0]?.llave.extractable).toBe(false);
        const [fila] = crudo.llaves;
        if (fila) {
            await expect(crypto.subtle.exportKey("raw", fila.llave)).rejects.toThrow();
        }

        const contenido = await almacen.cargar();
        const leida = contenido.operaciones.find((o) => o.tipo === "orden.registrar_identidad");
        expect(leida?.datos).toMatchObject({ documentoNumero: DPI, frente: { base64: FOTO } });
        expect(contenido.operaciones).toHaveLength(2);
        almacen.cerrar();

        // Tras reabrir la base (otra pestaña o reinicio), se siguen leyendo.
        const otra = new AlmacenDexie({ nombre, dexie: { indexedDB, IDBKeyRange } });
        expect((await otra.cargar()).operaciones.find((o) => o.tipo === "orden.registrar_identidad")?.datos).toMatchObject({ documentoNumero: DPI });
        await otra.borrar();
    });

    it("al quedar aplicada se borra, y al cerrar la sesión se olvidan con su llave", async () => {
        const nombre = `iris-campo-cifrado-${++n}`;
        const almacen = new AlmacenDexie({ nombre, dexie: { indexedDB, IDBKeyRange } });
        const a = identidad("0190f000-0000-7000-8000-0000000000b1", 1);
        const b = identidad("0190f000-0000-7000-8000-0000000000b2", 2);
        await almacen.escribir({ operaciones: { poner: [a, b] } });
        // Aplicada o duplicada: el motor la quita de la cola.
        await almacen.escribir({ operaciones: { borrar: [a.id] } });
        expect((await almacen.cargar()).operaciones.map((o) => o.id)).toEqual([b.id]);

        await almacen.olvidarSensibles();
        const crudo = await filasCrudas(nombre);
        expect(crudo.operaciones).toHaveLength(0);
        expect(crudo.llaves).toHaveLength(0);
        expect((await almacen.cargar()).operaciones).toHaveLength(0);
        await almacen.borrar();
    });
});
