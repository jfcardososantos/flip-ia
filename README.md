# flip-ai

`flip-ai` expõe os modelos da Xiaomi Mimo e provedores gratuitos/oficiais por uma API simples e compatível com OpenAI e Ollama.

O fluxo principal para a Xiaomi Mimo é:

1. fazer login no Xiaomi AI Studio;
2. importar a sessão com a extensão do Chrome/Edge;
3. chamar a API enviando apenas `model` e payload.

Os endpoints públicos não exigem token do cliente. A autenticação com a Xiaomi fica encapsulada no backend por meio do arquivo `data/auth.json`.

## O que o projeto entrega

- Compatibilidade com OpenAI em `POST /v1/chat/completions`, `POST /v1/completions` e `GET /v1/models`
- Providers oficiais por prefixo de modelo: Gemini, Groq, OpenRouter e Cloudflare Workers AI
- Compatibilidade com Ollama em `POST /api/chat`, `POST /api/generate`, `GET /api/tags` e `GET /api/version`
- Streaming
- Tool calling
- Persistência de histórico em SQLite
- Dashboard local para operação e teste
- Extensão para importar sessões autenticadas suportadas
- Qwen Web com conversa real persistida no `chat.qwen.ai`, continuidade por `chat_id` e rollover automático
- Catálogo de modelos atualizado automaticamente no boot e em intervalos configuráveis

## Como funciona a autenticação

`flip-ai` não depende mais de `SERVICE_TOKEN`, `USER_ID`, `XIAOMI_CHATBOT_PH` ou `XIAOMI_COOKIE` no ambiente para operar.

A sessão ativa é lida de:

- `data/auth.json`

Esse arquivo é preenchido pela extensão ou, se você quiser automatizar isso sem navegador, pelo endpoint administrativo `POST /auth/import`.

Sessões web por provedor também podem ser persistidas nesse mesmo arquivo via `POST /auth/web/import`.

Se a Xiaomi invalidar a sessão:

1. `GET /auth/status` passa a indicar problema;
2. chamadas para `/v1/*` ou `/api/*` falham;
3. você reimporta a sessão pela extensão.

## Requisitos

- Go 1.24+ para execução local
- Node.js para o solver de Proof-of-Work do DeepSeek Web em execução local
- Docker e Docker Compose para execução em container
- Chrome ou Edge para usar a extensão

## Configuração

As variáveis de ambiente úteis agora são:

```env
PORT=3000
CORS_ORIGIN=*
API_KEY=
SETTINGS_PASSWORD=
DEFAULT_MODEL=mimo-v2.5-pro
REQUEST_API_KEY=
AUTH_STORE_PATH=
GEMINI_API_KEY=
GROQ_API_KEY=
OPENROUTER_API_KEY=
CLOUDFLARE_API_KEY=
CLOUDFLARE_ACCOUNT_ID=
MODEL_CATALOG_REFRESH_ON_STARTUP=true
MODEL_CATALOG_REFRESH_INTERVAL=6h
MODEL_CATALOG_TIMEOUT_SECONDS=15
```

Notas:

- `SETTINGS_PASSWORD` protege `/settings`, a tela de configuração de sessões e chaves.
- `API_KEY` continua opcional para ambientes que já usam esse segredo.
- Se `API_KEY`, `SETTINGS_PASSWORD` ou `CONFIG_PASSWORD` estiverem definidos, rotas administrativas de sessão/chaves como `POST /auth/import`, `POST /auth/extension/import`, `POST /auth/provider/import`, `POST /auth/clear` e `GET /auth/debug` exigem `Authorization: Bearer <segredo>` ou `api_key`.
- `DEFAULT_MODEL` define o modelo usado quando a request enviar `"model": "default"`. Se não for definido no env, pode ser salvo em `/settings`.
- `REQUEST_API_KEY` protege as rotas de inferência `/v1/*`, `/api/*` e `/open-apis/bot/chat`. Também pode ser salvo em `/settings`. A request deve enviar `Authorization: Bearer <REQUEST_API_KEY>` ou `X-API-Key`.
- `AUTH_STORE_PATH` é opcional. Se não for definido, a sessão fica em `data/auth.json`.
- As chaves Gemini/Groq/OpenRouter/Cloudflare são opcionais. Se não forem definidas, o Mimo continua funcionando normalmente e os modelos daquele provider retornam erro de configuração.
- Essas chaves podem vir do ambiente ou ser salvas em `data/auth.json` pela extensão em `POST /auth/provider/import`.
- `MODEL_CATALOG_REFRESH_INTERVAL=0` desativa apenas a atualização periódica. O refresh no boot continua controlado separadamente por `MODEL_CATALOG_REFRESH_ON_STARTUP`.

