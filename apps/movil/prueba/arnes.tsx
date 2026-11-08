// Arnés de las pantallas de campo: motor real de `@iris/sync` sobre memoria (`montar`), inventario
// falso con las formas del contrato y el marco completo (encabezado, barra discreta, vista y barra
// inferior), en claro u oscuro. El lienzo de firma se reemplaza (ver `firmaFalsa`): la firma con el
// dedo y su PNG se validan en un teléfono.
import { ErrorTransporte, type InventarioCampo, type LibresCampo, type MiBodega, type OrdenCampo, type OrdenCampoTomada, type OrdenesLibres, type OrdenLibre, type Paquete, type Traslado } from "@iris/sync";
import { itemDePrueba, ordenDePrueba, paqueteDePrueba, type Montaje } from "@iris/sync/pruebas";
import { render, screen } from "@testing-library/react-native";
import { ProveedorCampo, type ServiciosNativos, type Vista } from "../src/campo/contexto";
import { ProveedorAgenda } from "../src/campo/agenda";
import { ProveedorInventario } from "../src/campo/inventario";
import type { MotorChat } from "@iris/sync/chat";
import { ProveedorChat } from "../src/campo/chat";
import { ProveedorLibres } from "../src/campo/libres";
import { Marco } from "../src/pantallas/Marco";
import { PrimerUsoPush } from "../src/pantallas/Notificaciones";
import { ProveedorPush, type PropsProveedorPush } from "../src/push/contexto";
import type { SesionCampo } from "../src/sesion/sesion";
import { TemaDePrueba } from "./raiz";

export const sesion: SesionCampo = {
    usuarioId: 5,
    nombre: "Téc. Ana",
    correo: "ana@isp.gt",
    empresas: [],
    modoEmpresa: "sin_conector",
    empresaActiva: null,
    verificadoEn: Date.now(),
    permisos: [
        "campo.sincronizar",
        "ordenes.registrar",
        "ordenes.llegar",
        "ordenes.cerrar",
        "ordenes.pausar",
        "ordenes.reanudar",
        "agenda.crear_tarea",
        "agenda.completar_tarea",
        "agenda.anular_tarea",
        "ordenes.tomar",
    ],
};

/** Paquete sin ruta con una orden de visita y un ítem obligatorio. */
export function paqueteCon(orden: Partial<OrdenCampo>): (fecha: string) => Paquete {
    return (fecha) => {
        const base = paqueteDePrueba(fecha);
        delete base.ruta;
        return { ...base, ordenes: [ordenDePrueba(1, { tipo: "visita", checklist: [itemDePrueba(1, { etiqueta: "Señal verificada", obligatorio: true })], ...orden })] };
    };
}

/** «Por recibir» y «Mi bodega» con respuestas del contrato; `fallar` hace que la próxima lectura falle. */
export class InventarioFalso implements InventarioCampo {
    traslados: Traslado[] = [];
    /** Órdenes de la semana (`GET /campo/paquete?fecha=` del último día) para «Mañana» y «Semana». */
    ordenesAgenda: OrdenCampo[] = [];
    fallarAgenda: unknown = null;
    /** Último día pedido a la agenda. */
    hastaAgenda: string | null = null;
    bodega: MiBodega = bodegaDePrueba();
    fallarTraslados: unknown = null;
    fallarBodega: unknown = null;
    lecturas = { traslados: 0, bodega: 0 };

    porRecibir = async (): Promise<Traslado[]> => {
        this.lecturas.traslados += 1;
        if (this.fallarTraslados) {
            const e = this.fallarTraslados;
            this.fallarTraslados = null;
            throw e;
        }
        return structuredClone(this.traslados);
    };

    agenda = async (hasta: string): Promise<OrdenCampo[]> => {
        this.hastaAgenda = hasta;
        if (this.fallarAgenda) {
            const e = this.fallarAgenda;
            this.fallarAgenda = null;
            throw e;
        }
        return structuredClone(this.ordenesAgenda);
    };

    miBodega = async (): Promise<MiBodega> => {
        this.lecturas.bodega += 1;
        if (this.fallarBodega) {
            const e = this.fallarBodega;
            this.fallarBodega = null;
            throw e;
        }
        return structuredClone(this.bodega);
    };
}

export function trasladoDePrueba(id: number, extra: Partial<Traslado> = {}): Traslado {
    return {
        id,
        estado: "enviado",
        enviadoEn: "2026-10-28T14:00:00Z",
        lineas: [
            { lineaId: 1, productoId: 900, cantidad: 10, series: null },
            { lineaId: 2, productoId: 901, cantidad: 2, series: ["ZTE-1", "ZTE-2"] },
        ],
        ...extra,
    };
}

export function bodegaDePrueba(): MiBodega {
    return {
        bodega: { id: 7, empresaId: 1, nombre: "Bodega de Ana" } as MiBodega["bodega"],
        existencias: [
            { productoId: 900, descripcion: "Cable UTP", unidad: "m", cantidad: 50, modalidad: "consumible_gasto" },
            { productoId: 901, descripcion: "ONU ZTE", unidad: "u", cantidad: 2, modalidad: "comodato" },
        ],
        unidades: [{ id: 1, productoId: 901, descripcion: "ONU ZTE", serie: "ZTE-9", mac: "AA:BB:CC:00:11:22", estado: "en_bodega" }],
        kardex: [{ id: 3, productoId: 900, descripcion: "Cable UTP", cantidad: -10, tipo: "consumo", referencia: "OT-000001", ocurridoEn: "2026-10-27T15:00:00Z" }],
        trasladosPendientes: [],
        leidoEn: "2026-10-28T15:00:00Z",
    };
}

