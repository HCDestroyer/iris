// Resolutor de Jest: el de `react-native-reanimated/jest/resolver` (Reanimated y Worklets en su
// implementación de JS, sin módulo nativo) con una corrección para pnpm. El original decide por
// «la ruta contiene `react-native-worklets`», y con pnpm la carpeta de Reanimated también lo
// contiene (`.pnpm/react-native-reanimated@4.7.1_react-native-worklets@0.13.0…`): así cargaba
// la versión web de todo Reanimated, que pide react-native-web. Aquí se mira el paquete real.
const DENTRO_WORKLETS = /[\\/]node_modules[\\/]react-native-worklets[\\/]/;
const DENTRO_REANIMATED = /[\\/]node_modules[\\/]react-native-reanimated[\\/]/;

// Los módulos de Reanimated que en Jest usan su versión de JS (lista del resolutor original).
const SOLO_JS_EN_JEST = new Set([
    "initializers",
    "mutables",
    "mappers",
    "ConfigHelper",
    "UpdateLayoutAnimations",
    "useAnimatedRef",
    "useAnimatedStyle",
    "WorkletEventHandler",
    "JSPropsUpdater",
    "updateProps",
    "util",
    "css/component/AnimatedComponent",
]);

const sinNativo = (options) => ({ ...options, extensions: options.extensions?.filter((ext) => !ext.includes("native")) });

/** @type {import('jest-resolve').SyncResolver} */
module.exports = (request, options) => {
    const { defaultResolver, basedir } = options;
    const nombre = request.split("/").pop();
    const soloJs = [...SOLO_JS_EN_JEST].some((m) => (m.includes("/") ? request.endsWith(m) : nombre === m));
    if (request.startsWith(".") && soloJs && DENTRO_REANIMATED.test(`${basedir}/`)) {
        return defaultResolver(request, sinNativo(options));
    }
    if (DENTRO_WORKLETS.test(`${basedir}/`) || request === "react-native-worklets" || request.startsWith("react-native-worklets/")) {
        return defaultResolver(request, sinNativo(options));
    }
    return defaultResolver(request, options);
};
