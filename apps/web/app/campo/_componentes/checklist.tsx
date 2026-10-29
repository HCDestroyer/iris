"use client";

import { formularioConRespuestas, valorDe, type FormularioOrden, type ItemChecklist, type OrdenVista, type PlantillaFormulario } from "@iris/sync";
import {
    Boton,
    Camara,
    Dialogo,
    DialogoContenido,
    DialogoPie,
    Entrada,
    Etiqueta,
    GrupoSegmentado,
    InsigniaEstado,
    Selector,
    Tarjeta,
    TarjetaContenido,
    TarjetaDescripcion,
    TarjetaEncabezado,
    TarjetaTitulo,
} from "@iris/ui";
import { Check, CircleAlert, CircleDashed, FilePlus2, Trash2, X } from "lucide-react";
import { useId, useState } from "react";
import { esquemaNumeroItem } from "@/lib/campo/esquemas";
import { faltantesPorFormulario, ORIGENES_FORMULARIO } from "@/lib/campo/textos";
import { useCampo } from "./contexto";
import { EstadoAdjunto, Miniatura, useTomarFoto } from "./fotos";

/** Clave de un ítem que falta para cerrar (`<formularioId>:<código>`). */
const claveFaltante = (formularioId: string | undefined, codigo: string) => `${formularioId ?? ""}:${codigo}`;

/**
 * Formularios de la orden: cada uno con sus ítems (el checklist viene plano con `formularioId`),
 * su versión y su origen. «Agregar formulario» suma uno del catálogo (también sin señal) y
 * «Quitar» solo el que agregó el técnico sin respuestas.
 */
export function Checklist({ orden, editable, agregar }: { orden: OrdenVista; editable: boolean; agregar: boolean }) {
    const { estado } = useCampo();
    const items = [...(orden.checklist ?? [])].sort((a, b) => a.posicion - b.posicion);
    const formularios = orden.formularios ?? [];
    const conFormulario = new Set(formularios.map((f) => f.id));
    const sueltos = items.filter((i) => !i.formularioId || !conFormulario.has(i.formularioId));
    // Lo que el servidor dijo que falta para cerrar (`cierre_incompleto`), por formulario.
    const rechazoCierre = estado.rechazos.find((op) => op.ordenId === orden.id && op.tipo === "orden.cerrar");
    const faltan = new Map(faltantesPorFormulario(rechazoCierre?.error).map((f) => [claveFaltante(f.formularioId, f.codigo), f.mensaje]));
    const disponibles = estado.plantillas.filter((p) => p.activa && !formularios.some((f) => f.plantillaId === p.id));

    if (items.length === 0 && !agregar) {
        return null;
    }
    return (
        <section aria-label="Formularios" className="grid gap-4">
            {formularios.map((f) => (
                <TarjetaFormulario
                    key={f.id}
                    orden={orden}
                    formulario={f}
                    items={items.filter((i) => i.formularioId === f.id)}
                    editable={editable}
                    faltan={faltan}
                />
            ))}
            {sueltos.length > 0 ? (
                <Tarjeta>
                    <TarjetaEncabezado>
                        <TarjetaTitulo>Checklist</TarjetaTitulo>
                        <TarjetaDescripcion>{resumen(sueltos)}</TarjetaDescripcion>
                    </TarjetaEncabezado>
                    <TarjetaContenido>
                        <ListaItems orden={orden} items={sueltos} editable={editable} faltan={faltan} />
                    </TarjetaContenido>
                </Tarjeta>
            ) : null}
            {agregar ? <AgregarFormulario orden={orden} disponibles={disponibles} /> : null}
        </section>
    );
}

function resumen(items: readonly ItemChecklist[]): string {
    const hechos = items.filter((i) => valorDe(i) !== null && valorDe(i) !== "").length;
    return `${hechos} de ${items.length} completos`;
}

