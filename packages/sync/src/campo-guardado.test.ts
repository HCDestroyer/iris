// Un solo «Guardar» en los pasos (registro de borradores y su aviso) y la orden en pausa de solo
// lectura (reglas que comparten la PWA y la app nativa).
import { describe, expect, it, vi } from "vitest";
import { accionesDe, accionesRapidas, avisoGuardado, RegistroBorradores, type Borrador } from "./campo";
import { ordenDePrueba } from "./pruebas/fabricas";

function borrador(etiqueta: string, inicial: string, validar: (v: string) => true | string = () => true) {
    const estado = { escrito: inicial, guardado: "", guardadas: 0 };
    const b: Borrador = {
        etiqueta,
        pendiente: () => estado.escrito !== estado.guardado,
        guardar: async () => {
            const r = validar(estado.escrito);
            if (r === true) {
                estado.guardado = estado.escrito;
                estado.guardadas += 1;
            }
            return r;
        },
    };
    return { b, estado };
}

describe("RegistroBorradores", () => {
    it("guarda solo lo pendiente, una vez, y cuenta lo que queda escrito", async () => {
        const r = new RegistroBorradores();
        const oyente = vi.fn();
        r.suscribir(oyente);
        const potencia = borrador("Potencia", "-19.5");
        const nota = borrador("Observación", "");
        r.registrar("potencia", potencia.b);
        r.registrar("nota", nota.b);
        expect(r.pendientes()).toBe(1);
        expect(oyente).toHaveBeenCalledTimes(1);

        expect(await r.guardarTodo()).toEqual({ guardados: 1, errores: [] });
        expect(potencia.estado.guardadas).toBe(1);
        expect(nota.estado.guardadas).toBe(0);
        expect(r.pendientes()).toBe(0);
        // Sin cambios: nada que guardar.
        expect(await r.guardarTodo()).toEqual({ guardados: 0, errores: [] });
        expect(potencia.estado.guardadas).toBe(1);
    });

    it("lo que no valida queda escrito con su error; un error lanzado no corta a los demás", async () => {
        const r = new RegistroBorradores();
        const numero = borrador("Bajada (Mbps)", "abc", (v) => (Number.isFinite(Number(v)) ? true : "Escribe un número"));
        const roto: Borrador = { etiqueta: "Roto", pendiente: () => true, guardar: () => Promise.reject(new Error("Disco lleno")) };
        const bien = borrador("Serie", "ZTE-1");
        r.registrar("numero", numero.b);
        r.registrar("roto", roto);
        r.registrar("bien", bien.b);
        const res = await r.guardarTodo();
        expect(res.guardados).toBe(1);
        expect(res.errores).toEqual([
            { etiqueta: "Bajada (Mbps)", mensaje: "Escribe un número" },
            { etiqueta: "Roto", mensaje: "Disco lleno" },
        ]);
        expect(r.pendientes()).toBe(2);
    });

    it("al desmontarse un campo se quita del registro (y no quita a otro con la misma clave)", () => {
        const r = new RegistroBorradores();
        const a = borrador("A", "x");
        const b = borrador("B", "y");
        const quitarA = r.registrar("k", a.b);
        r.registrar("k", b.b);
        quitarA();
        expect(r.pendientes()).toBe(1);
    });
});

describe("avisoGuardado", () => {
    it("dice lo guardado, el error del campo o que todo estaba guardado", () => {
        expect(avisoGuardado({ guardados: 2, errores: [] }, { porEnviar: 3, enLinea: false })).toEqual({
            tono: "success",
            texto: "2 cambios guardados en el teléfono. Se enviarán al volver la señal.",
        });
        expect(avisoGuardado({ guardados: 1, errores: [] }, { porEnviar: 1, enLinea: true }).texto).toBe("Cambio guardado en el teléfono. Enviando…");
        expect(avisoGuardado({ guardados: 0, errores: [{ etiqueta: "Potencia", mensaje: "Escribe un número" }, { etiqueta: "X", mensaje: "y" }] }, { porEnviar: 0, enLinea: true })).toEqual({
            tono: "error",
            texto: "No se guardó «Potencia»: Escribe un número (y 1 más)",
        });
        expect(avisoGuardado({ guardados: 0, errores: [] }, { porEnviar: 2, enLinea: false })).toEqual({
            tono: "info",
            texto: "Todo está guardado en el teléfono · 2 por enviar. Se enviarán al volver la señal.",
        });
        expect(avisoGuardado({ guardados: 0, errores: [] }, { porEnviar: 0, enLinea: true })).toEqual({ tono: "success", texto: "Todo está guardado y enviado." });
    });
});

describe("orden en pausa: solo lectura", () => {
    it("pausada no admite trabajo ni agregar formularios; sí reanudar", () => {
        const a = accionesDe({ estado: "pausada", tipo: "instalacion", reasignada: false });
        expect(a).toMatchObject({ trabajo: false, enPausa: true, instalar: false, retirar: false, agregarFormulario: false, cerrar: false, reanudar: true, notas: true });
        expect(accionesDe({ estado: "en_sitio", tipo: "instalacion", reasignada: false })).toMatchObject({ trabajo: true, enPausa: false, instalar: true, agregarFormulario: true });
        // Reasignada: ni siquiera «en pausa» (ya no es suya).
        expect(accionesDe({ estado: "pausada", tipo: "visita", reasignada: true }).enPausa).toBe(false);
    });

    it("el «+» no ofrece agregar formulario a una orden pausada", () => {
        const ordenes = [ordenDePrueba(1, { estado: "pausada" }), ordenDePrueba(2, { estado: "en_sitio" })].map((o) => ({ ...o, reasignada: false }));
        const r = accionesRapidas(ordenes, [], ["ordenes.registrar"]);
        expect(r.formulario?.ordenes.map((o) => o.orden.id)).toEqual([2]);
    });
});
