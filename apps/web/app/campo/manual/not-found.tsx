import { Migas } from "@iris/ui";
import { ManualNoEncontrado } from "@/componentes/manual/pagina";

/** Una pantalla de la app sin página en /campo/manual. */
export default function ManualCampoSinPagina() {
    return (
        <>
            <Migas migas={[{ etiqueta: "Manual de la app", href: "/campo/manual" }, { etiqueta: "Sin manual" }]} />
            <ManualNoEncontrado indice="/campo/manual" />
        </>
    );
}
