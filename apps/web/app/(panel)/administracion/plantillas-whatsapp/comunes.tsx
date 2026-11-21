"use client";

import type { Schema } from "@iris/contratos";

import { Aviso, Boton, cn, InsigniaEstado } from "@iris/ui";
import { CircleCheck, CircleDashed, CircleX, Clock, Copy, ExternalLink, KeyRound, PauseCircle, Phone, Reply, RotateCw, Trash2, type LucideIcon } from "lucide-react";
import Link from "next/link";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { partesVistaPrevia } from "@/lib/plantillas-whatsapp/reglas";
import { estadoVisible, type DefinicionPWA, type PlantillaWA } from "@/lib/plantillas-whatsapp/tipos";

const ICONOS: Partial<Record<PlantillaWA["estado"], LucideIcon>> = {
    APPROVED: CircleCheck,
    FLAGGED: CircleCheck,
    REJECTED: CircleX,
    PAUSED: PauseCircle,
    DELETED: Trash2,
    PENDING: Clock,
    IN_APPEAL: Clock,
    BORRADOR: CircleDashed,
};

/** Estado en Meta: icono + texto + color. */
export function EstadoPlantilla({ plantilla, className }: { plantilla: Pick<PlantillaWA, "estado" | "pausadaHasta" | "borradaPor">; className?: string }) {
    const e = estadoVisible(plantilla);
    const icono = ICONOS[plantilla.estado];
    return <InsigniaEstado tono={e.tono} texto={e.texto} {...(icono ? { icono } : {})} className={className} data-estado={plantilla.estado} />;
}

/** El mensaje como lo verá el cliente (burbuja de WhatsApp). `valor` da lo que va en cada dato. */
export function VistaPrevia({ definicion, categoria, valor, titulo = "Así lo verá el cliente", className }: { definicion: DefinicionPWA; categoria: PlantillaWA["categoria"]; valor: (nombre: string) => string; titulo?: string; className?: string }) {
    const auth = categoria === "AUTHENTICATION";
    const minutos = definicion.autenticacion?.caducaMinutos;
    const cuerpo = auth
        ? `{{codigo}} es tu código de verificación.${definicion.autenticacion?.avisoSeguridad ? " Por tu seguridad, no lo compartas." : ""}${minutos ? ` Este código caduca en ${minutos} minutos.` : ""}`
        : definicion.cuerpo;
    const valorAuth = (n: string) => (n === "codigo" ? (definicion.botones.find((b) => b.tipo === "copiar_codigo")?.ejemplo ?? "482915") : valor(n));
    const pinta = (texto: string) =>
        partesVistaPrevia(texto, auth ? valorAuth : valor).map((p, i) =>
            p.dato ? (
                <strong key={i} className="rounded bg-primary/10 px-0.5 font-semibold text-foreground">
                    {p.texto || "…"}
                </strong>
            ) : (
                <span key={i}>{p.texto}</span>
            ),
        );
    const enc = definicion.encabezado;
    const iconoBoton = (tipo: string): LucideIcon => (tipo === "url" ? ExternalLink : tipo === "telefono" ? Phone : tipo === "copiar_codigo" ? Copy : Reply);
    return (
        <figure className={cn("grid gap-2", className)} aria-label="Vista previa" data-slot="vista-previa">
            <figcaption className="text-xs font-medium text-muted-foreground">{titulo}</figcaption>
            <div className="rounded-xl bg-[color-mix(in_srgb,var(--color-success)_8%,var(--color-background))] p-3">
                <div className="max-w-[20rem] rounded-lg rounded-tl-none bg-card p-2.5 text-sm shadow-sm">
                    {enc && !auth ? (
                        enc.tipo === "texto" ? (
                            <p className="mb-1 font-semibold">{pinta(enc.texto ?? "")}</p>
                        ) : (
                            <div className="mb-2 grid h-24 place-items-center rounded-md bg-muted text-xs text-muted-foreground">{enc.tipo === "ubicacion" ? "Ubicación" : enc.tipo === "imagen" ? "Imagen" : enc.tipo === "video" ? "Video" : "Documento"}</div>
                        )
                    ) : null}
                    <p className="break-words whitespace-pre-wrap">{cuerpo ? pinta(cuerpo) : <span className="text-muted-foreground">Escribe el mensaje…</span>}</p>
                    {definicion.pie && !auth ? <p className="mt-1 text-xs text-muted-foreground">{definicion.pie}</p> : null}
                    <p className="mt-1 text-right text-[0.7rem] text-muted-foreground">10:45</p>
                </div>
                {definicion.botones.length > 0 ? (
                    <ul className="mt-1 grid max-w-[20rem] gap-1" aria-label="Botones del mensaje">
                        {definicion.botones.map((b, i) => {
                            const Icono = iconoBoton(b.tipo);
                            return (
                                <li key={i} className="flex items-center justify-center gap-1.5 rounded-lg bg-card py-1.5 text-sm font-medium text-primary shadow-sm">
                                    <Icono aria-hidden className="size-3.5" />
                                    {b.tipo === "copiar_codigo" ? b.texto || "Copiar código" : b.texto || "…"}
                                </li>
                            );
                        })}
                    </ul>
                ) : null}
            </div>
        </figure>
    );
}

