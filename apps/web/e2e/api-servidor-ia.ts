import type { Schema } from "@iris/contratos";
import type { Route } from "@playwright/test";
import type { AvisoServidor, EnlaceCreado, Gpu, Motor, Paquete, PasoPrueba, PuntoSerie, Rango, ResultadoPrueba, Tablero, Umbrales, UsoEmpresa } from "../lib/servidor-ia/tipos";
import { DE_FABRICA } from "../lib/servidor-ia/umbrales";

/*
 * Servidor de IA en la API falsa (contratos/voz.md §4.8, §4.11 y §4.12): la tarjeta del servidor
 * (`/voz/motores`: enlazar, conectar, activar, probar, rotar, empresas, desactivar), el tablero por
 * rango y los umbrales. Tipado con los tipos de `packages/contratos`.
 */

type Problema = Schema<"Problema">;
type Permiso = Schema<"Permiso">;

const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
const problema = (route: Route, p: Problema) => route.fulfill({ status: p.status, contentType: "application/problem+json", body: JSON.stringify(p) });
const P = (codigo: string, status: number, title: string, extra: Partial<Problema> = {}): Problema => ({ type: `urn:iris:problema:${codigo}`, title, status, ...extra });

export const PERMISO_SIA_VER: Permiso = "servidor_ia.ver";
export const PERMISO_SIA_VER_USO: Permiso = "servidor_ia.ver_uso";
export const PERMISO_SIA_ADMINISTRAR: Permiso = "servidor_ia.administrar";

/** El «ahora» del escenario: martes 17 de noviembre de 2026, 11:02 en Guatemala. Las pruebas ponen el reloj aquí. */
export const AHORA_SIA = Date.parse("2026-11-17T17:02:00Z");
export const CODIGO_ENLACE = "iv1_7Kq2mXw9Rt4Lp8Zc3Ns6Vb1Yh5Df0GjAe";
export const HUELLA = "3fa91c07b2de4418905ae6d17c33f0428be15a996c0d7e2413f8a05bd6e27c91";
export const HUELLA_OTRA = "9b04e2a17f3c5d6681aa0c42e7b913d05f6c2e8847a1b3c9d0e5f61728394a5b";
export const MODELOS_SIA = ["qwen2.5:14b", "llama3.1:8b"];

const iso = (ms: number) => new Date(ms).toISOString();

export type OpcionesServidorIa = {
    /** Zoe (sin `empresas.todas`) recibe `servidor_ia.ver` y `.administrar` en vez de `ver_uso`: la API le responde 404. */
    zonaAdministra?: boolean;
    /**
     * Cómo está al abrir: `ninguno` (sin enlazar nada), `a_medias` (enlace creado, sin conectar),
     * `con_huella` (conectado, falta activar), `listo` (por defecto), `no_disponible` (caído hace 20
     * minutos), `incompatible` (hay que actualizar el servidor) o `incompatible_iris` (hay que actualizar Iris).
     */
    motor?: "ninguno" | "a_medias" | "con_huella" | "listo" | "degradado" | "no_disponible" | "incompatible" | "incompatible_iris" | "incompatible_protocolo" | "incompatible_iris_nuevo";
    /** Tarjetas gráficas que reporta (por defecto, 1). */
    gpus?: 0 | 1 | 2;
    /** Con `gpus: 0`: el servidor afirma que no tiene tarjeta (`sin_gpu`) en vez de no haberla leído (`omitido`). */
    sinGpu?: boolean;
    /** Respuestas de «activar», en orden (por defecto, `ok`). */
    activar?: ("ok" | "huella_cambiada" | "rotacion_en_curso")[];
    /** Las primeras `n` rotaciones responden 409 `rotacion_en_curso`. */
    rotarEnCurso?: number;
    /** Respuestas de «conectar», en orden (por defecto, `ok`). */
    conectar?: ("ok" | "fallo" | "429" | "vencido")[];
    /** Hay más empresas activas que plazas (§4.11). */
    plazasInsuficientes?: boolean;
    /** Lo que el admin de la instalación dejó elegir a las empresas (`null`, por defecto: todos). */
    modelosPermitidos?: string[] | null;
    /** El servidor está «listo» pero la última lectura de métricas falló. */
    lecturaFallida?: boolean;
    /** Rechazadas por saturación en el rango (por defecto, 0). */
    rechazadas?: number;
    /** Avisos del tablero (por defecto, uno de temperatura resuelto y, con rechazos, saturación activa). */
    avisos?: AvisoServidor[] | null;
    /** El `detalle` técnico del servidor (con `motor: "degradado"`). */
    detalle?: string;
    /** `lecturaOk: true` pero la muestra es de hace 5 minutos. */
    lecturaVieja?: boolean;
    /** Las primeras `n` rotaciones se hacen en el servidor pero la respuesta no llega. */
    rotarSinRespuesta?: number;
    /** El primer guardado de umbrales o de empresas responde 409. */
    conflicto?: boolean;
    /** El enlace sin activar ya caducó (24 h). */
    enlaceCaducado?: boolean;
};

