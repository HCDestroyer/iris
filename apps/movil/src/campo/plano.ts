// Primer o segundo plano de la app (AppState) para el chat: solo con la app al frente hay flujo SSE
// (o sondeo). Avisa solo cuando cambia (de `inactive` a `background` no repite), así el motor nunca
// abre ni cierra dos veces seguidas: una sola conexión y nada de batería en segundo plano.
import { AppState, type AppStateStatus } from "react-native";

const enSegundoPlano = (estado: string | null | undefined) => estado === "background" || estado === "inactive";

export function escucharPlano(fn: (alFrente: boolean) => void): () => void {
    // El motor del chat arranca «al frente». Si la app ya empezó en segundo plano (o inactiva), se le
    // avisa al suscribirse para que no abra el flujo; sin estado conocido (arranque), al frente.
    let alFrente = !enSegundoPlano(AppState.currentState);
    const s = AppState.addEventListener("change", (e: AppStateStatus) => {
        const ahora = e === "active";
        if (ahora !== alFrente) {
            alFrente = ahora;
            fn(ahora);
        }
    });
    if (!alFrente) {
        fn(false);
    }
    return () => s.remove();
}
