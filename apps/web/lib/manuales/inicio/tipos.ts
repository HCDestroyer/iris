import type { Schema } from "@iris/contratos";

/** Tecnología a la que aplica un manual de inicio. */
export type TecnologiaManual = "general" | "ftth" | "wisp";

/**
 * Manual de inicio: un ejemplo editable que el administrador carga en la biblioteca con «Cargar
 * manuales de inicio». `codigo` es estable (nunca se renombra) y, con `revision`, arma la
 * `Idempotency-Key` de su alta: si el texto cambia, sube la revisión.
 */
export type PlantillaManual = {
    codigo: string;
    revision: number;
    tecnologia: TecnologiaManual;
    titulo: string;
    resumen: string;
    /** Markdown del subconjunto seguro de `componentes/manuales/markdown.tsx`. */
    cuerpo: string;
    /** En minúsculas; la primera es la tecnología (`general`, `ftth` o `wisp`). */
    etiquetas: string[];
};

/** Cuerpo de `POST /mensajeria/manuales`. */
export type EntradaManual = Schema<"EntradaManual">;
