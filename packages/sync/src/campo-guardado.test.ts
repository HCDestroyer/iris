// La isla de los pasos (registro de borradores, «Guardar y seguir» solo con cambios y su
// confirmación) y la orden en pausa de solo lectura (reglas que comparten la PWA y la app nativa).
import { describe, expect, it, vi } from "vitest";
import { accionesDe, accionesRapidas, avisoGuardado, estadoIslaPasos, MS_GUARDADO, RegistroBorradores, type Borrador } from "./campo";
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
    it("el error va arriba con el campo; lo guardado se confirma en la isla; sin nada, silencio", () => {
        expect(avisoGuardado({ guardados: 2, errores: [] }, { enLinea: false })).toEqual({
            en: "isla",
            tono: "success",
            texto: "Guardado",
            detalle: "Guardado: 2 cambios en el teléfono. Se enviarán al volver la señal.",
        });
        expect(avisoGuardado({ guardados: 1, errores: [] }, { enLinea: true })).toMatchObject({ en: "isla", detalle: "Guardado: 1 cambio en el teléfono. Enviando…" });
        expect(avisoGuardado({ guardados: 1, errores: [{ etiqueta: "Potencia", mensaje: "Escribe un número" }, { etiqueta: "X", mensaje: "y" }] }, { enLinea: true })).toEqual({
            en: "aviso",
            tono: "error",
            texto: "No se guardó «Potencia»: Escribe un número (y 1 más)",
        });
        // Sin cambios no hay nada que decir (ni «Todo está guardado»: no se tocó «Guardar»).
        expect(avisoGuardado({ guardados: 0, errores: [] }, { enLinea: true })).toBeNull();
        expect(MS_GUARDADO).toBe(1500);
    });
});

describe("estadoIslaPasos", () => {
    const base = { paso: "checklist" as const, pendientes: 0, enPausa: false, recienGuardado: false };

    it("sin cambios: sin «Guardar», el primario solo avanza («Siguiente», «Ir a firma»)", () => {
        const e = estadoIslaPasos(base);
        expect(e.anterior).toEqual({ texto: "Anterior", destino: "llegada" });
        expect(e.primario).toEqual({ accion: "navegar", texto: "Siguiente", etiqueta: "Siguiente: materiales y equipos", icono: "siguiente", cuenta: null, destino: "materiales" });
        expect(e.guardado).toBe(false);
        expect(estadoIslaPasos({ ...base, paso: "fotos" }).primario).toMatchObject({ accion: "navegar", texto: "Ir a firma", etiqueta: "Ir a firma y cierre", destino: "cierre" });
        expect(estadoIslaPasos({ ...base, paso: "llegada" }).anterior).toEqual({ texto: "Resumen", destino: null });
        // Sin más pasos: «Terminar» vuelve al resumen.
        expect(estadoIslaPasos({ ...base, paso: "cierre" }).primario).toMatchObject({ accion: "navegar", texto: "Terminar", destino: null });
    });

    it("con cambios: «Guardar y seguir» con el contador (también en el último paso)", () => {
        expect(estadoIslaPasos({ ...base, pendientes: 2 }).primario).toEqual({
            accion: "guardar_y_seguir",
            texto: "Guardar y seguir",
            etiqueta: "Guardar y seguir (2 sin guardar)",
            icono: "guardar",
            cuenta: 2,
            destino: "materiales",
        });
        expect(estadoIslaPasos({ ...base, paso: "fotos", pendientes: 1 }).primario).toMatchObject({ accion: "guardar_y_seguir", cuenta: 1, destino: "cierre" });
    });

    it("tras guardar: «Guardado» y de nuevo sin «Guardar»; un cambio nuevo lo quita", () => {
        const e = estadoIslaPasos({ ...base, paso: "materiales", recienGuardado: true });
        expect(e.guardado).toBe(true);
        expect(e.primario.accion).toBe("navegar");
        expect(estadoIslaPasos({ ...base, recienGuardado: true, pendientes: 1 }).guardado).toBe(false);
    });

    it("en pausa: solo lectura, nunca guarda; «Siguiente» solo navega", () => {
        const e = estadoIslaPasos({ ...base, pendientes: 3, enPausa: true });
        expect(e.primario).toMatchObject({ accion: "navegar", texto: "Siguiente", cuenta: null });
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
