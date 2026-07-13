# CLAUDE.md — core (go-kiban-fullstack)

Guía para Claude Code al trabajar en cualquier proyecto que consuma este shared lib. Las reglas de abajo aplican para **todos** los proyectos Kiban que embeban `go-kiban-fullstack`. Cada proyecto debe importar este archivo en su propio `CLAUDE.md` vía `@../go-kiban-fullstack/CLAUDE.md` y agregar abajo sus reglas específicas.

## Proyectos que consumen este shared lib

Cuando un cambio acá afecte a los consumidores (bump de versión, regla nueva, breaking change), revisar/propagar en todos. Repos locales hermanos (`../<repo>`):

**Apps:** `consulta-por-kiban`, `crm-backend`, `datos-non-stop`, `kiban-cloud-backend`, `klin-backend`, `microservices`, `rekon-backend`, `reportalos-backend`, `workfloo-backend`.
**Libs compartidas que también lo consumen:** `go-kiban`, `go-kiban-design-system`.

Mantener esta lista al día cuando se agregue un proyecto nuevo.

## Arquitectura en una línea

Clean / Hexagonal: `cmd → http → controller/view → usecases → domain`, con `repository/` y `infrastructure/service/` implementando los puertos del dominio. **Las dependencias apuntan al centro; el dominio no importa frameworks.**

## Estructura

```
cmd/api/                       entrypoint + wireDependencies + rutas
internal/
  config/                      lectura de env (embebe config.Base del shared lib)
  controller/http/<feature>/   handlers Gin (HTTP ↔ usecase)
  domain/<entity>/             entidades + errores + puertos (Go puro)
  infrastructure/              http router, appContext, services externos
  repository/<entity>/         adaptadores Mongo (BSON ↔ entidad)
  usecases/<feature>/<action>/ un paquete por acción, expone Execute()
  view/<page>/                 componentes templ (.templ compila a .go)
static/                        assets servidos tal cual
tests/integration/             suite opt-in con //go:build integration
```

## Comandos

```bash
go build ./...                                           # compila
go vet ./...                                             # estático
go test ./...                                            # unit tests
go test ./internal/usecases/... -coverprofile=tests/coverage/usecases.out  # unit tests con cobertura
go test -tags=integration ./tests/integration/... -v     # integración (requiere Docker)
go run ./cmd/api                                         # arranca local
templ generate                                           # regenera .templ → _templ.go
```

## Reglas para cambios de código