/** Órdenes libres de prueba (`GET /campo/ordenes-libres`, `POST /ordenes/{id}/tomar`). */
export class LibresFalso implements LibresCampo {
    ordenes: OrdenLibre[] = [];
    /** id → quién la tomó primero (409 `orden_ya_tomada`). */
    tomadasPorOtro = new Map<number, string>();
    tomadas: number[] = [];
    lecturas = 0;

    async listar(): Promise<OrdenesLibres> {
        this.lecturas += 1;
        const data = this.ordenes.filter((o) => !this.tomadas.includes(o.id));
        return { data, nextCursor: null, total: data.length, orden: "distancia", zonas: [{ id: 3, nombre: "Zona Norte" }] };
    }

    async tomar(id: number): Promise<OrdenCampoTomada> {
        const o = this.ordenes.find((x) => x.id === id);
        if (!o) {
            throw new ErrorTransporte("http", "No encontrada", 404, { type: "urn:iris:problema:no_encontrado", title: "No encontrada", status: 404 });
        }
        const otro = this.tomadasPorOtro.get(id);
        if (otro) {
            throw new ErrorTransporte("http", "Ya tomada", 409, { type: "urn:iris:problema:orden_ya_tomada", title: "Ya tomada", status: 409, tomadaPor: { id: 999, nombre: otro } });
        }
        this.tomadas.push(id);
        return { ...o, tecnicoId: 5 };
    }
}

export interface OpcionesArnes {
    esquema?: "light" | "dark";
    vista?: Vista;
    inventario?: InventarioFalso;
    libres?: LibresFalso;
    servicios?: Partial<ServiciosNativos>;
    enLinea?: () => boolean;
    /** Otra sesión (p. ej. sin `ordenes.registrar`). */
    sesion?: SesionCampo;
    /** Chat interno (con `chat.ver`); sin él la app no ofrece el chat. */
    chat?: MotorChat;
    /** Primer/segundo plano para el chat (la app pasa `escucharPlano`, sobre AppState). */
    alCambiarPlano?: (fn: (alFrente: boolean) => void) => () => void;
    /** Notificaciones push (servicio, API y registro falsos): el marco va dentro de `ProveedorPush`. */
    push?: Omit<PropsProveedorPush, "children" | "primerUso" | "alVolverAlFrente" | "empresa" | "baseUrl"> & { alVolverAlFrente?: PropsProveedorPush["alVolverAlFrente"] };
    /** Margen seguro del teléfono (barra de estado, muesca); sin él, 0 en todos los lados. */
    margen?: { top: number; bottom: number; left: number; right: number };
    /** «Cerrar sesión» de la app (por defecto, no hace nada). */
    cerrarSesion?: (opciones: { conservarPendientes: boolean }) => Promise<void>;
    /** La API pide cambiar la contraseña (la raíz lleva a «Crea tu contraseña»). */
    alExigirContrasena?: () => void;
}

export async function abrirCampo(m: Montaje, o: OpcionesArnes = {}) {
    const inventario = o.inventario ?? new InventarioFalso();
    const libres = o.libres ?? new LibresFalso();
    const servicios: ServiciosNativos = { tomarFoto: async () => null, posicion: async () => null, enLinea: o.enLinea ?? (() => true), ...o.servicios };
    const marco = <Marco empresa={null} alSesionVencida={jest.fn()} {...(o.alExigirContrasena ? { alExigirContrasena: o.alExigirContrasena } : {})} />;
    const conPush = o.push ? (
        <ProveedorPush {...o.push} empresa={null} baseUrl="http://iris.prueba" alVolverAlFrente={o.push.alVolverAlFrente ?? (() => () => undefined)} primerUso={(p) => <PrimerUsoPush {...p} />}>
            {marco}
        </ProveedorPush>
    ) : (
        marco
    );
    await render(
        <TemaDePrueba esquema={o.esquema ?? "light"} {...(o.margen ? { margen: o.margen } : {})}>
            <ProveedorCampo motor={m.motor} sesion={o.sesion ?? sesion} servicios={servicios} cerrarSesion={o.cerrarSesion ?? (async () => undefined)} vistaInicial={o.vista ?? { tipo: "lista" }}>
                <ProveedorInventario inventario={inventario}>
                    <ProveedorAgenda leer={inventario.agenda}>
                        <ProveedorLibres api={libres}>
                            {o.chat ? (
                                <ProveedorChat chat={o.chat} alCambiarPlano={o.alCambiarPlano}>
                                    {conPush}
                                </ProveedorChat>
                            ) : (
                                conPush
                            )}
                        </ProveedorLibres>
                    </ProveedorAgenda>
                </ProveedorInventario>
            </ProveedorCampo>
        </TemaDePrueba>,
    );
    return { inventario, libres };
}

/**
 * El aviso emergente visible (el `Toast` de HeroUI Native dibuja además una copia invisible para
 * medir su alto: la primera es la de verdad).
 */
export const avisoArriba = () => {
    const [visible] = screen.getAllByTestId("notificacion");
    if (!visible) {
        throw new Error("No hay aviso emergente");
    }
    return visible;
};

export const tipos = (m: Montaje) => m.motor.obtenerEstado().cola.map((op) => op.tipo);

/** La nube de sincronización del encabezado (conexión y envíos): su nombre accesible dice el estado. */
export const nube = () => screen.getByTestId("indicador-sincronizacion");
export const TODO_ENVIADO = "Todo enviado, toca para ver el estado";
