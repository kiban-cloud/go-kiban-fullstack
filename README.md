# go-kiban-fullstack

Librería compartida por los backends Kiban (kiban-cloud, klin, crm, rekon, link, workfloo, …): config, logger, middlewares HTTP/HTMX, errores canónicos y helpers de test.

- Convenciones de código (logging, manejo de errores, tests, credenciales): [CLAUDE.md](CLAUDE.md)
- Patrón de errores HTMX: [HTMX_ERROR_HANDLING.md](HTMX_ERROR_HANDLING.md)

## Credenciales para desarrollo local

**No hay llaves de service account.** Todo es keyless: la ADC (*Application Default Credentials*) resuelve al **runtime SA por metadata** en Cloud Run, y en local a lo que configures con `gcloud`.

> ⚠️ **La ADC es global**: la comparten terraform, las librerías de Google y tu app. Según lo que vayas a hacer necesitás una u otra — no podés tener las dos a la vez.

### Para probar proyectos que interactúan con el bucket o URLs firmadas

Corré la app con la **misma identidad que Cloud Run** (el runtime SA), sin bajar ninguna llave:

```bash
gcloud auth application-default login \
  --impersonate-service-account=kiban-cloud-infra-test@learned-shape-443815-u9.iam.gserviceaccount.com
```

Aplica a **todo lo que use ADC**: subir/bajar de GCS, firmar signed URLs, Secret Manager, BigQuery, Pub/Sub, Logging.

**Requisito**: tu usuario necesita `roles/iam.serviceAccountTokenCreator` sobre ese SA. Se otorga al grupo `devs@kiban.com` desde el terraform de kiban-cloud (`terraform/envs/shared-test/main.tf`, recurso `devs_impersonate_runtime`). Si falta:

```
Permission 'iam.serviceAccounts.getAccessToken' denied
```

> Ese permiso **no** viene en los roles básicos (ni Owner ni Editor lo incluyen): Google exige otorgar la impersonation explícitamente. Si el token sale, es porque el grant existe — y ese token **es del SA**.

**Verificá que quedó bien** (debe decir el SA, **no** tu usuario):

```bash
jq .type ~/.config/gcloud/application_default_credentials.json   # "impersonated_service_account"

TOKEN=$(gcloud auth application-default print-access-token)
curl -s "https://oauth2.googleapis.com/tokeninfo?access_token=$TOKEN" | jq .email
```

Sin ese chequeo la prueba no vale: si tu usuario tiene permisos altos, todo te va a funcionar aunque la impersonation no esté activa — y después revienta en el deploy.

### Si vas a aplicar un terraform

Terraform **también usa ADC**. Si dejás la impersonation puesta, terraform corre como el *runtime SA* — que tiene permisos de app (storage, bigquery, run…) pero **ninguno de IAM admin**, y el plan se cae leyendo políticas existentes:

```
Permission 'iam.serviceAccounts.getIamPolicy' denied
The caller does not have permission (retrieving IAM policy for project ...)
```

Volvé a tu usuario **antes** de correr terraform:

```bash
gcloud auth application-default login   # sin --impersonate-service-account
terraform plan
```

> El `apply` siempre lo corre una persona, nunca Claude (ver el CLAUDE.md de kiban-cloud).

### Si alternás seguido entre ambos

Mantené los dos contextos en paralelo con configs separadas de gcloud:

```bash
# contexto normal (terraform, gcloud) → tu usuario
gcloud auth application-default login

# contexto app → ADC aparte con impersonation
CLOUDSDK_CONFIG=~/.gcloud-impersonated gcloud auth application-default login \
  --impersonate-service-account=kiban-cloud-infra-test@learned-shape-443815-u9.iam.gserviceaccount.com

# al correr la app
GOOGLE_APPLICATION_CREDENTIALS=~/.gcloud-impersonated/application_default_credentials.json go run ./cmd/api
```

Ese `GOOGLE_APPLICATION_CREDENTIALS` apunta a una **config de impersonation** (no contiene ninguna llave privada, solo referencia tus credenciales) — no es lo mismo que apuntarlo a una llave de service account, que está prohibido.

### En VS Code: una config de launch aparte

Para no tener que exportar la variable a mano en cada corrida, dejá el contexto impersonado en una configuración propia del `.vscode/launch.json`, **además** de la normal. Así elegís identidad desde el dropdown: la default corre como tu usuario, la otra como el runtime SA. Referencia viva: [`klin-backend/.vscode/launch.json`](https://bitbucket.org/alexandregrin/klin-backend/src/develop/.vscode/launch.json).

```jsonc
{
  // Corre el proyecto con la MISMA identidad que Cloud Run (el runtime SA),
  // en vez de con tu usuario. Útil para probar todo lo que use ADC:
  // firmar signed URLs, subir/bajar de GCS, Secret Manager, BigQuery.
  //
  // Requiere crear el contexto UNA vez (deja tu ADC normal intacta, para que
  // terraform y gcloud sigan corriendo como vos):
  //
  //   CLOUDSDK_CONFIG=~/.gcloud-impersonated gcloud auth application-default login \
  //     --impersonate-service-account=kiban-cloud-infra-test@learned-shape-443815-u9.iam.gserviceaccount.com
  //
  // Si el archivo no existe, la ADC falla duro: usá la config normal hasta crearlo.
  "name": "<proyecto>: Launch Debug (SA impersonado)",
  "type": "go",
  "request": "launch",
  "mode": "debug",
  "program": "${workspaceFolder}/cmd/api",
  "cwd": "${workspaceFolder}",
  "env": {
    "GOOGLE_APPLICATION_CREDENTIALS": "${userHome}/.gcloud-impersonated/application_default_credentials.json"
  }
}
```

`${userHome}` lo resuelve VS Code, así que la config es portable entre máquinas.

**Lo que tiene que hacer cada dev** (una sola vez, y solo si va a tocar GCS / signed URLs):

1. Estar en el grupo `devs@kiban.com` — el grant de `serviceAccountTokenCreator` es al grupo.
2. Correr el comando `CLOUDSDK_CONFIG=…` del comentario de arriba.
3. Elegir **"Launch Debug (SA impersonado)"** en el dropdown de VS Code. Con la config default seguís corriendo como tu usuario — que suele tener permisos más altos que el SA y te esconde los errores que después aparecen en el deploy.

### Notas

- El runtime SA de prod es `kiban-cloud-infra-prod@kiban-cloud`. Los devs **no** lo impersonan.
- `gcloud auth application-default login` afecta la **ADC** (apps + terraform). Los comandos `gcloud ...` siguen corriendo con tu usuario, salvo que uses `gcloud config set auth/impersonate_service_account SA_EMAIL`.
- **CORS es otra cosa**: es config del bucket vs el `Origin` del navegador; ninguna credencial lo afecta.
- **Login de staff con Google en local** (OAuth, otra familia que la ADC): el origen `http://localhost:<puerto>` debe estar registrado en *Orígenes autorizados de JavaScript* del OAuth Client ID (Cloud Console → APIs y servicios → Credenciales). Si no: `Error 400: origin_mismatch`. La config de ese cliente **no está en terraform**, es manual en la Console.
