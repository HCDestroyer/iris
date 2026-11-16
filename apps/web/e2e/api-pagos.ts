import type { Schema } from "@iris/contratos";
import type { Route } from "@playwright/test";

/*
 * Boletas que se aprueban o rechazan desde la conversación y autologin Iris → Argos en la API
 * falsa (pedido B3; contratos/conversaciones.md §6.4 y contrato con Argos 0.7.0 §7.7), tipado con
 * packages/contratos: `GET …/conversaciones/{id}/boletas`, `GET /boletas/{id}`, `aprobar`,
 * `rechazar` y `POST /integraciones/argosisp/abrir` (enlace de un solo uso; repetir la clave: 409).
 */

type Problema = Schema<"Problema">;
type Permiso = Schema<"Permiso">;
type Boleta = Schema<"Boleta">;

const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
const problema = (route: Route, p: Problema) => route.fulfill({ status: p.status, contentType: "application/problem+json", body: JSON.stringify(p) });
const P = (codigo: string, status: number, title: string, extra: Partial<Problema> = {}): Problema => ({ type: `urn:iris:problema:${codigo}`, title, status, ...extra });

/** `boletas.*` solo los tienen `admin` y `caja`; quien atiende la bandeja solo puede abrir Argos. */
export const PERMISOS_PAGOS: Permiso[] = ["boletas.aprobar", "boletas.rechazar", "integraciones.abrir_argos"];
export const PERMISOS_ABRIR_ARGOS: Permiso[] = ["integraciones.abrir_argos"];

type CodigoDecidir = "argos_sin_permiso" | "argos_sin_empresa" | "argos_usuario_inactivo" | "otra_persona_decidio" | "argos_no_disponible" | "argos_sin_aprobacion" | "monto_cambio" | "comprobante_no_es_de_iris";
type CodigoAbrir = "conector_apagado" | "argos_no_disponible" | "demasiados_intentos";

export type OpcionesPagos = {
    /** Quien pulsa no tiene su usuario de Argos vinculado: 409 `argos_sin_vinculo` hasta que abre Argos («Vincular»). */
    sinVinculo?: boolean;
    /** El primer aprobar o rechazar responde este error. */
    errorDecidir?: CodigoDecidir;
    /** El primer `POST /integraciones/argosisp/abrir` responde este error. */
    errorAbrir?: CodigoAbrir;
    /** Permisos que el admin **no** tiene (por defecto tiene los tres de `PERMISOS_PAGOS`). */
    sinPermisos?: Permiso[];
    /** `GET …/boletas` falla la primera vez (500). */
    lecturaFalla?: boolean;
    /** `GET /boletas/{id}` (la relectura tras un 409) falla esta cantidad de veces (500). */
    relecturaFalla?: number;
    /** El enlace que devuelve `abrir` no es del panel de Argos del conector (otro sitio, o `http` por la red). */
    enlace?: "otro_sitio" | "http";
    /** Sesión abierta desde Argos: `abrir` responde 403 `reautenticacion_requerida` hasta confirmar la contraseña. */
    sesionSso?: boolean;
    /** La boleta pendiente trae este `confirmacionArgos`. */
    confirmacionArgos?: "pendiente" | "agotada";
};

const ESTADO_DE: Record<CodigoDecidir, number> = {
    argos_sin_permiso: 403,
    argos_sin_empresa: 403,
    argos_usuario_inactivo: 403,
    otra_persona_decidio: 409,
    argos_no_disponible: 502,
    argos_sin_aprobacion: 422,
    monto_cambio: 409,
    comprobante_no_es_de_iris: 409,
};

export const BOLETA_PENDIENTE = "0193d000-0000-7000-8000-000000000001";
export const URL_ARGOS = "https://argos.isp.gt";

const boleta = (datos: Partial<Boleta> & Pick<Boleta, "id" | "referencia" | "monto">): Boleta => ({
    conversacionId: 42,
    comprobanteId: 9001,
    creadaEn: "2026-10-28T16:30:00Z",
    estado: "registrada",
    detalle: null,
    puedeDecidir: true,
    pagoId: null,
    motivoRechazo: null,
    causaRechazo: null,
    verificadoPorNombre: null,
    decididaEn: null,
    avisoCliente: null,
    confirmacionArgos: null,
    ...datos,
});

