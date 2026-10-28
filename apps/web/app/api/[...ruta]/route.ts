import { reenviar } from "@/lib/proxy";

// Proxy hacia la API: ver lib/proxy.ts. Siempre dinámico y en Node (lee IRIS_API_URL al vuelo).
export const dynamic = "force-dynamic";
export const runtime = "nodejs";

const manejar = (peticion: Request) => reenviar(peticion);

export { manejar as DELETE, manejar as GET, manejar as HEAD, manejar as OPTIONS, manejar as PATCH, manejar as POST, manejar as PUT };