## Catálogo automático de modelos

Ao iniciar, o proxy carrega o último catálogo salvo e consulta em paralelo as listas atuais da DeepSeek, Qwen, Xiaomi, Gemini, Groq, OpenRouter e Cloudflare. A DeepSeek é descoberta pela tabela oficial `Models & Pricing`, sem chave de API. Providers que precisam de credenciais só são consultados quando estão configurados. O OpenRouter é público e, por padrão, o catálogo mantém somente os modelos gratuitos.

O resultado é salvo em `data/model_catalog.json` (ou em `MODEL_CATALOG_PATH`), reaproveitado quando um provider estiver indisponível e atualizado novamente a cada seis horas. Quando uma chave ou sessão é importada pelo dashboard/extensão, um novo refresh é disparado em segundo plano.

Os IDs recebem os prefixos esperados pelo roteador (`groq/`, `openrouter/`, `cf/` e `qwen-web/`). O alias `qwen-web` acompanha automaticamente o modelo principal ativo informado pela Qwen, salvo se `QWEN_WEB_DEFAULT_MODEL` estiver definido. Os IDs oficiais `deepseek-*` são atualizados pela documentação oficial; famílias `*-pro` usam o Expert Mode do adapter web e as demais acompanham o modo web padrão.

Variáveis opcionais:

```env
MODEL_CATALOG_REFRESH_ON_STARTUP=true
MODEL_CATALOG_REFRESH_INTERVAL=6h
MODEL_CATALOG_TIMEOUT_SECONDS=15
MODEL_CATALOG_PATH=
OPENROUTER_FREE_MODELS_ONLY=true
# DEEPSEEK_MODELS_URL=https://api-docs.deepseek.com/quick_start/pricing/
```

`deepseek-search` e os aliases Kimi continuam controlados pelo proxy porque representam capacidades/modos dos adapters web, não nomes oficiais de modelos.

## Providers oficiais

Além dos modelos `mimo-*`, o endpoint `POST /v1/chat/completions` roteia automaticamente por prefixo:

| Modelo no proxy | Provider | Variáveis necessárias |
|-----------------|----------|-----------------------|
| `gemini-3.5-flash` | Google Gemini API | `GEMINI_API_KEY` |
| `groq/llama-3.1-8b-instant` | Groq | `GROQ_API_KEY` |
| `kimi-k3` | Kimi Web (sessão do navegador) | importe a sessão Kimi pela extensão |
| `kimi-k2.6` | Kimi Web (sessão do navegador) | importe a sessão Kimi pela extensão |
| `qwen-web` | Alias do modelo Qwen Web principal atual | importe a sessão Qwen pela extensão |
| `qwen-web/qwen3.8-max` | Qwen Web, contexto anunciado de 1M | importe a sessão Qwen pela extensão |
| `qwen-web/qwen3.7-plus` | Qwen Web, contexto anunciado de 1M | importe a sessão Qwen pela extensão |
| `qwen-web/qwen3.7-max` | Qwen Web, contexto anunciado de 1M | importe a sessão Qwen pela extensão |
| `openrouter/meta-llama/llama-3.1-8b-instruct:free` | OpenRouter | `OPENROUTER_API_KEY` |
| `cf/@cf/meta/llama-3.1-8b-instruct` | Cloudflare Workers AI | `CLOUDFLARE_API_KEY`, `CLOUDFLARE_ACCOUNT_ID` |

Exemplo:

```bash
curl http://localhost:3000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $REQUEST_API_KEY" \
  -d '{
    "model": "default",
    "messages": [
      {"role": "user", "content": "Responda em uma frase."}
    ]
  }'
```

## Executando

### Docker

```bash
docker-compose up -d
```

### Go

```bash
go mod tidy
go run main.go
```

Depois abra:

- `http://localhost:3000/`

## Setup recomendado com a extensão

### 1. Login na Xiaomi

Entre em:

- `https://aistudio.xiaomimimo.com/`

### 2. Baixe a extensão

Pela dashboard, baixe:

- `/downloads/flip-ai-session-extension.zip`

Ou use a pasta local `extension/`.

### 3. Instale no navegador

1. abra `chrome://extensions` ou `edge://extensions`
2. ative `Developer mode`
3. clique em `Load unpacked`
4. selecione a pasta `extension`

