# Operação e ciclo de sessão

Estes recursos exigem uma imagem construída deste checkout ou uma release que
os inclua. A tag publicada `0.1.2` mantém o comportamento anterior. Esta entrega
é uma PR, sem release ou validação com conta WhatsApp real.

## Início, desconexão e logout

As rotas exigem o token correspondente à sessão. `start-session` conecta uma
sessão existente ou inicia pareamento. O nome precisa ter de 1 a 64 letras
ASCII, dígitos, `_` ou `-`. Caminhos, dois-pontos e nomes que poderiam colidir
após sanitização são rejeitados. O formato legado `session:token` continua
permitido na URL de autenticação; o nome de sessão é a parte anterior a `:`.
Há limite de 100 sessões vivas por processo.

```bash
curl -fsS -X POST "$BASE_URL/api/$SESSION/start-session" \
  -H "Authorization: Bearer $TOKEN"

curl -fsS -X POST "$BASE_URL/api/$SESSION/close-session" \
  -H "Authorization: Bearer $TOKEN"

# Reconectar credenciais preservadas:
curl -fsS -X POST "$BASE_URL/api/$SESSION/start-session" \
  -H "Authorization: Bearer $TOKEN"

# Revogar o dispositivo conectado e exigir novo QR no próximo início:
curl -fsS -X POST "$BASE_URL/api/$SESSION/logout-session" \
  -H "Authorization: Bearer $TOKEN"
```

| Ação | Credenciais | Reinício do processo |
| --- | --- | --- |
| `close-session` | Preservadas; clientes e bancos fechados. | Sessão fica fechada até novo `start-session`. |
| `logout-session` bem-sucedido | Dispositivo revogado e credenciais removidas. | Não restaura; próximo início exige pareamento. |
| SIGTERM/SIGINT | Preservadas. | Restaura as sessões ativas que tenham credenciais. |
| Queda temporária de conexão | Preservadas; presença em cache apagada. | Reconexão automática do SDK, enquanto o cliente está ativo. |

Logout exige sessão autenticada no WhatsApp. Se estiver fechada, retorna `409`;
inicie e conecte primeiro. Falha da revogação retorna erro e preserva o handle.
O servidor não simula sucesso apagando somente o arquivo local. Uma revogação
externa ou falha permanente fecha o cliente e desativa sua restauração; uma
revogação conhecida também impede reutilizar credenciais antigas na próxima
abertura. O índice de metadados de chats permanece preservado.

Estados incluem `INITIALIZING`, `QRCODE`, `CONNECTED`, `DISCONNECTED`,
`UNPAIRED` e `CLOSED`. Eventos de status usam `status-find`; QR expirado ou
concluído não continua sendo oferecido pelo endpoint. Após a remoção de um
handle, `status-session` retorna `CLOSED`.

## Restauração e arquivos

`AUTO_RESTORE_SESSIONS=true` é o padrão. Na inicialização, o gerenciador procura
arquivos `<sessao>.db` válidos no `DATA_DIR`, abre os dispositivos persistidos e
inicia as sessões elegíveis. Bancos antigos pareados sem registro são migrados.
Bancos vazios não criam QR automaticamente. Um banco corrompido gera erro no
log e não impede a restauração das demais sessões.

Cada `<sessao>.session.json` registra nome, intenção de restauração e revogação.
O arquivo é escrito via temporário, sync e renomeação, sem chaves ou tokens.
Preserve-o junto com `<sessao>.db` e `<sessao>.directory.db` no volume `/data`.
Não apague os registros de sessões fechadas, pois eles impedem a migração
legada de reativá-las. Desabilitar restauração mantém os arquivos intactos e
permite `start-session` manual.

`/healthz` informa a vida do processo; `/readyz` retorna `503` enquanto a
restauração inicial está em andamento. A conclusão com erros parciais fica no
log; readiness não certifica que cada conta esteja conectada. Consulte
`status-session` por conta. Nenhuma das duas sondas retorna credenciais.

## Shutdown

