import type { Schema } from "@iris/contratos";
import type { Route } from "@playwright/test";

/*
 * API comercial falsa (e2e/api-falsa.ts), tipada con packages/contratos y fiel al README de
 * apps/api/internal/comercial: prospectos con cursor y filtros, detalle con `acciones` (misma
 * regla que `acciones()` en prospectos.go), transiciones con `version` (409) e
 * `Idempotency-Key`, factibilidad (distancia o manual; indeterminado con motivo), planes y
 * cobertura (con o sin conector), cotizaciones con revisiones y totales calculados aquí (la «API»),
 * contrato con firma SVG validada, ganar con sus requisitos y alta local o en Argos.
 */

type Problema = Schema<"Problema">;
type Permiso = Schema<"Permiso">;
type Prospecto = Schema<"Prospecto">;
type Detalle = Schema<"ProspectoDetalle">;
type Factibilidad = Schema<"Factibilidad">;
type Cotizacion = Schema<"Cotizacion">;
type Contrato = Schema<"Contrato">;
type Actividad = Schema<"Actividad">;
type Evento = Schema<"EventoProspecto">;
type Pendiente = Schema<"PendienteArgos">;
type Doc = Schema<"DocumentoPDF">;

const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
const problema = (route: Route, p: Problema) => route.fulfill({ status: p.status, contentType: "application/problem+json", body: JSON.stringify(p) });
const regla = (route: Route, codigo: string, detail: string) => problema(route, { type: `urn:iris:problema:${codigo}`, title: "Regla de negocio", status: 422, detail });
const validacion = (route: Route, field: string, message: string) => problema(route, { type: "urn:iris:problema:validacion", title: "Datos inválidos", status: 422, errors: [{ field, message }] });
const conflicto = (route: Route) => problema(route, { type: "urn:iris:problema:conflicto_version", title: "Conflicto de versión", status: 409, detail: "El prospecto cambió desde que lo leíste" });
const noEncontrado = (route: Route) => problema(route, { type: "urn:iris:problema:no_encontrado", title: "No encontrado", status: 404 });
const prohibido = (route: Route) => problema(route, { type: "urn:iris:problema:prohibido", title: "Sin permiso", status: 403 });

export const PDF_BYTES = Buffer.from("%PDF-1.4\n1 0 obj<<>>endobj\ntrailer<<>>\n%%EOF\n");
export const ROUTER_NORTE: Schema<"RouterCobertura"> = { id: 501, nombre: "R-Norte", zonaId: 4, lat: 14.64, lon: -90.51, sincronizadoEn: "2026-10-28T12:00:00Z" };
export const PLANES_ARGOS: Schema<"PlanComercial">[] = [
    { id: 71, empresaId: 10, nombre: "Fibra 100", precio: "250.00", estado: "activo" },
    { id: 72, empresaId: 10, nombre: "Fibra 300", precio: "400.00", estado: "activo" },
];

const ahora = "2026-10-28T15:00:00Z";
let uuid = 0;
const nuevoUuid = () => `00000000-0000-4000-8000-${String(++uuid).padStart(12, "0")}`;
const doc = (estado: Doc["estado"] = "completo"): Doc => {
    const id = nuevoUuid();
    return { adjuntoId: id, estado, sha256: estado === "completo" ? "a".repeat(64) : null, urlDescarga: `/api/v1/adjuntos/${id}` };
};

/** Centavos de un texto decimal y de vuelta (como la API: sin flotantes). */
const centavos = (t: string) => {
    const [e, d = ""] = t.split(".");
    return Number(e) * 100 + Number((d + "00").slice(0, 2));
};
const texto = (c: number) => `${c < 0 ? "-" : ""}${Math.floor(Math.abs(c) / 100)}.${String(Math.abs(c) % 100).padStart(2, "0")}`;