### 4. Importe a sessão

No popup da extensão:

1. informe a URL do proxy
2. informe a `API_KEY`/`SETTINGS_PASSWORD` apenas se você protegeu a rota administrativa
3. clique em `Import Xiaomi Session`

Se der certo, a sessão será salva em `data/auth.json`.

### Providers com API key

No popup da extensão também há botões para Gemini, Groq, OpenRouter e Cloudflare:

1. clique em `Open <Provider>` para abrir a tela de criação de chave
2. cole a API key no campo correspondente
3. clique em `Save <Provider>`

Para Cloudflare, informe também o `CLOUDFLARE_ACCOUNT_ID`. Para OpenRouter, `HTTP Referer` e `App title` são opcionais.

## Dashboard

A home em `/` e `/dashboard` foi desenhada para operação rápida e não exibe formulários de credenciais:

- quantidade de requests da API
- status da API
- modelos utilizáveis conforme as credenciais configuradas
- modelo default ativo
- estado da proteção por API key nas requests
- status dos provedores
- exemplos de uso

As configurações ficam em `/settings` e exigem `SETTINGS_PASSWORD` no ambiente. Nessa tela ficam o modelo default, a API key das requests, os formulários de Xiaomi, DeepSeek, Gemini, Groq, OpenRouter, Cloudflare e limpeza do arquivo local de credenciais.

A tela também expõe um formulário genérico de sessão web para guardar `cookie`, `token`, `headers`, `storage`, `origin`, `referer` e `user-agent` por provedor, preparando adapters no padrão browser-session proxy.

## Endpoints

### OpenAI

#### `POST /v1/chat/completions`

Compatível com o formato de chat da OpenAI.

```bash
curl http://localhost:3000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $REQUEST_API_KEY" \
  -d '{
    "model": "default",
    "messages": [
      {"role": "user", "content": "Explique em uma frase o que este proxy faz."}
    ],
    "stream": false
  }'
```

#### `POST /v1/completions`

Endpoint legado. O proxy converte `prompt` internamente para chat.

#### `GET /v1/models`

Lista o catálogo combinado mais recente dos providers e os aliases dos adapters web.

### Ollama

#### `POST /api/chat`

Se `stream` não for enviado, o proxy assume `false`.

```bash
curl http://localhost:3000/api/chat \
  -H "Content-Type: application/json" \
  -d '{
    "model": "mimo-v2.5-pro",
    "messages": [
      {"role": "user", "content": "Olá"}
    ]
  }'
```

#### `POST /api/generate`

Compatível com o formato `generate` do Ollama.
Aceita os mesmos modelos roteáveis do chat: `mimo-*`, `deepseek*`, `kimi-k3`, `kimi-k2.6`, `gemini-*`, `groq/*`, `openrouter/*` e `cf/*`.

Se `stream` não for enviado, o proxy assume `false`.

#### `GET /api/tags`

Lista os modelos no formato Ollama.

#### `GET /api/version`

Expõe a versão compatível do adaptador Ollama.

### Operação e diagnóstico

#### `GET /health`

Retorna estado do processo e da sessão. Não exige API key, mesmo quando `REQUEST_API_KEY` estiver configurada.

#### `GET /auth/status`

Mostra se a sessão carregada está configurada.

#### `GET /auth/debug`

Rota administrativa para inspecionar os campos persistidos. Se `API_KEY` estiver definida, precisa de autenticação.

#### `POST /auth/clear`

Remove a sessão salva.

#### `POST /auth/import`

Permite importar a sessão manualmente por payload, se você realmente precisar automatizar isso sem a extensão.

#### `POST /auth/extension/import`

Endpoint usado pela extensão.

#### `GET /auth/models/catalog`

Mostra o snapshot atual, a origem, a última atualização e eventuais erros por provider.

#### `POST /auth/models/refresh`

Força imediatamente uma nova descoberta de modelos. A rota mantém o último catálogo válido de cada provider quando uma fonte estiver temporariamente indisponível.

#### `POST /auth/web/import`

Importa uma sessão web genérica por provedor. Payload típico:

```json
{
  "provider": "deepseek",
  "raw_cookie": "foo=bar; baz=qux",
  "token": "token-opcional",
  "user_agent": "Mozilla/5.0 ...",
  "origin": "https://chat.deepseek.com",
  "referer": "https://chat.deepseek.com/",
  "headers": {"x-csrf-token": "..."},
  "storage": {"userToken": "..."},
  "source": "chrome-extension"
}
```

