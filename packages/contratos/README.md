# @iris/contratos

Contrato HTTP de la API de Iris para el panel (`apps/web`), la app de campo y la futura app de Expo.

- `openapi.json`: OpenAPI 3.1 **generado desde `apps/api`**. Nunca se edita a mano: `make openapi` lo
  sobrescribe con el que produce la API. Mientras `apps/api` no existe, trae un marcador con
  `GET /api/v1/salud` para que el paquete compile.
- `src/esquema.ts`: tipos generados con `openapi-typescript` (`pnpm contratos:build`). Tampoco se
  edita a mano.
- `src/index.ts`: exporta `paths`, `components`, `Schema<'X'>` y `crearCliente()` (cliente de
  `openapi-fetch` con `credentials: 'include'`, para la cookie de sesión del panel).

## Regenerar

```sh
make contratos     # make openapi (desde apps/api) + pnpm contratos:build
```

Si cambias un endpoint, regenera y commitea este paquete en la misma PR. CI (job `contratos`)
falla si lo commiteado no coincide con lo regenerado.

## Uso

```ts
import { crearCliente, type Schema } from "@iris/contratos";

const api = crearCliente({ baseUrl: process.env.IRIS_API_URL });
const { data, error } = await api.GET("/api/v1/salud");
const salud: Schema<"Salud"> | undefined = data;
```

No emite ni consume eventos.
