"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import {
    avisar,
    Aviso,
    Boton,
    Campo,
    CampoArea,
    CampoNumero,
    CampoSelector,
    Casilla,
    cn,
    Esqueleto,
    FormularioPagina,
    GrupoOpciones,
    GrupoSegmentado,
    liberarProteccion,
    SeccionFormulario,
    SelectorArchivo,
} from "@iris/ui";
import { CircleCheck, CircleX, Clock, ListChecks, Paperclip, Plus, Send, X } from "lucide-react";
import { useRouter } from "next/navigation";
import { useMemo, useState } from "react";
import { useFieldArray, useForm, useWatch } from "react-hook-form";
import { ErrorLectura } from "@/componentes/error-lectura";
import { ErrorApi } from "@/lib/api/errores";
import { subirAdjunto } from "@/lib/conversaciones/adjuntar";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import * as P from "@/lib/plantillas-whatsapp/api";
import { definicionDe, esquemaPlantilla, insertarDato, MAX, nombreMetaDe, revisar, revisarArchivoEjemplo, sincronizarVariables, type ValoresPlantilla } from "@/lib/plantillas-whatsapp/reglas";
import { CATEGORIAS, IDIOMAS, PERMISO_CREAR, RUTA, USOS, type DatoPWA, type DefinicionPWA, type PlantillaWADetalle } from "@/lib/plantillas-whatsapp/tipos";
import { useSesion } from "@/lib/sesion";
import { ErrorPlantilla, VistaPrevia } from "./comunes";

/*
 * Pantalla b de la maqueta: nueva plantilla, editar un borrador o «Corregir y volver a enviar» una que
 * ya está en Meta. Formulario de más de 3 campos: pantalla propia (FormularioPagina). La lista «Antes de
 * enviar a Meta» se calcula al escribir (reglas.ts, espejo del servidor); un borrador se guarda aunque
 * no las cumpla.
 */

export type ModoFormulario = { modo: "nueva"; waba: string; inicial?: ValoresPlantilla } | { modo: "borrador"; plantilla: PlantillaWADetalle } | { modo: "corregir"; plantilla: PlantillaWADetalle };

const VACIO: ValoresPlantilla = {
    etiqueta: "",
    nombre: "",
    idioma: "es",
    categoria: "UTILITY",
    usos: ["avisos", "conversaciones"],
    encabezado: "ninguno",
    encabezadoTexto: "",
    encabezadoAdjunto: "",
    cuerpo: "",
    pie: "",
    botones: [],
    variables: [],
    avisoSeguridad: true,
};

/** Los datos que se ofrecen primero al escribir (los demás, en «Más datos»). */
const DATOS_RAPIDOS = ["cliente_nombre", "monto_con_moneda", "fecha_corte", "plan", "cliente_codigo"];

/** El área del mensaje: «Insertar dato» pone el dato donde está el cursor. */
const ID_CUERPO = "plantilla-wa-cuerpo";

const TIPOS_BOTON = [
    { valor: "url", texto: "Abrir página" },
    { valor: "telefono", texto: "Llamar" },
    { valor: "respuesta", texto: "Respuesta rápida" },
    { valor: "copiar_codigo", texto: "Copiar código" },
] as const;

export function FormularioPlantillaWA(props: ModoFormulario) {
    const datos = useLectura((signal) => P.leerDatos(signal));
    if (datos.estado === "cargando") {
        return (
            <div className="mx-auto grid w-full max-w-3xl gap-3" aria-busy="true">
                <Esqueleto className="h-10 w-64" />
                <Esqueleto className="h-64" />
            </div>
        );
    }
    if (datos.estado === "error") return <ErrorLectura error={datos.error} reintentar={datos.reintentar} que="los datos de las plantillas" />;
    return <Formulario {...props} datos={datos.datos} />;
}

