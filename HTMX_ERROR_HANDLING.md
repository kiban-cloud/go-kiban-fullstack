# HTMX Error Handling Pattern

> **Audiencia:** Claude Code working on Kiban projects that consume `go-kiban-fullstack`.
> **Objetivo:** Aplicar manejo consistente de errores en handlers HTMX para que el usuario reciba HTML útil **y** el monitoreo de Cloud Logging/Monitoring detecte los problemas reales.

---

## TL;DR para Claude Code

1. Los handlers HTMX **no deben devolver siempre 200**. Devuelven el status code que refleja qué pasó en el servidor (4xx para errores del cliente, 5xx para errores del sistema) + un fragment HTML apropiado.
2. El cliente HTMX se configura con la extensión `response-targets` para que swappee fragments aunque la respuesta sea 4xx/5xx.
3. Existe un helper `httperrors.RespondHTMX(c, err)` en `go-kiban-fullstack` que mapea errores de dominio a status + fragment automáticamente (si no existe en tu proyecto, este documento incluye la implementación).
4. El logger/middleware en `go-kiban-fullstack` ya capturan headers HTMX (`HX-Request`, `HX-Target`, etc.) automáticamente para debugging.

---

## Contexto: por qué este patrón existe

### El anti-pattern común

Por default HTMX descarta el response y no swappea nada si el status es no-2xx. Esto lleva a desarrolladores a devolver `200 OK` con un fragment de error para que el usuario vea algo:

```go
// ❌ ANTI-PATTERN
func ValidateCURP(c *gin.Context) {
    err := h.usecase.Validate(...)
    if err != nil {
        // Status 200 aunque RENAPO esté caído
        c.HTML(200, components.ErrorFragment("Servicio no disponible"))
        return
    }
    // ...
}
```

**Consecuencias:**
- Las log-based metrics y alertas de Cloud Monitoring **no detectan nada** porque todo se loguea como 200
- El monitoring de Kiban está configurado para alertar sobre `status=~"5..|429|408"`, no se dispara nunca
- Imposible distinguir "200 con éxito" vs "200 con error de RENAPO"

### La solución

Devolver el status correcto **y** el fragment correcto. El usuario ve el fragment, Cloud Monitoring ve el status.

---

## Principio fundamental

> **El status code refleja qué le pasó al servidor procesando el request, NO qué quieres mostrar al usuario.**
>
> El fragment HTML cubre la UX. El status code cubre el monitoreo.

---

## Tabla de mapping: error de dominio → status → fragment

| Situación | Status HTTP | Tipo de fragment | ¿Dispara alerta? |
|---|---|---|---|
| Operación exitosa | `200 OK` | Result fragment | No |
| Recurso creado | `201 Created` | Result fragment | No |
| Validación de input (CURP malformado, email inválido) | `422 Unprocessable Entity` | `ValidationError` | No |
| Recurso no existe (CURP no encontrado, user inexistente) | `404 Not Found` | `NotFoundError` | No |
| No autenticado / sesión expirada | `401 Unauthorized` | `UnauthorizedError` | No |
| Sin permisos | `403 Forbidden` | `ForbiddenError` | No |
| Conflicto (email ya registrado, duplicado) | `409 Conflict` | `ValidationError` | No |
| Demasiados requests | `429 Too Many Requests` | `ErrorBanner` | **Sí** |
| Dependencia externa caída (RENAPO, Banco Dondé, Stripe) | `502 Bad Gateway` o `503 Service Unavailable` | `ErrorBanner` | **Sí** |
| Bug en código, DB caída, panic | `500 Internal Server Error` | `ErrorBanner` | **Sí** |
| Timeout | `408 Request Timeout` o `504 Gateway Timeout` | `ErrorBanner` | **Sí** |

**Regla mental:**
- `4xx` (excepto 429) = el cliente o usuario hizo algo mal → UX normal, sin alerta
- `5xx` o `429` = el servidor tiene problemas → alerta + atención del equipo

---

## Setup del cliente

### Incluir la extensión response-targets

En el layout base del proyecto (típicamente `layouts/base.templ` o equivalente):

```html
<script src="https://unpkg.com/htmx.org@2.0.0"></script>
<script src="https://unpkg.com/htmx-ext-response-targets@2.0.0"></script>
```

Y en el `<body>`:

```html
<body hx-ext="response-targets">
```

Esto habilita los atributos `hx-target-4xx` y `hx-target-5xx` para todo el documento.

