// Metro con Uniwind (Tailwind 4 para React Native): compila `src/global.css` (tema Horus para
// HeroUI Native) y convierte cada `className` en estilos al empaquetar. Expo ya resuelve el
// monorepo de pnpm. `withUniwindConfig` tiene que ser el envoltorio de más afuera.
const { getDefaultConfig } = require("expo/metro-config");
const { withUniwindConfig } = require("uniwind/metro");

const config = getDefaultConfig(__dirname);

module.exports = withUniwindConfig(config, {
    cssEntryFile: "./src/global.css",
    dtsFile: "./src/uniwind-types.d.ts",
});
