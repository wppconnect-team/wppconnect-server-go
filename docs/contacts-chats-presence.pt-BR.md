# Contatos, chats e presença

> Source expansion update: the historical baseline below is superseded for
> message history, labels/community filters, durable events and administration
> by the [current English guide](roadmap-completion.md). New capabilities require
> a build containing this change. Experimental/unsupported features remain explicit.


Esta etapa está disponível a partir da imagem publicada `0.1.3`, para Linux
amd64 e arm64. A imagem antiga `0.1.2` não inclui esses recursos.
Veja o [guia do Portainer](docker-portainer.pt-BR.md).

Todas as rotas exigem uma sessão conectada e o token dessa sessão. Os exemplos
usam Bash, `curl`, `jq`, `BASE_URL`, `SESSION` e `TOKEN`, como nos guias anteriores.

## Rotas implementadas

Os caminhos abaixo têm o prefixo `/api/{session}`.

| Método | Caminho | Finalidade |
| --- | --- | --- |
| GET | `/all-contacts` | Contatos sincronizados no armazenamento do whatsmeow. |
| GET | `/contact/{phone}` | Contato individual; consulta também o mapeamento PN/LID local. |
| GET | `/contact/pn-lid/{pnLid}` | Associação entre número de telefone e LID. |
| GET | `/profile-pic/{phone}` | Metadados/URL da foto de perfil, respeitando acesso no WhatsApp. |
| GET | `/profile-status/{phone}` | Texto de perfil retornado pelo WhatsApp. |
| GET | `/all-chats` | Chats conhecidos pelo índice da sessão. |
| GET | `/all-chats-archived` | Chats marcados como arquivados no índice. |
| POST | `/list-chats` | Filtros e paginação do índice. Corpo opcional. |
| GET | `/chat-by-id/{phone}` | Metadados de um chat conhecido. |
| GET | `/chat-is-online/{phone}` | Última presença online/offline observada e recente. |
| GET | `/last-seen/{phone}` | Último horário online, se divulgado pelo protocolo. |
| POST | `/subscribe-presence` | Assinar eventos de presença de contatos individuais. |
| POST | `/set-online-presence` | Publicar a presença da própria conta. |
| POST | `/typing` | Iniciar ou parar digitação. |
| POST | `/recording` | Iniciar ou parar indicador de gravação. |
| POST | `/chat-state` | Formato legado: `0` digitação, `1` gravação, `2` pausa. |

Os envelopes e nomes de campos seguem as rotas equivalentes do Node. As
limitações de dados e as diferenças que afetam consumidores estão descritas
abaixo. Não há paridade com todos os campos internos do WhatsApp Web.

## Contatos e identificação

```bash
curl -fsS "$BASE_URL/api/$SESSION/all-contacts" \
  -H "Authorization: Bearer $TOKEN"

curl -fsS "$BASE_URL/api/$SESSION/contact/5511999999999" \
  -H "Authorization: Bearer $TOKEN"

curl -fsS "$BASE_URL/api/$SESSION/contact/pn-lid/5511999999999@c.us" \
  -H "Authorization: Bearer $TOKEN"
```

`all-contacts` e `contact` retornam `{"status":"success","response":...}`.
Os contatos incluem `id`, `name`, `shortName`, `pushname`, `verifiedName`,
`isBusiness`, `isMyContact`, `isUser` e `isGroup`. Nome e indicadores são
derivados dos dados disponíveis no armazenamento do SDK. A ausência de dados
comerciais não substitui uma consulta completa de perfil comercial.

Os identificadores de telefone aceitam dígitos com DDI/DDD, `@c.us` ou
`@s.whatsapp.net`. LIDs explícitos mantêm `@lid`, sem inferência pelo tamanho do
número. Os IDs retornados usam o formato Node com `_serialized`, `user` e
`server`, convertendo o sufixo de telefone para `@c.us`.

`contact/pn-lid` retorna diretamente os campos `phoneNumber`, `lid` e `contact`
disponíveis, como a rota Node. A associação vem do armazenamento local do SDK;
um campo ausente indica que a associação ainda não foi sincronizada. Não se
inventa um LID nem se faz uma busca irrestrita de todos os números no WhatsApp.

Um contato ainda não sincronizado retorna `404`. A lista de contatos pode estar
incompleta enquanto a sincronização inicial acontece. Perfil e foto são
consultados pelo protocolo e podem ser recusados conforme privacidade e acesso.
O servidor retorna a URL da foto, sem baixar uma URL arbitrária fornecida pelo
usuário.

## Índice persistente de chats

O whatsmeow não fornece a coleção de modelos de chats do navegador. O servidor
mantém um índice SQLite próprio por sessão, em
`DATA_DIR/<nome-da-sessao>.directory.db`, com até **10.000 entradas**.