### Atributos en los formularios

Cada formulario o elemento que dispare requests HTMX debe especificar qué hacer con respuestas no-2xx:

```html
<form
    hx-post="/curp/validate"
    hx-target="#curp-result"
    hx-target-4xx="#curp-result"
    hx-target-5xx="#global-error-banner"
    hx-swap="innerHTML">
    <!-- ... -->
</form>
```

**Estrategia recomendada:**
- `hx-target` → donde va el resultado exitoso (e.g. resultado de la validación)
- `hx-target-4xx` → mismo lugar que el target (errores de validación se muestran junto al form)
- `hx-target-5xx` → banner global de error (errores del sistema son cross-cutting)

### Layout con banner global

```html
<body hx-ext="response-targets">
    <div id="global-error-banner" class="hidden"></div>
    <main>
        @children
    </main>
</body>
```

El banner está oculto por default y HTMX lo llena cuando hay un 5xx.

### Limpiar feedback stale antes de cada request (regla obligatoria)

> **Regla:** Cuando se dispara un HTMX request, **todo feedback inline previo de ese contexto debe desaparecer antes de que llegue la respuesta nueva.**

**¿Por qué existe esta regla?**

Por default HTMX no limpia nada. Esto crea un bug sutil:

1. El usuario submitea el form → 409 → `hx-target-4xx="this"` swappea el form → aparece "Este correo ya está registrado." inline.
2. El usuario corrige y submitea de nuevo → 500 → `hx-target-5xx="#global-error-banner"` mete `ErrorBanner` en el banner global, **pero el form no se swappea** porque su `hx-target-4xx` no aplica al 5xx.
3. Resultado: el banner muestra "Ocurrió un error inesperado" mientras el form sigue mostrando "Este correo ya está registrado." Los dos errores son contradictorios.

Mismo problema con success messages stale ("Código reenviado" persistiendo después de un nuevo intento fallido).

**Implementación**

Agregar un listener global de `htmx:beforeRequest` en `head.templ` (o equivalente del layout base):

```html
<script>
    document.addEventListener('htmx:beforeRequest', function(evt) {
        // 1. Limpiar el banner global (donde caen los 5xx).
        var banner = document.getElementById('global-error-banner');
        if (banner) banner.innerHTML = '';

        // 2. Limpiar feedback inline dentro del form que dispara la request
        //    y dentro del swap target. Cubre ambos casos:
        //      - feedback dentro del form (login/register/profile)
        //      - feedback sibling del form, dentro de un wrapper (verify,
        //        donde el error vive dentro de #verifySection junto al form)
        var detail = evt.detail || {};
        [detail.elt, detail.target].forEach(function(el) {
            if (!el) return;
            var scope = el.tagName === 'FORM' ? el : (el.closest('form') || el);
            scope.querySelectorAll('[data-error-type],[data-success-type]').forEach(function(node) {
                node.remove();
            });
        });
    });
</script>
```

**Marcar los elementos clearables en los templates**

Cada div inline de error o success debe llevar el atributo de marcado correspondiente:

```templ
if form.Error != "" {
    <div role="alert" data-error-type="form-error" class="...">{ form.Error }</div>
}
if form.Success != "" {
    <div role="status" data-success-type="form-success" class="...">{ form.Success }</div>
}
```

Los componentes canónicos de `httperrors.RespondHTMX` (`ErrorBanner`, `ValidationError`, etc.) ya traen `data-error-type` por construcción. Solo necesitás agregar el marker a los divs hand-rolled que viven dentro de forms o secciones.

**¿Por qué dos atributos en vez de uno?**

`data-error-type` y `data-success-type` separan semánticamente errores de confirmaciones. Permite, por ejemplo, mantener un mensaje de éxito visible más tiempo (animación, toast) si querés cambiar la política — solo ajustás el selector del JS.

**Limitaciones / cuándo NO clearar**

El JS solo limpia dentro del form o swap target. Avisos de **estado persistente** (cuenta no verificada, pago vencido, enlace de reset expirado) que vivan en otros lugares de la página y no son consecuencia de un submit NO llevan el marker — siguen visibles hasta que el server los retire en el próximo render completo.

---

## Setup del servidor

### Helper httperrors.RespondHTMX

Este helper centraliza el mapping de errores de dominio a status + fragment. Si no existe aún en `go-kiban-fullstack`, créalo en `pkg/infrastructure/http/errors/htmx.go`:

