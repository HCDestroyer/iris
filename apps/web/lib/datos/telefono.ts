import type { Schema } from "@iris/contratos";

/*
 * Espejo de apps/api/internal/integracion/argos/formato (Go): mismo resultado para los mismos casos
 * (testdata/telefonos.json, que prueban los dos lados). Los datos de Argos ya llegan normalizados
 * desde la API (`telefonos`, `nitCuiMostrar`); esto es para lo que se captura en Iris (contacto de
 * un ticket u orden, prospectos) y así todo se muestra igual.
 */

export type Telefono = Schema<"Telefono">;
export type TipoTelefono = Telefono["tipo"];

const SEPARADORES = /\s*(?:[/,;|]|\s+y\s+|\s+o\s+)\s*/;
const PERMITIDOS = /^[\d\s+\-().]+$/;

/** Parte un campo de texto libre en sus números (puede traer varios) y normaliza cada uno. */
export function telefonos(tipo: TipoTelefono, texto: string | null | undefined): Telefono[] {
    return (texto ?? "")
        .trim()
        .split(SEPARADORES)
        .map((parte) => parte.trim().replace(/\s+/g, " "))
        .filter((parte) => parte !== "")
        .map((original) => {
            const n = normalizarTelefono(original);
            return { tipo, original, e164: n?.e164 ?? null, mostrar: n?.mostrar ?? original };
        });
}

/**
 * Guatemala (+502): 8 dígitos que empiezan en 2–7, con o sin 502, +502 o 00502 delante; se muestra
 * «+502 5512 3344». Otro país: solo con + o 00 y de 8 a 15 dígitos; se muestra como vino.
 * Cualquier otra cosa: null.
 */
export function normalizarTelefono(texto: string): { e164: string; mostrar: string } | null {
    const t = texto.trim();
    if (t === "" || !PERMITIDOS.test(t)) return null;
    let internacional = t.startsWith("+");
    let digitos = t.replace(/\D/g, "");
    if (!internacional && digitos.startsWith("00")) {
        internacional = true;
        digitos = digitos.slice(2);
    }
    const mas = (t.match(/\+/g) ?? []).length;
    if (mas > 1 || (mas === 1 && !internacional)) return null;
    let local: string;
    if (digitos.length === 8 && !internacional) {
        local = digitos;
    } else if (digitos.length === 11 && digitos.startsWith("502")) {
        local = digitos.slice(3);
    } else if (internacional && !digitos.startsWith("502") && digitos.length >= 8 && digitos.length <= 15 && !digitos.startsWith("0")) {
        return { e164: `+${digitos}`, mostrar: t.replace(/\s+/g, " ") };
    } else {
        return null;
    }
    const primero = local.charAt(0);
    if (primero < "2" || primero > "7") return null;
    return { e164: `+502${local}`, mostrar: `+502 ${local.slice(0, 4)} ${local.slice(4)}` };
}

/** El número para contactar: el primer móvil interpretable, si no el primer teléfono interpretable y, si ninguno, el primero. */
export function principal(lista: readonly Telefono[]): Telefono | null {
    for (const tipo of ["movil", "telefono"] as const) {
        const t = lista.find((x) => x.tipo === tipo && x.e164 !== null);
        if (t) return t;
    }
    return lista[0] ?? null;
}

/** Documento fiscal para mostrar: NIT 1234567-8, CUI 1234 56789 0101, CF; lo demás tal cual. */
export function documento(tipo: string, numero: string | null | undefined): string {
    const n = (numero ?? "").trim().toUpperCase();
    if (n === "") return "";
    if (tipo === "cf" || n === "CF") return "CF";
    if (tipo === "cui" && /^\d{13}$/.test(n)) return `${n.slice(0, 4)} ${n.slice(4, 9)} ${n.slice(9)}`;
    if (tipo === "nit" && /^\d{1,12}[\dK]$/.test(n)) return `${n.slice(0, -1)}-${n.slice(-1)}`;
    return n;
}

/** Enlace `tel:` para un número (E.164 si se pudo interpretar; si no, los dígitos que tenga). */
export function enlaceTel(t: Pick<Telefono, "e164" | "original">): string | null {
    if (t.e164) return `tel:${t.e164}`;
    const crudo = t.original.replace(/[^\d+]/g, "");
    return crudo.length >= 3 ? `tel:${crudo}` : null;
}
