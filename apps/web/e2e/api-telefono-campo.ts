import type { Schema } from "@iris/contratos";
import type { Page, Route } from "@playwright/test";
import type { ApiCampo } from "./api-campo";

/*
 * El teléfono de Iris para la app de campo (E4c fase 3, contratos/voz.md §3.12), encima de la API
 * falsa de campo (e2e/api-campo.ts): mi teléfono, `abrir` (con `dispositivo: app`), `llamar` desde la
 * orden y las llamadas de la orden (las de la empresa y las directas que anotó el lote). Se registra
 * después de `simularApiCampo` (la ruta más nueva atiende primero). El WebSocket lo atiende
 * e2e/sip-falso.ts.
 */

type TelefonoMio = Schema<"TelefonoMio">;
type Llamada = Schema<"Llamada">;
type Problema = Schema<"Problema">;

const BASE = "/api/v1/mensajeria";
const DOMINIO = "pbx.oficina";

export type OpcionesTelefonoCampo = {
    mio?: Partial<TelefonoMio>;
    /** `POST /telefono/llamar` responde 422 `limite_llamadas`. */
    limite?: boolean;
};

export type TelefonoCampo = {
    abiertos: { dispositivo: string }[];
    llamadas: Record<string, unknown>[];
    /** Las llamadas por la empresa como las ve el servidor (la prueba marca la contestada). */
    filas: Llamada[];
};

const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
const problema = (route: Route, p: Problema) => route.fulfill({ status: p.status, contentType: "application/problem+json", body: JSON.stringify(p) });

export async function simularTelefonoCampo(page: Page, campo: ApiCampo, o: OpcionesTelefonoCampo = {}): Promise<TelefonoCampo> {
    const t: TelefonoCampo = { abiertos: [], llamadas: [], filas: [] };
    const mio: TelefonoMio = {
        disponible: true,
        motivo: null,
        extension: "311",
        dispositivos: ["panel", "app"],
        puede: { llamar: true, transferir: false, recibir: true },
        limite: { porDia: 60, usadasHoy: 2 },
        ...o.mio,
    };
    let n = 0;
    await page.route(`**${BASE}/**`, async (route) => {
        if (campo.sinRed) return route.abort("internetdisconnected");
        const req = route.request();
        const url = new URL(req.url());
        const ruta = url.pathname;
        const metodo = req.method();
        if (metodo === "GET" && ruta === `${BASE}/telefono`) return json(route, mio);
        if (metodo === "POST" && ruta === `${BASE}/telefono/abrir`) {
            const c = req.postDataJSON() as { dispositivo: string };
            t.abiertos.push(c);
            n += 1;
            const ticket = `ticket-${n}-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx`;
            const sesion: Schema<"SesionTelefono"> = {
                ws: `${BASE}/telefono/sip`,
                ticket,
                protocolos: ["sip", `iris-ticket.${ticket}`],
                ticketVence: new Date(Date.now() + 30_000).toISOString(),
                sip: { uri: `sip:311@${DOMINIO}`, usuario: "311", dominio: DOMINIO, nombre: "Tomás Herrera" },
                ice: [],
                politicaIce: "relay",
                iceVence: new Date(Date.now() + 20 * 60_000).toISOString(),
            };
            return json(route, sesion, 201);
        }
        if (metodo === "POST" && ruta === `${BASE}/telefono/llamar`) {
            const c = req.postDataJSON() as Record<string, unknown>;
            t.llamadas.push(c);
            if (o.limite) return problema(route, { type: "urn:iris:problema:limite_llamadas", title: "Llegaste al límite de llamadas de hoy", status: 422 });
            const id = `0193f0aa-0000-7000-8000-0000000002${String(t.llamadas.length).padStart(2, "0")}`;
            const llamada = {
                id,
                empresaId: 1,
                origen: "centralita",
                direccion: "saliente",
                numero: "+50255551234",
                titulo: "María Castillo",
                contacto: { tipo: "cliente", clienteId: 3101, servicioId: 4101 },
                estado: "marcando",
                tramos: [],
                sonoEn: new Date().toISOString(),
                ordenId: typeof c.ordenId === "number" ? c.ordenId : undefined,
                puede: { contestar: false, rechazar: false, colgar: true, tomar: false, transferir: false, anotar: true, escucharGrabacion: false },
                version: 1,
            } as Llamada;
            t.filas.unshift(llamada);
            return json(route, { destino: `sip:iris-${id}@${DOMINIO}`, llamada, vence: new Date(Date.now() + 60_000).toISOString() }, 201);
        }
        if (metodo === "GET" && ruta === `${BASE}/centralitas/llamadas`) {
            const orden = Number(url.searchParams.get("ordenId"));
            const directas = campo.llamadasDirectas
                .filter((op) => op.ordenId === orden)
                .map(
                    (op) =>
                        ({
                            id: op.id,
                            empresaId: 1,
                            origen: "celular",
                            direccion: "saliente",
                            numero: "+50255551234",
                            titulo: "",
                            contacto: { tipo: "desconocido" },
                            estado: "terminada",
                            motivoFin: "directa",
                            tramos: [],
                            sonoEn: op.ocurridoEn,
                            ordenId: orden,
                            puede: { contestar: false, rechazar: false, colgar: false, tomar: false, transferir: false, anotar: true, escucharGrabacion: false },
                            version: 1,
                        }) as Llamada,
                );
            return json(route, { data: [...t.filas.filter((l) => l.ordenId === orden), ...directas], nextCursor: null });
        }
        return problema(route, { type: "urn:iris:problema:no_simulado", title: `Sin simular: ${metodo} ${ruta}`, status: 501 });
    });
    return t;
}
