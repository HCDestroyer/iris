// Formas del cable de la app de campo, tomadas de `@iris/contratos` (OpenAPI generado de la API).
// Nada aquí declara a mano una forma que viaje: solo alias y derivados (`datos` de cada
// operación = el cuerpo de la acción del panel sin `version`, ver `apps/api/internal/campo`).
import type { Schema } from "@iris/contratos";

export type Problema = Schema<"Problema">;
export type ErrorCampo = Schema<"ErrorCampo">;

export type OrdenCampo = Schema<"OrdenCampo">;
export type ItemChecklist = Schema<"ItemChecklist">;
export type NotaOrden = Schema<"NotaOrden">;
export type MaterialOrden = Schema<"MaterialOrden">;
export type EquipoOrden = Schema<"EquipoOrden">;
export type FirmaOrden = Schema<"FirmaOrden">;
export type AdjuntoOrden = Schema<"AdjuntoOrden">;
export type Comodato = Schema<"Comodato">;
export type EquipoComodato = Schema<"EquipoComodato">;
export type ClienteCampo = Schema<"ClienteCampo">;
export type ServicioCampo = Schema<"ServicioCampo">;

export type Paquete = Schema<"Paquete">;
export type Existencias = Schema<"Existencias">;
export type ExistenciasBodega = Schema<"ExistenciasBodega">;
export type Existencia = Schema<"Existencia">;
export type RutaCampo = Schema<"RutaCampo">;
export type Cambios = Schema<"Cambios">;
/** Turno del técnico ese día según la agenda, sin las ausencias (`Paquete.turno`, I2). */
export type TurnoCampo = Schema<"TurnoCampo">;
export type TramoCampo = Schema<"TramoCampo">;
/**
 * `GET /campo/cambios` aún no trae el turno; si la API lo agrega con la misma forma del paquete,
 * el motor lo toma sin cambios (hasta entonces solo cuenta el del paquete).
 */
export type CambiosConTurno = Cambios & { turno?: TurnoCampo };
export type CitaOrden = Schema<"CitaOrden">;
/** Formularios y pausa (I2, PR #29). */
export type PlantillaFormulario = Schema<"PlantillaFormulario">;
export type ItemPlantilla = Schema<"ItemPlantilla">;
export type FormularioOrden = Schema<"FormularioOrden">;
export type PausaOrden = Schema<"PausaOrden">;
export type MotivoPausa = Schema<"EntradaPausar">["motivo"];
/** Aviso de una operación aplicada (`version_anterior`, `pausa_ajustada`…). */
export type AvisoResultado = NonNullable<Schema<"ResultadoOperacion">["aviso"]>;

/** Ubicación en vivo del técnico (`POST /campo/posiciones`, I2). */
export type EntradaPosiciones = Schema<"EntradaPosiciones">;
export type PuntoPosicion = Schema<"PuntoPosicion">;
export type RespuestaPosiciones = Schema<"RespuestaPosiciones">;
export type CambioOrden = Schema<"CambioOrden">;

export type EntradaLote = Schema<"EntradaLote">;
export type OperacionLote = Schema<"OperacionLote">;
export type RespuestaLote = Schema<"RespuestaLote">;
export type ResultadoOperacion = Schema<"ResultadoOperacion">;
export type OrdenVersion = Schema<"EstadoOrden">;

/** Alta de un adjunto (`POST /adjuntos`, I1) y su respuesta. */
export type EntradaRegistrarAdjunto = Schema<"EntradaRegistrar">;
export type Adjunto = Schema<"Adjunto">;

export type EstadoOrden = OrdenCampo["estado"];
export type TipoOrden = OrdenCampo["tipo"];
export type TipoOperacion = OperacionLote["tipo"];
export type TipoAdjunto = Adjunto["tipo"];
export type TipoItem = ItemChecklist["tipo"];
export type MotivoFalla = Schema<"EntradaFallarOrden">["motivoFalla"];
export type CondicionEquipo = NonNullable<EquipoOrden["condicion"]>;

export const ESTADOS_ORDEN = [
    "pendiente",
    "programada",
    "en_camino",
    "en_sitio",
    "pausada",
    "cerrada",
    "fallida",
    "cancelada",
] as const satisfies readonly EstadoOrden[];

export const TIPOS_OPERACION = [
    "orden.iniciar_camino",
    "orden.llegar",
    "checklist.marcar",
    "nota.agregar",
    "material.usar",
    "equipo.instalar",
    "equipo.retirar",
    "firma.capturar",
    "adjunto.registrar",
    "orden.cerrar",
    "orden.fallar",
    "orden.pausar",
    "orden.reanudar",
    "orden.agregar_formulario",
    "orden.quitar_formulario",
] as const satisfies readonly TipoOperacion[];

export const MOTIVOS_PAUSA = ["esperando_material", "cliente_ausente", "clima", "acceso", "otro"] as const satisfies readonly MotivoPausa[];

export const MOTIVOS_FALLA = [
    "cliente_ausente",
    "sin_acceso",
    "sin_cobertura",
    "falta_material",
    "equipo_defectuoso",
    "clima",
    "cliente_rechaza",
    "otro",
] as const satisfies readonly MotivoFalla[];

/** Valor de una marca del checklist: si_no → boolean, texto → string, número y medición → number, foto → uuid. */
export type ValorChecklist = boolean | number | string;

type SinVersion<T> = Omit<T, "version">;

/** `datos` de cada operación: el cuerpo de la acción del panel, sin `version` (I2). */
export interface DatosPorTipo {
    "orden.iniciar_camino": SinVersion<Schema<"EntradaPosicion">>;
    "orden.llegar": SinVersion<Schema<"EntradaPosicion">>;
    "checklist.marcar": SinVersion<Schema<"EntradaMarcarChecklist">> & { valor: ValorChecklist | null };
    "nota.agregar": SinVersion<Schema<"EntradaAgregarNota">>;
    /** Siempre de la bodega de la orden: el lote rechaza `bodegaId`. */
    "material.usar": Omit<Schema<"EntradaUsarMaterial">, "version" | "bodegaId">;
    "equipo.instalar": SinVersion<Schema<"EntradaEquipo">>;
    "equipo.retirar": SinVersion<Schema<"EntradaEquipo">>;
    "firma.capturar": SinVersion<Schema<"EntradaCapturarFirma">>;
    "adjunto.registrar": SinVersion<Schema<"EntradaRegistrarAdjunto">>;
    "orden.cerrar": SinVersion<Schema<"EntradaCerrarOrden">>;
    "orden.fallar": SinVersion<Schema<"EntradaFallarOrden">>;
    "orden.pausar": SinVersion<Schema<"EntradaPausar">>;
    "orden.reanudar": SinVersion<Schema<"EntradaReanudar">>;
    "orden.agregar_formulario": Schema<"EntradaAgregarFormularioCampo">;
    "orden.quitar_formulario": Schema<"EntradaQuitarFormularioCampo">;
}

/** Una operación tal como viaja en `POST /sync/lote` (`OperacionLote` con `datos` tipados). */
export type OperacionCable = {
    [T in TipoOperacion]: Omit<OperacionLote, "tipo" | "datos" | "versionBase" | "ocurridoEn"> & {
        tipo: T;
        versionBase: number;
        ocurridoEn: string;
        datos: DatosPorTipo[T];
    };
}[TipoOperacion];
