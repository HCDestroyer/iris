import type { Schema } from "@iris/contratos";
import type { Page, Route } from "@playwright/test";
import { ADMIN, CONSULTA, CSRF, type Peticion } from "./api-falsa";

/*
 * API falsa de los tokens de API (identidad: listar, revocar, eliminar y el lote). Se registra
 * DESPUÉS de simularApi: lo que no es de aquí sigue con route.fallback() a la API falsa general.
 * Las respuestas van tipadas con packages/contratos.
 */

type TokenAPI = Schema<"TokenAPI">;

const AHORA = Date.now();
const iso = (min: number) => new Date(AHORA + min * 60_000).toISOString();

const token = (id: number, nombre: string, extra: Partial<TokenAPI> = {}): TokenAPI => ({
    id,
    usuarioId: ADMIN.id,
    nombre,
    prefijo: `iris_pat_${String(id).padStart(4, "0")}`,
    permisos: null,
    expiraEn: null,
    ultimoUso: iso(-120),
    revocadoEn: null,
    creadoPor: ADMIN.id,
    creadoEn: iso(-60 * 24 * 30),
    enUsoPorConector: false,
    ...extra,
});

export function tokensIniciales(): TokenAPI[] {
    return [
        token(1, "Script de reportes", { expiraEn: iso(60 * 24 * 40) }),
        token(2, "Bot de Horus 2025", { usuarioId: CONSULTA.id, revocadoEn: iso(-60 * 24 * 3) }),
        token(3, "Prueba vieja", { expiraEn: iso(-60 * 24), ultimoUso: null }),
        token(4, "Argos (canje)", { revocadoEn: iso(-60), enUsoPorConector: true }),
        token(5, "Argos (conector activo)", { enUsoPorConector: true }),
    ];
}

export type ApiTokens = {
    tokens: TokenAPI[];
    peticiones: Peticion[];
};

function json(route: Route, cuerpo: unknown, status = 200) {
    return route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
}

function problema(route: Route, status: number, codigo: string, detail: string) {
    return route.fulfill({
        status,
        contentType: "application/problem+json",
        body: JSON.stringify({ type: `urn:iris:problema:${codigo}`, title: codigo, status, detail } satisfies Schema<"Problema">),
    });
}

const inactivo = (t: TokenAPI) => t.revocadoEn !== null || (t.expiraEn !== null && Date.parse(t.expiraEn) <= Date.now());

/** Registra la API falsa de tokens (después de simularApi). */
export async function simularTokens(page: Page): Promise<ApiTokens> {
    const estado: ApiTokens = { tokens: tokensIniciales(), peticiones: [] };
    const eliminados = new Set<number>();
    const vivos = () => estado.tokens.filter((t) => !eliminados.has(t.id));
    const elegibles = () => vivos().filter((t) => inactivo(t) && !t.enUsoPorConector);

    await page.route("**/api/v1/**", async (route) => {
        const req = route.request();
        const url = new URL(req.url());
        const ruta = url.pathname;
        const metodo = req.method();
        const cabeceras = await req.allHeaders();
        if (!ruta.startsWith("/api/v1/tokens")) return route.fallback();
        if (metodo !== "GET" && cabeceras["x-csrf-token"] !== CSRF) return route.fallback();
        let cuerpo: unknown = undefined;
        try {
            cuerpo = req.postDataJSON();
        } catch {
            cuerpo = undefined;
        }
        estado.peticiones.push({ metodo, ruta, query: url.searchParams, empresa: cabeceras["x-iris-empresa"], idempotencia: cabeceras["idempotency-key"], cuerpo });

        if (metodo === "GET" && ruta === "/api/v1/tokens") {
            const vigentes = url.searchParams.get("vigentes") === "true";
            const data = vivos().filter((t) => !vigentes || !inactivo(t));
            return json(route, { data, nextCursor: null } satisfies Schema<"PaginaTokenAPI">);
        }
        if (metodo === "GET" && ruta === "/api/v1/tokens/eliminables") {
            const enUso = vivos().filter((t) => inactivo(t) && t.enUsoPorConector).length;
            return json(route, { cantidad: elegibles().length, enUso } satisfies Schema<"TokensEliminables">);
        }
        if (metodo === "POST" && ruta === "/api/v1/tokens/eliminar-inactivos") {
            const ids = elegibles().map((t) => t.id);
            const omitidos = vivos().filter((t) => inactivo(t) && t.enUsoPorConector).length;
            for (const id of ids) eliminados.add(id);
            return json(route, { eliminados: ids.length, omitidos, ids } satisfies Schema<"TokensEliminados">);
        }
        const accion = /^\/api\/v1\/tokens\/(\d+)\/(revocar|eliminar|revocar-y-eliminar)$/.exec(ruta);
        if (metodo === "POST" && accion) {
            const t = estado.tokens.find((x) => x.id === Number(accion[1]));
            if (!t) return problema(route, 404, "no_encontrado", "No existe");
            if (eliminados.has(t.id)) {
                if (accion[2] === "revocar") return problema(route, 404, "no_encontrado", "No existe");
                return json(route, { ...t, eliminadoEn: new Date().toISOString() });
            }
            // El de un conector activo no se toca (ni revocar): cortaría la integración.
            if (t.enUsoPorConector) {
                return problema(route, 409, "token_en_uso_por_conector", "Un conector activo de Argos u Horus usa este token. Desactiva o reemplaza el conector en Integraciones antes de eliminarlo");
            }
            if (accion[2] === "revocar-y-eliminar") {
                t.revocadoEn ??= new Date().toISOString();
                eliminados.add(t.id);
                return json(route, { ...t, eliminadoEn: new Date().toISOString() });
            }
            if (accion[2] === "revocar") {
                if (t.revocadoEn) return problema(route, 422, "token_revocado", "El token ya está revocado");
                t.revocadoEn = new Date().toISOString();
                return json(route, t);
            }
            if (!inactivo(t)) return problema(route, 409, "token_vigente", "El token aún vale: revócalo primero y luego elimínalo");
            eliminados.add(t.id);
            return json(route, { ...t, eliminadoEn: new Date().toISOString() });
        }
        return route.fallback();
    });
    return estado;
}