/** Qué hacer con un error de Meta o de las reglas, en llano (pantalla e de la maqueta). */
export function ErrorPlantilla({ error, reintentar, alUsarNombre, titulo }: { error: unknown; reintentar?: () => void; alUsarNombre?: (nombre: string) => void; titulo?: string }) {
    const e = error instanceof ErrorApi ? error : null;
    const problema: Partial<Schema<"Problema">> = e?.problema && typeof e.problema === "object" ? (e.problema as Partial<Schema<"Problema">>) : {};
    const m = mensajeDeError(error);
    const fecha = (iso?: string) => (iso ? new Intl.DateTimeFormat("es-GT", { timeZone: "America/Guatemala", day: "numeric", month: "long", hour: "2-digit", minute: "2-digit", hourCycle: "h23" }).format(Date.parse(iso)) : "");
    const reintentarBoton = reintentar ? (
        <Boton variante="contorno" tamano="sm" onClick={reintentar}>
            <RotateCw />
            Reintentar
        </Boton>
    ) : null;
    switch (e?.codigo) {
        case "meta_sin_permiso":
            return (
                <Aviso tono="error" titulo="La conexión con Meta no deja ver ni crear plantillas" data-error="meta_sin_permiso">
                    <p>El token del número sirve para mandar mensajes, pero no para administrar plantillas. Quien administra Meta debe darle el permiso «Administrar la cuenta de WhatsApp» (whatsapp_business_management) y pegar el token nuevo en Iris.</p>
                    <div className="mt-2 flex flex-wrap gap-2">
                        <Boton asChild variante="contorno" tamano="sm">
                            <Link href="/administracion/numeros">
                                <KeyRound />
                                Ir a credenciales
                            </Link>
                        </Boton>
                        {reintentarBoton}
                    </div>
                </Aviso>
            );
        case "meta_token_invalido":
            return (
                <Aviso tono="error" titulo="Meta no acepta el token del número" data-error="meta_token_invalido">
                    <p>Venció o lo revocaron. Pega uno nuevo en «Números de WhatsApp».</p>
                    <div className="mt-2 flex flex-wrap gap-2">
                        <Boton asChild variante="contorno" tamano="sm">
                            <Link href="/administracion/numeros">
                                <KeyRound />
                                Ir a credenciales
                            </Link>
                        </Boton>
                        {reintentarBoton}
                    </div>
                </Aviso>
            );
        case "meta_no_responde":
            return (
                <Aviso tono="error" titulo="No pudimos hablar con Meta" data-error="meta_no_responde">
                    <p>Meta no contestó a tiempo. No cambiamos nada: ves las plantillas como estaban y los avisos siguen saliendo con las aprobadas.</p>
                    {reintentarBoton ? <div className="mt-2">{reintentarBoton}</div> : null}
                </Aviso>
            );
        case "nombre_en_uso":
            return (
                <Aviso tono="error" titulo="Ya hay una plantilla con ese nombre en este idioma" data-error="nombre_en_uso">
                    <p>Meta no deja dos con el mismo nombre en el mismo idioma. Tu plantilla quedó guardada; no se perdió nada.</p>
                    {problema.sugerencia && alUsarNombre ? (
                        <Boton variante="contorno" tamano="sm" className="mt-2" onClick={() => alUsarNombre(problema.sugerencia ?? "")}>
                            Usar «{problema.sugerencia}»
                        </Boton>
                    ) : null}
                </Aviso>
            );
        case "nombre_bloqueado":
            return (
                <Aviso tono="error" titulo={problema.disponibleEn ? `Ese nombre se libera el ${fecha(problema.disponibleEn)}` : "Ese nombre está bloqueado"} data-error="nombre_bloqueado">
                    <p>Una plantilla aprobada con ese nombre se borró hace poco, y Meta no deja reusarlo por 30 días. Usa otro nombre.</p>
                    {problema.sugerencia && alUsarNombre ? (
                        <Boton variante="contorno" tamano="sm" className="mt-2" onClick={() => alUsarNombre(problema.sugerencia ?? "")}>
                            Usar «{problema.sugerencia}»
                        </Boton>
                    ) : null}
                </Aviso>
            );
        case "limite_ediciones":
            return (
                <Aviso tono="warning" titulo={problema.disponibleEn ? `Podrás editarla de nuevo el ${fecha(problema.disponibleEn)}` : "Ya se editó hoy"} data-error="limite_ediciones">
                    <p>Meta deja editar una plantilla aprobada 1 vez al día y 10 veces en 30 días. Mientras tanto sigue funcionando como está.</p>
                </Aviso>
            );
        case "marketing_no_permitido":
            return (
                <Aviso tono="error" titulo="Las plantillas de marketing están apagadas" data-error="marketing_no_permitido">
                    <p>Actívalas en «Ajustes» de esta pantalla. Meta cobra más por cada mensaje de marketing.</p>
                </Aviso>
            );
        case "sincronizada_hace_poco": {
            const segundos = e?.reintentarEn ?? 0;
            return (
                <Aviso tono="info" titulo="Ya se sincronizó hace un momento" data-error="sincronizada_hace_poco">
                    <p>
                        Las plantillas están al día con Meta. Puedes volver a sincronizar {segundos > 0 ? `en ${segundos < 60 ? `${segundos} s` : `${Math.ceil(segundos / 60)} min`}` : "en un par de minutos"}; los
                        cambios de Meta también llegan solos.
                    </p>
                </Aviso>
            );
        }
        case "meta_limite":
            return (
                <Aviso tono="warning" titulo="Meta pide esperar" data-error="meta_limite">
                    <p>Meta limita cuántas plantillas se crean o cambian por hora. Espera un rato y vuelve a intentar.</p>
                    {reintentarBoton ? <div className="mt-2">{reintentarBoton}</div> : null}
                </Aviso>
            );
    }
    return (
        <Aviso tono="error" titulo={titulo ?? m.titulo} data-error={e?.codigo ?? "error"}>
            {m.detalle ? <p>{m.detalle}</p> : null}
            {e?.codigo === "reglas_meta" && e.errores.length > 0 ? (
                <ul className="mt-1 list-disc pl-5">
                    {e.errores.map((x, i) => (
                        <li key={i}>{x.message}</li>
                    ))}
                </ul>
            ) : null}
            {reintentarBoton ? <div className="mt-2">{reintentarBoton}</div> : null}
        </Aviso>
    );
}