```go
package errors

import (
    "errors"
    "net/http"

    domain_errors "github.com/kiban-cloud/go-kiban-fullstack/pkg/domain/errors"
    "github.com/kiban-cloud/go-kiban-fullstack/pkg/infrastructure/logger"
    "github.com/kiban-cloud/go-kiban-fullstack/pkg/ui/components"

    "github.com/a-h/templ"
    "github.com/gin-gonic/gin"
)

// RespondHTMX maps a domain error to the appropriate HTTP status + HTML fragment.
// The caller is responsible for passing the original error from the use case.
//
// Usage in handlers:
//
//   result, err := h.usecase.Validate(ctx, input)
//   if err != nil {
//       httperrors.RespondHTMX(c, err)
//       return
//   }
//   c.HTML(http.StatusOK, components.CurpResult(result))
func RespondHTMX(c *gin.Context, err error) {
    status, component := mapErrorToResponse(err)

    // Stash the error in context so the logger middleware picks it up
    c.Set(ERROR_MESSAGE_CONTEXT_KEY, err)

    c.Status(status)
    c.Header("Content-Type", "text/html; charset=utf-8")

    if renderErr := component.Render(c.Request.Context(), c.Writer); renderErr != nil {
        logger.FromContext(c.Request.Context()).Error(
            "failed to render error fragment",
            "error", renderErr,
            "original_error", err,
        )
    }
}

func mapErrorToResponse(err error) (int, templ.Component) {
    switch {
    // Validation errors → 422
    case errors.Is(err, domain_errors.ErrInvalidInput),
        errors.Is(err, domain_errors.ErrValidation):
        return http.StatusUnprocessableEntity,
            components.ValidationError(err.Error())

    // Not found → 404
    case errors.Is(err, domain_errors.ErrNotFound):
        return http.StatusNotFound,
            components.NotFoundError(err.Error())

    // Auth errors
    case errors.Is(err, domain_errors.ErrUnauthorized):
        return http.StatusUnauthorized,
            components.UnauthorizedError()
    case errors.Is(err, domain_errors.ErrForbidden):
        return http.StatusForbidden,
            components.ForbiddenError()

    // Conflict (already exists, duplicate)
    case errors.Is(err, domain_errors.ErrAlreadyExists),
        errors.Is(err, domain_errors.ErrConflict):
        return http.StatusConflict,
            components.ValidationError(err.Error())

    // External dependency down → 502
    case errors.Is(err, domain_errors.ErrExternalServiceUnavailable),
        errors.Is(err, domain_errors.ErrRenapoUnavailable),
        errors.Is(err, domain_errors.ErrBancoUnavailable):
        return http.StatusBadGateway,
            components.ErrorBanner("Servicio externo no disponible. Intenta de nuevo en unos momentos.")

    // Rate limit
    case errors.Is(err, domain_errors.ErrRateLimited):
        return http.StatusTooManyRequests,
            components.ErrorBanner("Demasiados intentos. Espera unos segundos.")

    // Default: unknown error → 500
    default:
        return http.StatusInternalServerError,
            components.ErrorBanner("Ocurrió un error inesperado. El equipo ya fue notificado.")
    }
}
```

> **Nota:** Los errores de dominio referenciados (`ErrInvalidInput`, `ErrRenapoUnavailable`, etc.) deben existir en `pkg/domain/errors`. Si no existen, créalos siguiendo el patrón clean architecture del proyecto. Cada proyecto puede tener errores adicionales específicos de su dominio.

### Verificar que el logger middleware ya está aplicado

El middleware en `go-kiban-fullstack` (`pkg/infrastructure/http/middleware/logger.go`) ya captura:
- Headers HTMX (`HX-Request`, `HX-Target`, etc.) como grupo estructurado
- Skip de paths estáticos y health checks
- Truncado de response bodies cuando son HTML
- Redacción de campos sensibles (CURP, RFC, password, etc.)
- Respuesta dual JSON/HTML en panic recovery

**No requiere cambios.** Solo verifica que esté en la cadena de middleware del proyecto.

---

## Plantillas de fragments en Templ

Ubicación sugerida: `pkg/ui/components/errors.templ` en `go-kiban-fullstack` (para que ambos proyectos los compartan).

### ErrorBanner (errores 5xx / 429)

