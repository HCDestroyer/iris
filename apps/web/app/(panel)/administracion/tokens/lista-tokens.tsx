"use client";

import type { Schema } from "@iris/contratos";
import {
    avisar,
    Aviso,
    Boton,
    DialogoConfirmar,
    Esqueleto,
    EstadoVacio,
    InsigniaEstado,
    Interruptor,
    MenuDesplegable,
    MenuDesplegableContenido,
    MenuDesplegableDisparador,
    MenuDesplegableElemento,
    Tarjeta,
} from "@iris/ui";
import { Ban, Ellipsis, KeyRound, Trash2 } from "lucide-react";
import { useRef, useState } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { ErrorLectura } from "@/componentes/error-lectura";
import { PieLista } from "@/componentes/pie-lista";
import { api, claveTrasError, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { formatearFecha, haceCuanto } from "@/lib/fechas";
import { useLectura } from "@/lib/lectura";
import { usePaginado } from "@/lib/paginado";
import { useSesion } from "@/lib/sesion";
import { accionesToken, cuantosTokens, estadoToken, PRESENTACION_ESTADO, tituloLote, type TokenAPI } from "@/lib/tokens/presentacion";

type Accion = { tipo: "revocar" | "revocar_eliminar" | "eliminar"; token: TokenAPI } | { tipo: "lote"; cantidad: number };

/**
 * Administración → Tokens de API: los tokens iris_pat_ de personas e integraciones. Un token vigente
 * se revoca (deja de valer); uno revocado o vencido se elimina (sale de la lista y queda en la
 * auditoría). «Revocar y eliminar» hace las dos acciones con una sola confirmación.
 */
export function ListaTokens() {
    const { puede } = useSesion();
    const [todos, setTodos] = useState(false);
    const [version, setVersion] = useState(0);
    const puedeEliminar = puede(["tokens.eliminar"]);
    const eliminables = useLectura(async (signal) =>
        puedeEliminar ? await resolver(api().GET("/api/v1/tokens/eliminables", { signal })) : { cantidad: 0, enUso: 0 },
    );
    const refrescar = () => {
        setVersion((v) => v + 1);
        void eliminables.refrescar();
    };
    const [accion, setAccion] = useState<Accion | null>(null);
    const cantidad = eliminables.estado === "listo" ? eliminables.datos.cantidad : 0;

    return (
        <>
            <EncabezadoPagina
                titulo="Tokens de API"
                descripcion="Accesos de integraciones y scripts. Revoca un token para que deje de valer; elimínalo para quitarlo de la lista (lo que hizo sigue en la auditoría)."
            />
            <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
                <Interruptor etiqueta="Mostrar revocados y vencidos" checked={todos} onChange={(e) => setTodos(e.target.checked)} />
                {puedeEliminar && cantidad > 0 ? (
                    <Boton variante="secundario" onClick={() => setAccion({ tipo: "lote", cantidad })} data-testid="eliminar-inactivos">
                        <Trash2 />
                        Eliminar revocados y vencidos ({cantidad})
                    </Boton>
                ) : null}
            </div>
            <Lista key={`${todos}-${version}`} todos={todos} alAccion={setAccion} />
            {accion ? (
                <Confirmar
                    accion={accion}
                    alCancelar={() => setAccion(null)}
                    alHecho={() => {
                        setAccion(null);
                        refrescar();
                    }}
                />
            ) : null}
        </>
    );
}

function Lista({ todos, alAccion }: { todos: boolean; alAccion: (a: Accion) => void }) {
    const { puede } = useSesion();
    // Hora de la lectura (la lista se vuelve a montar al refrescar): vigente, vencido y «hace…».
    const [ahora] = useState(() => Date.now());
    const paginado = usePaginado<TokenAPI>((cursor, signal) =>
        resolver(
            api().GET("/api/v1/tokens", {
                params: { query: { limit: 100, ...(todos ? {} : { vigentes: true }), ...(cursor ? { cursor } : {}) } },
                ...(signal ? { signal } : {}),
            }),
        ),
    );
    // Nombres de los dueños (sin usuarios.ver, la tarjeta muestra el número).
    const usuarios = useLectura<Map<number, Schema<"Usuario">>>(async (signal) =>
        puede(["usuarios.ver"])
            ? new Map(((await resolver(api().GET("/api/v1/usuarios", { params: { query: { limit: 200 } }, signal }))).data ?? []).map((u) => [u.id, u]))
            : new Map(),
    );
    const { primera } = paginado;
    if (primera.estado === "cargando") {
        return (
            <div className="grid gap-3" aria-busy="true">
                <span className="sr-only" role="status">
                    Cargando tokens…
                </span>
                <Esqueleto className="h-20" />
                <Esqueleto className="h-20" />
            </div>
        );
    }
    if (primera.estado === "error") {
        return <ErrorLectura error={primera.error} que="los tokens" reintentar={primera.reintentar} />;
    }
    if (paginado.items.length === 0) {
        return (
            <Tarjeta>
                <EstadoVacio
                    icono={KeyRound}
                    titulo={todos ? "Sin tokens" : "Sin tokens vigentes"}
                    descripcion={todos ? "No hay tokens de API." : "Activa «Mostrar revocados y vencidos» para ver los demás."}
                />
            </Tarjeta>
        );
    }
    const nombres = usuarios.estado === "listo" ? usuarios.datos : new Map<number, Schema<"Usuario">>();
    return (
        <>
            <ul className="grid gap-3 md:grid-cols-2" aria-label="Tokens">
                {paginado.items.map((t) => {
                    const estado = estadoToken(t, ahora);
                    const p = PRESENTACION_ESTADO[estado];
                    const dueno = nombres.get(t.usuarioId);
                    const acciones = accionesToken(estado, puede(["tokens.revocar"]), puede(["tokens.eliminar"]));
                    return (
                        <li key={t.id} data-token={t.id}>
                            <Tarjeta className="flex h-full items-start gap-3 p-4">
                                <div className="grid min-w-0 flex-1 gap-1.5">
                                    <span className="truncate font-medium">{t.nombre}</span>
                                    <span className="truncate text-xs text-muted-foreground">
                                        <code>{t.prefijo}…</code> · {dueno ? `${dueno.nombre}${dueno.tipo === "integracion" ? " (integración)" : ""}` : `Usuario ${t.usuarioId}`}
                                    </span>
                                    <span>
                                        <InsigniaEstado tono={p.tono} icono={p.icono} texto={p.texto} />
                                    </span>
                                    <span className="text-xs text-muted-foreground">
                                        {estado === "revocado"
                                            ? `Revocado el ${formatearFecha(t.revocadoEn)}`
                                            : estado === "vencido"
                                              ? `Venció el ${formatearFecha(t.expiraEn)}`
                                              : [t.ultimoUso ? `Último uso ${haceCuanto(t.ultimoUso, ahora)}` : "Sin usar", t.expiraEn ? `vence ${formatearFecha(t.expiraEn)}` : "sin vencimiento"].join(" · ")}
                                    </span>
                                </div>
                                {acciones.length > 0 ? (
                                    <MenuDesplegable>
                                        <MenuDesplegableDisparador>
                                            <Boton variante="fantasma" tamano="sm" aria-label={`Acciones de ${t.nombre}`} className="size-9 shrink-0 p-0">
                                                <Ellipsis aria-hidden />
                                            </Boton>
                                        </MenuDesplegableDisparador>
                                        <MenuDesplegableContenido align="end" className="min-w-52">
                                            {acciones.map((a) => (
                                                <MenuDesplegableElemento key={a} icono={a === "revocar" ? Ban : Trash2} onSelect={() => alAccion({ tipo: a, token: t })}>
                                                    {a === "revocar" ? "Revocar" : a === "revocar_eliminar" ? "Revocar y eliminar" : "Eliminar"}
                                                </MenuDesplegableElemento>
                                            ))}
                                        </MenuDesplegableContenido>
                                    </MenuDesplegable>
                                ) : null}
                            </Tarjeta>
                        </li>
                    );
                })}
            </ul>
            <div className="mt-3 grid">
                <PieLista paginado={paginado} que="tokens" />
            </div>
        </>
    );
}

function Confirmar({ accion, alCancelar, alHecho }: { accion: Accion; alCancelar: () => void; alHecho: () => void }) {
    const [error, setError] = useState<unknown>(null);
    // Una clave por acción: si se reintenta tras un error de red, el servidor no repite lo hecho.
    const claves = useRef({ revocar: nuevaClaveIdempotencia(), eliminar: nuevaClaveIdempotencia() });

    async function confirmar() {
        setError(null);
        let paso: "revocar" | "eliminar" = "revocar";
        try {
            if (accion.tipo === "lote") {
                paso = "eliminar";
                const r = await resolver(api().POST("/api/v1/tokens/eliminar-inactivos", { params: { header: { "Idempotency-Key": claves.current.eliminar } }, body: {} }));
                avisar({ titulo: r.eliminados === 1 ? "1 token eliminado" : `${r.eliminados} tokens eliminados`, ...(r.omitidos > 0 ? { descripcion: `${cuantosTokens(r.omitidos)} siguen: los usa un conector activo.` } : {}) });
            } else {
                const id = accion.token.id;
                if (accion.tipo !== "eliminar") {
                    await resolver(api().POST("/api/v1/tokens/{id}/revocar", { params: { path: { id }, header: { "Idempotency-Key": claves.current.revocar } }, body: {} }));
                }
                if (accion.tipo !== "revocar") {
                    paso = "eliminar";
                    await resolver(api().POST("/api/v1/tokens/{id}/eliminar", { params: { path: { id }, header: { "Idempotency-Key": claves.current.eliminar } }, body: {} }));
                }
                avisar({ titulo: accion.tipo === "revocar" ? "Token revocado" : accion.tipo === "revocar_eliminar" ? "Token revocado y eliminado" : "Token eliminado" });
            }
            alHecho();
        } catch (e) {
            claves.current[paso] = claveTrasError(e, claves.current[paso]);
            setError(e);
        }
    }

    const nombre = accion.tipo === "lote" ? "" : accion.token.nombre;
    const textos = {
        revocar: { titulo: `¿Revocar «${nombre}»?`, descripcion: "Deja de valer ahora: quien lo use recibirá un error. Seguirá en la lista como revocado.", boton: "Revocar" },
        revocar_eliminar: {
            titulo: `¿Revocar y eliminar «${nombre}»?`,
            descripcion: "Deja de valer ahora y sale de la lista. Lo que hizo sigue en la auditoría. No se puede deshacer.",
            boton: "Revocar y eliminar",
        },
        eliminar: { titulo: `¿Eliminar «${nombre}»?`, descripcion: "Sale de la lista. Lo que hizo sigue en la auditoría. No se puede deshacer.", boton: "Eliminar" },
        lote: {
            titulo: accion.tipo === "lote" ? tituloLote(accion.cantidad) : "",
            descripcion: "Salen de la lista. Lo que hicieron sigue en la auditoría. Los que usa un conector activo se quedan. No se puede deshacer.",
            boton: "Eliminar",
        },
    }[accion.tipo];
    return (
        <DialogoConfirmar
            open
            onOpenChange={(a) => (a ? undefined : alCancelar())}
            peligro
            titulo={textos.titulo}
            descripcion={textos.descripcion}
            textoConfirmar={textos.boton}
            alConfirmar={confirmar}
        >
            {error ? (
                <Aviso tono="error" titulo="No se pudo completar">
                    {mensajeDeError(error).detalle ?? mensajeDeError(error).titulo}
                </Aviso>
            ) : null}
        </DialogoConfirmar>
    );
}
