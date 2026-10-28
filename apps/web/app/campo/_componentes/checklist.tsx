"use client";

import { valorDe, type ItemChecklist, type OrdenVista } from "@iris/sync";
import { Boton, Camara, Entrada, Etiqueta, GrupoSegmentado, InsigniaEstado, Tarjeta, TarjetaContenido, TarjetaDescripcion, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { Check, CircleDashed, X } from "lucide-react";
import { useId, useState } from "react";
import { esquemaNumeroItem } from "@/lib/campo/esquemas";
import { useCampo } from "./contexto";
import { EstadoAdjunto, Miniatura, useTomarFoto } from "./fotos";

/** Checklist de la plantilla: sí/no, texto, número, medición y foto. */
export function Checklist({ orden, editable }: { orden: OrdenVista; editable: boolean }) {
    const items = [...(orden.checklist ?? [])].sort((a, b) => a.posicion - b.posicion);
    if (items.length === 0) {
        return null;
    }
    const hechos = items.filter((i) => valorDe(i) !== null && valorDe(i) !== "").length;
    return (
        <Tarjeta>
            <TarjetaEncabezado>
                <TarjetaTitulo>Checklist</TarjetaTitulo>
                <TarjetaDescripcion>
                    {hechos} de {items.length} completos
                </TarjetaDescripcion>
            </TarjetaEncabezado>
            <TarjetaContenido>
                <ul className="grid divide-y divide-border">
                    {items.map((item) => (
                        <li key={item.itemId} className="grid gap-2 py-3 first:pt-0 last:pb-0">
                            <ItemFila orden={orden} item={item} editable={editable} />
                        </li>
                    ))}
                </ul>
            </TarjetaContenido>
        </Tarjeta>
    );
}

function Encabezado({ item }: { item: ItemChecklist }) {
    const v = valorDe(item);
    const marcado = v !== null && v !== "";
    return (
        <div className="flex items-start justify-between gap-2">
            <p className="text-sm font-medium">
                {item.etiqueta}
                {item.obligatorio ? <span className="ml-1 text-xs font-normal text-muted-foreground">(obligatorio)</span> : null}
            </p>
            {marcado ? null : <InsigniaEstado tono="neutral" icono={CircleDashed} texto="Sin marcar" />}
        </div>
    );
}

function ItemFila({ orden, item, editable }: { orden: OrdenVista; item: ItemChecklist; editable: boolean }) {
    const { motor, ejecutar, estado } = useCampo();
    const v = valorDe(item);
    const marcar = (valor: boolean | number | string) =>
        ejecutar(() => motor.registrar("checklist.marcar", orden.id, { itemId: item.itemId, valor }), `«${item.etiqueta}» guardado`);

    switch (item.tipo) {
        case "si_no":
            return (
                <>
                    <Encabezado item={item} />
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
            return <ItemFoto orden={orden} item={item} id={id} editable={editable} adjunto={estado.adjuntos.find((a) => a.id === id)} />;
        }
        default:
            return <ItemValor item={item} editable={editable} alGuardar={marcar} />;
    }
}

function ItemFoto({
    orden,
    item,
    id,
    editable,
    adjunto,
}: {
    orden: OrdenVista;
    item: ItemChecklist;
    id: string | null;
    editable: boolean;
    adjunto: Parameters<typeof EstadoAdjunto>[0]["adjunto"];
}) {
    const { motor, ejecutar } = useCampo();
    const { tomar, procesando } = useTomarFoto();
    return (
        <>
            <Encabezado item={item} />
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

function ItemValor({ item, editable, alGuardar }: { item: ItemChecklist; editable: boolean; alGuardar: (v: string | number) => Promise<boolean> }) {
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
            <Encabezado item={item} />
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