const paquete = (version: string, protocolo: string, motivo: Paquete["motivoIncompatible"] = null): Paquete => ({ version, protocolo, compatible: motivo === null, motivoIncompatible: motivo, versionDisponible: null, irisMin: "0.27.0", irisMax: null });
/** Antes de conectar todavía no se sabe nada del servidor. */
const SIN_CONOCER = { paquete: { version: null, protocolo: null, compatible: false, motivoIncompatible: null, versionDisponible: null, irisMin: null, irisMax: null }, capacidad: { maxSimultaneas: 0, enCurso: 0, empresasActivas: 0, plazasInsuficientes: false }, catalogo: { llm: null }, gpu: null } satisfies Partial<Motor>;

function motorBase(estado: Motor["estado"]): Motor {
    return {
        activado: estado !== "sin_enlazar",
        enlazado: true,
        gpu: { modelo: "NVIDIA RTX 4090", vramMb: 24_564 },
        restringeIp: false,
        id: 1,
        nombre: "Servidor de IA",
        url: "https://10.0.0.5:8443",
        version: 4,
        estado,
        detalle: null,
        ultimoLatidoEn: iso(AHORA_SIA - 4_000),
        enlazadoEn: "2026-11-11T15:00:00Z",
        enlaceVenceEn: null,
        paquete: paquete("0.1.0", "1"),
        capacidad: { maxSimultaneas: 4, enCurso: 2, empresasActivas: 1, plazasInsuficientes: false },
        catalogo: { llm: MODELOS_SIA.map((id) => ({ id })) },
        modelosPermitidos: null,
        huellaTls: HUELLA,
        tokenRotadoEn: "2026-11-02T15:00:00Z",
        certClienteVenceEn: "2027-11-11T15:00:00Z",
        ipObservada: "10.0.0.2",
        empresasPermitidas: null,
        simultaneasPorEmpresa: 2,
        ultimaPrueba: null,
    };
}

function motorInicial(o: OpcionesServidorIa): Motor | null {
    const como = o.motor ?? "listo";
    if (como === "ninguno") return null;
    if (como === "a_medias" || como === "con_huella") {
        return {
            ...motorBase("sin_enlazar"),
            version: 1,
            enlazadoEn: null,
            ...SIN_CONOCER,
            enlazado: como === "con_huella",
            tokenRotadoEn: null,
            huellaTls: como === "con_huella" ? HUELLA : null,
            enlaceVenceEn: iso(o.enlaceCaducado ? AHORA_SIA - 60_000 : AHORA_SIA + 23 * 3_600_000),
        };
    }
    if (como === "degradado") return { ...motorBase("degradado"), detalle: o.detalle ?? "El modelo tarda en cargar" };
    if (como === "no_disponible") return { ...motorBase("no_disponible"), detalle: "No responde", ultimoLatidoEn: iso(AHORA_SIA - 20 * 60_000) };
    if (como === "incompatible") return { ...motorBase("incompatible"), paquete: paquete("0.0.9", "0", "paquete_antiguo") };
    if (como === "incompatible_protocolo") return { ...motorBase("incompatible"), paquete: paquete("0.0.9", "0", "protocolo") };
    if (como === "incompatible_iris_nuevo") return { ...motorBase("incompatible"), paquete: paquete("0.0.9", "1", "iris_nuevo") };
    if (como === "incompatible_iris") return { ...motorBase("incompatible"), paquete: paquete("0.3.0", "3", "iris_antiguo") };
    return { ...motorBase("listo"), modelosPermitidos: o.modelosPermitidos ?? null, capacidad: { maxSimultaneas: 4, enCurso: 2, empresasActivas: o.plazasInsuficientes ? 6 : 1, plazasInsuficientes: o.plazasInsuficientes ?? false } };
}