function TarjetaFormulario({
    orden,
    formulario,
    items,
    editable,
    faltan,
}: {
    orden: OrdenVista;
    formulario: FormularioOrden;
    items: ItemChecklist[];
    editable: boolean;
    faltan: ReadonlyMap<string, string>;
}) {
    const { motor, ejecutar } = useCampo();
    const quitable = editable && formulario.origen === "tecnico" && !formularioConRespuestas(orden, formulario.id);
    const pendientes = items.filter((i) => faltan.has(claveFaltante(formulario.id, i.codigo))).length;
    // El servidor dijo qué falta en este formulario al rechazar el cierre (`cierre_incompleto`).
    return (
        <Tarjeta data-testid={`formulario-${formulario.codigo}`}>
            <TarjetaEncabezado>
                <div className="flex flex-wrap items-start justify-between gap-2">
                    <div className="grid gap-1">
                        <TarjetaTitulo>{formulario.nombre}</TarjetaTitulo>
                        <TarjetaDescripcion>
                            Versión {formulario.numeroVersion} · {ORIGENES_FORMULARIO[formulario.origen]} · {items.length > 0 ? resumen(items) : "sin ítems"}
                        </TarjetaDescripcion>
                    </div>
                    {quitable ? (
                        <Boton
                            variante="fantasma"
                            tamano="sm"
                            onClick={() =>
                                void ejecutar(() => motor.registrar("orden.quitar_formulario", orden.id, { formularioId: formulario.id }), `«${formulario.nombre}» quitado`)
                            }
                        >
                            <Trash2 aria-hidden />
                            Quitar
                        </Boton>
                    ) : null}
                </div>
                {pendientes > 0 ? (
                    <InsigniaEstado tono="error" icono={CircleAlert} texto={`Faltan ${pendientes} para cerrar`} className="w-fit" />
                ) : null}
            </TarjetaEncabezado>
            {items.length > 0 ? (
                <TarjetaContenido>
                    <ListaItems orden={orden} items={items} editable={editable} faltan={faltan} />
                </TarjetaContenido>
            ) : null}
        </Tarjeta>
    );
}

function ListaItems({
    orden,
    items,
    editable,
    faltan,
}: {
    orden: OrdenVista;
    items: readonly ItemChecklist[];
    editable: boolean;
    faltan: ReadonlyMap<string, string>;
}) {
    return (
        <ul className="grid divide-y divide-border">
            {items.map((item) => (
                <li key={`${item.formularioId ?? ""}-${item.itemId}`} className="grid gap-2 py-3 first:pt-0 last:pb-0">
                    <ItemFila orden={orden} item={item} editable={editable} falta={faltan.get(claveFaltante(item.formularioId, item.codigo))} />
                </li>
            ))}
        </ul>
    );
}

/** Sumar un formulario del catálogo del teléfono (versión que se ve = la que se guarda). */
function AgregarFormulario({ orden, disponibles }: { orden: OrdenVista; disponibles: readonly PlantillaFormulario[] }) {
    const { motor, ejecutar } = useCampo();
    const [abierto, setAbierto] = useState(false);
    const agregar = async (p: PlantillaFormulario) => {
        const ok = await ejecutar(() => motor.registrar("orden.agregar_formulario", orden.id, { plantillaId: p.id, versionId: p.versionId }), `«${p.nombre}» agregado`);
        if (ok) {
            setAbierto(false);
        }
    };
    return (
        <Dialogo open={abierto} onOpenChange={setAbierto}>
            <Boton variante="secundario" tamano="lg" onClick={() => setAbierto(true)}>
                <FilePlus2 aria-hidden />
                Agregar formulario
            </Boton>
            <DialogoContenido titulo="Agregar formulario" descripcion="Elige uno del catálogo. Se guarda en el teléfono y se envía al volver la señal.">
                {disponibles.length === 0 ? (
                    <p className="text-sm text-muted-foreground">No hay más formularios en el catálogo del teléfono.</p>
                ) : (
                    <ul className="grid gap-2" aria-label="Formularios disponibles">
                        {disponibles.map((p) => (
                            <li key={p.id}>
                                <Boton variante="secundario" className="h-auto w-full justify-start py-2 text-left whitespace-normal" onClick={() => void agregar(p)}>
                                    <span className="grid gap-0.5">
                                        <span className="font-semibold">{p.nombre}</span>
                                        <span className="text-xs font-normal text-muted-foreground">
                                            Versión {p.numeroVersion} · {(p.items ?? []).length} {(p.items ?? []).length === 1 ? "ítem" : "ítems"}
                                            {p.requiereFirma ? " · pide firma" : ""}
                                        </span>
                                    </span>
                                </Boton>
                            </li>
                        ))}
                    </ul>
                )}
                <DialogoPie>
                    <Boton variante="fantasma" onClick={() => setAbierto(false)}>
                        Volver
                    </Boton>
                </DialogoPie>
            </DialogoContenido>
        </Dialogo>
    );
}

