import { Aviso } from "@iris/ui";
import { Info } from "lucide-react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { MarkdownSeguro } from "@/componentes/manuales/markdown";
import { AgregarDesdeGuia } from "./agregar";
import { SaltarAncla } from "./saltar-ancla";
import { SECCIONES_GUIA } from "@/lib/numeros/guia";

/**
 * «Cómo conectar WhatsApp»: la guía paso a paso dentro del panel (`lib/numeros/guia.ts`). Cada
 * sección tiene su anclaje para los «¿Dónde lo encuentro?» de «Agregar número»; un enlace con
 * `#token` cae en su sección al abrir (`SaltarAncla`).
 */
export function GuiaWhatsApp() {
    return (
        <div className="mx-auto grid w-full max-w-3xl min-w-0 gap-4">
            <SaltarAncla />
            <EncabezadoPagina
                titulo="Cómo conectar WhatsApp"
                descripcion="Paso a paso para conectar un número de WhatsApp a Iris con la API oficial de Meta."
                acciones={<AgregarDesdeGuia />}
            />
            <Aviso tono="info" icono={Info} titulo="Los menús de Meta cambian">
                Meta renombra seguido sus menús y no siempre los traduce igual. Entre paréntesis va el nombre en inglés; si no lo encuentras, busca el más parecido.
            </Aviso>
            <nav aria-label="Contenido de la guía" className="surface-card p-4 sm:p-5">
                <h2 className="mb-2 text-base font-semibold">Contenido</h2>
                <ol className="grid gap-1 text-sm sm:grid-cols-2">
                    {SECCIONES_GUIA.map((s) => (
                        <li key={s.id}>
                            <a href={`#${s.id}`} className="text-primary underline-offset-2 hover:underline">
                                {s.titulo}
                            </a>
                        </li>
                    ))}
                </ol>
            </nav>
            {SECCIONES_GUIA.map((s) => (
                <section key={s.id} id={s.id} aria-labelledby={`${s.id}-titulo`} className="surface-card grid min-w-0 scroll-mt-20 gap-3 p-4 sm:p-5">
                    <h2 id={`${s.id}-titulo`} className="text-lg font-semibold tracking-[-0.015em]">
                        {s.titulo}
                    </h2>
                    <MarkdownSeguro fuente={s.cuerpo} />
                </section>
            ))}
        </div>
    );
}
