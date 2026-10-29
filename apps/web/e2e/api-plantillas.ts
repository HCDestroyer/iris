import type { Schema } from "@iris/contratos";
import type { Route } from "@playwright/test";

/*
 * Plantillas de formulario en la API falsa (e2e/api-falsa.ts), tipadas con packages/contratos y
 * fieles a internal/campo/plantillas.go: activas salvo `todas`, filtro por tipo (las del tipo y
 * las de cualquiera), versiones (editar una que estuvo activa crea la N+1; un borrador se
 * reescribe), `version` (409), código único (422 `codigo_duplicado`), comunes solo con
 * `empresas.todas` cuando hay empresas registradas (403) y la por defecto no se desactiva (422
 * `plantilla_por_defecto`).
 */

type Problema = Schema<"Problema">;
type Permiso = Schema<"Permiso">;
type Plantilla = Schema<"PlantillaFormulario">;
type Detalle = Schema<"PlantillaFormularioDetalle">;
type Item = Schema<"ItemPlantilla">;
type Version = Schema<"VersionPlantilla">;

const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
const problema = (route: Route, p: Problema) => route.fulfill({ status: p.status, contentType: "application/problem+json", body: JSON.stringify(p) });

const ahora = "2026-10-28T15:00:00Z";

/** Una plantilla con todas sus versiones (ítems por número de versión). */
export type PlantillaFalsa = {
    p: Plantilla;
    algunaVezActiva: boolean;
    versiones: (Version & { items: Item[]; descripcion: string; tipo: Plantilla["tipo"] })[];
};

let siguienteItem = 5000;
const items = (lista: Omit<Item, "id" | "posicion">[]): Item[] => lista.map((i, n) => ({ ...i, id: siguienteItem++, posicion: n + 1 }));

function plantilla(d: { id: number; codigo: string; nombre: string; tipo: Plantilla["tipo"]; empresaId: number | null; activa: boolean; porDefecto?: boolean; requiereFirma?: boolean; items: Omit<Item, "id" | "posicion">[]; versionesPrevias?: number[]; usos?: number[] }): PlantillaFalsa {
    const anteriores = (d.versionesPrevias ?? []).map((formularios, i) => ({
        id: d.id * 100 + i + 1,
        numero: i + 1,
        nombre: d.nombre,
        requiereFirma: d.requiereFirma ?? false,
        creadaPor: 1,
        creadaEn: "2026-10-01T15:00:00Z",
        formularios,
        items: items(d.items.slice(0, Math.max(1, d.items.length - 1))),
        descripcion: "",
        tipo: d.tipo,
    }));
    const numero = anteriores.length + 1;
    const actual = { id: d.id * 100 + numero, numero, nombre: d.nombre, requiereFirma: d.requiereFirma ?? false, creadaPor: 1, creadaEn: "2026-10-20T15:00:00Z", formularios: d.usos?.[0] ?? 0, items: items(d.items), descripcion: "", tipo: d.tipo };
    return {
        algunaVezActiva: d.activa || anteriores.length > 0,
        versiones: [...anteriores, actual],
        p: {
            id: d.id,
            codigo: d.codigo,
            nombre: d.nombre,
            descripcion: "",
            tipo: d.tipo,
            empresaId: d.empresaId,
            requiereFirma: d.requiereFirma ?? false,
            activa: d.activa,
            borrador: !d.activa && anteriores.length === 0,
            porDefecto: d.porDefecto ?? false,
            version: numero,
            versionId: actual.id,
            numeroVersion: numero,
            versionActual: numero,
            items: actual.items,
            creadoEn: "2026-10-01T15:00:00Z",
            actualizadoEn: "2026-10-20T15:00:00Z",
        },
    };
}