```go
package components

// ErrorBanner es el fragment usado para errores del sistema (5xx).
// Diseñado para reemplazar el contenido de #global-error-banner.
templ ErrorBanner(message string) {
    <div class="alert alert-error" role="alert" data-error-type="system">
        <svg xmlns="http://www.w3.org/2000/svg" class="stroke-current shrink-0 h-6 w-6" fill="none" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2m7-2a9 9 0 11-18 0 9 9 0 0118 0z"/>
        </svg>
        <span>{ message }</span>
        <button class="btn btn-sm btn-ghost"
                onclick="document.getElementById('global-error-banner').innerHTML = ''">
            Cerrar
        </button>
    </div>
}
```

### ValidationError (422 / 409)

```go
// ValidationError se usa para errores de validación del input del usuario.
// Diseñado para mostrarse junto al formulario que falló.
templ ValidationError(message string) {
    <div class="alert alert-warning" role="alert" data-error-type="validation">
        <svg xmlns="http://www.w3.org/2000/svg" class="stroke-current shrink-0 h-6 w-6" fill="none" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"/>
        </svg>
        <span>{ message }</span>
    </div>
}
```

### NotFoundError (404)

```go
templ NotFoundError(message string) {
    <div class="alert alert-info" role="alert" data-error-type="not-found">
        <span>{ message }</span>
    </div>
}
```

### UnauthorizedError (401) — con redirect

```go
// UnauthorizedError dispara una redirección al login vía HX-Redirect header.
// El fragment se muestra brevemente antes del redirect.
templ UnauthorizedError() {
    <div class="alert alert-warning" role="alert" data-error-type="unauthorized"
         hx-on::load="setTimeout(() => window.location.href = '/login', 1500)">
        <span>Tu sesión expiró. Redirigiendo al login...</span>
    </div>
}
```

> **Alternativa:** En lugar del JS, devolver el header `HX-Redirect: /login` desde el servidor. HTMX hace el redirect automáticamente sin mostrar fragment. Es más limpio pero pierde el feedback visual.

### ForbiddenError (403)

```go
templ ForbiddenError() {
    <div class="alert alert-error" role="alert" data-error-type="forbidden">
        <span>No tienes permisos para realizar esta acción.</span>
    </div>
}
```

---

## Ejemplos de handlers

### Ejemplo 1: validación de CURP

```go
func (h *CURPHandler) Validate(c *gin.Context) {
    var input struct {
        CURP string `form:"curp" binding:"required"`
    }

    // Bind error → 422 (no llega al usecase)
    if err := c.ShouldBind(&input); err != nil {
        httperrors.RespondHTMX(c, fmt.Errorf("%w: %v", domain_errors.ErrInvalidInput, err))
        return
    }

    result, err := h.usecase.Validate(c.Request.Context(), input.CURP)
    if err != nil {
        // El usecase devuelve errores tipados:
        // - ErrInvalidInput → 422
        // - ErrNotFound → 404
        // - ErrRenapoUnavailable → 502
        // - cualquier otro → 500
        httperrors.RespondHTMX(c, err)
        return
    }

    c.HTML(http.StatusOK, "", components.CurpResult(result))
}
```

### Ejemplo 2: subir un archivo de domiciliación

```go
func (h *DomiciliacionHandler) Upload(c *gin.Context) {
    file, header, err := c.Request.FormFile("file")
    if err != nil {
        httperrors.RespondHTMX(c, fmt.Errorf("%w: archivo requerido", domain_errors.ErrInvalidInput))
        return
    }
    defer file.Close()

    result, err := h.usecase.ProcessFile(c.Request.Context(), header.Filename, file)
    if err != nil {
        httperrors.RespondHTMX(c, err)
        return
    }

    c.HTML(http.StatusOK, "", components.UploadSuccess(result))
}
```

### Ejemplo 3: endpoint que también puede ser llamado como JSON API

Si el endpoint sirve tanto a HTMX como a clientes API, detectar el tipo de cliente:

```go
func (h *AuthHandler) Login(c *gin.Context) {
    var input LoginInput
    if err := c.ShouldBind(&input); err != nil {
        respondError(c, fmt.Errorf("%w: %v", domain_errors.ErrInvalidInput, err))
        return
    }

    session, err := h.usecase.Login(c.Request.Context(), input)
    if err != nil {
        respondError(c, err)
        return
    }

    if isHTMXRequest(c) {
        c.Header("HX-Redirect", "/dashboard")
        c.Status(http.StatusOK)
        return
    }
    c.JSON(http.StatusOK, session)
}

func respondError(c *gin.Context, err error) {
    if isHTMXRequest(c) {
        httperrors.RespondHTMX(c, err)
        return
    }
    httperrors.RespondJSON(c, err)
}

func isHTMXRequest(c *gin.Context) bool {
    return c.GetHeader("HX-Request") == "true"
}
```