SIGTERM e SIGINT interrompem a restauração e iniciam shutdown com prazo total
de `SHUTDOWN_TIMEOUT`, padrão `25s`. O servidor para de aceitar conexões HTTP,
aguarda as requisições em andamento, desconecta os clientes, remove handlers,
cancela QR/reconexão e fecha os bancos. Depois, encerra produtores de webhook
e tenta drenar a fila. Ao vencer o prazo, requisições pendentes são canceladas.
As credenciais das sessões ativas e sua intenção de restauração são mantidas.

As stacks usam `stop_grace_period: 35s`. Ao aumentar `SHUTDOWN_TIMEOUT`, aumente
também esse prazo no Docker/Portainer para permitir o encerramento antes do
SIGKILL. Uma parada forçada pode perder webhooks pendentes em memória.

## Webhooks limitados e retentativas

| Variável | Padrão | Faixa |
| --- | --- | --- |
| `WEBHOOK_URL` | vazio, desabilitado | URL HTTP(S) acessível pelo container. |
| `WEBHOOK_WORKERS` | `2` | 1 a 32 entregas concorrentes. |
| `WEBHOOK_QUEUE_SIZE` | `256` | 1 a 10.000 eventos aguardando entrega. |
| `WEBHOOK_ATTEMPTS` | `4` | 1 a 10 tentativas totais por evento. |
| `WEBHOOK_TIMEOUT` | `10s` | 1 ms a 1 minuto por tentativa. |

Payload serializado tem limite de 1 MiB. O descarte por tamanho ou fila cheia
é registrado; produtores não ficam bloqueados esperando espaço. Além dos
eventos em fila, há no máximo um evento ativo por worker. As tentativas repetem
o mesmo corpo e o mesmo header `X-WPP-Event-ID`; `X-WPP-Attempt` começa em `1`.
O receptor deve deduplicar IDs, pois uma falha de rede pode acontecer após ele
ter processado o evento.

Qualquer `2xx` confirma entrega. Erros de transporte, `408`, `425`, `429` e
`5xx` permitem retentativa. Outros códigos encerram a entrega; redirecionamentos
não são seguidos. O backoff começa em 250 ms, cresce exponencialmente e inclui
jitter; `Retry-After` em segundos ou data HTTP é respeitado até o teto de 30s.
O número de tentativas e o timeout limitam cada entrega. O log de shutdown
registra contadores de entregas, falhas, descartes, retentativas e fila restante.

A fila é em memória, sem armazenamento durável ou garantia de ordem. Dois
workers podem entregar eventos de uma sessão em ordem diferente. Receptores que
precisem de processamento persistente devem confirmar o webhook somente após
registrar o evento em seu próprio armazenamento.

## Docker e CI

O Dockerfile compila Go sem CGO para `TARGETOS/TARGETARCH`, inclui `server` e o
gerador `/app/token` e tem healthcheck próprio `/app/server -healthcheck`.
`DATA_DIR=/data` é o padrão da imagem. O healthcheck usa o `PORT` configurado.
As stacks mantêm a sonda com `wget` para compatibilidade com a imagem antiga.

A estratégia segue a documentação oficial de [builds multiplataforma](https://docs.docker.com/build/building/multi-platform/)
e [testes antes de publicar](https://docs.docker.com/build/ci/github-actions/test-before-push/).
O CI carrega e testa cada variante amd64/arm64, incluindo healthcheck positivo
e negativo, token, OpenAPI, parada limpa e preservação de fechamento após
reinício. A publicação depende desse job e gera ambas as plataformas.

Para repetir o teste da imagem sem uma conta WhatsApp:

```bash
docker buildx build --platform linux/amd64 --load -t wppconnect-go:check .
go run ./cmd/smoke-image -image wppconnect-go:check -platform linux/amd64
```

Use `linux/arm64` nas duas opções para testar a variante ARM com emulação ou
host compatível. A PR configura essas verificações no CI; somente resultados
de execução confirmam aprovação. Nenhuma imagem nova foi publicada nesta entrega.
