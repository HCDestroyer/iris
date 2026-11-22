import { EstadoVacio, Migas, Tarjeta, TarjetaContenido, type Miga } from "@iris/ui";
import { BookOpen } from "lucide-react";
import { MarkdownManual, type RutaClave } from "./markdown";

/** Una página del manual: migas, el contenido y un enlace al índice al pie. */
export function VistaPaginaManual({ fuente, migas, ruta, indice }: { fuente: string; migas: readonly Miga[]; ruta: RutaClave; indice: string }) {
    return (
        <>
            <Migas migas={migas} />
            <Tarjeta>
                <TarjetaContenido className="p-5 sm:p-7">
                    <article data-manual="pagina">
                        <MarkdownManual fuente={fuente} ruta={ruta} />
                    </article>
                </TarjetaContenido>
            </Tarjeta>
            <p className="text-sm text-muted-foreground">
                ¿No encontraste lo que buscabas?{" "}
                <a href={indice} className="font-medium text-primary underline underline-offset-2">
                    Ver el índice del manual
                </a>
            </p>
        </>
    );
}

/** Clave sin página: lo dice y lleva al índice (nunca una página en blanco). */
export function ManualNoEncontrado({ indice }: { indice: string }) {
    return (
        <Tarjeta>
            <EstadoVacio
                icono={BookOpen}
                titulo="Esta pantalla todavía no tiene manual"
                descripcion="Estamos escribiendo la ayuda de esta pantalla. Mientras tanto, busca en el índice del manual."
                accion={
                    <a href={indice} className="font-medium text-primary underline underline-offset-2">
                        Ir al índice del manual
                    </a>
                }
            />
        </Tarjeta>
    );
}
