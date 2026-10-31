import { notFound } from "next/navigation";
import Link from "next/link";

/** Pantalla de llegada de la demostración (solo con IRIS_DEMO_UI=1). */
export const dynamic = "force-dynamic";

export default function PaginaDestino() {
    if (process.env.IRIS_DEMO_UI !== "1") notFound();
    return (
        <main className="mx-auto grid max-w-3xl gap-4 p-4 sm:p-6">
            <h1 className="text-xl font-semibold">Destino de la demostración</h1>
            <Link href="/demo-ui" className="text-primary underline">
                Ir al formulario
            </Link>
        </main>
    );
}