O índice recebe:

- Metadados dos eventos de sincronização de histórico.
- Mensagens recebidas e envios confirmados pelas rotas de texto/mídia.
- Alterações de assunto/entrada em grupos.
- Eventos de arquivamento, fixação, marcação de leitura e exclusão vindos de
  outros dispositivos.

Somente metadados e até 64 IDs recentes de mensagem por chat são mantidos,
para deduplicação recente. Os corpos de mensagens e os arquivos de mídia não
são copiados para esse banco. O índice sobrevive a fechar e reabrir a sessão;
preserve o volume `/data` para mantê-lo nas atualizações.

Conversas ainda não entregues pela sincronização ou nunca observadas por este
servidor não aparecem na lista. Não se cria uma conversa para cada contato.
Nomes atuais de contatos sincronizados têm precedência sobre o nome antigo do
chat. As entradas são ordenadas por atividade decrescente e, em empate, pelo
ID; as mais antigas são removidas ao atingir o limite.

Exemplo de metadados:

```json
{
  "id": {"user":"5511999999999","server":"c.us","_serialized":"5511999999999@c.us"},
  "name": "Contato de suporte",
  "formattedTitle": "Contato de suporte",
  "isGroup": false,
  "isUser": true,
  "t": 1792670400,
  "unreadCount": 3,
  "unreadCountIsPartial": false,
  "archive": false,
  "isArchived": false,
  "pin": false
}
```

`t` usa segundos Unix. `unreadCountIsPartial: true` indica que o contador foi
formado a partir das mensagens observadas, sem uma contagem completa recebida
do protocolo. A deduplicação recente não garante reconstrução exata de toda
a caixa de entrada em caso de reenvios antigos ou lacunas de sincronização.
Eventos antigos de histórico não sobrescrevem configurações ou marcações de
leitura mais recentes. Uma exclusão observada impede que um histórico antigo
recrie o chat; uma mensagem nova pode torná-lo visível novamente.

## Listagem, filtros e paginação

```bash
curl -fsS "$BASE_URL/api/$SESSION/all-chats" \
  -H "Authorization: Bearer $TOKEN"

curl -fsS -X POST "$BASE_URL/api/$SESSION/list-chats" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"count":20,"onlyUsers":true,"onlyWithUnreadMessage":true}'

curl -fsS "$BASE_URL/api/$SESSION/chat-by-id/5511999999999" \
  -H "Authorization: Bearer $TOKEN"
```

`all-chats` usa o envelope `status`, `response` e `mapper: "chat"`.
`list-chats` retorna diretamente um array; `chat-by-id` retorna o objeto.
`all-chats-archived` retorna diretamente um array com HTTP `201`, preservando
o controller Node.

`list-chats` aceita `id`, `count`, `direction`, `onlyGroups`, `onlyUsers`,
`onlyWithUnreadMessage` e `onlyArchived`. O padrão é 20 entradas e a direção
`after`; `count` precisa estar entre 1 e 1.000.

A paginação segue a semântica Node observada: `after` começa **incluindo** o
chat de `id`; `before` devolve os chats imediatamente anteriores à âncora.
Sem âncora, `before` retorna uma lista vazia. Uma âncora inexistente no resultado
filtrado retorna `404`. O header `X-WPP-Chat-Source: synchronized-index` identifica
as respostas de listagem baseadas no índice.

IDs explícitos de grupo são aceitos. Para números sem sufixo, use
`?isGroup=true` ou `?isLid=true` em `chat-by-id` quando necessário. O valor de
`isGroup=false` é interpretado como falso. Flags conflitantes são rejeitadas.

## Presença da própria conta e assinatura

```bash
curl -fsS -X POST "$BASE_URL/api/$SESSION/set-online-presence" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"isOnline":true}'

curl -fsS -X POST "$BASE_URL/api/$SESSION/subscribe-presence" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"phone":["5511999999999","5521999999999"]}'

curl -fsS "$BASE_URL/api/$SESSION/chat-is-online/5511999999999" \
  -H "Authorization: Bearer $TOKEN"
```

O WhatsApp pode exigir que a própria conta esteja online para entregar eventos
de presença. `set-online-presence` aceita `isOnline`, com padrão `true`; envie
`false` para publicar indisponibilidade. Um push name ausente no dispositivo
pode impedir a publicação, retornando `409`.

`subscribe-presence` aceita `phone` como string/lista ou `all: true` para os
contatos locais. Há limite de 100 contatos por requisição. Assinaturas globais
de grupos retornam `501`; assine os contatos individuais desejados.
Privacidade ou falta de token de assinatura podem impedir atualizações.

