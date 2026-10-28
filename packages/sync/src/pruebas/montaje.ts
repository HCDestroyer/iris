import { AlmacenMemoria } from "../almacen-memoria";
import type { PaqueteDia } from "../contrato-sync";
import { MotorSync, type OpcionesMotor } from "../motor";
import { ordenDePrueba } from "./fabricas";
import { RelojManual } from "./reloj-manual";
import { SubidorFalso } from "./subidor-falso";
import { TransporteFalso } from "./transporte-falso";

export const FECHA = "2026-10-28";

export function paqueteDePrueba(fecha = FECHA): PaqueteDia {
    return {
        fecha,
        ordenes: [ordenDePrueba("A"), ordenDePrueba("B")],
        existencias: [{ productoId: "cable", nombre: "Cable UTP", unidad: "m", cantidad: 50 }],
        ruta: {
            id: "ruta-1",
            fecha,
            paradas: [
                { ordenId: "B", posicion: 1 },
                { ordenId: "A", posicion: 2 },
            ],
        },
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
