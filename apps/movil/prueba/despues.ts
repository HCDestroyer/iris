// Después de cargar Jest: en CI (o con la máquina cargada) el motor y los efectos tardan más que el
// segundo de espera por defecto de `findBy`/`waitFor`.
import { configure } from "@testing-library/react-native";

configure({ asyncUtilTimeout: 5000 });
