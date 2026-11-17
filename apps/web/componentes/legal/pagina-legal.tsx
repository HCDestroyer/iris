import { MarcaIris } from "@iris/ui";
import { Languages, Mail, MapPin, Phone } from "lucide-react";
import Link from "next/link";
import { SelectorTema } from "@/componentes/selector-tema";
import { documentoDe, type Bloque } from "@/lib/legal/contenido";
import { CLAVES, MARCA_PAGINA, RUTAS, TITULOS, type ClavePagina, type Idioma } from "@/lib/legal/paginas";
import { fechaISO, fechaLarga, ultimaActualizacion, type Titular } from "@/lib/legal/titular";

/*
 * Página legal pública (privacidad, términos o eliminación de datos): sin sesión, sin menú y sin
 * migas. Es un componente de servidor: todo el texto va en el HTML, para que lo lea el rastreador
 * de Meta sin ejecutar JavaScript. Los datos del titular llegan ya leídos (`titularDe`).
 */

const TEXTOS = {
    es: {
        actualizado: "Última actualización",
        indice: "Contenido",
        contacto: "Contacto",
        sinContacto: "Para cualquier consulta sobre este documento, escríbenos por el mismo canal en que te atendemos.",
        responsable: "Responsable",
        otroIdioma: "English",
        otroIdiomaAria: "Read this page in English",
        documentos: "Documentos legales",
        pie: "Atención al cliente con Iris",
    },
    en: {
        actualizado: "Last updated",
        indice: "Contents",
        contacto: "Contact",
        sinContacto: "For any question about this document, write to us through the same channel we use to assist you.",
        responsable: "Data controller",
        otroIdioma: "Español",
        otroIdiomaAria: "Leer esta página en español",
        documentos: "Legal documents",
        pie: "Customer service powered by Iris",
    },
} as const;

const ENLACE = "rounded-sm text-primary underline decoration-primary/40 underline-offset-4 hover:decoration-primary";

function Bloques({ bloques }: { bloques: readonly Bloque[] }) {
    return bloques.map((b, i) =>
        typeof b === "string" ? (
            <p key={i}>{b}</p>
        ) : (
            <ul key={i} className="grid list-disc gap-2 pl-5 marker:text-muted-foreground">
                {b.lista.map((item) => (
                    <li key={item}>{item}</li>
                ))}
            </ul>
        ),
    );
}

function Contacto({ titular, idioma }: { titular: Titular; idioma: Idioma }) {
    const t = TEXTOS[idioma];
    const hayContacto = titular.correo !== null || titular.telefono !== null || titular.direccion !== null;
    const direccion = [titular.direccion, titular.pais].filter((v) => v !== null).join(", ");
    return (
        <section id="contacto" aria-labelledby="titulo-contacto" className="grid scroll-mt-20 gap-3" data-slot="contacto-legal">
            <h2 id="titulo-contacto" className="text-lg font-semibold tracking-[-0.015em]">
                {t.contacto}
            </h2>
            {titular.conNombre ? (
                <p>
                    <span className="text-muted-foreground">{t.responsable}: </span>
                    <span className="font-medium">{titular.razonSocial ?? titular.nombre}</span>
                    {titular.razonSocial ? ` (${titular.nombre})` : null}
                </p>
            ) : null}
            {hayContacto ? (
                <ul className="grid gap-2">
                    {titular.correo ? (
                        <li className="flex items-start gap-2">
                            <Mail aria-hidden className="mt-1 size-4 shrink-0 text-muted-foreground" />
                            <a href={`mailto:${titular.correo}`} className={`${ENLACE} [overflow-wrap:anywhere]`}>
                                {titular.correo}
                            </a>
                        </li>
                    ) : null}
                    {titular.telefono ? (
                        <li className="flex items-start gap-2">
                            <Phone aria-hidden className="mt-1 size-4 shrink-0 text-muted-foreground" />
                            <a href={`tel:${titular.telefono.replace(/[^+\d]/g, "")}`} className={ENLACE}>
                                {titular.telefono}
                            </a>
                        </li>
                    ) : null}
                    {direccion ? (
                        <li className="flex items-start gap-2">
                            <MapPin aria-hidden className="mt-1 size-4 shrink-0 text-muted-foreground" />
                            <span>{direccion}</span>
                        </li>
                    ) : null}
                </ul>
            ) : (
                <p>{t.sinContacto}</p>
            )}
        </section>
    );
}

