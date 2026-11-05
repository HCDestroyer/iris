import { notFound } from "next/navigation";
import { DemoCapas } from "./capas";

/*
 * Capas flotantes de demostración (e2e/capas-flotantes.spec.ts). No existe en producción: responde
 * 404 salvo con IRIS_DEMO_UI=1.
 */
export const dynamic = "force-dynamic";

export default function PaginaCapas() {
    if (process.env.IRIS_DEMO_UI !== "1") notFound();
    return <DemoCapas />;
}
