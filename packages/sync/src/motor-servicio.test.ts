import { describe, expect, it } from "vitest";
import type { OrdenCampo } from "./contrato";
import { ErrorTransporte } from "./errores";
import { conDatosSensibles, sinRedNiUbicacion, VENTANA_DATOS_SERVICIO_MS } from "./motor";
import { ordenDePrueba } from "./pruebas/fabricas";
import { FECHA, montar, paqueteDePrueba } from "./pruebas/montaje";

/*
 * Contrato con Argos 0.8.0 §2.4 (revisión de seguridad): el teléfono guarda la red y la ubicación
 * del servicio solo mientras la orden es del técnico (abierta o terminada hace menos de 24 h). Una
 * orden que sale del paquete, reasignada, cancelada o terminada hace más de 24 h las pierde en
 * IndexedDB aunque siga guardada por su cola.
 */

const SERVICIO = {
    id: 4410,
    codigo: "S-4410",
    plan: "Fibra 50",
    zona: "Chitá",
    precioMensual: "250.00",
    moneda: "GTQ",
    router: "Nodo Centro",
    ip: "100.64.12.34",
    direccion: "3a calle 4-56 zona 1",
    lat: 14.6349,
    lon: -90.5069,
    estado: "activo",
    sincronizadoEn: "2026-10-28T15:00:00Z",
};

const conServicio = (id: number, parcial: Partial<OrdenCampo> = {}): OrdenCampo => ordenDePrueba(id, { servicio: { ...SERVICIO, id: 4400 + id }, ...parcial });

async function montarConServicio() {
    const m = await montar({}, false);
    m.transporte.paquete = (fecha) => ({ ...paqueteDePrueba(fecha), ordenes: [conServicio(1), conServicio(2)] });
    await m.motor.cargarPaquete(FECHA);
    return m;
}

/** Lo que quedó guardado en el almacén (IndexedDB) de la orden. */
const guardada = (m: Awaited<ReturnType<typeof montar>>, id: number) => m.almacen.ordenes.get(id)?.base.servicio;

describe("red y ubicación del servicio en el teléfono", () => {
    it("quita router, IP, dirección y coordenadas y deja plan, zona, precio y estado", () => {
        const base = conServicio(1);
        expect(conDatosSensibles(base)).toBe(true);
        const sin = sinRedNiUbicacion(base);
        expect(conDatosSensibles(sin)).toBe(false);
        expect(sin.servicio).toEqual({ id: 4401, codigo: "S-4410", plan: "Fibra 50", zona: "Chitá", precioMensual: "250.00", moneda: "GTQ", estado: "activo", sincronizadoEn: "2026-10-28T15:00:00Z" });
        // Un campo nuevo que no está en la lista blanca también se borra.
        const conNuevo = { ...base, servicio: { ...SERVICIO, mac: "AA:BB" } } as OrdenCampo;
        expect(conDatosSensibles(conNuevo)).toBe(true);
        expect(sinRedNiUbicacion(conNuevo).servicio).not.toHaveProperty("mac");
        // La dirección de la orden (su propia copia) no se toca.
        expect(sin.direccion).toBe(base.direccion);
    });

    it("una orden que sale del paquete y sigue por su cola pierde la red y la ubicación", async () => {
        const m = await montarConServicio();
        m.transporte.sinRed = true;
        await m.motor.registrar("nota.agregar", 2, { texto: "pendiente" });
        await expect(m.motor.sincronizar()).rejects.toBeInstanceOf(ErrorTransporte);
        m.transporte.sinRed = false;
        expect(guardada(m, 2)?.ip).toBe("100.64.12.34");
        m.transporte.paquete = (fecha) => ({ ...paqueteDePrueba(fecha), ordenes: [conServicio(1)] });
        await m.motor.cargarPaquete(FECHA);
        expect(guardada(m, 2)).not.toHaveProperty("ip");
        expect(guardada(m, 2)).not.toHaveProperty("direccion");
        expect(guardada(m, 2)).toMatchObject({ plan: "Fibra 50" });
        expect(guardada(m, 1)?.ip).toBe("100.64.12.34");
    });

    it("reasignada con cola: se conserva marcada, sin red ni ubicación", async () => {
        const m = await montarConServicio();
        m.transporte.sinRed = true;
        await m.motor.registrar("nota.agregar", 2, { texto: "pendiente" });
        await expect(m.motor.sincronizar()).rejects.toBeInstanceOf(ErrorTransporte);
        m.transporte.sinRed = false;
        m.transporte.cambios = () => ({ cambios: [{ tipo: "reasignada", ordenId: 2 }], cursor: "c2", hayMas: false });
        m.transporte.lote = () => ({ resultados: [] });
        await m.motor.sincronizar();
        expect(m.motor.obtenerOrden(2)).toMatchObject({ reasignada: true });
        expect(guardada(m, 2)).not.toHaveProperty("ip");
        expect(guardada(m, 2)).not.toHaveProperty("lat");
    });

    it("cancelada por cambios: sin red ni ubicación", async () => {
        const m = await montarConServicio();
        m.transporte.cambios = () => ({ cambios: [{ tipo: "cancelada", ordenId: 1 }], cursor: "c2", hayMas: false });
        await m.motor.sincronizar();
        expect(guardada(m, 1)).not.toHaveProperty("router");
    });

    it("cerrada hace más de 24 h: la pierde en la siguiente pasada, aunque no llegue ningún cambio", async () => {
        const m = await montar({}, false);
        const cerrada = new Date(m.reloj.ahora() - 60 * 60 * 1000).toISOString();
        m.transporte.paquete = (fecha) => ({ ...paqueteDePrueba(fecha), ordenes: [conServicio(1, { estado: "cerrada", finalizadaEn: cerrada }), conServicio(2)] });
        await m.motor.cargarPaquete(FECHA);
        await m.motor.sincronizar();
        // Cerrada hace 1 h: todavía la ve.
        expect(guardada(m, 1)?.ip).toBe("100.64.12.34");
        await m.reloj.avanzar(VENTANA_DATOS_SERVICIO_MS);
        await m.motor.sincronizar();
        expect(guardada(m, 1)).not.toHaveProperty("ip");
        expect(guardada(m, 1)).not.toHaveProperty("direccion");
        // La abierta no cambia.
        expect(guardada(m, 2)?.ip).toBe("100.64.12.34");
    });
});