export function plantillasIniciales(): PlantillaFalsa[] {
    return [
        plantilla({
            id: 1,
            codigo: "instalacion_basica",
            nombre: "Instalación básica",
            tipo: "instalacion",
            empresaId: null,
            activa: true,
            porDefecto: true,
            requiereFirma: true,
            versionesPrevias: [3],
            usos: [5],
            items: [
                { codigo: "potencia", etiqueta: "Potencia óptica", tipo: "medicion", obligatorio: true, unidad: "dBm", opciones: null },
                { codigo: "antena", etiqueta: "Foto de la roseta", tipo: "foto", obligatorio: true, unidad: "", opciones: null },
                { codigo: "navega", etiqueta: "El cliente navega", tipo: "si_no", obligatorio: true, unidad: "", opciones: null },
            ],
        }),
        plantilla({
            id: 2,
            codigo: "seguridad",
            nombre: "Seguridad en altura",
            tipo: null,
            empresaId: null,
            activa: true,
            items: [
                { codigo: "arnes", etiqueta: "Usó arnés", tipo: "si_no", obligatorio: true, unidad: "", opciones: null },
                { codigo: "escalera", etiqueta: "Tipo de escalera", tipo: "seleccion", obligatorio: false, unidad: "", opciones: ["Tijera", "Extensión", "Ninguna"] },
            ],
        }),
        plantilla({
            id: 3,
            codigo: "visita_diagnostico",
            nombre: "Diagnóstico de visita",
            tipo: "visita",
            empresaId: null,
            activa: true,
            porDefecto: true,
            items: [{ codigo: "falla", etiqueta: "Falla encontrada", tipo: "texto", obligatorio: true, unidad: "", opciones: null }],
        }),
        plantilla({
            id: 4,
            codigo: "encuesta_vieja",
            nombre: "Encuesta (descontinuada)",
            tipo: null,
            empresaId: null,
            activa: false,
            versionesPrevias: [2],
            items: [{ codigo: "satisfecho", etiqueta: "¿Quedó satisfecho?", tipo: "si_no", obligatorio: false, unidad: "", opciones: null }],
        }),
    ];
}

export type ContextoPlantillas = {
    route: Route;
    metodo: string;
    ruta: string;
    query: URLSearchParams;
    cuerpo: unknown;
    idempotencia: string | undefined;
    puede: (p: Permiso) => boolean;
    /** Hay empresas registradas (con conector): lo común exige `empresas.todas`. */
    hayEmpresas: boolean;
    empresaId: number | null;
};

export type ApiPlantillas = {
    plantillas: PlantillaFalsa[];
    /** Plantilla con los ítems de su versión actual (para agregar formularios a una orden). */
    buscar: (id: number) => PlantillaFalsa | undefined;
    /** Otra persona cambió la plantilla: sube su versión. */
    cambiarVersion: (id: number) => void;
    /** Agrega `n` plantillas activas comunes de cualquier tipo («Relleno 01»…), para paginar. */
    agregarRelleno: (n: number) => void;
    manejar: (c: ContextoPlantillas) => Promise<boolean>;
};