Os estados online/offline observados ficam em memória por **cinco minutos**,
com até 10.000 contatos por sessão. O cache é descartado na desconexão, no
fechamento da sessão ou no reinício. Uma resposta desconhecida é explícita:

```json
{"status":"success","response":null,"known":false,"observedAt":null}
```

Quando há um evento recente, `response` é booleano em `chat-is-online`.
`observedAt` informa a data UTC de observação. O valor é a última informação
observada e não uma garantia de estado neste exato instante.
`last-seen` retorna segundos Unix quando o protocolo divulga o horário;
horário oculto ou desconhecido resulta em `response: null`, `known: false`.
Essa distinção evita informar offline ou um horário inventado.

## Digitação e gravação

```bash
curl -fsS -X POST "$BASE_URL/api/$SESSION/typing" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"phone":"5511999999999","value":true}'

curl -fsS -X POST "$BASE_URL/api/$SESSION/recording" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"phone":"5511999999999","value":true,"duration":5000}'

curl -fsS -X POST "$BASE_URL/api/$SESSION/typing" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"phone":"5511999999999","value":false}'
```

`value` tem padrão `true`; `false` envia pausa. `isGroup` e LIDs explícitos
também são aceitos para sinais de conversa. `duration` é em **milissegundos**,
entre 1 e 10.000, como no uso do Node. Com duração, a requisição aguarda o prazo
e envia pausa ao terminar. Sem duração, envia um sinal imediato, sem renovação
periódica; envie novos sinais conforme o fluxo da aplicação.

Um comando mais recente para o mesmo JID substitui a propriedade do temporizador
anterior, para que uma gravação antiga não pare uma digitação mais recente.
Cancelamento da requisição tenta enviar pausa, com timeout próprio curto.
Lotes não são transações: em falhas, `completed` informa os contatos concluídos.
O timeout de 30 segundos cobre o lote inteiro, incluindo esperas de duração.

`chat-state` aceita `chatstate` numérico ou string: `0` digitação,
`1` gravação e `2` pausa. Não use o valor `1` para digitação.

## Eventos de presença

O webhook recebe `onpresencechanged`, com a sessão e campos no formato Node:

```json
{
  "event": "onpresencechanged",
  "session": "mysession",
  "id": "5511999999999@c.us",
  "isOnline": true,
  "isGroup": false,
  "isUser": true,
  "state": "available",
  "lastSeen": null,
  "t": 1792670400000
}
```

`t` usa milissegundos Unix. Eventos de conversa usam `state: "composing"`,
`"recording"` ou a extensão `"paused"`, e informam `author`. Em grupos,
`participants` identifica o autor do sinal. Esses eventos não provam a presença
online global, então `isOnline` é `null` quando o protocolo só forneceu o sinal
de digitação/gravação. Presença global usa `available`/`unavailable`.

O cache é atualizado antes do webhook. A entrega é assíncrona, com fila em
memória limitada e retentativas descritas no [guia de operação](operation.pt-BR.md).
Não há persistência da fila ou garantia de ordem.

## Limitações e erros explícitos

Filtros `withLabels` preenchidos e `onlyCommunities: true` retornam `501`,
pois esses dados não são indexados. As seguintes consultas de banco de
mensagens do navegador também retornam `501` com `status: "not_supported"`:

- `all-chats-with-messages`, `all-messages-in-chat/{phone}`;
- `all-new-messages`, `unread-messages`, `all-unread-messages`;
- `message-by-id/{messageId}`, `load-messages-in-chat/{phone}`, `get-messages/{phone}`.

Essas respostas descrevem o suporte desta implementação, sem afirmar que todo
o protocolo WhatsApp seja incapaz de fornecer dados históricos. Não há download
arbitrário do banco completo do navegador nem ações de arquivar/excluir chats
adicionadas nesta etapa. Eventos dessas ações em outros dispositivos atualizam
o índice. PN/LID pode produzir conversas distintas até que a sincronização e os
mapeamentos locais permitam localizá-las; não há fusão destrutiva automática.

Corpos JSON têm limite de 64 KiB. Entradas inválidas retornam `400`, token
incorreto `401`, dados desconhecidos/sessão desconectada `404`, corpo excessivo
`413`, falta de permissão `403`, timeout `504` e falhas de backend `502`.
Cancelamento pode retornar `408`; sessão desconectada no SDK pode retornar `503`.

Os testes cobrem armazenamento SQLite real, reabertura, limites, eventos fora
de ordem, filtros/paginação, identificação PN/LID, sinais e cancelamento,
isolamento entre sessões e webhooks HTTP simulados. A validação com conta
pareada e a publicação de uma imagem permanecem pendentes.