export function PaginaLegal({ clave, idioma, titular }: { clave: ClavePagina; idioma: Idioma; titular: Titular }) {
    const t = TEXTOS[idioma];
    const doc = documentoDe(clave, idioma, titular);
    const otro: Idioma = idioma === "es" ? "en" : "es";
    const fecha = ultimaActualizacion(titular);
    const marca = { [MARCA_PAGINA]: clave };
    return (
        <div className="flex min-h-dvh flex-col" lang={idioma} {...marca} data-idioma={idioma}>
            <header className="glass-material sticky top-0 z-20 border-b">
                <div className="mx-auto flex h-14 max-w-5xl items-center gap-3 px-4">
                    <MarcaIris />
                    {titular.conNombre ? <span className="min-w-0 truncate border-l pl-3 text-sm text-muted-foreground">{titular.nombre}</span> : null}
                    <div className="ml-auto flex shrink-0 items-center gap-3">
                        <Link href={RUTAS[clave][otro]} hrefLang={otro} lang={otro} aria-label={t.otroIdiomaAria} className={`inline-flex items-center gap-1.5 text-sm font-medium ${ENLACE}`}>
                            <Languages aria-hidden className="size-4" />
                            {t.otroIdioma}
                        </Link>
                        <SelectorTema className="hidden sm:inline-flex" />
                    </div>
                </div>
            </header>

            <main className="mx-auto w-full max-w-5xl flex-1 px-4 pt-8 pb-16">
                <div className="grid gap-2 border-b pb-6">
                    <h1 className="text-2xl font-semibold tracking-[-0.02em] sm:text-3xl">{TITULOS[clave][idioma]}</h1>
                    {titular.conNombre ? <p className="text-base text-muted-foreground">{titular.razonSocial ?? titular.nombre}</p> : null}
                    <p className="text-sm text-muted-foreground" data-slot="actualizado">
                        {t.actualizado}: <time dateTime={fechaISO(fecha)}>{fechaLarga(fecha, idioma)}</time>
                    </p>
                </div>

                <div className="grid gap-8 pt-6 lg:grid-cols-[15rem_minmax(0,1fr)] lg:gap-12">
                    <nav aria-label={t.indice} className="self-start rounded-[var(--hx-radius-card)] border bg-background-secondary p-4 lg:sticky lg:top-20" data-slot="indice-legal">
                        <p className="pb-2 text-sm font-semibold">{t.indice}</p>
                        <ol className="grid list-decimal gap-1.5 pl-5 text-sm marker:text-muted-foreground">
                            {doc.secciones.map((s) => (
                                <li key={s.id}>
                                    <a href={`#${s.id}`} className={ENLACE}>
                                        {s.titulo}
                                    </a>
                                </li>
                            ))}
                            <li>
                                <a href="#contacto" className={ENLACE}>
                                    {t.contacto}
                                </a>
                            </li>
                        </ol>
                    </nav>

                    <article className="grid max-w-[68ch] gap-8 text-[0.9375rem] leading-relaxed [overflow-wrap:anywhere]">
                        <div className="grid gap-3 text-base">
                            {doc.resumen.map((p) => (
                                <p key={p}>{p}</p>
                            ))}
                        </div>
                        {doc.secciones.map((s, i) => (
                            <section key={s.id} id={s.id} aria-labelledby={`titulo-${s.id}`} className="grid scroll-mt-20 gap-3">
                                <h2 id={`titulo-${s.id}`} className="text-lg font-semibold tracking-[-0.015em]">
                                    {i + 1}. {s.titulo}
                                </h2>
                                <Bloques bloques={s.bloques} />
                            </section>
                        ))}
                        <Contacto titular={titular} idioma={idioma} />
                    </article>
                </div>
            </main>

            <footer className="border-t px-4 py-6 text-sm text-muted-foreground">
                <div className="mx-auto flex max-w-5xl flex-col items-center gap-4 sm:flex-row sm:justify-between">
                    <nav aria-label={t.documentos}>
                        <ul className="flex flex-wrap justify-center gap-x-5 gap-y-2">
                            {CLAVES.map((c) => (
                                <li key={c}>
                                    <Link href={RUTAS[c][idioma]} aria-current={c === clave ? "page" : undefined} className={c === clave ? "font-medium text-foreground" : ENLACE}>
                                        {TITULOS[c][idioma]}
                                    </Link>
                                </li>
                            ))}
                        </ul>
                    </nav>
                    <SelectorTema className="sm:hidden" />
                    <span className="text-xs">{t.pie}</span>
                </div>
            </footer>
        </div>
    );
}
