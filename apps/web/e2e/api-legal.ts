import type { Schema } from "@iris/contratos";
import type { Route } from "@playwright/test";
import { PERMISO_CONFIGURAR_LEGAL, PERMISO_VER_LEGAL, type DatosLegales, type EntradaLegal } from "../lib/legal/datos";

/*
 * Datos legales del titular en la API falsa (`.claude/integration/contratos/legal.md`), tipada con
 * `packages/contratos`. Una sola fila (la de la instalación): exige operar sin límite de empresa
 * (403), guarda lo que venga aunque falten datos, calcula `faltan` y `completo` como el servidor,
 * y no sube la versión si nada cambió.
 */

type Problema = Schema<"Problema">;

export const PERMISOS_LEGAL = [PERMISO_VER_LEGAL, PERMISO_CONFIGURAR_LEGAL] as const;

type Campos = Omit<EntradaLegal, "version">;

export type OpcionesLegal = {
    /** Datos ya guardados. */
    guardados?: Partial<Campos>;
    /** El primer `GET` falla con 500. */
    fallaLeer?: boolean;
    /** El primer `guardar` responde 409. */
    conflicto?: boolean;
    /** El primer `guardar` responde 422 con ese campo. */
    rechaza?: { field: string; message: string };
};

export const LEGAL_COMPLETO: Campos = {
    razonSocial: "Redes del Sur, Sociedad Anónima",
    nombreComercial: "RedSur",
    correoPrivacidad: "privacidad@redsur.example",
    telefono: "+502 2222 3333",
    direccion: "5a avenida 1-23, zona 1, Ciudad de Guatemala",
    pais: "Guatemala",
};

const VACIO: Campos = { razonSocial: "", nombreComercial: "", correoPrivacidad: "", telefono: "", direccion: "", pais: "" };
const OBLIGATORIOS = ["razonSocial", "nombreComercial", "correoPrivacidad", "direccion", "pais"] as const;

const P = (codigo: string, status: number, title: string, extra: Partial<Problema> = {}): Problema => ({ type: `urn:iris:problema:${codigo}`, title, status, ...extra });
const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
const problema = (route: Route, p: Problema) => route.fulfill({ status: p.status, headers: { "Content-Type": "application/problem+json" }, body: JSON.stringify(p) });

type Pedido = { route: Route; metodo: string; ruta: string; cuerpo: Record<string, unknown>; idempotencia: string | undefined; puedeVer: boolean; puedeConfigurar: boolean; sinLimiteDeEmpresa: boolean };

export function apiLegal(opciones: OpcionesLegal | undefined) {
    let fila: (Campos & { version: number; actualizadoEn: string }) | null = opciones?.guardados ? { ...VACIO, ...opciones.guardados, version: 1, actualizadoEn: "2026-11-10T16:00:00Z" } : null;
    let fallaLeer = opciones?.fallaLeer ?? false;
    let conflicto = opciones?.conflicto ?? false;
    let rechaza = opciones?.rechaza;
    const guardados: { cuerpo: EntradaLegal; idempotencia: string | undefined }[] = [];

    function ver(): DatosLegales {
        const campos: Campos = fila ? { razonSocial: fila.razonSocial, nombreComercial: fila.nombreComercial, correoPrivacidad: fila.correoPrivacidad, telefono: fila.telefono, direccion: fila.direccion, pais: fila.pais } : VACIO;
        const faltan = OBLIGATORIOS.filter((c) => campos[c] === "");
        return { ...campos, configurado: fila !== null, version: fila?.version ?? 0, actualizadoEn: fila?.actualizadoEn ?? null, completo: faltan.length === 0, faltan };
    }

    async function manejar(x: Pedido): Promise<boolean> {
        const m = /^\/api\/v1\/datos-legales(\/guardar)?$/.exec(x.ruta);
        if (!m) return false;
        const { route, metodo } = x;
        const prohibido = () => problema(route, P("prohibido", 403, "Sin permiso"));
        if (!x.sinLimiteDeEmpresa) return (await prohibido(), true);
        if (!m[1] && metodo === "GET") {
            if (!x.puedeVer && !x.puedeConfigurar) return (await prohibido(), true);
            if (fallaLeer) {
                fallaLeer = false;
                return (await problema(route, P("error_interno", 500, "Error del servidor")), true);
            }
            return (await json(route, ver()), true);
        }
        if (!m[1] || metodo !== "POST") return false;
        if (!x.puedeConfigurar) return (await prohibido(), true);
        const entrada = x.cuerpo as unknown as EntradaLegal;
        guardados.push({ cuerpo: entrada, idempotencia: x.idempotencia });
        if (conflicto || entrada.version !== (fila?.version ?? 0)) {
            conflicto = false;
            return (await problema(route, P("conflicto_version", 409, "Conflicto de versión")), true);
        }
        if (rechaza) {
            const r = rechaza;
            rechaza = undefined;
            return (await problema(route, P("validacion", 422, "Datos inválidos", { errors: [{ field: r.field, message: r.message }] })), true);
        }
        const { version: _version, ...campos } = entrada;
        void _version;
        const igual = fila !== null && OBLIGATORIOS.every((c) => fila?.[c] === campos[c]) && fila.telefono === campos.telefono;
        if (!igual) fila = { ...campos, version: (fila?.version ?? 0) + 1, actualizadoEn: "2026-11-17T18:00:00Z" };
        return (await json(route, ver()), true);
    }

    return { manejar, guardados, ver };
}

export type ApiLegal = ReturnType<typeof apiLegal>;
