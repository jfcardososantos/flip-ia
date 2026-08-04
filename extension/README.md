# flip-ai Session Importer

Extensão Chrome/Edge para capturar sessões autenticadas suportadas e enviar ao `flip-ai`.

## Instalação

1. extraia este diretório em uma pasta local
2. abra `chrome://extensions` ou `edge://extensions`
3. ative `Developer mode`
4. clique em `Load unpacked`
5. selecione a pasta `extension`

## Uso

1. abra o popup da extensão
2. informe a URL pública do seu `flip-ai`
3. se você protegeu rotas administrativas, informe a `API_KEY`
4. para Xiaomi ou DeepSeek, faça login no provedor no mesmo navegador e clique em `Import Xiaomi Session` ou `Import DeepSeek`
5. para Kimi, abra `https://www.kimi.com/`, faça login e clique em `Import Kimi`; a extensão captura o `access_token` e os cookies da sessão
6. para Qwen, abra `https://chat.qwen.ai/`, faça login e clique em `Import Qwen`; mantenha o Chrome aberto para que a ponte Baxia/AWSC execute as gerações protegidas na aba autenticada
7. para Gemini, Groq, OpenRouter ou Cloudflare, abra o painel do provider, gere uma API key e salve no campo correspondente

Se funcionar, a sessão ou chave será salva em `data/auth.json` e o backend passará a autenticar internamente as chamadas para o provedor.