export function apiPagos(actor: () => { id: number; nombre: string }, opciones: OpcionesPagos | undefined, reautenticado: () => boolean = () => true) {
    const activo = opciones !== undefined;
    const boletas: Boleta[] = activo
        ? [
              boleta({ id: BOLETA_PENDIENTE, referencia: "BI-778812", monto: "250.00", confirmacionArgos: opciones.confirmacionArgos ?? null }),
              boleta({
                  id: "0193d000-0000-7000-8000-000000000002",
                  referencia: "BI-701144",
                  monto: "199.00",
                  comprobanteId: 8990,
                  creadaEn: "2026-09-28T16:30:00Z",
                  estado: "aprobada",
                  puedeDecidir: false,
                  pagoId: 55001,
                  verificadoPorNombre: "Marta Caal",
                  decididaEn: "2026-09-28T17:02:00Z",
                  avisoCliente: "enviado",
              }),
          ]
        : [];
    let vinculado = !opciones?.sinVinculo;
    let errorDecidir = opciones?.errorDecidir;
    let errorAbrir = opciones?.errorAbrir;
    let lecturaFalla = opciones?.lecturaFalla ?? false;
    let relecturaFalla = opciones?.relecturaFalla ?? 0;
    const decisiones: { ruta: string; cuerpo: unknown; idempotencia: string | undefined }[] = [];
    const aperturas: { cuerpo: unknown; idempotencia: string | undefined }[] = [];
    const clavesAbrir = new Set<string>();
    let rechazadasPorSesion = 0;

    async function manejar(x: { route: Route; metodo: string; ruta: string; cuerpo: unknown; idempotencia: string | undefined; puede: (p: Permiso) => boolean }): Promise<boolean> {
        const { route, metodo, ruta, idempotencia, puede } = x;
        const cuerpo = (x.cuerpo ?? {}) as Record<string, unknown>;
        const prohibido = async () => (await problema(route, P("permiso_denegado", 403, "Sin permiso")), true);

        if (ruta === "/api/v1/integraciones/argosisp/abrir" && metodo === "POST") {
            if (!puede("integraciones.abrir_argos")) return prohibido();
            if (opciones?.sesionSso && !reautenticado()) {
                rechazadasPorSesion += 1;
                return (await problema(route, P("reautenticacion_requerida", 403, "Re-autenticación requerida", { detail: "Entraste desde Argos: confirma tu contraseña antes de esta acción" })), true);
            }
            aperturas.push({ cuerpo, idempotencia });
            if (idempotencia && clavesAbrir.has(idempotencia)) return (await problema(route, P("idempotencia_no_repetible", 409, "Pide otro enlace con otra clave")), true);
            if (idempotencia) clavesAbrir.add(idempotencia);
            if (errorAbrir) {
                const codigo = errorAbrir;
                errorAbrir = undefined;
                if (codigo === "demasiados_intentos") {
                    return (await route.fulfill({ status: 429, headers: { "Content-Type": "application/problem+json", "Retry-After": "40" }, body: JSON.stringify(P(codigo, 429, "Demasiados intentos")) }), true);
                }
                return (await problema(route, P(codigo, codigo === "conector_apagado" ? 409 : 502, codigo === "conector_apagado" ? "El conector con Argos está apagado" : "Argos no respondió")), true);
            }
            // Sin destino es «Vincular»: Argos pide el correo y la contraseña y crea el vínculo.
            if (!cuerpo.destino) vinculado = true;
            const destino = cuerpo.destino as { tipo: string; idExterno: string } | undefined;
            const base = opciones?.enlace === "otro_sitio" ? "https://argos.isp.gt.otro.sitio" : opciones?.enlace === "http" ? URL_ARGOS.replace("https:", "http:") : URL_ARGOS;
            return (await json(route, { url: `${base}/sso/entrar#ticket-${aperturas.length}${destino ? `-${destino.tipo}-${destino.idExterno}` : ""}`, expiraEn: new Date(Date.now() + 60_000).toISOString() }), true);
        }

        const lista = /^\/api\/v1\/mensajeria\/conversaciones\/(\d+)\/boletas$/.exec(ruta);
        if (lista && metodo === "GET") {
            if (!puede("conversaciones.ver")) return prohibido();
            if (lecturaFalla) {
                lecturaFalla = false;
                return (await problema(route, P("error_interno", 500, "Error del servidor")), true);
            }
            return (await json(route, { data: boletas.filter((b) => b.conversacionId === Number(lista[1])), nextCursor: null }), true);
        }

        const m = /^\/api\/v1\/mensajeria\/boletas\/([^/]+)(?:\/(aprobar|rechazar))?$/.exec(ruta);
        if (!m) return false;
        const b = boletas.find((v) => v.id === m[1]);
        if (!b) return (await problema(route, P("no_encontrado", 404, "No existe")), true);
        if (!m[2]) {
            if (metodo !== "GET") return false;
            if (relecturaFalla > 0) {
                relecturaFalla -= 1;
                return (await problema(route, P("error_interno", 500, "Error del servidor")), true);
            }
            return (await json(route, b), true);
        }
        if (metodo !== "POST") return false;
        if (!puede(m[2] === "aprobar" ? "boletas.aprobar" : "boletas.rechazar")) return prohibido();
        decisiones.push({ ruta, cuerpo, idempotencia });
        if (!vinculado) return (await problema(route, P("argos_sin_vinculo", 409, "Vincula tu usuario de Argos")), true);
        if (errorDecidir) {
            const codigo = errorDecidir;
            errorDecidir = undefined;
            if (codigo === "otra_persona_decidio") {
                // La boleta local queda sincronizada con lo que decidió caja.
                Object.assign(b, { estado: "aprobada", puedeDecidir: false, pagoId: 55010, verificadoPorNombre: "Marta Caal", decididaEn: new Date().toISOString(), avisoCliente: "enviado" } satisfies Partial<Boleta>);
            }
            // Caja corrigió el monto del comprobante en Argos mientras el diálogo estaba abierto.
            if (codigo === "monto_cambio") b.monto = "275.00";
            return (await problema(route, P(codigo, ESTADO_DE[codigo], codigo === "argos_no_disponible" ? "Argos no respondió" : "No se pudo decidir")), true);
        }
        if (b.estado !== "registrada") return (await problema(route, P("boleta_ya_decidida", 409, "La boleta ya está decidida")), true);
        // El monto que la persona confirmó ya no es el de la boleta: no se aprueba nada.
        if (m[2] === "aprobar" && cuerpo.montoEsperado !== undefined && cuerpo.montoEsperado !== b.monto) {
            return (await problema(route, { ...P("monto_cambio", 409, "El monto de la boleta cambió"), montoActual: b.monto } as Problema), true);
        }
        if (m[2] === "rechazar" && String(cuerpo.motivo ?? "").trim().length < 3) {
            return (await problema(route, P("validacion", 422, "Datos inválidos", { errors: [{ field: "body.motivo", message: "Entre 3 y 500 caracteres" }] })), true);
        }
        const yo = actor();
        Object.assign(
            b,
            {
                estado: m[2] === "aprobar" ? "aprobada" : "rechazada",
                puedeDecidir: false,
                pagoId: m[2] === "aprobar" ? 55020 : null,
                motivoRechazo: m[2] === "rechazar" ? String(cuerpo.motivo) : null,
                causaRechazo: m[2] === "rechazar" ? ((cuerpo.causa as Boleta["causaRechazo"]) ?? "otro") : null,
                verificadoPorNombre: `${yo.nombre} (Argos)`,
                decididaPor: { id: yo.id, nombre: yo.nombre },
                decididaEn: new Date().toISOString(),
                avisoCliente: "enviado",
                confirmacionArgos: null,
            } satisfies Partial<Boleta>,
        );
        return (await json(route, b), true);
    }

    return { manejar, boletas, decisiones, aperturas, activo, sinPermisos: opciones?.sinPermisos ?? [], rechazadasPorSesion: () => rechazadasPorSesion };
}

export type ApiPagos = ReturnType<typeof apiPagos>;
