import type { Schema } from "@iris/contratos";
import type { Route } from "@playwright/test";
import type { ApiCentralita } from "./api-centralita";

/*
 * El teléfono de Iris (E4c) en la API falsa (e2e/api-falsa.ts), tipado con packages/contratos y fiel
 * a contratos/voz.md §3.12 «E4c»: mi teléfono, `abrir` (ticket y TURN), `llamar` (el destino SIP lo
 * pone el servidor), los destinos de «Transferir» y, en la centralita, la configuración del WSS,
 * los grupos y la credencial de cada extensión. El WebSocket SIP lo atiende e2e/sip-falso.ts.
 */

type TelefonoMio = Schema<"TelefonoMio">;
type SesionTelefono = Schema<"SesionTelefono">;
type Llamada = Schema<"Llamada">;
type Problema = Schema<"Problema">;
type Grupo = Schema<"GrupoCentralita">;
type PruebaTelefono = Schema<"PruebaTelefono">;
type PruebaRegistro = Schema<"PruebaRegistro">;

const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
const problema = (route: Route, p: Problema) => route.fulfill({ status: p.status, contentType: "application/problem+json", body: JSON.stringify(p) });
const P = (codigo: string, status: number, title: string): Problema => ({ type: `urn:iris:problema:${codigo}`, title, status });

export const DOMINIO_SIP = "pbx.oficina";
export const HUELLA_WSS = "a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90";
const BASE = "/api/v1/mensajeria";

export type OpcionesTelefono = {
    /** Mi teléfono (por defecto disponible, extensión 204, puede llamar y transferir). */
    mio?: Partial<TelefonoMio>;
    /** Lo que responde `abrir` (por defecto, la sesión). */
    abrir?: "ok" | "registro_sin_probar" | "demasiados_intentos";
    /** Lo que responde «Probar» el WSS. */
    prueba?: PruebaTelefono["estado"];
    /** Lo que responde «Probar registro». */
    registro?: PruebaRegistro["estado"];
    /** Teléfono de la empresa encendido al empezar (con la centralita conectada). */
    encendido?: boolean;
};

export const PERSONAS_TRANSFERIR = [
    { usuarioId: 14, nombre: "Carlos Méndez", extension: "205" },
    { usuarioId: 15, nombre: "Lucía Ramírez", extension: "206" },
];