function prospecto(d: Partial<Prospecto> & Pick<Prospecto, "id" | "nombre" | "estado">): Prospecto {
    return {
        numero: `PR-${String(d.id).padStart(6, "0")}`,
        empresaId: null,
        origen: "panel",
        origenDetalle: null,
        documento: null,
        telefono: "5555-0000",
        correo: null,
        direccion: "Zona 1, Ciudad de Guatemala",
        lat: null,
        lon: null,
        zonaId: 4,
        planInteresId: null,
        planInteresNombre: null,
        vendedorId: null,
        notas: "",
        motivoPerdida: null,
        perdidoEn: null,
        ganadoEn: null,
        obraConfirmada: false,
        creadoEn: ahora,
        actualizadoEn: ahora,
        version: 1,
        ...d,
    };
}

export type EscenarioComercial = {
    prospectos: Prospecto[];
    factibilidades: Map<number, Factibilidad>;
    cotizaciones: Cotizacion[];
    contratos: Contrato[];
    actividades: Actividad[];
    eventos: Map<number, Evento[]>;
    pendientes: Map<number, Pendiente[]>;
    altas: Map<number, Schema<"Alta">>;
};

export type ApiComercial = {
    escenario: EscenarioComercial;
    /** Otra persona cambió el prospecto: sube su versión. */
    cambiarVersion: (id: number) => void;
    manejar: (c: ContextoComercial) => Promise<boolean>;
};

export type ContextoComercial = {
    route: Route;
    metodo: string;
    ruta: string;
    query: URLSearchParams;
    cuerpo: unknown;
    idempotencia: string | undefined;
    actual: Schema<"Usuario">;
    puede: (p: Permiso) => boolean;
};

