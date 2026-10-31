import { notFound } from "next/navigation";
import { DemoComponentes } from "./demo";

/*
 * Demostración de los componentes base de la fase 0 (HeroUI) para la prueba de humo de Playwright
 * (e2e/componentes-base.spec.ts). No existe en producción: responde 404 salvo con IRIS_DEMO_UI=1.
 */
export const dynamic = "force-dynamic";

export default function PaginaDemo() {
    if (process.env.IRIS_DEMO_UI !== "1") notFound();
    return <DemoComponentes />;
}