export function apiPlantillas(): ApiPlantillas {
    const plantillas = plantillasIniciales();
    let siguiente = 100;

    function detalle(f: PlantillaFalsa, numero?: number): Detalle {
        const v = f.versiones.find((x) => x.numero === (numero ?? f.p.versionActual)) ?? f.versiones.at(-1);
        return {
            ...f.p,
            ...(v ? { versionId: v.id, numeroVersion: v.numero, items: v.items, nombre: v.nombre, requiereFirma: v.requiereFirma } : {}),
            versiones: [...f.versiones].reverse().map((x) => ({ id: x.id, numero: x.numero, nombre: x.nombre, requiereFirma: x.requiereFirma, creadaPor: x.creadaPor, creadaEn: x.creadaEn, formularios: x.formularios })),
        };
    }

    const itemsDe = (lista: Schema<"EntradaItemPlantilla">[]): Item[] =>
        lista.map((i, n) => ({ id: siguienteItem++, posicion: n + 1, codigo: i.codigo, etiqueta: i.etiqueta, tipo: i.tipo, obligatorio: i.obligatorio ?? false, unidad: i.unidad ?? "", opciones: i.opciones ?? null }));

    function validarItems(route: Route, lista: Schema<"EntradaItemPlantilla">[] | null | undefined): Promise<void> | undefined {
        const vistos = new Set<string>();
        for (const [n, i] of (lista ?? []).entries()) {
            if (vistos.has(i.codigo)) return problema(route, { type: "urn:iris:problema:validacion", title: "Datos inválidos", status: 422, errors: [{ field: `items[${n}].codigo`, message: "Código repetido en la plantilla" }] });
            vistos.add(i.codigo);
            if (i.tipo === "seleccion" && !(i.opciones ?? []).length) return problema(route, { type: "urn:iris:problema:validacion", title: "Datos inválidos", status: 422, errors: [{ field: `items[${n}].opciones`, message: "Al menos una opción" }] });
        }
        return undefined;
    }

    return {
        plantillas,
        buscar: (id) => plantillas.find((x) => x.p.id === id),
        cambiarVersion: (id) => {
            const f = plantillas.find((x) => x.p.id === id);
            if (f) f.p.version++;
        },
        agregarRelleno: (n) => {
            for (let i = 1; i <= n; i++) {
                const id = 900 + i;
                plantillas.push(
                    plantilla({
                        id,
                        codigo: `relleno_${String(i).padStart(2, "0")}`,
                        nombre: `Relleno ${String(i).padStart(2, "0")}`,
                        tipo: null,
                        empresaId: null,
                        activa: true,
                        items: [{ codigo: "ok", etiqueta: "Todo en orden", tipo: "si_no", obligatorio: false, unidad: "", opciones: null }],
                    }),
                );
            }
        },
        manejar: async ({ route, metodo, ruta, query, cuerpo, idempotencia, puede, hayEmpresas, empresaId }) => {
            if (!ruta.startsWith("/api/v1/plantillas")) return false;
            const negar = async () => {
                await problema(route, { type: "urn:iris:problema:prohibido", title: "Sin permiso", status: 403 });
                return true;
            };
            const visibles = plantillas.filter((f) => f.p.empresaId === null || f.p.empresaId === empresaId);
            const tocaComun = (f: { empresaId: number | null }) => f.empresaId !== null || !hayEmpresas || puede("empresas.todas");
            if (metodo !== "GET" && !idempotencia) {
                await problema(route, { type: "urn:iris:problema:validacion", title: "Falta Idempotency-Key", status: 422 });
                return true;
            }
            if (metodo === "GET" && ruta === "/api/v1/plantillas") {
                if (!puede("plantillas.ver")) return negar();
                const tipo = query.get("tipo");
                const todas = query.get("todas") === "true";
                // Como la #35: por nombre, con cursor y `limit` (50 por defecto).
                const lista = visibles
                    .filter((f) => (todas || f.p.activa) && (!tipo || f.p.tipo === null || f.p.tipo === tipo))
                    .map((f) => f.p)
                    .sort((a, b) => a.nombre.localeCompare(b.nombre, "es") || a.id - b.id);
                const desde = Number(query.get("cursor")?.replace("c:", "") ?? 0);
                const limite = Number(query.get("limit") ?? 50);
                const corte = lista.slice(desde, desde + limite + 1);
                await json(route, { data: corte.slice(0, limite), nextCursor: corte.length > limite ? `c:${desde + limite}` : null } satisfies Schema<"PaginaPlantillaFormulario">);
                return true;
            }
            if (metodo === "POST" && ruta === "/api/v1/plantillas") {
                if (!puede("plantillas.crear")) return negar();
                const e = cuerpo as Schema<"EntradaCrearPlantilla">;
                const comun = e.comun === true || !hayEmpresas;
                if (e.comun && !tocaComun({ empresaId: null })) return negar();
                const mal = validarItems(route, e.items);
                if (mal) return (await mal, true);
                const empresa = comun ? null : empresaId;
                if (plantillas.some((f) => f.p.codigo === e.codigo && f.p.empresaId === empresa)) {
                    await problema(route, { type: "urn:iris:problema:codigo_duplicado", title: "Regla de negocio", status: 422, detail: `Ya hay una plantilla con el código ${e.codigo}` });
                    return true;
                }
                const id = siguiente++;
                const its = itemsDe(e.items ?? []);
                const borrador = e.borrador === true;
                const f: PlantillaFalsa = {
                    algunaVezActiva: !borrador,
                    versiones: [{ id: id * 100 + 1, numero: 1, nombre: e.nombre, requiereFirma: e.requiereFirma ?? false, creadaPor: 1, creadaEn: ahora, formularios: 0, items: its, descripcion: e.descripcion ?? "", tipo: e.tipo ?? null }],
                    p: {
                        id,
                        codigo: e.codigo,
                        nombre: e.nombre,
                        descripcion: e.descripcion ?? "",
                        tipo: e.tipo ?? null,
                        empresaId: empresa,
                        requiereFirma: e.requiereFirma ?? false,
                        activa: !borrador,
                        borrador,
                        porDefecto: false,
                        version: 1,
                        versionId: id * 100 + 1,
                        numeroVersion: 1,
                        versionActual: 1,
                        items: its,
                        creadoEn: ahora,
                        actualizadoEn: ahora,
                    },
                };
                plantillas.push(f);
                await json(route, f.p, 201);
                return true;
            }
            const m = /^\/api\/v1\/plantillas\/(\d+)(?:\/(editar|activar|desactivar))?$/.exec(ruta);
            if (!m) return false;
            const f = visibles.find((x) => x.p.id === Number(m[1]));
            if (!f) {
                await problema(route, { type: "urn:iris:problema:no_encontrado", title: "No encontrado", status: 404 });
                return true;
            }
            const sub = m[2];
            if (metodo === "GET" && !sub) {
                if (!puede("plantillas.ver")) return negar();
                const n = query.get("numeroVersion");
                await json(route, detalle(f, n ? Number(n) : undefined));
                return true;
            }
            if (metodo !== "POST" || !sub) return false;
            if (!puede(`plantillas.${sub}` as Permiso) || !tocaComun(f.p)) return negar();
            const e = cuerpo as { version: number };
            if (e.version !== f.p.version) {
                await problema(route, { type: "urn:iris:problema:conflicto_version", title: "Conflicto de versión", status: 409, detail: "La plantilla cambió desde que la leíste" });
                return true;
            }
            if (sub === "activar" || sub === "desactivar") {
                const activa = sub === "activar";
                if (f.p.activa === activa) {
                    await problema(route, { type: "urn:iris:problema:sin_cambios", title: "Regla de negocio", status: 422, detail: "Ya estaba así" });
                    return true;
                }
                if (!activa && f.p.porDefecto) {
                    await problema(route, { type: "urn:iris:problema:plantilla_por_defecto", title: "Regla de negocio", status: 422, detail: `«${f.p.nombre}» es la plantilla por defecto de un tipo de orden: no se puede desactivar` });
                    return true;
                }
                Object.assign(f.p, { activa, borrador: false, version: f.p.version + 1 });
                if (activa) f.algunaVezActiva = true;
                await json(route, f.p);
                return true;
            }
            const ed = cuerpo as Schema<"EntradaEditarPlantilla">;
            const mal = validarItems(route, ed.items);
            if (mal) return (await mal, true);
            const its = itemsDe(ed.items ?? []);
            const actual = f.versiones.at(-1);
            const datos = { nombre: ed.nombre, requiereFirma: ed.requiereFirma ?? false, items: its, descripcion: ed.descripcion ?? "", tipo: ed.tipo ?? null };
            if (actual && !f.algunaVezActiva && actual.formularios === 0) {
                Object.assign(actual, datos);
            } else {
                const numero = (actual?.numero ?? 0) + 1;
                f.versiones.push({ id: f.p.id * 100 + numero, numero, creadaPor: 1, creadaEn: ahora, formularios: 0, ...datos });
            }
            const v = f.versiones.at(-1);
            Object.assign(f.p, { nombre: ed.nombre, descripcion: ed.descripcion ?? "", tipo: ed.tipo ?? null, requiereFirma: ed.requiereFirma ?? false, items: its, version: f.p.version + 1, versionId: v?.id, numeroVersion: v?.numero, versionActual: v?.numero });
            await json(route, f.p);
            return true;
        },
    };
}
