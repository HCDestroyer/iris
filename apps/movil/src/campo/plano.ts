// Primer o segundo plano de la app (AppState) para el chat: solo con la app al frente hay flujo SSE
// (o sondeo). Avisa solo cuando cambia (de `inactive` a `background` no repite), así el motor nunca
// abre ni cierra dos veces seguidas: una sola conexión y nada de batería en segundo plano.
import { AppState, type AppStateStatus } from "react-native";

export function escucharPlano(fn: (alFrente: boolean) => void): () => void {
    // Sin estado conocido (arranque) cuenta como al frente: el motor arranca así.
    let alFrente = AppState.currentState !== "background" && AppState.currentState !== "inactive";
    const s = AppState.addEventListener("change", (e: AppStateStatus) => {
        const ahora = e === "active";
        if (ahora !== alFrente) {
            alFrente = ahora;
            fn(ahora);
        }
    });
    return () => s.remove();
}
