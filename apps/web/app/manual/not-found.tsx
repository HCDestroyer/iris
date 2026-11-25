import { Migas } from "@iris/ui";
import { ManualNoEncontrado } from "@/componentes/manual/pagina";

/** Una clave sin página en /manual. */
export default function ManualSinPagina() {
    return (
        <>
            <Migas migas={[{ etiqueta: "Manual", href: "/manual" }, { etiqueta: "Sin manual" }]} />
            <ManualNoEncontrado indice="/manual" />
        </>
    );
}