Os adapters web implementados são `deepseek`, `kimi` e `qwen`. No Qwen, o proxy cria uma conversa real na conta, persiste `chat_id` e `parent_message_id` no SQLite e envia somente os turnos novos. Ao atingir `QWEN_WEB_ROLLOVER_TOKENS`, ele abre automaticamente outra conversa com um handoff do contexto recente.

Variáveis opcionais do Qwen:

```env
QWEN_WEB_DEFAULT_MODEL=qwen3.8-max
QWEN_WEB_ROLLOVER_TOKENS=850000
QWEN_WEB_HANDOFF_CHARS=120000
QWEN_WEB_THINKING=true
QWEN_WEB_TIMEZONE=Mon Aug 03 2026 10:00:00 GMT-0300
QWEN_WEB_VERSION=0.2.81
QWEN_WEB_USE_AUTHORIZATION=false
```

Como esse adapter usa a sessão web, mudanças do site ou verificações anti-bot podem exigir abrir o Qwen no Chrome e importar a sessão novamente.
No modo web, a Qwen autentica pelas cookies da sessão e o proxy não envia `Authorization` por padrão. Ative `QWEN_WEB_USE_AUTHORIZATION=true` somente se estiver reproduzindo um cliente desktop que realmente envie esse header. Sem `QWEN_WEB_DEFAULT_MODEL` e `QWEN_WEB_VERSION`, o proxy acompanha automaticamente o modelo principal ativo e a versão do frontend publicados pelo site oficial.

As gerações atuais do Qwen Web são protegidas por sinais Baxia/AWSC calculados no navegador para cada requisição. A extensão 1.3 mantém uma ponte autenticada em segundo plano: deixe o Chrome aberto e uma sessão válida no `chat.qwen.ai`; Hermes e Kilo continuam chamando somente a API OpenAI-compatible do proxy. Se a ponte não estiver conectada, o adapter tenta o transporte direto por compatibilidade.

Para garantir que várias chamadas continuem exatamente no mesmo chat Qwen, envie um valor estável em `user`:

```json
{
  "model": "qwen-web",
  "user": "meu-projeto-estavel",
  "messages": [{"role": "user", "content": "Continue o trabalho."}]
}
```

Sem `user`, o proxy deriva a sessão da primeira mensagem do usuário.

## Integração com IDEs e clientes

### OpenAI-compatible

Configure o cliente para usar:

- Base URL: `http://localhost:3000/v1`

Se `REQUEST_API_KEY` estiver configurada, envie `Authorization: Bearer <REQUEST_API_KEY>` ou `X-API-Key`. A rota `/health` continua aberta para monitoramento.

### Ollama-compatible

Configure o cliente para usar:

- Base URL: `http://localhost:3000/api`

## Tool calling e agentes

O projeto converte ferramentas do formato OpenAI para o formato esperado pelo Mimo e devolve `tool_calls` compatíveis.

Para DeepSeek, o proxy usa sessão web do navegador.

Recomendações:

- mantenha o `model` explícito
- envie `stream: true` apenas quando quiser resposta em streaming
- use `parallel_tool_calls: false` se o cliente tiver dificuldade com múltiplas tools por turno
- para coding/agentes com DeepSeek web, mantenha prompts/tool instructions curtos e `parallel_tool_calls: false` quando o cliente não lida bem com múltiplas tools

## Persistência

Arquivos relevantes:

- `data/auth.json`: sessão da Xiaomi
- `data/history.db`: histórico local em SQLite

Se estiver em Docker, mantenha `data/` em volume persistente.

## Troubleshooting

### A API responde que a sessão não está configurada

Causa provável:

- a extensão ainda não importou a sessão
- `data/auth.json` foi apagado
- a sessão foi salva em outro caminho por `AUTH_STORE_PATH`

### A Xiaomi devolve 401

Causa provável:

- sessão expirada

Correção:

1. faça login de novo no Xiaomi AI Studio
2. reimporte a sessão pela extensão

### `GET /v1/models` não lista modelos

Verifique:

- se `GET /auth/status` mostra sessão válida
- se a conta usada no Xiaomi AI Studio ainda tem acesso aos modelos

## Extensão

Arquivos:

- `extension/manifest.json`
- `extension/popup.html`
- `extension/popup.js`

Resumo:

- lê cookies da Xiaomi no navegador
- monta a sessão
- envia para `POST /auth/extension/import`

## Licença

MIT
