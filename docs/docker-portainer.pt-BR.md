# Docker e Portainer

Sim, o wppconnect-server-go tem Docker e pode ser instalado pelo Portainer.
A imagem oficial é [`wppconnect/wppconnect-server-go`](https://hub.docker.com/r/wppconnect/wppconnect-server-go).
Este guia usa a tag publicada `0.1.3`, disponível para **Linux amd64 e arm64**. O serviço
é uma API HTTP, sem painel web, Chromium, Redis ou banco externo. As credenciais
do WhatsApp ficam em arquivos SQLite no volume montado em `/data`.

## Imagem publicada e recursos deste checkout

- `0.1.2`: permite subir a API e usar as rotas existentes com um token bcrypt
  bruto enviado no header. Gere esse token com `cmd/token -raw`, como abaixo.
- A partir de `0.1.3`, a imagem inclui `POST /api/{session}/{secretkey}/generate-token` e
  aceita tokens no formato URL-safe do Node, além do campo `full` na URL.
  inclui mídia, grupos, contatos/chats/presença, restauração e shutdown ordenado.
  A tag antiga `0.1.2` não contém esses recursos.
- Confira as [tags publicadas](https://hub.docker.com/r/wppconnect/wppconnect-server-go/tags)
  antes de trocar a versão. `latest` acompanha releases estáveis; uma tag
  `sha-*` fixa uma revisão. Não presuma que uma alteração local já foi publicada.
- A tag antiga `0.1.2` é `linux/amd64`; `0.1.3` publica amd64/arm64, com testes
  das duas imagens no CI antes do push.
- Restauração automática, shutdown ordenado e limites/retentativas de webhook
  estão implementados neste checkout. Veja o [guia de operação](operation.pt-BR.md).

## Stack completa para Docker Standalone

No Portainer, abra o ambiente Docker, entre em **Stacks > Add stack**, use o nome
`wppconnect-go` e selecione **Web editor**. Cole o conteúdo de
[`deploy/portainer-stack.yml`](../deploy/portainer-stack.yml):

```yaml
services:
  wppconnect-go:
    image: wppconnect/wppconnect-server-go:${WPP_SERVER_GO_TAG:-0.1.3}
    restart: unless-stopped
    stop_grace_period: 35s
    environment:
      PORT: "21465"
      SECRET_KEY: "${SECRET_KEY:?Set SECRET_KEY in Portainer}"
      WEBHOOK_URL: "${WEBHOOK_URL:-}"
      DATA_DIR: "/data"
      AUTO_RESTORE_SESSIONS: "${AUTO_RESTORE_SESSIONS:-true}"
      SHUTDOWN_TIMEOUT: "${SHUTDOWN_TIMEOUT:-25s}"
      WEBHOOK_WORKERS: "${WEBHOOK_WORKERS:-2}"
      WEBHOOK_QUEUE_SIZE: "${WEBHOOK_QUEUE_SIZE:-256}"
      WEBHOOK_ATTEMPTS: "${WEBHOOK_ATTEMPTS:-4}"
      WEBHOOK_TIMEOUT: "${WEBHOOK_TIMEOUT:-10s}"
    ports:
      - "${WPP_SERVER_GO_PORT:-21465}:21465"
    volumes:
      - wpp_go_data:/data
    healthcheck:
      test: ["CMD-SHELL", "wget -q --spider http://127.0.0.1:21465/healthz || exit 1"]
      interval: 30s
      timeout: 10s
      retries: 5
      start_period: 10s
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"

volumes:
  wpp_go_data: {}
```

Em **Environment variables**, configure:

| Variável | Valor / finalidade |
| --- | --- |
| `SECRET_KEY` | Obrigatória. Gere um segredo privado com `openssl rand -hex 24`. |
| `WPP_SERVER_GO_TAG` | `0.1.3`, ou outra tag publicada que você tenha escolhido. |
| `WPP_SERVER_GO_PORT` | `21465`, porta publicada no servidor Docker. |
| `WEBHOOK_URL` | Opcional. URL HTTP(S) do seu receptor de eventos. |

Você pode carregar o [`.env.example`](../.env.example) depois de preencher
`SECRET_KEY`. Clique em **Deploy the stack**. O healthcheck deve ficar `healthy`.
A porta interna continua `21465` mesmo se a porta externa mudar.

O volume terá um nome como `wppconnect-go_wpp_go_data`, com o prefixo da stack.
Mantenha o mesmo nome da stack e o mesmo volume nas atualizações. Se já usa
outra instalação, reutilize explicitamente o volume existente, por exemplo:

```yaml
volumes:
  wpp_go_data:
    external: true
    name: nome-real-do-volume-existente
```

Para uso público, coloque a API atrás de um proxy com HTTPS e restrinja o acesso
à porta publicada. A rota de geração recebe o segredo na URL por compatibilidade
com o Node; exclua essa rota dos access logs do proxy ou use o gerador local.

## Stack para Docker Swarm

Use o arquivo completo [`deploy/portainer-swarm.yml`](../deploy/portainer-swarm.yml)
no editor de stacks do ambiente Swarm, com as mesmas variáveis da tabela.
Antes de implantar, execute no manager, substituindo `NOME_DO_NO` pelo nó que
guardará os dados:

```bash
docker node update --label-add wppconnect_go_data=true NOME_DO_NO
```

Adicione esse label em **um único nó** Linux amd64 ou arm64. A stack usa uma réplica,
volume local e atualização `stop-first`, para que duas instâncias não acessem
as mesmas sessões simultaneamente. O volume pertence ao nó escolhido. Se ele
ficar indisponível, o serviço ficará pendente até sua recuperação ou até uma
migração com restauração dos dados. Isso não configura alta disponibilidade.

No Portainer, informe as variáveis individualmente ou carregue-as na seção de
variáveis. O YAML usa `environment` com interpolação e dispensa `env_file`.
Para validar pela CLI, com as variáveis já exportadas:

```bash
docker stack config -c deploy/portainer-swarm.yml
docker stack deploy -c deploy/portainer-swarm.yml wppconnect-go
docker stack services wppconnect-go
docker service logs -f wppconnect-go_wppconnect-go
```

`docker stack deploy` não carrega `.env` automaticamente. No Swarm, a porta
publicada usa a routing mesh; controle o acesso nos nós do cluster.

## Exemplo completo: saúde, token, QR e primeira mensagem

Os comandos abaixo usam Bash, `curl` e `jq`. No PowerShell, use `curl.exe`
ou `Invoke-RestMethod` e adapte as variáveis do shell. Use um nome simples para
a sessão, sem `:` ou `/`. Sessão + segredo devem somar no máximo 72 bytes, o
limite do bcrypt usado pelo gerador.

### 1. Verificar o serviço

Execute no servidor Docker ou substitua o endereço pelo IP/DNS desse servidor:

```bash
export BASE_URL=http://127.0.0.1:21465
export SESSION=mysession
curl -fsS -o /dev/null -w 'HTTP %{http_code}\n' "$BASE_URL/healthz"
# Esperado: HTTP 200, sem corpo.
```

`/healthz` verifica a API. O estado da conexão WhatsApp é consultado na rota
`status-session` depois da autenticação.

### 2. Gerar um token offline

Na raiz deste checkout, com Go instalado, informe o **mesmo** `SECRET_KEY`
configurado no Portainer. O helper funciona offline, sem iniciar uma sessão
WhatsApp ou acessar o banco. As dependências Go precisam estar disponíveis ou
ser baixadas na primeira execução.

```bash
read -rsp 'SECRET_KEY do Portainer: ' SECRET_KEY
echo
export SECRET_KEY
export TOKEN="$(go run ./cmd/token -session "$SESSION" | jq -er .token)"
test -n "$TOKEN"
```

Acrescente `-raw` se ainda usa `0.1.2`: essa versão não decodifica os tokens URL-safe do
Node. O token bruto pode conter `/` e deve ser enviado no header, nunca na URL.
O comando `cmd/token` está disponível neste checkout; se sua cópia antiga não
o contém, atualize para uma revisão que inclua esse comando.

### 3. Alternativa: gerar pela API publicada a partir de 0.1.3

Com `0.1.3` ou uma imagem posterior que inclua a rota, substitua o passo anterior
por este. O exemplo assume sessão simples e segredo hexadecimal, sem caracteres
que precisem de escape na URL:

```bash
read -rsp 'SECRET_KEY do Portainer: ' SECRET_KEY
echo
export TOKEN="$(curl -fsS -X POST \
  "$BASE_URL/api/$SESSION/$SECRET_KEY/generate-token" | jq -er .token)"
test -n "$TOKEN"
```

A resposta tem status HTTP `201` e os campos `status`, `session`, `token` e
`full`. O token pertence à combinação sessão + segredo. Ele não expira por tempo;
trocar `SECRET_KEY` invalida os tokens anteriores. Para evitar transportar o
segredo na URL, use o gerador local também nas novas versões, sem `-raw`.

### 4. Iniciar e parear a sessão

```bash
curl -fsS -X POST "$BASE_URL/api/$SESSION/start-session" \
  -H "Authorization: Bearer $TOKEN"

curl -fsS "$BASE_URL/api/$SESSION/status-session" \
  -H "Authorization: Bearer $TOKEN"

curl -fsS "$BASE_URL/api/$SESSION/qrcode-session" \
  -H "Authorization: Bearer $TOKEN" -o qrcode.png
```

A conexão é assíncrona. Aguarde `QRCODE` no status antes de baixar o PNG. Se o
QR ainda não estiver disponível, a rota retorna JSON `QRCODE_NOT_AVAILABLE`
com HTTP `200`; nesse caso aguarde e repita o download. Abra o PNG e leia-o
em **WhatsApp > Aparelhos conectados > Conectar aparelho**. Consulte o status
novamente até obter `CONNECTED`. O webhook, se configurado, recebe `qrcode`
com `urlcode` e `status-find` com `CONNECTED`.

### 5. Enviar uma mensagem

Troque `5511999999999` por um número real com DDI e DDD, somente dígitos:

```bash
curl -fsS -X POST "$BASE_URL/api/$SESSION/send-message" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"phone":"5511999999999","message":"Olá! API Go no Portainer funcionando.","isGroup":false}'
```

O envio bem-sucedido retorna HTTP `201` com `{"status":"success"}`. O projeto
ainda está em MVP: consulte o [roadmap](roadmap.md) para a cobertura de rotas.

## Webhook

Configure `WEBHOOK_URL` com um endereço acessível **de dentro do container**.
`localhost` aponta para o próprio container da API. Se o receptor estiver em
outra stack, conecte os serviços a uma rede compartilhada ou use um endereço
alcançável do receptor. Os eventos atuais são `qrcode`, `status-find` e
`onmessage`, enviados como JSON com `event` e `session`.

Exemplo de mensagem recebida:

```json
{"event":"onmessage","session":"mysession","from":"5511999999999@s.whatsapp.net","body":"Olá","id":"ID_DA_MENSAGEM"}
```

A imagem `0.1.2` não tem retentativas de webhook. Este checkout adiciona fila
em memória limitada, workers, retentativas com backoff e prazo de shutdown;
consulte o [guia de operação](operation.pt-BR.md) para configuração e deduplicação.
Não há fila persistente ou garantia de ordem. A imagem `0.1.2`
não normaliza eventos de mídia nem oferece download de arquivos. Neste checkout,
esses recursos estão implementados; veja o [guia de mídia](media.pt-BR.md) para
os campos dos eventos, limites e exemplos de envio/download.
O checkout também inclui operações e eventos de grupos, descritos no
[guia de grupos](groups.pt-BR.md).
As novas consultas de contatos/chats e os eventos de presença estão no
[guia de contatos, chats e presença](contacts-chats-presence.pt-BR.md).
O índice de chats usa arquivos `.directory.db` no mesmo volume `/data` e deve
ser preservado junto com os arquivos de sessão e registros `.session.json`.

## Atualização, reinício e backup

1. Faça backup do volume e registre a tag atual.
2. Altere `WPP_SERVER_GO_TAG` no Portainer e use **Update the stack**, solicitando
   o pull da imagem quando a opção estiver disponível.
3. Preserve o volume e `SECRET_KEY`. Na imagem `0.1.2`, chame `start-session`
   com cada nome original após reiniciar. Neste checkout, sessões pareadas
   elegíveis são restauradas automaticamente por padrão; fechamento manual e
   revogação desativam essa restauração até novo início. Com credenciais
   válidas, não é necessário novo QR.
4. Para voltar à versão anterior, restaure a tag anterior. Se uma futura versão
   alterar o formato do banco, use também o backup compatível com essa versão.

Para um backup consistente de SQLite, pare o container no Standalone ou escale
o serviço para zero no Swarm. No host que guarda os dados, identifique o nome
real com `docker volume ls`, defina `VOLUME` e execute:

```bash
export VOLUME=wppconnect-go_wpp_go_data
docker run --rm \
  -v "$VOLUME:/data:ro" \
  -v "$PWD:/backup" \
  alpine:3.20 tar -czf /backup/wppconnect-go-data.tar.gz -C /data .
```

Reinicie o serviço e chame `start-session` para reconectar. Guarde o backup em
local privado: ele contém credenciais de sessão. Para restaurar, pare o serviço
e extraia o arquivo em um volume vazio, no nó correto:

```bash
docker volume create wppconnect-go-restored
docker run --rm \
  -v wppconnect-go-restored:/data \
  -v "$PWD:/backup:ro" \
  alpine:3.20 tar -xzf /backup/wppconnect-go-data.tar.gz -C /data
```

Aponte `wpp_go_data` para esse volume externo, conforme o exemplo anterior.
Não remova o volume ao excluir ou recriar a stack.

## Construir alterações locais antes da próxima release

No host Docker Standalone gerenciado pelo Portainer:

```bash
docker build -t wppconnect/wppconnect-server-go:local .
```

Defina `WPP_SERVER_GO_TAG=local` e implante sem solicitar pull dessa tag. Esse
build inclui as alterações do checkout, que podem ser posteriores à versão
publicada `0.1.3`.
O Dockerfile deve ser construído
na raiz do repositório. No Swarm, publique a imagem construída em um registry
acessível aos nós e ajuste `image` no YAML para esse endereço/tag antes do deploy.

Para usar Compose diretamente:

```bash
cp .env.example .env
# Edite .env, preenchendo SECRET_KEY e escolhendo a tag.
docker compose --env-file .env -f deploy/portainer-stack.yml config --quiet
docker compose --env-file .env -f deploy/portainer-stack.yml up -d
docker compose --env-file .env -f deploy/portainer-stack.yml ps
```

## Problemas comuns

| Sintoma | Verificação |
| --- | --- |
| Stack rejeita `SECRET_KEY` | Preencha a variável na seção Environment variables. |
| `401` | Use token bcrypt da mesma sessão e segredo. Na `0.1.2`, gere com `-raw`. |
| `404` em `generate-token` | A imagem é anterior à implementação; use `cmd/token` ou construa este checkout. |
| `disconnected` | Inicie a sessão e aguarde `CONNECTED`. |
| JSON no lugar do PNG | Aguarde `QRCODE` e repita o download antes de abrir a imagem. |
| QR novo após atualização | Confira o nome da sessão, o volume montado e, no Swarm, o nó que contém os dados. |
| Swarm em `Pending` | Verifique o label do nó, sua disponibilidade e arquitetura. |
| `no matching manifest` | Confirme a arquitetura do host e a tag: `0.1.2` é amd64; `0.1.3` inclui amd64/arm64. |
| Webhook não chega | Teste a conectividade do container até o receptor e evite `localhost`. |
| Porta não abre | Verifique porta publicada, firewall e logs do container/serviço. |

## Referências

- [Imagem oficial e tags](https://hub.docker.com/r/wppconnect/wppconnect-server-go/tags)
- [Portainer: criar uma stack e configurar variáveis](https://docs.portainer.io/user/docker/stacks/add)
- [Docker: implantar stacks no Swarm](https://docs.docker.com/engine/swarm/stack-deploy/)
- [Docker: serviços, volumes e restrições de posicionamento](https://docs.docker.com/engine/swarm/services/)
