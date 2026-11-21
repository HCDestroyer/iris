import { z } from "zod";
import type { BotonPWA, Categoria, DatoPWA, DefinicionPWA, ReglaPWA, VariablePWA } from "./tipos";

/*
 * Espejo en el panel de `plantillas_wa_validar.go` (reglas de Meta antes de enviar): la lista «Antes
 * de enviar a Meta» se calcula al escribir, sin ir al servidor. El servidor vuelve a revisar todo al
 * enviar (y además si el nombre está libre en la cuenta).
 */

export const MAX = { nombre: 512, cuerpo: 1024, encabezado: 60, pie: 60, botones: 10, textoBoton: 25, url: 2000, telefono: 20, codigo: 20, botonesUrl: 2, botonesTel: 1, varsAuth: 15 } as const;

const RE_NOMBRE = /^[a-z0-9_]+$/;
const RE_IDIOMA = /^[a-z]{2,3}([_-][A-Za-z]{2,4})?$/;
const RE_VAR = /\{\{\s*([^{}]*?)\s*\}\}/g;
const RE_NOMBRE_VAR = /^[a-z][a-z0-9_]{0,59}$/;
const RE_POSICION = /^[1-9][0-9]?$/;
const RE_TEL = /^\+?[0-9]{6,19}$/;
const RE_SEGUIDAS = /\}\}\s*\{\{/;
const RE_VAR_FINAL = /\{\{\s*[^{}]+\s*\}\}$/;

export type EntradaReglas = { nombre: string; idioma: string; categoria: Categoria; definicion: DefinicionPWA };

export function variablesDe(texto: string | null | undefined): string[] {
    return [...(texto ?? "").matchAll(RE_VAR)].map((m) => m[1] ?? "");
}

const largo = (s: string) => [...s].length;
const emoji = (s: string) => /\p{Extended_Pictographic}/u.test(s);

/** Todas las variables de la plantilla, en orden (encabezado, cuerpo, URL de botones). */
export function todasLasVariables(d: DefinicionPWA): string[] {
    const enc = d.encabezado?.tipo === "texto" ? variablesDe(d.encabezado.texto) : [];
    const url = (d.botones ?? []).filter((b) => b.tipo === "url").flatMap((b) => variablesDe(b.url));
    return [...enc, ...variablesDe(d.cuerpo), ...url];
}

export function formatoDe(nombres: readonly string[]): "NAMED" | "POSITIONAL" | "" {
    const pos = nombres.filter((n) => RE_POSICION.test(n)).length;
    if (pos > 0 && pos < nombres.length) return "";
    return pos > 0 ? "POSITIONAL" : "NAMED";
}

type Reglas = Map<string, ReglaPWA>;

const BASE: readonly [string, string][] = [
    ["nombre", "Nombre en Meta con minúsculas, números y _ (hasta 512)"],
    ["idioma", "Idioma de Meta (es, es_MX…)"],
    ["categoria", "Categoría: servicio, marketing o verificación"],
    ["cuerpo", "Mensaje obligatorio, hasta 1024 caracteres"],
    ["encabezado", "Encabezado: texto de hasta 60 con un dato, o un archivo de ejemplo"],
    ["pie", "Pie de hasta 60 caracteres y sin datos"],
    ["variables_formato", "Datos con nombre o por posición, sin mezclar"],
    ["variables_bordes", "El mensaje no empieza ni termina con un dato"],
    ["variables_seguidas", "Datos separados por texto (no hay dos seguidos)"],
    ["ejemplos", "Cada dato tiene su ejemplo"],
    ["datos", "Cada dato es uno que Iris sabe llenar"],
    ["botones", "Botones: respuestas rápidas juntas; máximo 2 de página, 1 de llamar y 1 de copiar código"],
    ["largos", "Largos dentro del límite: encabezado 60, mensaje 1024, pie 60, botón 25"],
    ["autenticacion", "Verificación: sin enlaces, archivos ni emojis"],
];

/** Revisa la plantilla como el servidor. `datos`: el diccionario (para nombrar cada dato en llano). */
export function revisar(e: EntradaReglas, datos: readonly DatoPWA[] = []): ReglaPWA[] {
    const r: Reglas = new Map(BASE.map(([regla, mensaje]) => [regla, { regla: regla as ReglaPWA["regla"], ok: true, mensaje, campo: null }]));
    const falla = (regla: string, campo: string, mensaje: string) => {
        const x = r.get(regla);
        if (!x || !x.ok) return;
        r.set(regla, { ...x, ok: false, mensaje, campo });
    };
    const d = e.definicion;
    const etiqueta = (n: string) => {
        const v = d.variables?.find((x) => x.nombre === n);
        const clave = v?.dato ?? n;
        return datos.find((x) => x.clave === clave)?.etiqueta ?? n;
    };
    if (!RE_NOMBRE.test(e.nombre) || largo(e.nombre) > MAX.nombre) falla("nombre", "nombre", "El nombre en Meta solo lleva minúsculas sin tildes, números y _ (hasta 512)");
    if (!RE_IDIOMA.test(e.idioma)) falla("idioma", "idioma", "Elige un idioma de Meta (es, es_MX, en_US…)");
    const auth = e.categoria === "AUTHENTICATION";

    let varsEnc: string[] = [];
    const enc = d.encabezado;
    if (enc) {
        if (enc.tipo === "texto") {
            const t = (enc.texto ?? "").trim();
            varsEnc = variablesDe(t);
            if (t === "") falla("encabezado", "definicion.encabezado.texto", "Escribe el texto del encabezado o quítalo");
            else if (largo(t) > MAX.encabezado) falla("largos", "definicion.encabezado.texto", `El encabezado tiene ${largo(t)} letras: el límite es ${MAX.encabezado}`);
            else if (varsEnc.length > 1) falla("encabezado", "definicion.encabezado.texto", "El encabezado admite un solo dato");
            if (RE_SEGUIDAS.test(t)) falla("variables_seguidas", "definicion.encabezado.texto", "Hay dos datos pegados en el encabezado");
        } else if (enc.tipo !== "ubicacion" && !enc.adjuntoId) {
            falla("encabezado", "definicion.encabezado.adjuntoId", "Con imagen, video o documento, Meta pide un archivo de ejemplo");
        }
        if (auth) falla("autenticacion", "definicion.encabezado", "Una plantilla de verificación no lleva encabezado");
    }

    const cuerpo = (d.cuerpo ?? "").trim();
    const varsCuerpo = variablesDe(cuerpo);
    if (!auth) {
        if (cuerpo === "") falla("cuerpo", "definicion.cuerpo", "Escribe el mensaje");
        else if (largo(cuerpo) > MAX.cuerpo) falla("largos", "definicion.cuerpo", `El mensaje tiene ${largo(cuerpo)} letras: el límite es ${MAX.cuerpo}`);
        if (cuerpo !== "") {
            if (/^\{\{/.test(cuerpo) && varsCuerpo[0] !== undefined) falla("variables_bordes", "definicion.cuerpo", `Meta no acepta un mensaje que empieza con un dato. Agrega algo antes de «${etiqueta(varsCuerpo[0])}».`);
            const ultima = varsCuerpo[varsCuerpo.length - 1];
            if (cuerpo.endsWith("}}") && ultima !== undefined) falla("variables_bordes", "definicion.cuerpo", `Meta no acepta un mensaje que termina con un dato. Agrega algo después de «${etiqueta(ultima)}», como «. ¡Gracias!».`);
            if (RE_SEGUIDAS.test(cuerpo)) falla("variables_seguidas", "definicion.cuerpo", "Hay dos datos pegados sin texto entre ellos: Meta no sabe dónde termina uno. Deja una palabra o un signo entre los dos.");
        }
    }

    if (d.pie != null) {
        const p = d.pie.trim();
        if (largo(p) > MAX.pie) falla("largos", "definicion.pie", `El pie tiene ${largo(p)} letras: el límite es ${MAX.pie}`);
        else if (variablesDe(p).length > 0) falla("pie", "definicion.pie", "El pie no lleva datos");
        if (auth && emoji(p)) falla("autenticacion", "definicion.pie", "Una plantilla de verificación no lleva emojis");
    }

    const botones = d.botones ?? [];
    const cuenta: Record<string, number> = {};
    let bloques = 0;
    let previa = "";
    const varsUrl: string[] = [];
    if (botones.length > MAX.botones) falla("botones", "definicion.botones", `Meta admite hasta ${MAX.botones} botones`);
    botones.forEach((b, i) => {
        const campo = `definicion.botones[${i}]`;
        cuenta[b.tipo] = (cuenta[b.tipo] ?? 0) + 1;
        if (b.tipo === "respuesta" && previa !== "respuesta") bloques++;
        previa = b.tipo;
        if (b.tipo !== "copiar_codigo") {
            const t = b.texto.trim();
            if (t === "") falla("botones", `${campo}.texto`, "Escribe lo que dice el botón");
            else if (largo(t) > MAX.textoBoton) falla("largos", `${campo}.texto`, `El botón «${t}» tiene ${largo(t)} letras: el límite es ${MAX.textoBoton}`);
        }
        if (b.tipo === "url") {
            const u = (b.url ?? "").trim();
            const vs = variablesDe(u);
            let valida = false;
            try {
                const x = new URL(u.replace(RE_VAR, "x"));
                valida = (x.protocol === "https:" || x.protocol === "http:") && x.hostname !== "";
            } catch {
                valida = false;
            }
            if (u === "") falla("botones", `${campo}.url`, "Escribe la dirección de la página");
            else if (largo(u) > MAX.url) falla("largos", `${campo}.url`, `La dirección tiene ${largo(u)} caracteres: el límite es ${MAX.url}`);
            else if (!valida) falla("botones", `${campo}.url`, "La dirección debe empezar con https://");
            else if (vs.length > 1 || (vs.length === 1 && !RE_VAR_FINAL.test(u))) falla("botones", `${campo}.url`, "La dirección admite un solo dato y va al final");
            else if (vs.length === 1 && !(b.ejemplo ?? "").trim()) falla("ejemplos", `${campo}.ejemplo`, "Pon un ejemplo del dato del final de la dirección");
            varsUrl.push(...vs);
            if (auth) falla("autenticacion", campo, "Una plantilla de verificación no lleva enlaces");
        } else if (b.tipo === "telefono") {
            const t = (b.telefono ?? "").replace(/\s/g, "");
            if (largo(t) > MAX.telefono) falla("largos", `${campo}.telefono`, `El teléfono tiene más de ${MAX.telefono} caracteres`);
            else if (!RE_TEL.test(t)) falla("botones", `${campo}.telefono`, "El teléfono va con código de país (+502…)");
        } else if (b.tipo === "copiar_codigo") {
            const ej = (b.ejemplo ?? "").trim();
            if (ej === "") falla("ejemplos", `${campo}.ejemplo`, "Pon un código de ejemplo");
            else if (largo(ej) > MAX.codigo) falla("largos", `${campo}.ejemplo`, `El código de ejemplo tiene más de ${MAX.codigo} caracteres`);
        }
        if (auth && emoji(b.texto)) falla("autenticacion", `${campo}.texto`, "Una plantilla de verificación no lleva emojis");
    });
    if ((cuenta.url ?? 0) > MAX.botonesUrl) falla("botones", "definicion.botones", "Máximo 2 botones de página");
    else if ((cuenta.telefono ?? 0) > MAX.botonesTel) falla("botones", "definicion.botones", "Máximo 1 botón de llamar");
    else if ((cuenta.copiar_codigo ?? 0) > 1) falla("botones", "definicion.botones", "Máximo 1 botón de copiar código");
    else if (bloques > 1) falla("botones", "definicion.botones", "Las respuestas rápidas van juntas, antes o después de los demás botones");

    const todas = [...varsEnc, ...varsCuerpo, ...varsUrl];
    const formato = formatoDe(todas);
    if (formato === "") falla("variables_formato", "definicion.cuerpo", "No mezcles datos con nombre y datos por número");
    for (const [vs, campo] of [
        [varsEnc, "definicion.encabezado.texto"],
        [varsCuerpo, "definicion.cuerpo"],
    ] as const) {
        const vistos = new Set<string>();
        vs.forEach((n, i) => {
            if (formato === "POSITIONAL" && n !== String(i + 1)) falla("variables_formato", campo, "Los datos por número van en orden: {{1}}, {{2}}, {{3}}…");
            else if (formato === "NAMED" && !RE_NOMBRE_VAR.test(n)) falla("variables_formato", campo, `«${n}» no es un nombre de dato válido (minúsculas, números y _)`);
            else if (vistos.has(n)) falla("variables_formato", campo, `«${etiqueta(n)}» aparece dos veces: usa cada dato una vez`);
            vistos.add(n);
        });
    }
    const declaradas = new Map<string, VariablePWA>();
    (d.variables ?? []).forEach((v, i) => {
        const campo = `definicion.variables[${i}]`;
        if (declaradas.has(v.nombre)) falla("variables_formato", `${campo}.nombre`, "Dato repetido en la lista de ejemplos");
        declaradas.set(v.nombre, v);
        if (!todas.includes(v.nombre)) falla("ejemplos", campo, `«${etiqueta(v.nombre)}» tiene ejemplo pero no está en el mensaje: quítalo`);
        if (v.dato && datos.length > 0 && !datos.some((x) => x.clave === v.dato)) falla("datos", `${campo}.dato`, `Iris no conoce el dato «${v.dato}»`);
    });
    for (const n of todas) {
        if (!(declaradas.get(n)?.ejemplo ?? "").trim()) falla("ejemplos", "definicion.variables", `Falta el ejemplo de «${etiqueta(n)}» (Meta lo pide; nunca un dato real)`);
    }
    if (auth) {
        if (todas.length > MAX.varsAuth) falla("autenticacion", "definicion.variables", `Una plantilla de verificación admite hasta ${MAX.varsAuth} datos`);
        if ((cuenta.copiar_codigo ?? 0) !== 1) falla("autenticacion", "definicion.botones", "Una plantilla de verificación lleva un botón «Copiar código»");
    }
    return [...r.values()];
}

/** Las variables de la definición según el texto: conserva las que ya tenían ejemplo y dato. */
export function sincronizarVariables(d: DefinicionPWA, datos: readonly DatoPWA[]): VariablePWA[] {
    const antes = new Map((d.variables ?? []).map((v) => [v.nombre, v]));
    const vistos = new Set<string>();
    const out: VariablePWA[] = [];
    for (const n of todasLasVariables(d)) {
        if (vistos.has(n)) continue;
        vistos.add(n);
        const previa = antes.get(n);
        if (previa) {
            out.push(previa);
            continue;
        }
        const dato = datos.find((x) => x.clave === n);
        out.push(dato ? { nombre: n, dato: dato.clave, ejemplo: dato.ejemplo } : { nombre: n, ejemplo: "" });
    }
    return out;
}

/** El texto con los ejemplos (o los datos) en lugar de las variables, en partes para resaltarlas. */
export function partesVistaPrevia(texto: string, valores: (nombre: string) => string): { texto: string; dato: boolean }[] {
    const out: { texto: string; dato: boolean }[] = [];
    let i = 0;
    for (const m of texto.matchAll(RE_VAR)) {
        const desde = m.index ?? 0;
        if (desde > i) out.push({ texto: texto.slice(i, desde), dato: false });
        out.push({ texto: valores(m[1] ?? ""), dato: true });
        i = desde + m[0].length;
    }
    if (i < texto.length) out.push({ texto: texto.slice(i), dato: false });
    return out;
}

/** Propone el nombre en Meta desde el nombre para personas (igual que el servidor). */
export function nombreMetaDe(etiqueta: string): string {
    const s = etiqueta.trim().toLowerCase().normalize("NFD").replace(/\p{M}/gu, "");
    const n = s.replace(/[^a-z0-9]+/g, "_").replace(/^_+|_+$/g, "");
    return (n || "plantilla").slice(0, 500);
}

/** Inserta `{{clave}}` en la posición del cursor (con un espacio si hace falta). */
export function insertarDato(texto: string, desde: number, hasta: number, clave: string): { texto: string; cursor: number } {
    const antes = texto.slice(0, desde);
    const despues = texto.slice(hasta);
    const sep = antes === "" || /\s$/.test(antes) ? "" : " ";
    const token = `${sep}{{${clave}}}`;
    return { texto: antes + token + despues, cursor: antes.length + token.length };
}

/* Formulario (react-hook-form + zod): lo que el servidor rechaza de entrada. Las reglas de Meta van
 * aparte (lista «Antes de enviar»): un borrador se puede guardar aunque no las cumpla. */

const boton = z.object({
    tipo: z.enum(["respuesta", "url", "telefono", "copiar_codigo"]),
    texto: z.string().max(100),
    url: z.string().max(4000).optional(),
    telefono: z.string().max(40).optional(),
    ejemplo: z.string().max(200).optional(),
});

export const esquemaPlantilla = z.object({
    etiqueta: z.string().trim().min(1, "Escribe el nombre").max(80, "Hasta 80 caracteres"),
    nombre: z.string().trim().min(1, "Escribe el nombre en Meta").max(MAX.nombre).regex(RE_NOMBRE, "Solo minúsculas sin tildes, números y _"),
    idioma: z.string().regex(RE_IDIOMA, "Elige un idioma"),
    categoria: z.enum(["UTILITY", "MARKETING", "AUTHENTICATION"]),
    usos: z.array(z.enum(["avisos", "conversaciones", "asistente"])),
    encabezado: z.enum(["ninguno", "texto", "imagen", "video", "documento", "ubicacion"]),
    encabezadoTexto: z.string().max(200),
    cuerpo: z.string().max(4000, "Demasiado largo"),
    pie: z.string().max(200),
    botones: z.array(boton).max(20),
    variables: z.array(z.object({ nombre: z.string().max(60), dato: z.string().max(60).optional(), ejemplo: z.string().max(200) })).max(40),
    caducaMinutos: z.number().int().min(1).max(90).optional(),
    avisoSeguridad: z.boolean(),
});
export type ValoresPlantilla = z.infer<typeof esquemaPlantilla>;

/** Formulario → definición de la API. */
export function definicionDe(v: ValoresPlantilla): DefinicionPWA {
    const auth = v.categoria === "AUTHENTICATION";
    const d: DefinicionPWA = {
        cuerpo: auth ? "" : v.cuerpo,
        botones: v.botones.map((b): BotonPWA => {
            const x: BotonPWA = { tipo: b.tipo, texto: b.texto };
            if (b.tipo === "url") x.url = b.url ?? "";
            if (b.tipo === "telefono") x.telefono = b.telefono ?? "";
            if ((b.tipo === "url" && variablesDe(b.url).length > 0) || b.tipo === "copiar_codigo") x.ejemplo = b.ejemplo ?? "";
            return x;
        }),
        variables: v.variables.map((x) => (x.dato ? { nombre: x.nombre, dato: x.dato, ejemplo: x.ejemplo } : { nombre: x.nombre, ejemplo: x.ejemplo })),
    };
    if (!auth && v.encabezado !== "ninguno") d.encabezado = v.encabezado === "texto" ? { tipo: "texto", texto: v.encabezadoTexto } : { tipo: v.encabezado };
    if (!auth && v.pie.trim()) d.pie = v.pie;
    if (auth) d.autenticacion = { avisoSeguridad: v.avisoSeguridad, ...(v.caducaMinutos ? { caducaMinutos: v.caducaMinutos } : {}) };
    return d;
}

/** Definición de la API → formulario. */
export function valoresDe(p: { etiqueta: string; nombre: string; idioma: string; categoria: Categoria; usos: string[]; definicion: DefinicionPWA }): ValoresPlantilla {
    const d = p.definicion;
    return {
        etiqueta: p.etiqueta,
        nombre: p.nombre,
        idioma: p.idioma,
        categoria: p.categoria,
        usos: p.usos.filter((u): u is "avisos" | "conversaciones" | "asistente" => ["avisos", "conversaciones", "asistente"].includes(u)),
        encabezado: d.encabezado?.tipo ?? "ninguno",
        encabezadoTexto: d.encabezado?.texto ?? "",
        cuerpo: d.cuerpo,
        pie: d.pie ?? "",
        botones: (d.botones ?? []).map((b) => ({ tipo: b.tipo, texto: b.texto, url: b.url ?? "", telefono: b.telefono ?? "", ejemplo: b.ejemplo ?? "" })),
        variables: (d.variables ?? []).map((x) => ({ nombre: x.nombre, ejemplo: x.ejemplo, ...(x.dato ? { dato: x.dato } : {}) })),
        ...(d.autenticacion?.caducaMinutos ? { caducaMinutos: d.autenticacion.caducaMinutos } : {}),
        avisoSeguridad: d.autenticacion?.avisoSeguridad ?? true,
    };
}