export function apiTelefono(o: OpcionesTelefono, centralita: ApiCentralita) {
    let n = 0;
    const recibido = {
        abiertos: 0,
        llamadas: [] as unknown[],
        configuraciones: [] as unknown[],
        activaciones: 0,
        extensiones: [] as unknown[],
        registros: 0,
        grupos: [] as unknown[],
    };
    const grupos: Grupo[] = [{ id: 1, nombre: "Técnicos de guardia", numero: "600", activo: true, version: 1 }];
    let mio: TelefonoMio = {
        disponible: true,
        motivo: null,
        extension: "204",
        dispositivos: ["panel", "app"],
        puede: { llamar: true, transferir: true, recibir: true },
        limite: { porDia: 60, usadasHoy: 3 },
        ...o.mio,
    };
    if (o.encendido) centralita.telefono({ activo: true, huellaWss: HUELLA_WSS, probadoEn: new Date().toISOString(), restriccionesConfirmadasEn: new Date().toISOString() });

    function sesion(): SesionTelefono {
        n += 1;
        const ticket = `ticket-${n}-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx`;
        return {
            ws: `${BASE}/telefono/sip`,
            ticket,
            protocolos: ["sip", `iris-ticket.${ticket}`],
            ticketVence: new Date(Date.now() + 30_000).toISOString(),
            sip: { uri: `sip:204@${DOMINIO_SIP}`, usuario: "204", dominio: DOMINIO_SIP, nombre: "Ana López" },
            // Sin servidores TURN en la prueba: el navegador no junta candidatos (no hay red de verdad).
            ice: [],
            politicaIce: "relay",
            iceVence: new Date(Date.now() + 20 * 60_000).toISOString(),
        };
    }

    async function manejar({ route, metodo, ruta, cuerpo }: { route: Route; metodo: string; ruta: string; cuerpo: unknown }): Promise<boolean> {
        const c = (cuerpo ?? {}) as Record<string, unknown>;
        if (metodo === "GET" && ruta === `${BASE}/telefono`) return (await json(route, mio), true);
        if (metodo === "POST" && ruta === `${BASE}/telefono/abrir`) {
            recibido.abiertos += 1;
            if (o.abrir === "registro_sin_probar") return (await problema(route, P("registro_sin_probar", 422, "La centralita no aceptó la contraseña")), true);
            if (o.abrir === "demasiados_intentos") return (await problema(route, P("demasiados_intentos", 429, "Demasiados intentos")), true);
            return (await json(route, sesion(), 201), true);
        }
        if (metodo === "POST" && ruta === `${BASE}/telefono/llamar`) {
            recibido.llamadas.push(c);
            if (typeof c.numero === "string") {
                const d = c.numero.replace(/\D/g, "");
                if (/^[0-9]{2,6}$/.test(d)) return (await json(route, { destino: `sip:${d}@${DOMINIO_SIP}`, vence: new Date(Date.now() + 60_000).toISOString() }, 201), true);
                if (!/^[2-7][0-9]{7}$/.test(d.length === 11 ? d.slice(3) : d)) return (await problema(route, P("destino_no_permitido", 422, "Ese número no se puede marcar")), true);
            }
            const id = `0193f0aa-0000-7000-8000-0000000001${String(recibido.llamadas.length).padStart(2, "0")}`;
            const llamada: Llamada = {
                id,
                empresaId: 1,
                origen: "centralita",
                direccion: "saliente",
                numero: typeof c.numero === "string" ? `+502${c.numero.replace(/\D/g, "").slice(-8)}` : "+50255123480",
                titulo: c.clienteId || c.ticketId ? "María Fernanda Pérez López" : "",
                contacto: c.clienteId || c.ticketId ? { tipo: "cliente", clienteId: 4101, servicioId: 41010 } : { tipo: "desconocido" },
                estado: "marcando",
                tramos: [],
                sonoEn: new Date().toISOString(),
                puede: { contestar: false, rechazar: false, colgar: true, tomar: false, transferir: false, anotar: true, escucharGrabacion: false },
                version: 1,
                ...(typeof c.ticketId === "number" ? { ticketId: c.ticketId } : {}),
            };
            centralita.llamadas.unshift(llamada);
            mio = { ...mio, limite: { ...mio.limite, usadasHoy: mio.limite.usadasHoy + 1 } };
            return (await json(route, { destino: `sip:iris-${id}@${DOMINIO_SIP}`, llamada, vence: new Date(Date.now() + 60_000).toISOString() }, 201), true);
        }
        if (metodo === "GET" && ruta === `${BASE}/telefono/transferir/destinos`) {
            return (await json(route, { personas: PERSONAS_TRANSFERIR, grupos: grupos.filter((g) => g.activo).map(({ id, nombre, numero }) => ({ id, nombre, numero })) }), true);
        }

        // Administración (centralita.configurar / centralita.asignar_extension).
        const adm = /^\/api\/v1\/mensajeria\/centralitas\/(\d+)\/(telefono|telefono\/probar|telefono\/activar|telefono\/desactivar|grupos|grupos\/(\d+)\/actualizar|extensiones\/(\d+)\/telefono|extensiones\/(\d+)\/probar-registro)$/.exec(ruta);
        if (!adm) return false;
        const cen = centralita.centralita();
        if (!cen) return (await problema(route, P("no_encontrado", 404, "La centralita no existe")), true);
        const sub = adm[2] ?? "";
        if (sub === "telefono" && metodo === "POST") {
            recibido.configuraciones.push(c);
            const antes = cen.telefono;
            const cambia = !antes || antes.wssPuerto !== c.wssPuerto || antes.wssRuta !== c.wssRuta || antes.huellaWss !== c.huellaWss;
            centralita.telefono({
                wssPuerto: Number(c.wssPuerto),
                wssRuta: String(c.wssRuta),
                huellaWss: String(c.huellaWss),
                sipDominio: typeof c.sipDominio === "string" ? c.sipDominio : null,
                prefijoSalida: String(c.prefijoSalida ?? ""),
                llamadasPorUsuarioDia: Number(c.llamadasPorUsuarioDia),
                llamadasSimultaneas: Number(c.llamadasSimultaneas),
                restriccionesConfirmadasEn: c.confirmoRestricciones ? (antes?.restriccionesConfirmadasEn ?? new Date().toISOString()) : null,
                ...(cambia ? { activo: false, probadoEn: null } : {}),
                ...(c.confirmoRestricciones ? {} : { activo: false }),
            });
            return (await json(route, centralita.centralita()), true);
        }
        if (sub === "telefono/probar") {
            const estado = o.prueba ?? "ok";
            if (estado === "ok" && !c.wssPuerto && cen.telefono?.huellaWss) centralita.telefono({ probadoEn: new Date().toISOString() });
            const r: PruebaTelefono = { estado, huella: estado === "ok" || estado === "huella_distinta" ? HUELLA_WSS : null, ...(c.wssPuerto ? {} : { centralita: centralita.centralita() ?? undefined }) };
            return (await json(route, r), true);
        }
        if (sub === "telefono/activar") {
            const t = cen.telefono;
            if (!t?.probadoEn) return (await problema(route, P("sin_probar", 422, "Prueba el teléfono antes de encenderlo")), true);
            if (!t.restriccionesConfirmadasEn) return (await problema(route, P("restricciones_sin_confirmar", 422, "Confirma la guía de la Issabel")), true);
            recibido.activaciones += 1;
            centralita.telefono({ activo: true });
            return (await json(route, centralita.centralita()), true);
        }
        if (sub === "telefono/desactivar") {
            centralita.telefono({ activo: false });
            return (await json(route, centralita.centralita()), true);
        }
        if (sub === "grupos" && metodo === "GET") return (await json(route, { data: grupos }), true);
        if (sub === "grupos" && metodo === "POST") {
            recibido.grupos.push(c);
            if (grupos.some((g) => g.activo && g.numero === c.numero)) return (await problema(route, P("grupo_existente", 409, "Ya hay un grupo con ese número")), true);
            const g: Grupo = { id: grupos.length + 1, nombre: String(c.nombre), numero: String(c.numero), activo: true, version: 1 };
            grupos.push(g);
            return (await json(route, g, 201), true);
        }
        if (adm[3]) {
            const g = grupos.find((x) => x.id === Number(adm[3]));
            if (!g) return (await problema(route, P("no_encontrado", 404, "El grupo no existe")), true);
            Object.assign(g, { nombre: String(c.nombre), numero: String(c.numero), activo: Boolean(c.activo), version: g.version + 1 });
            return (await json(route, g), true);
        }
        const extId = Number(adm[4] ?? adm[5]);
        const x = centralita.extensiones.find((e) => e.id === extId);
        if (!x) return (await problema(route, P("no_encontrado", 404, "La extensión no existe")), true);
        if (adm[4]) {
            recibido.extensiones.push(c);
            if (c.tipo === "navegador") {
                const usuario = typeof c.sipUsuario === "string" ? c.sipUsuario : x.extension;
                if (typeof c.sipSecreto !== "string" && (!x.sipGuardadoEn || usuario !== x.sipUsuario)) return (await problema(route, P("clave_requerida", 422, "Escribe la contraseña de la extensión")), true);
                Object.assign(x, { tipo: "navegador", sipUsuario: usuario, sipGuardadoEn: typeof c.sipSecreto === "string" ? new Date().toISOString() : x.sipGuardadoEn, sipProbadoEn: null });
            } else {
                Object.assign(x, { tipo: "escritorio", sipUsuario: null, sipGuardadoEn: null, sipProbadoEn: null, sipRechazadoEn: null });
            }
            return (await json(route, x), true);
        }
        // Como el servidor: la contraseña que la Issabel ya rechazó no se vuelve a probar (429).
        if (x.sipRechazadoEn && (!x.sipGuardadoEn || Date.parse(x.sipGuardadoEn) <= Date.parse(x.sipRechazadoEn))) {
            return (await problema(route, P("credencial_rechazada", 429, "La centralita ya rechazó esta contraseña")), true);
        }
        recibido.registros += 1;
        const estado = !x.sipGuardadoEn ? "sin_credencial" : (o.registro ?? "ok");
        if (estado === "ok") Object.assign(x, { sipProbadoEn: new Date().toISOString(), sipRechazadoEn: null });
        if (estado === "credenciales") Object.assign(x, { sipProbadoEn: null, sipRechazadoEn: new Date().toISOString() });
        return (await json(route, { estado, extension: x } satisfies PruebaRegistro), true);
    }

    return {
        manejar,
        recibido,
        grupos,
        cambiarMio: (cambio: Partial<TelefonoMio>) => {
            mio = { ...mio, ...cambio };
        },
    };
}

export type ApiTelefono = ReturnType<typeof apiTelefono>;
