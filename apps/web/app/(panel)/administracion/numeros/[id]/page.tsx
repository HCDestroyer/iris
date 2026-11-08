import { redirect } from "next/navigation";

/** `/administracion/numeros/[id]` no tiene ficha propia: la miga del número lleva a sus credenciales. */
export default async function Pagina({ params }: { params: Promise<{ id: string }> }) {
    const { id } = await params;
    redirect(/^\d+$/.test(id) ? `/administracion/numeros/${id}/credenciales` : "/administracion/numeros");
}
