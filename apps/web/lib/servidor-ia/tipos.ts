import type { Permiso } from "@/lib/menu";

/*
 * Formas del Servidor de IA (contratos/voz.md §4.8, §4.11 y §4.12).
 *
 * PROVISIONAL: el servidor aún no publica el OpenAPI de estas rutas, así que estos tipos se
 * escribieron a mano siguiendo el contrato. Donde el contrato no fija la forma (el cuerpo de
 * `GET /servidor-ia/tablero`, los umbrales) es un supuesto del panel, anotado en
 * `.claude/integration/i3.md`. Cuando `packages/contratos` las traiga, este archivo pasa a ser
 * solo alias de `Schema<"…">`, como el resto de `lib/*`/tipos.
 */

/** Los permisos nuevos (§8.2) todavía no están en el tipo generado. */
const permiso = (nombre: string) => nombre as unknown as Permiso;
export const PERMISO_VER = permiso("servidor_ia.ver");
export const PERMISO_VER_USO = permiso("servidor_ia.ver_uso");
export const PERMISO_ADMINISTRAR = permiso("servidor_ia.administrar");

export type EstadoMotor = "sin_enlazar" | "listo" | "degradado" | "no_disponible" | "incompatible" | "desactivado";

export type Paquete = { version: string; protocolo: string; compatible: boolean; motivoIncompatible: string | null; versionDisponible: string | null };

export type PasoPrueba = { codigo: "conexion" | "compatibilidad" | "gpu" | "stt" | "tts" | "llm" | "medios" | "capacidad"; estado: "ok" | "error" | "omitido"; ms: number | null; detalle: string | null };
export type ResultadoPrueba = { estado: "ok" | "error"; pasos: PasoPrueba[] };

/** La tarjeta del servidor (`GET /voz/motores`, §4.8). */
export type Motor = {
    id: number;
    nombre: string;
    url: string;
    version: number;
    estado: EstadoMotor;
    detalle: string | null;
    ultimoLatidoEn: string | null;
    enlazadoEn: string | null;
    /** Supuesto: cuándo caduca un enlace sin activar (24 h, §4.8 paso 5). */
    enlaceVenceEn?: string | null;
    paquete: Paquete | null;
    capacidad: { maxSimultaneas: number; enCurso: number } | null;
    catalogo: { llm: { id: string }[] } | null;
    /** La huella del certificado del servidor que vio Iris; `null` hasta «Conectar». */
    huellaTls: string | null;
    tokenRotadoEn: string | null;
    certClienteVenceEn: string | null;
    ipObservada: string | null;
    /** `null`: lo pueden usar todas las empresas. */
    empresasPermitidas: number[] | null;
    simultaneasPorEmpresa: number;
    ultimaPrueba: { estado: "ok" | "error"; probadoEn: string } | null;
};

/** `POST /voz/motores/enlazar` → 201. El código se muestra una sola vez. */
export type EnlaceCreado = { motor: Motor; codigo: string; venceEn: string };

export type Rango = "ahora" | "1h" | "24h" | "7d";
export const RANGOS: readonly Rango[] = ["ahora", "1h", "24h", "7d"];

export type Gpu = { indice: number; nombre: string; usoPct: number | null; vramUsadaMb: number | null; vramTotalMb: number | null; temperaturaC: number | null; potenciaW: number | null };
export type ModeloInstalado = { id: string; tamanoMb: number | null; cargado: boolean; vramMb: number | null; cargadoHasta: string | null };

/** Un punto de las series (cada 10 s en «ahora», por minuto hasta 24 h, por hora en 7 días). */
export type PuntoSerie = {
    t: string;
    /** `null`: en ese instante el servidor no respondió (o no hay tarjeta gráfica). */
    usoPct: number | null;
    vramPct: number | null;
    peticionesMin: number | null;
    latenciaMediaMs: number | null;
    latenciaP95Ms: number | null;
};

export type MotivoError = "tiempo_agotado" | "no_disponible" | "ocupado" | "respuesta_invalida" | "modelo_no_instalado" | "otro";
export type UsoEmpresa = { empresaId: number; nombre: string; peticiones: number; tokensEntrada: number; tokensSalida: number; latenciaMediaMs: number | null; errores: number; rechazadas: number };
export type ErrorReciente = { motivo: MotivoError; cuenta: number; ultimoEn: string };

export type TipoAviso = "caido" | "incompatible" | "vram" | "disco" | "temperatura" | "saturacion";
export type AvisoServidor = { tipo: TipoAviso; activo: boolean; desde: string; hasta: string | null; valor: number | null; umbral: number | null };

/** `GET /servidor-ia/tablero?rango=` (§4.12). */
export type Tablero = {
    estado: {
        /** `null`: no hay ningún servidor enlazado. */
        motorId: number | null;
        estado: EstadoMotor | null;
        detalle: string | null;
        version: string | null;
        compatible: boolean | null;
        motivoIncompatible: string | null;
        encendidoSeg: number | null;
        /** Cuándo se tomó la última muestra que sí se pudo leer. */
        tomadoEn: string | null;
        /** `false`: la última lectura falló; lo de `servidor` no es de ahora y no se muestra como actual. */
        lecturaOk: boolean;
        modelosDisponibles: string[];
        /** Cuántas empresas lo usan (solo para quien ve todo). */
        empresasQueLoUsan?: string[];
    };
    /** Solo con `servidor_ia.ver`; `null` para el admin de empresa o sin lectura. */
    servidor: {
        gpus: Gpu[] | { estado: "omitido" };
        disco: { usadoMb: number; totalMb: number } | null;
        modelos: ModeloInstalado[];
        llm: { enCurso: number; maxSimultaneas: number };
        /** Rechazadas por saturación (`servidor_ia_ocupado`) en el rango. */
        rechazadas: number;
    } | null;
    /** Con `ver_uso`, solo las peticiones y los tiempos de la empresa activa (`usoPct` y `vramPct` en `null`). */
    series: PuntoSerie[] | null;
    uso: {
        /** Con `ver_uso`, una sola fila: la de la empresa activa. */
        empresas: UsoEmpresa[];
        errores: ErrorReciente[];
    } | null;
    avisos: AvisoServidor[];
};

/** `GET /servidor-ia/umbrales` y `POST /servidor-ia/umbrales/actualizar` (§4.12). */
export type Umbrales = {
    version: number;
    vramPct: number;
    vramMinutos: number;
    discoPct: number;
    temperaturaC: number;
    temperaturaMinutos: number;
    saturacionRechazos: number;
    saturacionMinutos: number;
};

/** Lo que devuelven los ajustes del asistente de cada empresa (§4.11). */
export type ServidorIaEnAsistente = { disponible: boolean; modelos: string[] };
