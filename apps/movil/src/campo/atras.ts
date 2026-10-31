// «Atrás» de Android: vuelve a la vista anterior de la app; en «Hoy» sin nada debajo deja que el
// sistema salga de la app.
import { useEffect } from "react";
import { BackHandler } from "react-native";

export function useAtrasAndroid(atras: () => boolean): void {
    useEffect(() => {
        const s = BackHandler.addEventListener("hardwareBackPress", atras);
        return () => s.remove();
    }, [atras]);
}