---

## Cómo verificar que funciona

### 1. Provocar errores y revisar el Network tab

Con el navegador abierto en DevTools → Network:

- Submitea un form con input inválido → debe ver `422` en el status, fragment de validación en el response
- Submitea con CURP que no exista → `404`
- Detén el servicio externo (mock RENAPO) → `502`
- Provoca un panic intencional → `500` + fragment genérico

En cada caso, el usuario ve el fragment correcto en la UI gracias a `hx-target-4xx`/`hx-target-5xx`.

### 2. Revisar Cloud Logging

Filtros útiles en Logs Explorer:

```
# Todos los errores 4xx/5xx del servicio
resource.labels.service_name="<nombre-servicio>"
httpRequest.status>=400

# Solo errores HTMX
resource.labels.service_name="<nombre-servicio>"
jsonPayload.htmx.is_htmx=true
httpRequest.status>=400

# Errores en un endpoint específico
resource.labels.service_name="<nombre-servicio>"
httpRequest.requestUrl=~"/curp/validate"
httpRequest.status>=400
```

### 3. Verificar Cloud Monitoring

Las policies existentes deben empezar a detectar 5xx una vez deployado:
- `Cloud Run - High error rate per endpoint (>5%)` — alerta sobre tasa de error
- `Cloud Run - 5xx errors spike per service` — alerta sobre volumen absoluto de 5xx

---

## Checklist de migración por handler

Cuando aplicas este patrón a un handler existente:

- [ ] El usecase devuelve errores tipados de dominio (no `fmt.Errorf("...")` genéricos)
- [ ] El handler no tiene `c.HTML(200, errorFragment)` — todos los caminos de error pasan por `httperrors.RespondHTMX`
- [ ] El template del form en el cliente tiene `hx-target-4xx` y `hx-target-5xx`
- [ ] El layout base incluye `hx-ext="response-targets"` y el `<div id="global-error-banner">`
- [ ] El layout base monta el listener de `htmx:beforeRequest` que limpia banner + `[data-error-type]`/`[data-success-type]`
- [ ] Los divs inline de error y success del template llevan `data-error-type="form-error"` / `data-success-type="form-success"` (sin esto, un 5xx que sigue a un 4xx deja el error inline previo stale)
- [ ] Los errores específicos del proyecto están mapeados en `mapErrorToResponse`
- [ ] Probaste manualmente al menos un camino de error y verificaste que:
  - El usuario ve el fragment correcto
  - El status code en Network tab es el esperado
  - Cloud Logging registra el error con `jsonPayload.htmx.is_htmx=true`

---

## Anti-patterns a evitar

❌ **Devolver 200 con fragment de error** — rompe el monitoreo

❌ **Devolver JSON desde un handler HTMX** — HTMX no sabe qué hacer con JSON

❌ **No tipar los errores del usecase** — el handler no puede mapear correctamente

❌ **Usar `c.AbortWithStatus(500)` sin fragment** — el usuario ve una página rota

❌ **Loguear el error en el handler antes de devolver** — el middleware ya lo hace, evitar duplicación

❌ **Olvidar el `hx-target-5xx`** — el usuario ve "nada" cuando el sistema falla

❌ **No limpiar feedback stale entre submits** — un 4xx deja un error inline en el form; el siguiente 5xx mete un banner en `#global-error-banner` pero NO retoca el form, así que el usuario ve ambos errores (contradictorios) al mismo tiempo. El listener de `htmx:beforeRequest` en el layout es obligatorio y los divs inline necesitan `data-error-type` / `data-success-type` para que el listener los encuentre.

---

## Recursos

- Documentación HTMX response-targets: https://htmx.org/extensions/response-targets/
- Status codes HTTP: https://developer.mozilla.org/en-US/docs/Web/HTTP/Status
- Cloud Monitoring policies de Kiban: ver dashboard `Kiban - Cloud Run Requests` en GCP Console

---

## Origen de este documento

Este patrón se diseñó en una sesión de Claude.ai en mayo 2026 como parte de la migración del frontend de Kiban a Go + HTMX + Templ. La motivación fue alinear el manejo de errores con el sistema de monitoreo basado en log-based metrics + PromQL alerts ya configurado en Cloud Monitoring.