function Encabezado({ item, falta }: { item: ItemChecklist; falta?: string }) {
    const v = valorDe(item);
    const marcado = v !== null && v !== "";
    return (
        <div className="flex items-start justify-between gap-2">
            <p className="text-sm font-medium">
                {item.etiqueta}
                {item.obligatorio ? <span className="ml-1 text-xs font-normal text-muted-foreground">(obligatorio)</span> : null}
            </p>
            {falta ? (
                <InsigniaEstado tono="error" icono={CircleAlert} texto="Falta para cerrar" title={falta} />
            ) : marcado ? null : (
                <InsigniaEstado tono="neutral" icono={CircleDashed} texto="Sin marcar" />
            )}
        </div>
    );
}

function ItemFila({ orden, item, editable, falta }: { orden: OrdenVista; item: ItemChecklist; editable: boolean; falta?: string }) {
    const { motor, ejecutar, estado } = useCampo();
    const v = valorDe(item);
    const marcar = (valor: boolean | number | string) =>
        ejecutar(() => motor.registrar("checklist.marcar", orden.id, { itemId: item.itemId, valor }), `«${item.etiqueta}» guardado`);

    switch (item.tipo) {
        case "si_no":
            return (
                <>
                    <Encabezado item={item} falta={falta} />
                    <GrupoSegmentado
                        etiqueta={item.etiqueta}
                        className={editable ? "w-full" : "pointer-events-none w-full opacity-70"}
                        opciones={[
                            { valor: "si", etiqueta: "Sí", icono: Check },
                            { valor: "no", etiqueta: "No", icono: X },
                        ]}
                        valor={v === true ? "si" : v === false ? "no" : undefined}
                        alCambiar={(x) => void marcar(x === "si")}
                    />
                </>
            );
        case "foto": {
            const id = typeof v === "string" ? v : null;
            return <ItemFoto orden={orden} item={item} id={id} editable={editable} falta={falta} adjunto={estado.adjuntos.find((a) => a.id === id)} />;
        }
        case "seleccion":
            return <ItemSeleccion item={item} editable={editable} falta={falta} alGuardar={marcar} />;
        default:
            return <ItemValor item={item} editable={editable} falta={falta} alGuardar={marcar} />;
    }
}

function ItemFoto({
    orden,
    item,
    id,
    editable,
    falta,
    adjunto,
}: {
    orden: OrdenVista;
    item: ItemChecklist;
    id: string | null;
    editable: boolean;
    falta?: string;
    adjunto: Parameters<typeof EstadoAdjunto>[0]["adjunto"];
}) {
    const { motor, ejecutar } = useCampo();
    const { tomar, procesando } = useTomarFoto();
    return (
        <>
            <Encabezado item={item} falta={falta} />
            <div className="flex items-center gap-3">
                {id ? <Miniatura id={id} alt={item.etiqueta} /> : null}
                {id ? <EstadoAdjunto adjunto={adjunto} /> : null}
                {editable ? (
                    <Camara
                        className="ml-auto"
                        tamano="md"
                        disabled={procesando}
                        aria-label={`${id ? "Cambiar foto" : "Tomar foto"}: ${item.etiqueta}`}
                        alElegir={async (archivo) => {
                            const nuevo = await tomar(archivo, { ordenId: orden.id, obligatorio: item.obligatorio, item: { itemId: item.itemId, codigo: item.codigo } });
                            if (nuevo) {
                                await ejecutar(() => motor.registrar("checklist.marcar", orden.id, { itemId: item.itemId, valor: nuevo }), "Foto guardada");
                            }
                        }}
                    >
                        {procesando ? "Procesando…" : id ? "Cambiar foto" : "Tomar foto"}
                    </Camara>
                ) : null}
            </div>
        </>
    );
}