- **No crees archivos `*.md` ni `README`** salvo que se pida explícitamente.
- **Prefiere editar** archivos existentes antes que crear nuevos.
- Al arreglar un bug, **no refactorices** código colindante. Cambio mínimo.
- **Todo bug que se arregle debe quedar cubierto por un test de regresión** que reproduzca el bug y valide el fix. Detalle del flujo en [Tests de regresión para bugs](#tests-de-regresión-para-bugs).
- **No añadas comentarios obvios** ni docstrings para código que ya es autoexplicativo. Solo comenta lógica no evidente.
- **No añadas manejo de errores defensivo** para casos imposibles. Confía en garantías internas; valida solo en bordes (HTTP, repo, servicios externos).
- Si detectas un bug fuera del alcance del pedido, **menciónalo**; no lo arregles silenciosamente.
- **No hagas `git commit` ni `git push`** salvo que se pida explícitamente.

## Convenciones Go

### Manejo de errores

**Principio:** `errorsWrapper` (de `github.com/pkg/errors`) captura stacktrace; `fmt.Errorf` no. La regla decide según **dónde nace el error**: si el error se origina aquí (ya sea desde una librería externa o desde una condición nuestra), **siempre** se usa `errorsWrapper` para capturar el stack en el punto de origen. Si el error ya viene con stack capturado desde otra capa nuestra, se usa `fmt.Errorf` para solo añadir contexto sin duplicar frames.

La pregunta clave: **¿este error se está originando ahora en esta línea, o se está propagando desde otra función nuestra?**

- **Se origina ahora desde una librería externa** → `errorsWrapper.Wrap(err, "contexto")`.
  La función llama directamente a código de terceros (stripe, mongo driver, sendgrid, bcrypt, jwt, http.Client, os, etc.) y envuelve el error justo ahí, donde cruza la frontera al código nuestro. No depende de la carpeta: es quien hace la llamada. Ejemplos: `repository/*` envolviendo errores del driver Mongo, `infrastructure/service/*` envolviendo errores de Stripe/SendGrid/OAuth, o cualquier helper (incluso en `domain/`) que llame directo a una lib externa.

- **Se origina ahora desde una condición/validación en código nuestro** → `errorsWrapper.New("mensaje")` o, si hay sentinel, `errorsWrapper.Wrap(ErrSentinel, "contexto")`.
  El error **nace** en esta función porque una condición falló (`if !user.Verified`, `if len(items) == 0`, `if !slices.Contains(invalid, name)`, subscription sin items, token con signing method inválido, error fatal de arranque, etc.). No había error antes, lo estamos creando aquí. `pkg/errors` preserva `errors.Is` con `Unwrap`, así que `Wrap(sentinel, …)` sigue permitiendo comparar con el sentinel.

- **Se propaga un error que ya viene de otra capa nuestra** → `fmt.Errorf("contexto: %w", err)`.
  El `err` ya pasó por una función nuestra que lo originó con `errorsWrapper` (repo, service, usecase, helper propio). Nunca usar `errorsWrapper` aquí — duplicaría stacktrace. Ejemplos: usecase recibiendo un error del repo/service, controller recibiendo un error del usecase, middleware recibiendo un error de un servicio interno.

**Test rápido:**
- ¿El error sale de una función `package.Foo()` de un módulo externo, sin pasar antes por código nuestro? → `errorsWrapper.Wrap`.
- ¿No existía el error antes de esta línea y lo estás creando vos (con o sin sentinel)? → `errorsWrapper.New` / `errorsWrapper.Wrap(sentinel, …)`.
- ¿El error ya venía con stack capturado desde otra función nuestra? → `fmt.Errorf("...: %w", err)`.

**Excepción: callbacks pasados a librerías externas.** Cuando escribís una función que vos no llamás directamente sino que se la pasás a una lib externa para que ella la ejecute (ej: el `Keyfunc` de `jwt.ParseWithClaims`, handlers HTTP de middleware ajeno, callbacks de reintentos, etc.), ese callback corre *dentro* del flujo de la lib externa y el error que retorna vuelve a tu código recién cuando la lib te lo devuelve. Si capturás stacktrace dentro del callback **y** después con `Wrap` en la frontera, el stacktrace bueno (el de la frontera) sobrescribe al del callback y perdés la línea real. **En callbacks así, retorná el error sin stack** — usá `fmt.Errorf("…")` o `errors.New("…")`. El `errorsWrapper.Wrap` que envuelve la llamada a la lib externa captura el stack correcto, en la frontera donde el error vuelve a nuestro código.

Usar `errors.Is` / `errors.As` para comparaciones. Errores tipados (sentinels) viven en `internal/domain/<entity>/errors.go`.
- Un usecase = un paquete = un struct con `New*Usecase(...)` y `Execute(ctx, input) (..., error)`.
- Inputs de update parciales: campos opcionales como zero-value; el usecase ignora los vacíos.

### Binding de forms

**Regla:** ningún handler lee `c.PostForm("...")` directamente. Cada controller que acepta un `POST` con `application/x-www-form-urlencoded` define un **request struct** en `internal/controller/http/<feature>/requests.go` con tags `form:"..."` (nombre del input HTML) y `binding:"..."` (validaciones). El handler hace:

```go
var req loginRequest
if err := c.ShouldBind(&req); err != nil {
    fields := form_binding.FieldErrors(err)          // map[formField]mensaje (español)
    // … re-renderiza form con fields / mensaje global
    return
}
// usar req.Email, req.Password, …
```

**El nombre del input lo decide el struct tag.** No hay convención global snake_case vs camelCase — usá el que ya tenga el form/HTML. El compilador bloquea typos: `req.Nmae` no compila; `c.PostForm("nmae")` sí.

**Tags de validación soportados** (traducción en `internal/controller/http/binding/binding.go`): `required`, `email`, `min=N`, `max=N`, `len=N`, `url`, `oneof=a b c`, `gt=N`, `gte=N`, `eqfield=OtherField`. Si agregás un tag nuevo, extendé `messageFor` con su traducción; por default devuelve `"Valor inválido"`.

**Dónde vive qué:**
- El request struct y sus helpers (`toInput`, `values`, `trimmedX`) viven en el paquete del controller, **no** en el usecase. El usecase mantiene su tipo de input en `internal/usecases/<feature>/<action>/` y sigue ignorando HTTP.
- El trim (`strings.TrimSpace`) ocurre en el helper del request struct — el binding por sí solo no trimea.
- Validaciones que van más allá de los tags (regex custom, reglas condicionales, etc.) se hacen **después** del `ShouldBind`, acumulando en el mismo map `fieldErrs` que vuelve al template.

**`form_binding.Init()`** registra una `TagNameFunc` en el validator de Gin para que los errores usen el nombre del tag `form:` (no el nombre del struct field). Corre automáticamente desde `init()` al importar el paquete, así que main y tests ven el mismo comportamiento sin setup extra.

**Los templs usan `<input name="...">` que debe coincidir con el tag `form:"..."` del request struct.** Solo importa la coherencia entre tag y HTML — no hay restricción sobre mayúsculas o underscores.

## Convenciones de vistas (templ)

- Archivos `.templ` en `internal/view/<page>/`; generan `*_templ.go` que **se commitea**.
- Cuando modifiques un `.templ`, ejecuta `templ generate` antes de correr tests.
- HTMX para interactividad: `hx-post`, `hx-target`, `hx-swap` — no JavaScript custom salvo casos puntuales.
- Renderizado server-side vía `view.Render(c, status, Component(data))`.

### Texto en español: signos de pregunta y exclamación

Todo texto visible al usuario está en español. **Las preguntas siempre abren con `¿` y cierran con `?`** — la apertura no es opcional. Aplica a títulos de página, copys de botones/links, mensajes en `hx-confirm`, banners de error o éxito y cualquier label. Lo mismo con exclamaciones: abren con `¡` y cierran con `!`.

Ejemplos:
- ✅ `¿No tienes cuenta?`, `¿Olvidaste tu contraseña?`, `¿Estás seguro de eliminar este contacto?`
- ❌ `No tienes cuenta?`, `Olvidaste tu contraseña?`, `Estás seguro de eliminar este contacto?`

Cuando agregues una pregunta nueva en cualquier `.templ`, abrí siempre con `¿`. Si refactorizás un componente compartido (ej. modales de confirmación en `view/common/`), respetá el mismo criterio.

### Tipos que reciben las views — cuándo domain, cuándo viewModel

**Regla:** si el struct de dominio tiene **algún campo sensible**, las views reciben un *View* definido en `internal/view/common/` y el controller construye el *View* con `common.ToXxxView(...)` antes de llamar a `view.Render`. Si el struct de dominio **no** tiene campos sensibles, se pasa directo.

**Qué cuenta como "sensible":**
- **Secretos que nunca deben salir del servidor**: hashes de password, códigos OTP, tokens OAuth, API keys.
- **Identificadores internos de pago**: IDs de Stripe (Customer, Subscription). El View los reemplaza por booleans (`HasStripeCustomer`, `HasActiveSubscription`). Si un flujo backoffice legítimamente necesita el ID (ej. link a dashboard de Stripe), el controller lo pasa como **campo explícito separado** en la page-data — jamás re-embediendo el dominio crudo.

El proyecto consumidor debe mantener un test de regresión (`TestNoSensitiveFieldsInTemplates` o equivalente) que recorra todos los `.templ` y falle si encuentra accesos prohibidos. Cuando agregues un campo sensible nuevo al dominio, añadí su regex al test.

**Cuándo crear un nuevo View en `internal/view/common/`:**
1. El domain gana un campo sensible (secreto o identificador interno).
2. Repetís el mismo mapeo en >1 controller.
Caso contrario, un DTO local en `internal/view/<page>/types.go` sigue siendo aceptable si sólo carga estado de formulario (`FormData{Values, Error, ...}`).

## Convenciones de tests

### Unit tests

- Al lado del código: `foo.go` + `foo_test.go`, mismo package.
- **Testify** (`github.com/stretchr/testify/assert`, `require`) para aserciones.
- **Mocks manuales** en el propio `_test.go`: struct que implementa la interfaz del usecase, con campos para retornos (`user`, `findErr`) y flags de invocación (`findByEmailCalled`). No usar mockery ni gomock.
- Helpers compartidos vienen del shared lib `github.com/kiban-cloud/go-kiban-fullstack/pkg/testutils`:
  - `testutils.ErrorAssertions(t, err, target, mustHaveStackTrace)` cuando el error contiene un sentinel (`errors.Is`).
  - `testutils.ErrorMessageAssertions(t, err, msg, mustHaveStackTrace)` cuando el usecase crea el error con `errorsWrapper.New` sin sentinel.
  - El proyecto puede wrappar localmente (ej. `internal/testUtils/`) para agregar helpers específicos de dominio.
- Estructura: `TestXxxUsecase_Execute` con subtests `t.Run("escenario", ...)`. Cubrir como mínimo: (a) error de cada dependencia, (b) cada sentinel de dominio retornado, (c) happy path verificando output y que todas las dependencias esperadas fueron llamadas.
- **Orden de los subtests = orden del código.** Los `t.Run(...)` se escriben en el mismo orden en que las ramas aparecen al leer `Execute` de arriba hacia abajo: primero el error de la primera dependencia invocada, luego los sentinels/validaciones que surgen de su resultado, después la segunda dependencia, y así sucesivamente. El happy path va al final. Leer el test debe ser como leer el usecase.
- Regla `mustHaveStackTrace` (consistente con la sección "Manejo de errores"):
  - `true` cuando el usecase origina el error con `errorsWrapper.New` / `errorsWrapper.Wrap(sentinel, ...)`.
  - `false` cuando el usecase sólo propaga (`fmt.Errorf("...: %w", err)`).
- Cobertura: `go test ./internal/usecases/... -coverprofile=tests/coverage/usecases.out`.

**Checklist antes de commitear un usecase test:**
1. Abrí `foo.go` y `foo_test.go` en paralelo (split view).
2. Recorré `Execute` de arriba hacia abajo. Para cada línea que puede devolver error (`if err != nil`, `if x == nil`, validación de sentinel, llamada a dependencia), confirmá que existe un subtest correspondiente **en la misma posición relativa** dentro del `Test...Execute`.
3. Verificá que cada subtest de error chequea `assert.True(m.xCalled)` para las dependencias que debieron correr antes, y `assert.False(m.yCalled)` para las que no debieron alcanzarse.
4. Confirmá que el happy path es el último subtest y valida (a) el output esperado, (b) que **todas** las dependencias fueron invocadas.
5. `go test ./internal/usecases/<feature>/<action>/ -cover` verde, cobertura ≥ 90%.

### Tests de views (templ)

Toda vista `.templ` debe tener un test que la renderice. Cuando crees una vista nueva o modifiques una existente, **siempre** agrega/actualizá tests en el mismo paquete.

- Archivo: `<templ-name>_test.go` al lado de `<templ-name>.templ`, con package externo `<pkg>_test`.
- Helper de render: función local `render<Component>(t, ...) string` que crea un `bytes.Buffer`, llama a `Component(args).Render(context.Background(), &buf)` y devuelve `buf.String()`. Si el helper es trivial (un solo componente en el archivo), inline está bien.
- Aserciones con `strings.Contains` sobre el body:
  - HTML generado por templ escapa `&` como `&amp;` y muchos caracteres acentuados como entidades HTML (`&oacute;`, `&iacute;`, etc.). Cuando dudes, imprimí el render una vez y buscá la cadena exacta.
  - Evitá depender de estilos Tailwind salvo que sean el único marcador del estado — la clase es ruido y suele romper tests. Preferí textos, atributos `hx-*`, valores (`value="..."`), o URLs que la vista emite.
- Cubrir como mínimo cada rama condicional del `.templ`:
  - Estado vacío / default (nada seteado).
  - Error global (`form.Error != ""`), éxito global (`form.Success != ""`).
  - Errores por campo (`form.Field("x") != ""`) + prefill de valores (`form.Val("x")`).
  - Cada `if`/`else if` relevante.
- Helpers del package (formularios, inputs, etc.) se testean directamente: los casos `nil-safe` son obligatorios porque el template los invoca sin chequeo previo.
- Para páginas envueltas en `layout.Page` / similares: agregar un test corto que renderice la `*Page` completa. Verifica que (a) el doctype está presente, (b) el título/heading aparece, (c) los elementos propios del wrapper se emiten.
- Datos sensibles: si tu test pasa un domain object crudo con campos sensibles a una vista, estás violando la regla de viewModels — usá el View correspondiente o un DTO local del package.
- Correr: `go test ./internal/view/<pkg>/... -cover`. Apuntá a ≥ 60% por package.

**Checklist al cambiar un `.templ`:**
1. `templ generate` corrido.
2. Por cada `if` / `else if` nuevo o modificado, hay al menos un test que entra en esa rama.
3. `go test ./internal/view/<pkg>/... -cover` verde y el número no bajó.

### Integration tests (`tests/integration/`)

- **Siempre** `//go:build integration` como primera línea.
- Package: `integration_test` (externo).
- Usar el harness compartido `tests/integration/testhelper/`:
  - `testApp.CleanDatabase(t)` al inicio de cada test.
  - `testApp.SeedTenant/SeedUser/SeedSession/...` para estado (seeders propios del proyecto).
  - Helpers HTTP vienen del shared lib: `testutils.PostForm/Get/Delete/PostJSON/ReadBody` en `pkg/testutils/`, wrappeados como métodos de `*TestApp`.
  - Assertions contra Mongo vía métodos del testhelper local (`FindUserByEmail`, `FindTenant`, etc.).
- **Dos aserciones por test**: respuesta HTTP + estado persistido en DB (+ efectos en mocks cuando aplique).
- Nombres: `TestFeature_Scenario` (ej. `TestProfile_UpdateAllFields`).
- Requiere Docker corriendo (testcontainers levanta `mongo:7`).

### Tests de regresión para bugs

**Regla:** todo bug que se arregla queda cubierto por un test que (a) reproduce el bug, (b) falla **antes** del fix, y (c) pasa **después** del fix. No se considera "arreglado" un bug sin este test. Aplica al mismo cambio donde se arregla — no se posterga.

**Flujo obligatorio (red-green):**

1. **Reproducir.** Antes de tocar el código de producción, escribir un test que falle con el mismo síntoma del bug. El test debe afirmar el comportamiento correcto, no el observado.
2. **Confirmar rojo.** Correr el test para verificar que efectivamente falla con el bug presente. Si pasa, el test no está reproduciendo el bug — repensar la aserción.
3. **Arreglar.** Cambio mínimo en el código de producción.
4. **Confirmar verde.** Correr el test; debe pasar. Correr la suite completa (`go test ./...` + integration si aplica) para no romper otros casos.

**Dónde poner el test (mismo criterio que el resto de la suite):**

- Bug en una rama de `Execute` de un usecase → subtest nuevo en el `_test.go` del usecase, en la **misma posición** que la rama dentro de `Execute` (recordá: orden de subtests = orden del código).
- Bug en lo que renderiza un `.templ` (URL incorrecta, atributo faltante, escapado mal, condicional roto) → test nuevo en `view/<pkg>/<templ>_test.go` con assertions sobre el HTML emitido (`strings.Contains` sobre el body).
- Bug en el flujo HTTP (middleware, status code, header, redirect, cookie) → test nuevo en `tests/integration/<feature>_test.go`.
- Bug que requiere DB + HTTP juntos → integration test que afirme **ambos** lados (respuesta + estado en Mongo).

**Nombre del test:** describir el comportamiento correcto, no el bug. `TestPortalCommentsForm_IncludesTokenInPostURL` (lo que debe hacer) en lugar de `TestPortalCommentsForm_BugTokenMissing` (lo que rompía). Quien lea el test después no debería necesitar saber que hubo un bug.

**Verificación red→green explícita.** Cuando arreglés un bug, dejá constancia en la respuesta de que ejecutaste los pasos 2 y 4 — por ejemplo, un revert temporal del fix para confirmar que el test atrapa la regresión, y luego restaurá el fix. No es opcional: sin este paso, el test puede pasar trivialmente sin estar verificando lo que crees.

**Cuándo se permite saltearlo:** nunca. Si el bug está en un punto que físicamente no se puede testear (ej. arranque del proceso, código de boot que panic), el "test" puede ser un assert de configuración o un check estático — pero algo debe quedar registrado.

## Logging

**Regla: nunca logs sueltos con `log.Printf` / `fmt.Print*` en código que corre dentro de un request.** Todo logging está centralizado en el middleware logger (`go-kiban-fullstack/logger`): emite UN log estructurado por request (método, path, status, bodies, headers HTMX) hacia stdout/Cloud Logging. Un printf suelto muere sin correlación con la petición — no lleva request_id, ni path, ni trace — y duplica lo que el middleware ya registra.

Cómo llega un error al log centralizado:

- **La rama de error responde el request** → `htmxerror.Respond(c, err, ...)`. Setea el status real (4xx/5xx), renderiza el fragmento y deja el error en el context para que el middleware lo registre. No agregues un printf al lado.
- **Degradación parcial que igual responde 200** (fallback, campo opcional que no cargó, snapshot que no decodificó) → `htmxerror.TagError(c, err)`. El middleware registra el error a nivel ERROR aunque el status sea <400 (logger ≥ v0.4.2 — versiones anteriores descartaban los <400 en Cloud Run prod). Si en la rama no hay un `err` (condición sin error, `usecaseErrors` sueltos, panic recuperado), construí uno: `TagError(c, fmt.Errorf("contexto: %v", detalle))`.
- **Ramas que responden ≥400 sin taggear** ya se loguean solas (warn/error con request y response body), pero sin el `err` subyacente — taggealo igual para no perder el detalle.

**Los helpers no se tragan errores.** Un helper llamado desde un handler que puede fallar de forma no-fatal debe **retornar el error al caller** (que tiene el `*gin.Context` y lo taggea) o, si la ergonomía lo amerita (helpers de hidratación usados inline en view-data), **recibir `c *gin.Context`** y taggear adentro. Lo que no puede hacer es capturar el error y printearlo: ahí muere el log.

**Capas sin gin (usecases, repos, services) con operaciones best-effort.** Si una capa que solo tiene `context.Context` (p.ej. un usecase con `appContext.RequestContext`) necesita loguear un fallo no-fatal que por diseño no se propaga (un save de log best-effort), usá `logger.FromContext(ctx).Error("...", "error", err.Error())` — el middleware inyecta request_id/trace al `Request.Context()`, así que el log sale estructurado y correlacionado al request. Nunca `fmt.Printf` ni importar gin en el dominio.

**Única excepción: código donde no existe request.** Carga inicial del server (`init()`, seed de caches embebidos, wiring de boot) y goroutines de background (warm-ups, jobs) no tienen petición a la cual asociar el error — ahí `log.Printf` es aceptable (o `log.Fatal` si el error es fatal de arranque). Dejá un comentario indicando por qué se loguea suelto.

## Seguridad

- Nunca loguear passwords, tokens, API keys ni firmas de webhook.
- Nunca aceptar input del usuario sin `strings.TrimSpace` en el controller.

## Qué NO asumir

- No hay ORM: Mongo crudo con driver v2 y BSON.
- No hay framework de DI: wiring manual en `cmd/api/routes.go`.
- No hay hot-reload integrado para `.templ`: hay que regenerar.
- No hay migraciones de schema: Mongo es flexible; los campos nuevos se agregan con `omitempty`.

## Antes de terminar una tarea

1. `go build ./...` compila sin errores.
2. `go vet ./...` limpio.
3. Si tocaste `.templ`, `templ generate` corrido **y** tests de views agregados/actualizados. `go test ./internal/view/<pkg>/... -cover` verde.
4. Si tocaste algo bajo `tests/integration/`, documenta cómo correrlo en la respuesta.
5. Si tocaste un usecase, `go test ./internal/usecases/<feature>/<action>/...` verde y el checklist de unit tests aplicado.
6. **Si arreglaste un bug**: hay test de regresión nuevo o actualizado, y verificaste que falla sin el fix y pasa con el fix (ver [Tests de regresión para bugs](#tests-de-regresión-para-bugs)). Reportalo explícitamente en la respuesta.

## Minimizar código en proyectos consumidores

**Regla:** `go-kiban-fullstack` es la base de todos los proyectos Kiban. Cualquier código que **no cambie entre proyectos** vive acá. Los proyectos solo declaran lo que es genuinamente propio de su negocio: identidad visual (templ), errores específicos de su dominio, wiring, y nada más.

**Aplicar cuando:**
- Estás por escribir un wrapper de una sola línea que delega a un símbolo del shared lib. No lo hagas — exponé el símbolo del shared lib directamente o agregá un wrapper de paquete-nivel acá.
- Estás por copiar la misma función entre proyectos. Subila al shared.
- Estás por declarar el mismo error sentinel en dos proyectos. Subilo a `pkg/domain/errors/canonical.go`.

**Patrón "Default + package-level wrappers" para singletons con wiring per-project:**

Cuando un helper necesita configuración por proyecto (componentes de view, mappers específicos, etc.) pero la API que ven los handlers es siempre la misma, usar este patrón:

```go
// shared lib
package htmx

// Default holds the project-specific wiring set at boot.
var Default Config

// Top-level wrappers delegate to Default — call sites use these directly.
func RespondHTMX(c *gin.Context, err error, opts ...Option) {
    Default.RespondHTMX(c, err, opts...)
}
```

```go
// proyecto: internal/infrastructure/http/httperrors/htmx.go
package httperrors

func init() {
    htmx.Default = htmx.Config{
        Fragments:     /* templ del proyecto */,
        ProjectMapper: projectMap, // errores específicos del proyecto
        Render:        htmx.DefaultRender,
    }
}
```

```go
// cmd/api/app.go — import por side-effect para que init() corra
_ "miproyecto/internal/infrastructure/http/httperrors"
```

Los call sites usan `htmx.RespondHTMX(c, err)` directamente. **No** se crea un wrapper local de una línea en el proyecto. **No** se duplica la firma. La única superficie del proyecto en `httperrors` es: `init()` que setea `htmx.Default` + `projectMap` con sus errores propios.

**Pista de revisión:** si un archivo del proyecto es 100% wrappers de una línea sobre el shared lib, está mal — borralo y movelo al shared.

## HTMX Error Handling

Los handlers HTMX en proyectos que consumen este módulo siguen el patrón
documentado en `HTMX_ERROR_HANDLING.md`.

El módulo provee:
- `pkg/domain/errors/canonical.go` — 8 sentinels canónicos (`ErrInvalidInput`, `ErrValidation`, `ErrNotFound`, `ErrUnauthorized`, `ErrForbidden`, `ErrAlreadyExists`, `ErrConflict`, `ErrExternalServiceUnavailable`). Los proyectos importan estos directamente desde `pkg/domain/errors/` — **no se re-exportan** en el `internal/domain/errors/` del proyecto.
- `pkg/infrastructure/http/htmx/respond.go` — `Config`, `Default`, `RespondHTMX`, `TagError`, `StatusForError`, `WithFormFallback`, `DefaultRender`, `Renderable`. Es la API completa; los proyectos no envuelven.
- `pkg/infrastructure/http/htmx/htmxtest/` — helpers de test (`NewCtx`, `Render`, `Marker`) reutilizables.
- `pkg/infrastructure/http/middleware/logger.go` — captura headers HTMX y lee `ERROR_MESSAGE_CONTEXT_KEY` para registrar el error real aunque el status sea 200.

Al implementar handlers HTMX, leer `HTMX_ERROR_HANDLING.md` primero.