export function apiComercial(opciones: { conector: boolean }): ApiComercial {
    const conector = opciones.conector;
    const factibilidad = (prospectoId: number, d: Partial<Factibilidad> & Pick<Factibilidad, "resultado">): Factibilidad => ({
        id: prospectoId * 10 + Math.floor(Math.random() * 9),
        prospectoId,
        motor: "distancia",
        motivo: null,
        lat: 14.641,
        lon: -90.512,
        routerId: null,
        routerNombre: null,
        caja: null,
        distanciaM: null,
        viableM: null,
        obraM: null,
        notas: "",
        evaluadaPor: 1,
        creadoEn: ahora,
        ...d,
    });

    const esc: EscenarioComercial = {
        prospectos: [
            prospecto({ id: 1, nombre: "Ana Gómez", estado: "nuevo", lat: 14.642, lon: -90.513, origen: "web", correo: "ana@correo.test" }),
            prospecto({ id: 2, nombre: "Bruno Díaz", estado: "contactado", lat: 14.6435, lon: -90.509, vendedorId: 1, planInteresNombre: "Fibra 100" }),
            prospecto({ id: 3, nombre: "Carla Ruiz", estado: "factibilidad", lat: 14.6, lon: -90.55, origen: "referido", origenDetalle: "Vecina de Ana" }),
            prospecto({ id: 4, nombre: "Diego López", estado: "cotizado", lat: 14.638, lon: -90.515, vendedorId: 1 }),
            prospecto({ id: 5, nombre: "Elsa Martínez", estado: "contrato", lat: 14.65, lon: -90.52, documento: "1234567890101" }),
            prospecto({ id: 6, nombre: "Fernando Paz", estado: "ganado", ganadoEn: ahora, vendedorId: 1 }),
            prospecto({ id: 7, nombre: "Gloria Ríos", estado: "perdido", motivoPerdida: "Eligió a la competencia", perdidoEn: ahora, zonaId: 5 }),
            // Relleno del embudo: «Nuevo» con más de una página.
            ...Array.from({ length: 22 }, (_, i) => prospecto({ id: 100 + i, nombre: `Interesado ${i + 1}`, estado: "nuevo", origen: "web", zonaId: 5 })),
        ],
        factibilidades: new Map(),
        cotizaciones: [],
        contratos: [],
        actividades: [
            {
                id: 1,
                prospectoId: 2,
                tipo: "llamada",
                descripcion: "Le interesa el plan de 100 Mbps",
                resultado: "Pide visita",
                ocurridaEn: "2026-10-27T16:00:00Z",
                proximaAccion: "Evaluar factibilidad",
                proximaFecha: "2026-10-29T15:00:00Z",
                autorId: 1,
                creadoEn: "2026-10-27T16:00:00Z",
            },
        ],
        eventos: new Map(),
        pendientes: new Map(),
        altas: new Map(),
    };
    esc.factibilidades.set(3, factibilidad(3, { resultado: "indeterminado", motivo: "sin_conector" }));
    esc.factibilidades.set(4, factibilidad(4, { resultado: "viable", routerId: 501, routerNombre: "R-Norte", distanciaM: 182.4, viableM: 300, obraM: 1000 }));
    esc.factibilidades.set(5, factibilidad(5, { resultado: "requiere_obra", routerId: 501, routerNombre: "R-Norte", distanciaM: 640, viableM: 300, obraM: 1000 }));
    esc.factibilidades.set(6, factibilidad(6, { resultado: "viable", routerId: 501, routerNombre: "R-Norte", distanciaM: 90, viableM: 300, obraM: 1000 }));

    const cot = (d: Partial<Cotizacion> & Pick<Cotizacion, "id" | "prospectoId">): Cotizacion => ({
        numero: `COT-${String(d.id).padStart(6, "0")}`,
        revision: 1,
        estado: "vigente",
        vencida: false,
        moneda: "GTQ",
        totalMensual: "250.00",
        totalUnico: "150.00",
        vigenteHasta: "2026-11-12T05:59:59Z",
        planId: conector ? 71 : null,
        notas: "",
        lineas: [
            { posicion: 1, tipo: "plan", planId: conector ? 71 : null, productoId: null, descripcion: "Fibra 100", cantidad: "1.00", precioUnitario: "250.00", recurrente: true, total: "250.00" },
            { posicion: 2, tipo: "instalacion", planId: null, productoId: null, descripcion: "Instalación", cantidad: "1.00", precioUnitario: "150.00", recurrente: false, total: "150.00" },
        ],
        reemplazadaPor: null,
        aceptadaEn: null,
        aceptadaPor: null,
        creadoPor: 1,
        creadoEn: "2026-10-28T14:00:00Z",
        version: 1,
        pdf: doc(),
        ...d,
    });
    esc.cotizaciones.push(cot({ id: 41, prospectoId: 4 }), cot({ id: 51, prospectoId: 5, estado: "aceptada", aceptadaEn: ahora, aceptadaPor: 1, version: 2 }), cot({ id: 61, prospectoId: 6, estado: "aceptada", aceptadaEn: ahora }));
    const contrato = (d: Partial<Contrato> & Pick<Contrato, "id" | "prospectoId" | "cotizacionId">): Contrato => ({
        numero: `CTR-${String(d.id).padStart(6, "0")}`,
        plantilla: "estandar_v1",
        estado: "borrador",
        firmanteNombre: null,
        firmanteDocumento: null,
        firmadoEn: null,
        firmadoPor: null,
        motivoAnulacion: null,
        creadoEn: ahora,
        version: 1,
        pdf: doc(),
        ...d,
    });
    esc.contratos.push(
        contrato({ id: 52, prospectoId: 5, cotizacionId: 51 }),
        contrato({ id: 62, prospectoId: 6, cotizacionId: 61, estado: "firmado", firmanteNombre: "Fernando Paz", firmadoEn: ahora, firmadoPor: 1, pdfFirmado: doc() }),
    );
    esc.altas.set(6, { modo: "argos", estado: "esperando_servicio", clienteArgosId: 9001, servicioArgosId: null, ordenId: null, ordenNumero: null });
    esc.pendientes.set(6, [
        { id: 801, operacion: "crear_cliente", idempotencyKey: "iris:cliente:6", estado: "hecho", intentos: 1, ultimoError: null, version: 2 },
        { id: 802, operacion: "crear_servicio", idempotencyKey: "iris:servicio:6", estado: "agotado", intentos: 8, ultimoError: "Argos no respondió (timeout)", version: 3 },
    ]);
    const evento = (id: number, tipo: string, extra: Partial<Evento> = {}) => {
        const lista = esc.eventos.get(id) ?? [];
        lista.push({ id: lista.length + 1 + id * 100, tipo, de: null, a: null, motivo: null, datos: {}, actorTipo: "usuario", actorId: 1, creadoEn: ahora, ...extra });
        esc.eventos.set(id, lista);
    };
    for (const p of esc.prospectos) evento(p.id, "creado");

    const cobertura = (): Schema<"Cobertura"> => ({
        disponible: conector,
        motivo: conector ? null : "sin_conector",
        motor: "distancia",
        umbrales: { empresaId: conector ? 10 : null, viableM: 300, obraM: 1000, porDefecto: true, version: 0 },
        routers: conector ? [ROUTER_NORTE] : [],
    });

    const viva = (id: number) => esc.cotizaciones.filter((c) => c.prospectoId === id && c.estado !== "reemplazada").at(-1);
    const contratoVivo = (id: number) => esc.contratos.filter((c) => c.prospectoId === id && c.estado !== "anulado").at(-1);

    function detalle(p: Prospecto): Detalle {
        const f = esc.factibilidades.get(p.id);
        const c = viva(p.id);
        const k = contratoVivo(p.id);
        const e = p.estado;
        const factible = f !== undefined && (f.resultado === "viable" || f.resultado === "requiere_obra");
        const ganar = e === "contrato" && k?.estado === "firmado" && factible;
        const alta = esc.altas.get(p.id);
        return {
            ...p,
            ...(f ? { factibilidad: f } : {}),
            ...(c ? { cotizacion: c } : {}),
            ...(k ? { contrato: k } : {}),
            ...(alta ? { alta } : {}),
            pendientes: esc.pendientes.get(p.id) ?? [],
            acciones: {
                contactar: e === "nuevo",
                evaluar: ["nuevo", "contactado", "factibilidad", "cotizado"].includes(e),
                perder: e !== "ganado" && e !== "perdido",
                reabrir: e === "perdido",
                cotizar: factible && ["factibilidad", "cotizado", "contrato"].includes(e) && (!k || k.estado === "borrador"),
                aceptar: e === "cotizado" && c?.estado === "vigente" && !c.vencida,
                contratar: e === "cotizado" && c?.estado === "aceptada",
                firmar: e === "contrato" && k?.estado === "borrador",
                ganar,
                confirmaObra: ganar && f?.resultado === "requiere_obra",
            },
        };
    }

    const cambiar = (p: Prospecto, cambios: Partial<Prospecto>) => {
        Object.assign(p, cambios, { version: p.version + 1, actualizadoEn: ahora });
    };
    const estadoA = (p: Prospecto, a: Prospecto["estado"], motivo: string | null = null) => {
        evento(p.id, "estado", { de: p.estado, a, motivo });
        cambiar(p, { estado: a });
    };

    function listar(q: URLSearchParams) {
        const estados = q.get("estado")?.split(",").filter(Boolean) ?? [];
        const texto = q.get("q")?.toLowerCase();
        let lista = [...esc.prospectos].sort((a, b) => b.id - a.id);
        if (estados.length) lista = lista.filter((p) => estados.includes(p.estado));
        if (q.get("origen")) lista = lista.filter((p) => p.origen === q.get("origen"));
        if (q.get("vendedorId")) lista = lista.filter((p) => String(p.vendedorId) === q.get("vendedorId"));
        if (q.get("sinVendedor") === "true") lista = lista.filter((p) => p.vendedorId === null);
        if (q.get("zonaId")) lista = lista.filter((p) => String(p.zonaId) === q.get("zonaId"));
        if (q.get("conCoordenadas") === "true") lista = lista.filter((p) => p.lat !== null && p.lon !== null);
        if (texto) lista = lista.filter((p) => [p.nombre, p.direccion, p.telefono ?? "", p.numero].some((x) => x.toLowerCase().includes(texto)));
        const desde = Number(q.get("cursor")?.replace("c:", "") ?? 0);
        const limite = Number(q.get("limit") ?? 50);
        const corte = lista.slice(desde, desde + limite + 1);
        return { data: corte.slice(0, limite), nextCursor: corte.length > limite ? `c:${desde + limite}` : null } satisfies Schema<"PaginaProspecto">;
    }

    const docs = () => [...esc.cotizaciones.map((c) => c.pdf), ...esc.contratos.flatMap((c) => [c.pdf, c.pdfFirmado])].filter((d): d is Doc => d !== undefined);

    return {
        escenario: esc,
        cambiarVersion: (id) => {
            const p = esc.prospectos.find((x) => x.id === id);
            if (p) p.version++;
        },
        manejar: async ({ route, metodo, ruta, query, cuerpo, idempotencia, actual, puede }) => {
            const adjunto = /^\/api\/v1\/adjuntos\/([0-9a-f-]{36})$/.exec(ruta);
            if (metodo === "GET" && adjunto && docs().some((d) => d.adjuntoId === adjunto[1])) {
                if (!puede("adjuntos.ver")) return (await prohibido(route), true);
                await route.fulfill({ status: 200, contentType: "application/pdf", body: PDF_BYTES });
                return true;
            }
            const reintento = /^\/api\/v1\/integraciones\/pendientes\/(\d+)\/reintentar$/.exec(ruta);
            if (metodo === "POST" && reintento) {
                const pid = Number(reintento[1]);
                for (const [prospectoId, lista] of esc.pendientes) {
                    const p = lista.find((x) => x.id === pid);
                    if (!p) continue;
                    if (!puede("integraciones.reintentar")) return (await prohibido(route), true);
                    if ((cuerpo as { version: number }).version !== p.version) return (await conflicto(route), true);
                    Object.assign(p, { estado: "hecho", version: p.version + 1, ultimoError: null, intentos: p.intentos + 1 });
                    esc.altas.set(prospectoId, { modo: "argos", estado: "completa", clienteArgosId: 9001, servicioArgosId: 9002, ordenId: 44, ordenNumero: "OT-000044" });
                    await json(route, { ...p });
                    return true;
                }
                return false;
            }
            if (!ruta.startsWith("/api/v1/prospectos") && !ruta.startsWith("/api/v1/comercial") && !ruta.startsWith("/api/v1/cotizaciones") && !ruta.startsWith("/api/v1/contratos")) {
                return false;
            }
            const necesita = (p: Permiso) => {
                if (puede(p)) return false;
                void prohibido(route);
                return true;
            };
            if (metodo !== "GET" && !idempotencia) {
                await problema(route, { type: "urn:iris:problema:validacion", title: "Falta Idempotency-Key", status: 422 });
                return true;
            }

            if (metodo === "GET" && ruta === "/api/v1/comercial/cobertura") {
                if (necesita("prospectos.ver")) return true;
                await json(route, cobertura());
                return true;
            }
            if (metodo === "GET" && ruta === "/api/v1/comercial/planes") {
                if (necesita("cotizaciones.crear")) return true;
                await json(route, (conector ? { disponible: true, motivo: null, data: PLANES_ARGOS } : { disponible: false, motivo: "sin_conector", data: [] }) satisfies Schema<"PlanesDisponibles">);
                return true;
            }
            if (metodo === "GET" && ruta === "/api/v1/prospectos") {
                if (necesita("prospectos.ver")) return true;
                await json(route, listar(query));
                return true;
            }
            if (metodo === "POST" && ruta === "/api/v1/prospectos") {
                if (necesita("prospectos.crear")) return true;
                const e = cuerpo as Schema<"EntradaCrearProspecto">;
                if (e.nombre.toLowerCase().includes("duplicado")) {
                    await validacion(route, "telefono", "Ya hay un prospecto con ese teléfono");
                    return true;
                }
                const id = Math.max(...esc.prospectos.map((p) => p.id)) + 1;
                const p = prospecto({
                    id,
                    nombre: e.nombre,
                    estado: "nuevo",
                    direccion: e.direccion,
                    origen: e.origen ?? "panel",
                    telefono: e.telefono ?? null,
                    correo: e.correo ?? null,
                    zonaId: e.zonaId ?? null,
                    lat: e.lat ?? null,
                    lon: e.lon ?? null,
                    vendedorId: actual.id,
                });
                esc.prospectos.push(p);
                evento(id, "creado");
                await json(route, p, 201);
                return true;
            }

            const m = /^\/api\/v1\/prospectos\/(\d+)(?:\/([a-z]+))?$/.exec(ruta);
            if (m) {
                const p = esc.prospectos.find((x) => x.id === Number(m[1]));
                if (!p) return (await noEncontrado(route), true);
                const sub = m[2];
                if (metodo === "GET") {
                    if (necesita("prospectos.ver")) return true;
                    if (!sub) return (await json(route, detalle(p)), true);
                    if (sub === "eventos") return (await json(route, esc.eventos.get(p.id) ?? []), true);
                    if (sub === "actividades") return (await json(route, { data: esc.actividades.filter((a) => a.prospectoId === p.id), nextCursor: null }), true);
                    if (sub === "cotizaciones") return (await json(route, { data: esc.cotizaciones.filter((c) => c.prospectoId === p.id).reverse(), nextCursor: null }), true);
                    return false;
                }
                const e = (cuerpo ?? {}) as { version?: number; motivo?: string };
                if (sub === "actividades") {
                    if (necesita("prospectos.registrar_actividad")) return true;
                    const a = cuerpo as Schema<"EntradaActividad">;
                    const nueva: Actividad = {
                        id: esc.actividades.length + 1,
                        prospectoId: p.id,
                        tipo: a.tipo,
                        descripcion: a.descripcion,
                        resultado: a.resultado ?? null,
                        ocurridaEn: a.ocurridaEn ?? ahora,
                        proximaAccion: a.proximaAccion ?? null,
                        proximaFecha: a.proximaFecha ?? null,
                        autorId: actual.id,
                        creadoEn: ahora,
                    };
                    esc.actividades.push(nueva);
                    await json(route, nueva, 201);
                    return true;
                }
                const permisos: Record<string, Permiso> = {
                    contactar: "prospectos.contactar",
                    asignar: "prospectos.asignar",
                    perder: "prospectos.perder",
                    reabrir: "prospectos.reabrir",
                    ganar: "prospectos.ganar",
                    factibilidad: "factibilidad.evaluar",
                    cotizaciones: "cotizaciones.crear",
                    contrato: "contratos.crear",
                };
                const permiso = sub ? permisos[sub] : undefined;
                if (!permiso) return false;
                if (necesita(permiso)) return true;
                if (e.version !== p.version) return (await conflicto(route), true);
                const d = detalle(p);
                switch (sub) {
                    case "contactar":
                        if (!d.acciones.contactar) return (await regla(route, "transicion_invalida", `No se puede contactar un prospecto en ${p.estado}`), true);
                        estadoA(p, "contactado", e.motivo ?? null);
                        break;
                    case "asignar": {
                        const v = (cuerpo as Schema<"EntradaAsignarProspecto">).vendedorId ?? null;
                        if (v === p.vendedorId) return (await regla(route, "sin_cambios", "El prospecto ya tiene ese vendedor"), true);
                        evento(p.id, "asignado");
                        cambiar(p, { vendedorId: v });
                        break;
                    }
                    case "perder":
                        if (!d.acciones.perder) return (await regla(route, "transicion_invalida", "Ya está cerrado"), true);
                        estadoA(p, "perdido", e.motivo ?? null);
                        cambiar(p, { motivoPerdida: e.motivo ?? null, perdidoEn: ahora });
                        break;
                    case "reabrir":
                        if (!d.acciones.reabrir) return (await regla(route, "transicion_invalida", "Solo se reabre un perdido"), true);
                        estadoA(p, "contactado", e.motivo ?? null);
                        cambiar(p, { motivoPerdida: null, perdidoEn: null });
                        break;
                    case "factibilidad": {
                        const f = cuerpo as Schema<"EntradaFactibilidad">;
                        let nueva: Factibilidad;
                        if (f.manual) {
                            if ([...(f.notas ?? "")].length < 5) return (await validacion(route, "notas", "Describe la visita o el criterio (al menos 5 caracteres)"), true);
                            nueva = factibilidad(p.id, { resultado: f.manual.resultado, motor: "manual", caja: f.manual.caja ?? null, notas: f.notas ?? "" });
                        } else if (p.lat === null) {
                            nueva = factibilidad(p.id, { resultado: "indeterminado", motivo: "sin_coordenadas", lat: null, lon: null });
                        } else if (!conector) {
                            nueva = factibilidad(p.id, { resultado: "indeterminado", motivo: "sin_conector" });
                        } else {
                            nueva = factibilidad(p.id, { resultado: "viable", routerId: 501, routerNombre: "R-Norte", distanciaM: 182.4, viableM: 300, obraM: 1000 });
                        }
                        esc.factibilidades.set(p.id, nueva);
                        evento(p.id, "factibilidad", { a: nueva.resultado });
                        if (p.estado === "nuevo" || p.estado === "contactado") estadoA(p, "factibilidad");
                        else cambiar(p, {});
                        await json(route, nueva);
                        return true;
                    }
                    case "cotizaciones": {
                        if (!d.acciones.cotizar) return (await regla(route, "factibilidad_requerida", "Evalúa la factibilidad antes de cotizar"), true);
                        const c = cuerpo as Schema<"EntradaCrearCotizacion">;
                        const entrada = c.lineas ?? [];
                        const planes = entrada.filter((l) => l.tipo === "plan");
                        if (planes.length !== 1) return (await regla(route, "plan_requerido", "La cotización lleva exactamente una línea de plan"), true);
                        let mensual = 0;
                        let unico = 0;
                        const lineas = entrada.map((l, i): Schema<"LineaCotizacion"> => {
                            const plan = l.planId ? PLANES_ARGOS.find((x) => x.id === l.planId) : undefined;
                            const precio = plan?.precio ?? l.precioUnitario ?? "0";
                            const cant = l.cantidad ?? "1";
                            let total = Math.round((centavos(precio) * centavos(cant)) / 100);
                            if (l.tipo === "descuento") total = -total;
                            const recurrente = l.tipo === "plan" || (l.tipo !== "instalacion" && l.recurrente === true);
                            if (recurrente) mensual += total;
                            else unico += total;
                            return { posicion: i + 1, tipo: l.tipo, planId: l.planId ?? null, productoId: null, descripcion: plan?.nombre ?? l.descripcion ?? "", cantidad: texto(centavos(cant)), precioUnitario: texto(centavos(precio)), recurrente, total: texto(total) };
                        });
                        if (mensual < 0 || unico < 0) return (await regla(route, "descuento_excesivo", "Los descuentos dejan un total negativo"), true);
                        const previa = viva(p.id);
                        if (previa) Object.assign(previa, { estado: "reemplazada", version: previa.version + 1 });
                        const k = contratoVivo(p.id);
                        if (k && k.estado === "borrador") Object.assign(k, { estado: "anulado", motivoAnulacion: "Nueva cotización" });
                        const id = Math.max(0, ...esc.cotizaciones.map((x) => x.id)) + 100;
                        const nueva = cot({
                            id,
                            prospectoId: p.id,
                            numero: previa?.numero ?? `COT-${String(id).padStart(6, "0")}`,
                            revision: (previa?.revision ?? 0) + 1,
                            totalMensual: texto(mensual),
                            totalUnico: texto(unico),
                            lineas,
                            notas: c.notas ?? "",
                            ...(c.vigenteHasta ? { vigenteHasta: c.vigenteHasta } : {}),
                        });
                        esc.cotizaciones.push(nueva);
                        evento(p.id, "cotizacion");
                        if (p.estado !== "cotizado") estadoA(p, "cotizado");
                        else cambiar(p, {});
                        await json(route, nueva, 201);
                        return true;
                    }
                    case "contrato": {
                        if (!d.acciones.contratar) return (await regla(route, "cotizacion_no_aceptada", "El contrato se genera con la cotización aceptada por el cliente"), true);
                        const c = viva(p.id);
                        const nuevo = contrato({ id: Math.max(0, ...esc.contratos.map((x) => x.id)) + 100, prospectoId: p.id, cotizacionId: c?.id ?? 0 });
                        esc.contratos.push(nuevo);
                        evento(p.id, "contrato");
                        estadoA(p, "contrato");
                        await json(route, nuevo, 201);
                        return true;
                    }
                    case "ganar": {
                        const k = contratoVivo(p.id);
                        if (k?.estado !== "firmado") return (await regla(route, "contrato_no_firmado", "Para ganar, el contrato debe estar firmado por el cliente"), true);
                        if (d.acciones.confirmaObra && (cuerpo as Schema<"EntradaGanar">).confirmarObra !== true) {
                            return (await validacion(route, "confirmarObra", "La factibilidad requiere obra: confírmala"), true);
                        }
                        estadoA(p, "ganado");
                        cambiar(p, { ganadoEn: ahora, obraConfirmada: d.acciones.confirmaObra });
                        evento(p.id, "ganado");
                        if (conector) {
                            esc.altas.set(p.id, { modo: "argos", estado: "esperando_cliente", clienteArgosId: null, servicioArgosId: null, ordenId: null, ordenNumero: null });
                            esc.pendientes.set(p.id, [{ id: 900 + p.id, operacion: "crear_cliente", idempotencyKey: `iris:cliente:${p.id}`, estado: "pendiente", intentos: 0, ultimoError: null, version: 1 }]);
                        } else {
                            esc.altas.set(p.id, { modo: "local", estado: "completa", clienteArgosId: null, servicioArgosId: null, ordenId: 45, ordenNumero: "OT-000045" });
                        }
                        break;
                    }
                }
                await json(route, p);
                return true;
            }

            const c = /^\/api\/v1\/cotizaciones\/(\d+)\/(aceptar|pdf)$/.exec(ruta);
            if (metodo === "POST" && c) {
                const q = esc.cotizaciones.find((x) => x.id === Number(c[1]));
                if (!q) return (await noEncontrado(route), true);
                if (c[2] === "pdf") {
                    if (necesita("cotizaciones.crear")) return true;
                    q.pdf = doc();
                    return (await json(route, q), true);
                }
                if (necesita("cotizaciones.aceptar")) return true;
                if ((cuerpo as { version: number }).version !== q.version) return (await conflicto(route), true);
                if (q.vencida) return (await regla(route, "cotizacion_vencida", "La cotización venció; crea una revisión nueva"), true);
                Object.assign(q, { estado: "aceptada", aceptadaEn: ahora, aceptadaPor: actual.id, version: q.version + 1 });
                const p = esc.prospectos.find((x) => x.id === q.prospectoId);
                if (p) {
                    evento(p.id, "cotizacion_aceptada");
                    cambiar(p, {});
                }
                return (await json(route, q), true);
            }
            const k = /^\/api\/v1\/contratos\/(\d+)\/(firmar|pdf)$/.exec(ruta);
            if (metodo === "POST" && k) {
                const ct = esc.contratos.find((x) => x.id === Number(k[1]));
                if (!ct) return (await noEncontrado(route), true);
                if (k[2] === "pdf") {
                    if (necesita("contratos.crear")) return true;
                    ct.pdf = doc();
                    return (await json(route, ct), true);
                }
                if (necesita("contratos.firmar")) return true;
                const f = cuerpo as Schema<"EntradaFirmar">;
                if (f.version !== ct.version) return (await conflicto(route), true);
                if (!/^<svg[^>]*\swidth="[\d.]+"[^>]*\sheight="[\d.]+"/.test(f.svg) || !/<path d="M[\d.\s,MLCQHVZ-]+"/.test(f.svg)) {
                    return (await validacion(route, "svg", "La firma no es un SVG de trazos válido"), true);
                }
                Object.assign(ct, { estado: "firmado", firmanteNombre: f.firmanteNombre, firmanteDocumento: f.firmanteDocumento ?? null, firmadoEn: ahora, firmadoPor: actual.id, version: ct.version + 1, pdfFirmado: doc() });
                const p = esc.prospectos.find((x) => x.id === ct.prospectoId);
                if (p) {
                    evento(p.id, "contrato_firmado");
                    cambiar(p, {});
                }
                return (await json(route, ct), true);
            }
            return false;
        },
    };
}