function Formulario(props: ModoFormulario & { datos: DatoPWA[] }) {
    const { datos } = props;
    const router = useRouter();
    const { puede } = useSesion();
    // Enviar a Meta es de quien crea plantillas (el servidor lo exige igual).
    const puedeEnviar = puede([PERMISO_CREAR]);
    const clave = useClaveIdempotencia();
    const claveEnvio = useClaveIdempotencia();
    const plantilla = props.modo === "nueva" ? null : props.plantilla;
    const waba = props.modo === "nueva" ? props.waba : props.plantilla.wabaId;
    const corregir = props.modo === "corregir";
    const aprobada = plantilla?.estado === "APPROVED" || plantilla?.estado === "PAUSED" || plantilla?.estado === "FLAGGED";
    const inicial = useMemo<ValoresPlantilla>(() => {
        if (props.modo === "nueva") return props.inicial ?? VACIO;
        const p = props.plantilla;
        const d = p.definicion;
        return {
            ...VACIO,
            etiqueta: p.etiqueta,
            nombre: p.nombre,
            idioma: p.idioma,
            categoria: p.categoria,
            usos: p.usos.filter((u): u is ValoresPlantilla["usos"][number] => ["avisos", "conversaciones", "asistente"].includes(u)),
            encabezado: d.encabezado?.tipo ?? "ninguno",
            encabezadoTexto: d.encabezado?.texto ?? "",
            encabezadoAdjunto: d.encabezado?.adjuntoId ?? "",
            cuerpo: d.cuerpo,
            pie: d.pie ?? "",
            botones: d.botones.map((b) => ({ tipo: b.tipo, texto: b.texto, url: b.url ?? "", telefono: b.telefono ?? "", ejemplo: b.ejemplo ?? "" })),
            variables: d.variables.map((x) => ({ nombre: x.nombre, ejemplo: x.ejemplo, ...(x.dato ? { dato: x.dato } : {}) })),
            ...(d.autenticacion?.caducaMinutos ? { caducaMinutos: d.autenticacion.caducaMinutos } : {}),
            avisoSeguridad: d.autenticacion?.avisoSeguridad ?? true,
        };
    }, [props]);
    const form = useForm<ValoresPlantilla>({ resolver: zodResolver(esquemaPlantilla), defaultValues: inicial });
    const botones = useFieldArray({ control: form.control, name: "botones" });
    const valores = useWatch({ control: form.control }) as ValoresPlantilla;
    const [nombreTocado, setNombreTocado] = useState(props.modo !== "nueva" || Boolean(props.inicial));
    const [error, setError] = useState<unknown>(null);
    const [subiendo, setSubiendo] = useState(false);
    const [errorArchivo, setErrorArchivo] = useState<string | null>(null);

    const auth = valores.categoria === "AUTHENTICATION";
    // Las variables siguen al texto: se conservan los ejemplos ya escritos.
    const definicion = useMemo((): DefinicionPWA => {
        const d = definicionDe({ ...VACIO, ...valores, botones: valores.botones ?? [], variables: valores.variables ?? [], usos: valores.usos ?? [] });
        return { ...d, variables: sincronizarVariables(d, datos) };
    }, [valores, datos]);
    const reglas = useMemo(() => revisar({ nombre: valores.nombre ?? "", idioma: valores.idioma ?? "", categoria: valores.categoria ?? "UTILITY", definicion }, datos), [valores, definicion, datos]);
    const fallan = reglas.filter((r) => !r.ok);

    const datoDe = (clave: string) => datos.find((d) => d.clave === clave);
    const etiquetaDe = (nombre: string) => {
        const v = definicion.variables.find((x) => x.nombre === nombre);
        return datoDe(v?.dato ?? nombre)?.etiqueta ?? nombre;
    };
    const ejemplo = (nombre: string) => definicion.variables.find((x) => x.nombre === nombre)?.ejemplo || etiquetaDe(nombre);

    function cambiarEtiqueta(texto: string) {
        form.setValue("etiqueta", texto, { shouldDirty: true });
        if (!nombreTocado) form.setValue("nombre", nombreMetaDe(texto), { shouldDirty: true });
    }

    function insertar(claveDato: string) {
        const area = document.getElementById(ID_CUERPO) as HTMLTextAreaElement | null;
        const texto = form.getValues("cuerpo");
        const desde = area?.selectionStart ?? texto.length;
        const hasta = area?.selectionEnd ?? texto.length;
        const r = insertarDato(texto, desde, hasta, claveDato);
        form.setValue("cuerpo", r.texto, { shouldDirty: true });
        requestAnimationFrame(() => {
            area?.focus();
            area?.setSelectionRange(r.cursor, r.cursor);
        });
    }

    function cambiarEjemplo(nombre: string, texto: string) {
        const lista = definicion.variables.map((v) => (v.nombre === nombre ? { ...v, ejemplo: texto } : v));
        form.setValue(
            "variables",
            lista.map((v) => ({ nombre: v.nombre, ejemplo: v.ejemplo, ...(v.dato ? { dato: v.dato } : {}) })),
            { shouldDirty: true },
        );
    }

    async function elegirArchivo(archivo: File) {
        setErrorArchivo(null);
        const tipo = valores.encabezado;
        if (tipo !== "imagen" && tipo !== "video" && tipo !== "documento") return;
        const problema = revisarArchivoEjemplo(tipo, archivo);
        if (problema) {
            setErrorArchivo(problema);
            return;
        }
        setSubiendo(true);
        try {
            const id = await subirAdjunto(archivo, waba, { entidad: "plantilla_whatsapp" });
            form.setValue("encabezadoAdjunto", id, { shouldDirty: true });
        } catch {
            setErrorArchivo("No se pudo subir el archivo de ejemplo. Vuelve a intentarlo.");
        } finally {
            setSubiendo(false);
        }
    }

    async function guardar(v: ValoresPlantilla, enviar: boolean) {
        setError(null);
        const d0 = definicionDe(v);
        const d: DefinicionPWA = { ...d0, variables: sincronizarVariables(d0, datos) };
        try {
            if (props.modo === "nueva") {
                const r = await P.crearPlantilla(
                    { wabaId: waba, etiqueta: v.etiqueta.trim(), nombre: v.nombre.trim(), idioma: v.idioma, categoria: v.categoria, usos: v.usos, definicion: d, enviar },
                    clave.actual(),
                );
                clave.renovar();
                avisar(
                    r.errorEnvio
                        ? { titulo: "Quedó como borrador", descripcion: "Meta no la aceptó todavía; revisa el error en la plantilla.", tono: "warning" }
                        : { titulo: enviar ? `«${r.etiqueta}» enviada a Meta` : `Borrador «${r.etiqueta}» guardado` },
                );
                await liberarProteccion();
                router.replace(`${RUTA}/${r.id}`);
                return;
            }
            const p = props.plantilla;
            if (props.modo === "borrador") {
                const g = await P.actualizarBorrador(p.id, { etiqueta: v.etiqueta.trim(), nombre: v.nombre.trim(), idioma: v.idioma, categoria: v.categoria, definicion: d, version: p.version }, clave.actual());
                clave.renovar();
                if (enviar) {
                    await P.enviar(p.id, g.version, claveEnvio.actual());
                    claveEnvio.renovar();
                }
                avisar({ titulo: enviar ? `«${g.etiqueta}» enviada a Meta` : `Borrador «${g.etiqueta}» guardado` });
            } else {
                const g = await P.corregirEnMeta(p.id, { definicion: d, version: p.version, ...(aprobada ? {} : { categoria: v.categoria }) }, clave.actual());
                clave.renovar();
                avisar({ titulo: `«${g.etiqueta}» enviada otra vez a Meta`, descripcion: "Meta la revisa en hasta 24 horas." });
            }
            await liberarProteccion();
            router.replace(`${RUTA}/${p.id}`);
        } catch (e) {
            clave.trasError(e);
            claveEnvio.trasError(e);
            if (!erroresACampos(e, ["etiqueta", "nombre", "idioma", "categoria"], form.setError)) setError(e);
        }
    }

    const titulo = props.modo === "nueva" ? "Nueva plantilla" : corregir ? `Corregir «${props.plantilla.etiqueta}»` : `Editar «${props.plantilla.etiqueta}»`;
    const conflicto = error instanceof ErrorApi && error.codigo === "conflicto_version";
    const enviando = form.formState.isSubmitting;
    const textoEnviar = corregir ? "Volver a enviar a Meta" : "Enviar a Meta";

    return (
        <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_20rem] xl:items-start">
            <FormularioPagina
                className="xl:max-w-none"
                titulo={titulo}
                descripcion="Escríbela como te gustaría que la lea el cliente. Iris revisa las reglas de Meta antes de enviarla."
                alEnviar={form.handleSubmit((v) => guardar(v, corregir))}
                navegar={router.push}
                volverA={plantilla ? `${RUTA}/${plantilla.id}` : `${RUTA}?cuenta=${encodeURIComponent(waba)}`}
                sucio={form.formState.isDirty}
                barra={{
                    textoGuardar: corregir ? textoEnviar : "Guardar borrador",
                    textoEnviando: "Guardando…",
                    enviando,
                    ...(corregir && fallan.length > 0 ? { faltan: fallan.map((r) => r.mensaje), motivo: `Corrige ${fallan.length === 1 ? "1 cosa" : `${fallan.length} cosas`} para poder enviarla.` } : {}),
                    nota: corregir ? (
                        aprobada ? (
                            "Meta deja editar una aprobada 1 vez al día y 10 al mes."
                        ) : null
                    ) : (
                        <span className="flex flex-wrap items-center gap-2">
                            {fallan.length > 0 ? (
                                <span className="flex items-center gap-1 text-error" data-slot="faltan-reglas">
                                    <CircleX aria-hidden className="size-4" />
                                    Corrige {fallan.length === 1 ? "1 cosa" : `${fallan.length} cosas`} para poder enviarla.
                                </span>
                            ) : null}
                            {puedeEnviar ? (
                                <Boton variante="secundario" disabled={enviando || fallan.length > 0} onClick={() => void form.handleSubmit((v) => guardar(v, true))()} data-accion="enviar-meta">
                                    <Send />
                                    {textoEnviar}
                                </Boton>
                            ) : null}
                        </span>
                    ),
                }}
                error={
                    error ? (
                        conflicto ? (
                            <Aviso tono="error" titulo="Alguien más cambió esta plantilla">
                                Vuelve a abrirla para ver el cambio.
                            </Aviso>
                        ) : (
                            <ErrorPlantilla error={error} alUsarNombre={(n) => (form.setValue("nombre", n, { shouldDirty: true }), setNombreTocado(true))} />
                        )
                    ) : null
                }
            >
                <SeccionFormulario titulo="1. Qué es">
                    <div className="grid gap-4 sm:grid-cols-2">
                        <div className="grid gap-1">
                            <Campo
                                etiqueta="Nombre"
                                maxLength={80}
                                autoComplete="off"
                                disabled={corregir}
                                value={valores.etiqueta ?? ""}
                                onChange={(e) => cambiarEtiqueta(e.target.value)}
                                error={form.formState.errors.etiqueta?.message}
                            />
                            {nombreTocado || corregir ? (
                                <Campo
                                    etiqueta="Nombre en Meta"
                                    className="font-mono"
                                    maxLength={MAX.nombre}
                                    autoComplete="off"
                                    disabled={corregir}
                                    error={form.formState.errors.nombre?.message}
                                    {...form.register("nombre")}
                                />
                            ) : (
                                <p className="text-xs text-muted-foreground">
                                    Nombre en Meta: <code className="font-mono">{valores.nombre || "…"}</code> ·{" "}
                                    <Boton variante="enlace" tamano="sm" className="h-auto p-0 text-xs" onClick={() => setNombreTocado(true)}>
                                        cambiar
                                    </Boton>
                                </p>
                            )}
                        </div>
                        <CampoSelector etiqueta="Idioma" ayuda="Meta no traduce: el texto va tal cual lo escribes." disabled={corregir} {...form.register("idioma")}>
                            {IDIOMAS.map((i) => (
                                <option key={i.valor} value={i.valor}>
                                    {i.texto}
                                </option>
                            ))}
                        </CampoSelector>
                    </div>
                    <GrupoOpciones
                        leyenda="Categoría"
                        nombre="categoria"
                        valor={valores.categoria}
                        alCambiar={(c) => form.setValue("categoria", c, { shouldDirty: true })}
                        disabled={corregir && aprobada}
                        opciones={CATEGORIAS.map((c) => ({ valor: c.valor, etiqueta: c.texto, detalle: c.detalle }))}
                        ayuda="Si Meta cree que un aviso de servicio en realidad promociona algo, lo aprueba como marketing y te lo decimos."
                    />
                    {props.modo === "nueva" ? (
                        <fieldset className="grid gap-2">
                            <legend className="mb-1 text-sm font-medium">Dónde la va a usar Iris</legend>
                            <div className="flex flex-wrap gap-x-4 gap-y-2">
                                {USOS.map((u) => (
                                    <Casilla
                                        key={u.valor}
                                        etiqueta={u.texto}
                                        checked={(valores.usos ?? []).includes(u.valor)}
                                        onChange={(e) =>
                                            form.setValue("usos", e.target.checked ? [...(valores.usos ?? []), u.valor] : (valores.usos ?? []).filter((x) => x !== u.valor), { shouldDirty: true })
                                        }
                                    />
                                ))}
                            </div>
                            <p className="text-xs text-muted-foreground">Puedes cambiarlo después, sin pasar por Meta.</p>
                        </fieldset>
                    ) : null}
                </SeccionFormulario>

                {auth ? (
                    <SeccionFormulario titulo="2. Código de verificación" descripcion="El texto lo pone Meta; solo eliges los minutos y el botón.">
                        <div className="grid gap-4 sm:grid-cols-2">
                            <CampoNumero
                                etiqueta="Caduca en (minutos)"
                                minimo={1}
                                maximo={90}
                                value={valores.caducaMinutos ?? ""}
                                onChange={(e) => form.setValue("caducaMinutos", e.target.value ? Number(e.target.value) : undefined, { shouldDirty: true })}
                            />
                            <Casilla etiqueta="Agregar «Por tu seguridad, no lo compartas»" checked={valores.avisoSeguridad ?? true} onChange={(e) => form.setValue("avisoSeguridad", e.target.checked, { shouldDirty: true })} />
                        </div>
                    </SeccionFormulario>
                ) : (
                    <>
                        <SeccionFormulario titulo="2. Encabezado (opcional)">
                            <GrupoSegmentado
                                etiqueta="Tipo de encabezado"
                                className="max-w-full overflow-x-auto"
                                valor={valores.encabezado}
                                alCambiar={(e) => form.setValue("encabezado", e, { shouldDirty: true })}
                                opciones={[
                                    { valor: "ninguno", etiqueta: "Ninguno" },
                                    { valor: "texto", etiqueta: "Texto" },
                                    { valor: "imagen", etiqueta: "Imagen" },
                                    { valor: "video", etiqueta: "Video" },
                                    { valor: "documento", etiqueta: "Documento" },
                                ]}
                            />
                            {valores.encabezado === "texto" ? (
                                <Campo etiqueta="Texto del encabezado" ayuda={`Hasta ${MAX.encabezado} letras y un dato.`} maxLength={200} autoComplete="off" {...form.register("encabezadoTexto")} />
                            ) : null}
                            {valores.encabezado === "imagen" || valores.encabezado === "video" || valores.encabezado === "documento" ? (
                                <div className="grid gap-1.5">
                                    <div className="flex flex-wrap items-center gap-2">
                                        <SelectorArchivo
                                            etiqueta="Elegir archivo de ejemplo"
                                            icono={Paperclip}
                                            variante="contorno"
                                            disabled={subiendo}
                                            accept={valores.encabezado === "imagen" ? "image/jpeg,image/png" : valores.encabezado === "video" ? "video/mp4" : "application/pdf"}
                                            alElegir={(a) => void elegirArchivo(a)}
                                        >
                                            {subiendo ? "Subiendo…" : valores.encabezadoAdjunto ? "Cambiar archivo" : "Elegir archivo de ejemplo"}
                                        </SelectorArchivo>
                                        {valores.encabezadoAdjunto ? (
                                            <span className="flex items-center gap-1 text-sm text-success">
                                                <CircleCheck aria-hidden className="size-4" />
                                                Archivo listo
                                            </span>
                                        ) : null}
                                    </div>
                                    <p className="text-xs text-muted-foreground">Meta pide un ejemplo; al enviar, cada cliente recibe el archivo de su aviso.</p>
                                    {errorArchivo ? <p className="text-sm text-error">{errorArchivo}</p> : null}
                                </div>
                            ) : null}
                        </SeccionFormulario>

                        <SeccionFormulario titulo="3. Mensaje" descripcion="Toca un dato para ponerlo donde está el cursor. Al enviar, Iris lo llena con lo de cada cliente.">
                            <div className="flex flex-wrap items-center gap-1.5" role="group" aria-label="Insertar dato">
                                <span className="text-sm text-muted-foreground">Insertar dato:</span>
                                {DATOS_RAPIDOS.map((c) => datoDe(c))
                                    .filter((d): d is DatoPWA => Boolean(d))
                                    .map((d) => (
                                        <Boton key={d.clave} variante="contorno" tamano="sm" onClick={() => insertar(d.clave)}>
                                            <Plus />
                                            {d.etiqueta}
                                        </Boton>
                                    ))}
                                <CampoSelector
                                    etiqueta="Más datos"
                                    tamano="sm"
                                    className="w-44"
                                    value=""
                                    onChange={(e) => {
                                        if (e.target.value) insertar(e.target.value);
                                    }}
                                >
                                    <option value="">Más datos…</option>
                                    {datos
                                        .filter((d) => !DATOS_RAPIDOS.includes(d.clave) && d.grupo !== "verificacion")
                                        .map((d) => (
                                            <option key={d.clave} value={d.clave}>
                                                {d.etiqueta}
                                            </option>
                                        ))}
                                </CampoSelector>
                            </div>
                            <CampoArea
                                etiqueta="Mensaje"
                                rows={5}
                                maxLength={4000}
                                ayuda={`${[...(valores.cuerpo ?? "")].length}/${MAX.cuerpo} · *negrita*, _cursiva_ y emojis como en WhatsApp.`}
                                error={reglas.find((r) => !r.ok && r.campo === "definicion.cuerpo")?.mensaje}
                                id={ID_CUERPO}
                                {...form.register("cuerpo")}
                            />
                            {definicion.variables.length > 0 ? (
                                <div className="grid gap-2" data-slot="ejemplos">
                                    <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">Ejemplos para Meta (obligatorios; nunca datos reales)</p>
                                    {definicion.variables.map((v) => {
                                        const d = datoDe(v.dato ?? "");
                                        return (
                                            <div key={v.nombre} className="grid items-center gap-2 rounded-lg border border-border p-2.5 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,14rem)]">
                                                <span className="w-fit rounded bg-primary/10 px-1.5 py-0.5 text-sm font-medium text-primary">{d?.etiqueta ?? v.nombre}</span>
                                                <span className="text-xs text-muted-foreground">{d?.origen ?? "Dato propio (escribe el ejemplo)"}</span>
                                                <Campo etiqueta={`Ejemplo de ${d?.etiqueta ?? v.nombre}`} className="[&>label]:sr-only" maxLength={200} value={v.ejemplo} onChange={(e) => cambiarEjemplo(v.nombre, e.target.value)} />
                                            </div>
                                        );
                                    })}
                                </div>
                            ) : null}
                        </SeccionFormulario>

                        <SeccionFormulario titulo="4. Pie (opcional)">
                            <Campo etiqueta="Pie" ayuda={`Texto gris bajo el mensaje. Hasta ${MAX.pie} letras, sin datos.`} maxLength={200} autoComplete="off" {...form.register("pie")} />
                        </SeccionFormulario>
                    </>
                )}

                <SeccionFormulario
                    titulo={auth ? "3. Botón" : "5. Botones (opcional)"}
                    descripcion={auth ? "Una plantilla de verificación lleva el botón «Copiar código»." : "El cliente los toca en el mensaje. Las respuestas rápidas van juntas, antes o después de los demás."}
                >
                    {botones.fields.length > 0 ? (
                        <ul className="grid gap-2">
                            {botones.fields.map((b, i) => {
                                const tipo = valores.botones?.[i]?.tipo ?? b.tipo;
                                return (
                                    <li key={b.id} className="grid gap-2 rounded-lg border border-border p-2.5 sm:grid-cols-[8rem_minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-end" data-boton={tipo}>
                                        <span className="text-sm text-muted-foreground sm:pb-2.5">{TIPOS_BOTON.find((t) => t.valor === tipo)?.texto}</span>
                                        {tipo !== "copiar_codigo" ? <Campo etiqueta="Texto del botón" maxLength={100} autoComplete="off" {...form.register(`botones.${i}.texto`)} /> : <span />}
                                        {tipo === "url" ? <Campo etiqueta="Dirección (https://…)" maxLength={4000} autoComplete="off" inputMode="url" {...form.register(`botones.${i}.url`)} /> : null}
                                        {tipo === "telefono" ? <Campo etiqueta="Teléfono (+502…)" maxLength={40} autoComplete="off" inputMode="tel" {...form.register(`botones.${i}.telefono`)} /> : null}
                                        {tipo === "copiar_codigo" ? <Campo etiqueta="Código de ejemplo" maxLength={20} autoComplete="off" {...form.register(`botones.${i}.ejemplo`)} /> : null}
                                        {tipo === "respuesta" ? <span className="text-xs text-muted-foreground sm:pb-2.5">Llega a la bandeja como mensaje del cliente</span> : null}
                                        <Boton variante="fantasma" tamano="icono" aria-label="Quitar botón" onClick={() => botones.remove(i)}>
                                            <X />
                                        </Boton>
                                    </li>
                                );
                            })}
                        </ul>
                    ) : null}
                    {botones.fields.length < MAX.botones ? (
                        <CampoSelector
                            etiqueta="Agregar botón"
                            tamano="sm"
                            className="w-52"
                            value=""
                            onChange={(e) => {
                                const t = e.target.value as (typeof TIPOS_BOTON)[number]["valor"];
                                if (t) botones.append({ tipo: t, texto: "", url: "", telefono: "", ejemplo: "" });
                            }}
                        >
                            <option value="">Agregar botón…</option>
                            {TIPOS_BOTON.filter((t) => !auth || t.valor === "copiar_codigo").map((t) => (
                                <option key={t.valor} value={t.valor}>
                                    {t.texto}
                                </option>
                            ))}
                        </CampoSelector>
                    ) : null}
                </SeccionFormulario>

                <SeccionFormulario
                    titulo={
                        <span className="flex flex-wrap items-center gap-2">
                            <ListChecks aria-hidden className="size-4" />
                            Antes de enviar a Meta
                            {fallan.length > 0 ? (
                                <span className="rounded-full bg-error/10 px-2 py-0.5 text-xs font-medium text-error">{fallan.length} por corregir</span>
                            ) : (
                                <span className="rounded-full bg-success/10 px-2 py-0.5 text-xs font-medium text-success">Lista</span>
                            )}
                        </span>
                    }
                >
                    <ul className="grid gap-1.5 text-sm" data-slot="reglas">
                        {reglas
                            .filter((r) => !auth || !["cuerpo", "variables_bordes"].includes(r.regla))
                            .map((r) => (
                                <li key={r.regla} className="flex items-start gap-2" data-regla={r.regla} data-ok={r.ok}>
                                    {r.ok ? <CircleCheck aria-hidden className="mt-0.5 size-4 shrink-0 text-success" /> : <CircleX aria-hidden className="mt-0.5 size-4 shrink-0 text-error" />}
                                    <span className={cn("min-w-0 break-words", !r.ok && "text-error")}>{r.mensaje}</span>
                                </li>
                            ))}
                    </ul>
                </SeccionFormulario>
            </FormularioPagina>

            <aside className="grid gap-3 xl:sticky xl:top-4" aria-label="Vista previa">
                <VistaPrevia definicion={definicion} categoria={valores.categoria ?? "UTILITY"} valor={ejemplo} />
                <Aviso tono="info" icono={Clock} titulo="Qué pasa al enviar">
                    Meta la revisa en hasta 24 horas. Te avisamos aquí y en la campana cuando la apruebe o la rechace.
                </Aviso>
            </aside>
        </div>
    );
}