/** `seleccion`: una de las opciones de la plantilla (pocas: botones; muchas: lista). */
function ItemSeleccion({
    item,
    editable,
    falta,
    alGuardar,
}: {
    item: ItemChecklist;
    editable: boolean;
    falta?: string;
    alGuardar: (v: string) => Promise<boolean>;
}) {
    const idCampo = useId();
    const v = valorDe(item);
    const opciones = item.opciones ?? [];
    const actual = typeof v === "string" ? v : "";
    return (
        <>
            <Encabezado item={item} falta={falta} />
            {opciones.length <= 3 ? (
                <GrupoSegmentado
                    etiqueta={item.etiqueta}
                    className={editable ? "w-full" : "pointer-events-none w-full opacity-70"}
                    opciones={opciones.map((o) => ({ valor: o, etiqueta: o }))}
                    valor={actual || undefined}
                    alCambiar={(x) => void alGuardar(x)}
                />
            ) : (
                <>
                    <Etiqueta htmlFor={idCampo} className="sr-only">
                        {item.etiqueta}
                    </Etiqueta>
                    <Selector id={idCampo} value={actual} disabled={!editable} onChange={(e) => e.target.value && void alGuardar(e.target.value)}>
                        <option value="">Elige una opción</option>
                        {opciones.map((o) => (
                            <option key={o} value={o}>
                                {o}
                            </option>
                        ))}
                    </Selector>
                </>
            )}
        </>
    );
}

function ItemValor({
    item,
    editable,
    falta,
    alGuardar,
}: {
    item: ItemChecklist;
    editable: boolean;
    falta?: string;
    alGuardar: (v: string | number) => Promise<boolean>;
}) {
    const idCampo = useId();
    const v = valorDe(item);
    const [texto, setTexto] = useState(v === null ? "" : String(v));
    const [error, setError] = useState<string | undefined>();
    const numerico = item.tipo === "numero" || item.tipo === "medicion";
    const guardar = async () => {
        if (numerico) {
            const r = esquemaNumeroItem.safeParse(texto);
            if (!r.success) {
                setError(r.error.issues[0]?.message);
                return;
            }
            setError(undefined);
            await alGuardar(r.data);
            return;
        }
        const t = texto.trim();
        if (t === "" || t.length > 2000) {
            setError(t === "" ? "Escribe el valor" : "Máximo 2000 caracteres");
            return;
        }
        setError(undefined);
        await alGuardar(t);
    };
    return (
        <>
            <Encabezado item={item} falta={falta} />
            <div className="grid gap-1.5">
                <Etiqueta htmlFor={idCampo} className="sr-only">
                    {item.etiqueta}
                </Etiqueta>
                <div className="flex gap-2">
                    <div className="relative flex-1">
                        <Entrada
                            id={idCampo}
                            value={texto}
                            disabled={!editable}
                            onChange={(e) => setTexto(e.target.value)}
                            inputMode={numerico ? "decimal" : "text"}
                            aria-invalid={error ? true : undefined}
                            className={item.unidad ? "pr-14" : undefined}
                        />
                        {item.unidad ? (
                            <span className="pointer-events-none absolute inset-y-0 right-3 grid place-content-center text-sm text-muted-foreground">{item.unidad}</span>
                        ) : null}
                    </div>
                    {editable ? (
                        <Boton variante="secundario" onClick={() => void guardar()} aria-label={`Guardar ${item.etiqueta}`}>
                            Guardar
                        </Boton>
                    ) : null}
                </div>
                {error ? <p className="text-sm text-error">{error}</p> : null}
            </div>
        </>
    );
}