const PASO: Record<Rango, { n: number; ms: number }> = { ahora: { n: 60, ms: 10_000 }, "1h": { n: 60, ms: 60_000 }, "24h": { n: 96, ms: 15 * 60_000 }, "7d": { n: 168, ms: 3_600_000 } };

/** Series deterministas: una ola suave, con un tramo sin respuesta en «24 horas». */
function series(rango: Rango, conGpu: boolean, caidoDesde: number | null, escala = 1): PuntoSerie[] {
    const { n, ms } = PASO[rango];
    return Array.from({ length: n }, (_, i) => {
        const t = AHORA_SIA - (n - 1 - i) * ms;
        const hueco = (rango === "24h" && i >= 40 && i < 46) || (caidoDesde !== null && t > caidoDesde);
        if (hueco) return { t: iso(t), usoPct: null, vramPct: null, peticionesMin: null, latenciaMediaMs: null, latenciaP95Ms: null };
        const ola = (Math.sin(i / 5) + 1) / 2;
        const rizo = (Math.sin(i * 1.7) + 1) / 2;
        const media = 1_400 + ola * 900;
        return {
            t: iso(t),
            usoPct: conGpu ? Math.round(35 + ola * 40 + rizo * 8) : null,
            vramPct: conGpu ? Math.round(52 + ola * 6) : null,
            peticionesMin: Math.round((4 + ola * 14 + rizo * 3) * escala),
            latenciaMediaMs: Math.round(media),
            latenciaP95Ms: Math.round(media * 1.9 + rizo * 600),
        };
    });
}

const GPU_1: Gpu = { indice: 0, nombre: "NVIDIA RTX 4090", usoPct: 63, vramUsadaMb: 14_336, vramTotalMb: 24_564, temperaturaC: 71, potenciaW: 310 };
const GPU_2: Gpu = { indice: 1, nombre: "NVIDIA RTX 3090", usoPct: 22, vramUsadaMb: 6_144, vramTotalMb: 24_576, temperaturaC: 78, potenciaW: 180 };

const FACTOR: Record<Rango, number> = { ahora: 0.004, "1h": 0.045, "24h": 1, "7d": 6.6 };

