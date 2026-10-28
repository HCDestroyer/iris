import { AlmacenMemoria } from "../almacen-memoria";
import type { Paquete } from "../contrato";
import { MotorSync, type OpcionesMotor } from "../motor";
import { ordenDePrueba } from "./fabricas";
import { RelojManual } from "./reloj-manual";
import { SubidorFalso } from "./subidor-falso";
import { TransporteFalso } from "./transporte-falso";

export const FECHA = "2026-10-28";

/** Producto de las existencias de prueba (cable UTP, en metros). */
export const CABLE = 900;

export function paqueteDePrueba(fecha = FECHA): Paquete {
    return {
        fecha,
        servidorEn: "2026-10-28T15:00:00Z",
        ordenes: [ordenDePrueba(1), ordenDePrueba(2)],
        existencias: {
            disponible: true,
            motivo: null,
            bodegas: [{ bodegaId: 7, lineas: [{ productoId: CABLE, descripcion: "Cable UTP", unidad: "m", cantidad: 50 }] }],
        },
        ruta: { id: 1, ordenIds: [2, 1] },
        cursor: "c1",
    };
}

export interface Montaje {
    motor: MotorSync;
    almacen: AlmacenMemoria;
    transporte: TransporteFalso;
    subidor: SubidorFalso;
    reloj: RelojManual;
    /** Abre otro motor sobre el mismo almacén (simula cerrar y abrir la app). */
    reabrir: (extra?: Partial<OpcionesMotor>) => Promise<MotorSync>;
}

/** Motor con almacén en memoria, transporte y subidor falsos y el paquete del día ya cargado. */
export async function montar(extra: Partial<OpcionesMotor> = {}, conPaquete = true): Promise<Montaje> {
    const almacen = new AlmacenMemoria();
    const transporte = new TransporteFalso();
    const subidor = new SubidorFalso();
    const reloj = new RelojManual();
    transporte.paquete = (fecha) => paqueteDePrueba(fecha);
    const opciones = (): OpcionesMotor => ({ almacen, transporte, subidor, reloj, ...extra });
    const motor = await MotorSync.abrir(opciones());
    if (conPaquete) {
        await motor.cargarPaquete(FECHA);
        transporte.llamadas.length = 0;
    }
    return {
        motor,
        almacen,
        transporte,
        subidor,
        reloj,
        reabrir: (mas = {}) => MotorSync.abrir({ ...opciones(), ...mas }),
    };
}
