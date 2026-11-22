# Agente del manual: dependencias

Contrato con el botón «?» de cada pantalla: `docs/manual/claves.md` (pantalla → clave → archivo).

## Lo que pide a otros

- **Rediseño (`agente/rediseno`, `BotonManual({ clave })` en `@iris/ui`):**
    - usar las claves de `docs/manual/claves.md` (formato `modulo/pantalla`); si el rediseño crea el mismo
      archivo, se integran las dos tablas en uno;
    - dentro de `/campo`, enlazar a `/campo/manual/<pantalla>` (clave `campo/<pantalla>`): esas páginas las
      precachea el service worker y abren sin señal. `/manual/campo/<pantalla>` también sirve, pero solo con
      señal (redirige a `/campo/manual/…`; el service worker solo controla `/campo`);
    - en el panel, `/manual/<clave>`; una clave sin página muestra «Esta pantalla todavía no tiene manual».
- **Quien cambie una pantalla:** actualizar su `.md` en `docs/manual/` y correr
  `pnpm --filter @iris/web manual` (la prueba `lib/manual/contenido.test.ts` falla si el JSON no está al día).

## Lo que ofrece

- `/manual` (índice con buscador), `/manual/<modulo>/<pantalla>` y `/campo/manual[/<pantalla>]`.
- 78 páginas en `docs/manual/`, con las cinco partes: «Para qué sirve», «Cómo se usa», «Qué significa cada
  estado», «Preguntas frecuentes» y «Permisos necesarios».