export function apiServidorIa(opciones: OpcionesServidorIa = {}) {
    let motor = motorInicial(opciones);
    let umbrales: Umbrales = { version: 2, ...DE_FABRICA };
    let conflicto = opciones.conflicto ?? false;
    let caidoDesde: number | null = motor?.estado === "no_disponible" ? AHORA_SIA - 20 * 60_000 : null;
    let lecturas = 0;
    let sinRespuesta = opciones.rotarSinRespuesta ?? 0;
    let fallaTablero = 0;
    const conectar = [...(opciones.conectar ?? [])];
    const activar = [...(opciones.activar ?? [])];
    let rotarEnCurso = opciones.rotarEnCurso ?? 0;
    /** Lo que recibió el servidor, para comprobarlo en la prueba. */
    const recibido = {
        enlaces: [] as { cuerpo: unknown; idempotencia: string | undefined }[],
        conexiones: 0,
        activaciones: [] as unknown[],
        pruebas: 0,
        rotaciones: [] as unknown[],
        clavesRotar: [] as (string | undefined)[],
        empresas: [] as unknown[],
        modelos: [] as unknown[],
        desactivaciones: [] as unknown[],
        umbrales: [] as unknown[],
        tableros: [] as Rango[],
    };

    function usoDe(rango: Rango, empresas: { id: number; nombre: string }[]): UsoEmpresa[] {
        const f = FACTOR[rango];
        return empresas.map((e, i) => ({
            empresaId: e.id,
            nombre: e.nombre,
            peticiones: Math.round((i === 0 ? 10_613 : 4_208) * f),
            tokensEntrada: Math.round((i === 0 ? 2_600_000 : 900_000) * f),
            tokensSalida: Math.round((i === 0 ? 800_000 : 310_000) * f),
            latenciaMediaMs: i === 0 ? 1_500 : 1_820,
            errores: rango === "ahora" ? 0 : i === 0 ? 4 : 0,
            rechazadas: i === 0 ? (opciones.rechazadas ?? 0) : 0,
        }));
    }

    function tablero(rango: Rango, global: boolean, empresas: { id: number; nombre: string }[], ahora: number): Tablero {
        if (!motor) {
            return { estado: { motorId: null, estado: null, detalle: null, version: null, compatible: null, motivoIncompatible: null, encendidoSeg: null, tomadoEn: null, lecturaOk: false, modelosDisponibles: [] }, servidor: null, series: null, uso: null, avisos: [] };
        }
        const vivo = motor.estado === "listo" || motor.estado === "degradado";
        const lecturaOk = vivo && !opciones.lecturaFallida;
        const nGpus = opciones.gpus ?? 1;
        const rechazadas = opciones.rechazadas ?? 0;
        const estado: Tablero["estado"] = {
            motorId: global ? motor.id : null,
            estado: motor.estado,
            detalle: motor.detalle,
            version: motor.paquete?.version ?? null,
            compatible: motor.paquete?.compatible ?? null,
            motivoIncompatible: motor.paquete?.motivoIncompatible ?? null,
            // Al admin de una empresa (`ver_uso`) no le llega cuánto lleva encendido.
            encendidoSeg: vivo && global ? 6 * 86_400 + 3_600 : null,
            // La muestra buena es de hace 4 s en el reloj de la página (que la prueba adelanta).
            tomadoEn: iso(caidoDesde ?? (opciones.lecturaFallida ? AHORA_SIA - 3 * 60_000 : opciones.lecturaVieja ? ahora - 5 * 60_000 : ahora - 4_000)),
            lecturaOk,
            modelosDisponibles: vivo ? MODELOS_SIA : [],
            ...(global ? { empresasQueLoUsan: empresas.map((e) => e.nombre), empresasActivas: opciones.plazasInsuficientes ? 6 : empresas.length, plazasInsuficientes: opciones.plazasInsuficientes ?? false } : {}),
        };
        const avisos: AvisoServidor[] | null =
            opciones.avisos !== undefined
                ? opciones.avisos
                : (caidoDesde !== null
                ? [{ tipo: "caido", activo: true, desde: iso(caidoDesde), hasta: null, valor: null, umbral: null }]
                : [
                      ...(rechazadas > 0 ? [{ tipo: "saturacion", activo: true, desde: iso(AHORA_SIA - 4 * 60_000), hasta: null, valor: rechazadas, umbral: umbrales.saturacionRechazos } satisfies AvisoServidor] : []),
                      { tipo: "temperatura", activo: false, desde: "2026-11-15T20:10:00Z", hasta: "2026-11-15T20:31:00Z", valor: 88, umbral: umbrales.temperaturaC },
                  ]);
        const errores: Tablero["uso"] = {
            empresas: usoDe(rango, global ? empresas : empresas.slice(0, 1)),
            errores: rango === "ahora" ? [] : [{ motivo: "tiempo_agotado", cuenta: 4, ultimoEn: iso(AHORA_SIA - 38 * 60_000) }, ...(rechazadas > 0 ? [{ motivo: "ocupado" as const, cuenta: rechazadas, ultimoEn: iso(AHORA_SIA - 2 * 60_000) }] : [])],
        };
        if (!global) {
            // El admin de una empresa: estado general y solo lo suyo; nada de la tarjeta gráfica ni de la saturación global.
            return { estado, servidor: null, series: series(rango, false, caidoDesde, 0.7), uso: errores, avisos: [] };
        }
        return {
            estado,
            servidor: lecturaOk
                ? {
                      gpus: nGpus === 0 ? { estado: opciones.sinGpu ? "sin_gpu" : "omitido", lista: [] } : { estado: "ok", lista: nGpus === 2 ? [GPU_1, GPU_2] : [GPU_1] },
                      cpu: { nucleos: 16, usoPct: 12 },
                      memoria: { usadoMb: 21_504, totalMb: 65_536 },
                      disco: { usadoMb: 61_440, totalMb: 204_800 },
                      modelos: [
                          { id: "qwen2.5:14b", tamanoMb: 9_216, permitido: true, cargado: true, vramMb: 10_240, cargadoHasta: iso(AHORA_SIA + 4 * 60_000) },
                          { id: "llama3.1:8b", tamanoMb: 4_813, permitido: true, cargado: true, vramMb: 4_096, cargadoHasta: iso(AHORA_SIA + 2 * 60_000) },
                          { id: "gemma3:27b", tamanoMb: 17_408, permitido: false, cargado: false, vramMb: 0, cargadoHasta: null },
                      ],
                      // Cambia en cada lectura: así se ve que el tablero se refresca.
                      llm: { enCurso: 1 + (lecturas % 3), maxSimultaneas: 4 },
                      rechazadas,
                  }
                : null,
            series: series(rango, nGpus > 0, caidoDesde),
            uso: errores,
            avisos,
        };
    }

    async function manejar(x: {
        route: Route;
        metodo: string;
        ruta: string;
        query: URLSearchParams;
        cuerpo: unknown;
        idempotencia: string | undefined;
        puede: (p: Permiso) => boolean;
        empresas: { id: number; nombre: string }[];
        empresaId: number | null;
    }): Promise<boolean> {
        const { route, metodo, ruta, puede } = x;
        const cuerpo = (x.cuerpo ?? {}) as Record<string, unknown>;
        const SERVIDOR = "/api/v1/mensajeria/servidor-ia";
        const MOTORES = "/api/v1/mensajeria/voz/motores";
        if (!ruta.startsWith(SERVIDOR) && !ruta.startsWith(MOTORES)) return false;
        // `servidor_ia.ver` y `.administrar` solo valen con `empresas.todas` (§4.8): sin el permiso, 403;
        // con el permiso pero sin `empresas.todas`, 404.
        const global = puede(PERMISO_SIA_VER) && puede("empresas.todas");
        const administra = global && puede(PERMISO_SIA_ADMINISTRAR);
        const prohibido = async (p: Permiso = PERMISO_SIA_VER) =>
            puede(p) ? (await problema(route, P("no_encontrado", 404, "No existe")), true) : (await problema(route, P("prohibido", 403, "Sin permiso")), true);
        const conflictoVersion = async () => (await problema(route, P("conflicto_version", 409, "Alguien más lo cambió")), true);

        if (ruta === `${SERVIDOR}/tablero` && metodo === "GET") {
            // Ni `ver` global ni `ver_uso`: 404.
            if (!global && !puede(PERMISO_SIA_VER_USO)) return (await problema(route, P("no_encontrado", 404, "No existe")), true);
            const rango = (x.query.get("rango") ?? "ahora") as Rango;
            recibido.tableros.push(rango);
            if (fallaTablero > 0) {
                fallaTablero--;
                return (await problema(route, P("error_interno", 500, "Error interno")), true);
            }
            lecturas++;
            const propias = global ? x.empresas : x.empresas.filter((e) => e.id === x.empresaId);
            // El «ahora» del servidor es el reloj simulado de la página.
            const ahora = await route
                .request()
                .frame()
                .evaluate(() => Date.now())
                .catch(() => AHORA_SIA);
            return (await json(route, tablero(rango, global, propias, ahora)), true);
        }
        if (ruta === `${SERVIDOR}/umbrales` && metodo === "GET") {
            if (!global) return prohibido();
            return (await json(route, umbrales), true);
        }
        if (ruta === `${SERVIDOR}/umbrales/actualizar` && metodo === "POST") {
            if (!administra) return prohibido(PERMISO_SIA_ADMINISTRAR);
            recibido.umbrales.push({ ...cuerpo, idempotencia: x.idempotencia });
            if (conflicto) {
                conflicto = false;
                return conflictoVersion();
            }
            if (cuerpo.version !== umbrales.version) return conflictoVersion();
            // El servidor valida más estricto que el panel en un campo, para ver el 422 junto al campo.
            if (Number(cuerpo.saturacionRechazos) === 999) {
                return (await problema(route, P("validacion", 422, "Datos inválidos", { errors: [{ field: "body.saturacionRechazos", message: "Demasiado alto para este servidor" }] })), true);
            }
            // Los dos de retención son opcionales al actualizar.
            umbrales = { ...umbrales, ...(cuerpo as unknown as Partial<Umbrales>), version: umbrales.version + 1 };
            return (await json(route, umbrales), true);
        }

        if (!ruta.startsWith(MOTORES)) return false;
        if (ruta === MOTORES && metodo === "GET") {
            if (!global) return prohibido();
            return (await json(route, { data: motor ? [motor] : [], nextCursor: null }), true);
        }
        if (!administra) return prohibido(PERMISO_SIA_ADMINISTRAR);
        if (ruta === `${MOTORES}/enlazar` && metodo === "POST") {
            recibido.enlaces.push({ cuerpo, idempotencia: x.idempotencia });
            if (motor && motor.estado !== "sin_enlazar") return (await problema(route, P("motor_ya_enlazado", 409, "Ya hay un servidor enlazado")), true);
            if (String(cuerpo.url).includes("127.0.0.1")) return (await problema(route, P("url_no_permitida", 422, "Dirección no permitida", { errors: [{ field: "body.url", message: "Esa dirección no se puede usar" }] })), true);
            if (String(cuerpo.url).includes("10.9.9.9")) return (await problema(route, P("motor_no_responde", 502, "El servidor no responde")), true);
            motor = { ...motorBase("sin_enlazar"), version: 1, nombre: String(cuerpo.nombre), url: String(cuerpo.url), enlazadoEn: null, ...SIN_CONOCER, enlazado: false, tokenRotadoEn: null, huellaTls: null, enlaceVenceEn: iso(AHORA_SIA + 15 * 60_000) };
            const creado: EnlaceCreado = { motor, codigo: CODIGO_ENLACE, venceEn: iso(AHORA_SIA + 15 * 60_000) };
            return (await json(route, creado, 201), true);
        }
        const accion = /^\/api\/v1\/mensajeria\/voz\/motores\/(\d+)\/([a-z-]+(?:\/actualizar)?)$/.exec(ruta);
        if (!accion || metodo !== "POST" || !motor || Number(accion[1]) !== motor.id) return (await problema(route, P("no_encontrado", 404, "No existe")), true);
        switch (accion[2]) {
            case "conectar": {
                recibido.conexiones++;
                const como = conectar.shift() ?? "ok";
                // La misma respuesta para cualquier falla: no se dice si el código estaba usado o mal escrito.
                if (como === "fallo") return (await problema(route, P("motor_no_responde", 502, "El servidor no responde")), true);
                if (como === "429") return (await problema(route, P("demasiados_intentos", 429, "Demasiados intentos")), true);
                // La ventana de 15 minutos del código se cerró.
                if (como === "vencido") return (await problema(route, P("enlace_vencido", 409, "El enlace venció")), true);
                // Conectado: ahora hay 24 horas para activar.
                motor = { ...motor, enlazado: true, huellaTls: HUELLA, version: motor.version + 1, enlaceVenceEn: iso(AHORA_SIA + 24 * 3_600_000) };
                return (await json(route, motor), true);
            }
            case "activar": {
                recibido.activaciones.push(cuerpo);
                if (!motor.huellaTls) return (await problema(route, P("motor_sin_enlazar", 409, "Falta conectar")), true);
                if (typeof cuerpo.huellaTls !== "string" || cuerpo.huellaTls === "") return (await problema(route, P("validacion", 422, "Falta la huella", { errors: [{ field: "body.huellaTls", message: "Falta la huella comparada" }] })), true);
                const comoActivar = activar.shift() ?? "ok";
                if (comoActivar === "rotacion_en_curso") return (await problema(route, P("rotacion_en_curso", 409, "Hay un cambio de llave en curso")), true);
                // El servidor enlazado ya no es el que se comparó: su huella es otra.
                if (comoActivar === "huella_cambiada") motor = { ...motor, huellaTls: HUELLA_OTRA, version: motor.version + 1 };
                if (cuerpo.huellaTls !== motor.huellaTls) return (await problema(route, P("huella_cambiada", 409, "La huella cambió")), true);
                if (motor.enlaceVenceEn && Date.parse(motor.enlaceVenceEn) < AHORA_SIA) return (await problema(route, P("enlace_vencido", 409, "El enlace venció")), true);
                motor = { ...motorBase("listo"), nombre: motor.nombre, url: motor.url, version: motor.version + 1, enlazadoEn: iso(AHORA_SIA), tokenRotadoEn: iso(AHORA_SIA) };
                return (await json(route, motor), true);
            }
            case "probar": {
                recibido.pruebas++;
                const ok = motor.estado === "listo";
                const paso = (codigo: PasoPrueba["codigo"], estado: PasoPrueba["estado"], ms = 0, detalle = ""): PasoPrueba => ({ codigo, estado, ms, detalle });
                const ultimaPrueba: ResultadoPrueba = ok
                    ? { estado: "ok", probadoEn: iso(AHORA_SIA), pasos: [paso("conexion", "ok", 42), paso("compatibilidad", "ok", 3), paso("gpu", "ok", 5), paso("llm", "ok", 1_840), paso("stt", "omitido")] }
                    : { estado: "error", probadoEn: iso(AHORA_SIA), pasos: [paso("conexion", "error", 0, "El servidor no respondió en 2 segundos."), paso("llm", "omitido")] };
                // El resultado va en `ultimaPrueba` de la tarjeta.
                motor = { ...motor, ultimaPrueba };
                return (await json(route, motor), true);
            }
            case "rotar-token": {
                recibido.rotaciones.push(cuerpo);
                recibido.clavesRotar.push(x.idempotencia);
                // La misma clave no rota dos veces: devuelve lo que ya se hizo.
                const repetida = x.idempotencia !== undefined && recibido.clavesRotar.indexOf(x.idempotencia) < recibido.clavesRotar.length - 1;
                if (repetida) return (await json(route, motor), true);
                if (rotarEnCurso > 0) {
                    rotarEnCurso--;
                    return (await problema(route, P("rotacion_en_curso", 409, "Hay un cambio de llave en curso")), true);
                }
                if (sinRespuesta > 0) {
                    sinRespuesta--;
                    motor = { ...motor, tokenRotadoEn: iso(AHORA_SIA), version: motor.version + 1 };
                    return (await route.abort("connectionreset"), true);
                }
                motor = { ...motor, tokenRotadoEn: iso(AHORA_SIA), version: motor.version + 1 };
                return (await json(route, motor), true);
            }
            case "empresas/actualizar": {
                recibido.empresas.push({ ...cuerpo, idempotencia: x.idempotencia });
                if (conflicto) {
                    conflicto = false;
                    return conflictoVersion();
                }
                if (cuerpo.version !== motor.version) return conflictoVersion();
                motor = { ...motor, empresasPermitidas: cuerpo.empresas as number[] | null, simultaneasPorEmpresa: Number(cuerpo.simultaneasPorEmpresa), version: motor.version + 1 };
                return (await json(route, motor), true);
            }
            case "modelos/actualizar": {
                recibido.modelos.push({ ...cuerpo, idempotencia: x.idempotencia });
                if (cuerpo.version !== motor.version) return conflictoVersion();
                const pedidos = cuerpo.modelos as string[] | null;
                const catalogo = (motor.catalogo.llm ?? []).map((m) => m.id);
                if (pedidos && pedidos.some((m) => !catalogo.includes(m))) return (await problema(route, P("modelo_no_instalado", 422, "Ese modelo no está en el servidor")), true);
                motor = { ...motor, modelosPermitidos: pedidos, version: motor.version + 1 };
                return (await json(route, motor), true);
            }
            case "desactivar": {
                recibido.desactivaciones.push(cuerpo);
                motor = { ...motor, estado: "desactivado", version: motor.version + 1 };
                const respuesta = motor;
                // Desenlazado: para el panel ya no hay servidor.
                motor = null;
                return (await json(route, respuesta), true);
            }
            default:
                return (await problema(route, P("no_encontrado", 404, "No existe")), true);
        }
    }

    return {
        manejar,
        recibido,
        motor: () => motor,
        umbrales: () => umbrales,
        lecturas: () => lecturas,
        /** El servidor deja de responder ahora. */
        caer: () => {
            if (motor) motor = { ...motor, estado: "no_disponible", detalle: "No responde" };
            caidoDesde = AHORA_SIA - 30_000;
        },
        /** En el servidor quitaron un modelo del catálogo: guardar con él responde 422. */
        quitarDelCatalogo: (id: string) => {
            if (motor) motor = { ...motor, catalogo: { llm: (motor.catalogo.llm ?? []).filter((m) => m.id !== id) } };
        },
        /** Las próximas `n` lecturas del tablero responden 500. */
        fallarTablero: (n: number) => (fallaTablero = n),
    };
}

export type ApiServidorIa = ReturnType<typeof apiServidorIa>;
