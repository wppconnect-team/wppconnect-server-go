# Grupos

A etapa de grupos está implementada neste checkout. As novas rotas precisam de
uma imagem construída com este código ou de uma release que inclua a alteração.
A imagem `0.1.2` do [guia do Portainer](docker-portainer.pt-BR.md) ainda tem
somente a listagem básica e os placeholders antigos.

Todas as rotas exigem uma sessão conectada e `Authorization: Bearer TOKEN`
daquela sessão. A autorização de administrador e a participação no grupo são
validadas pelo WhatsApp usando a conta conectada. Não há troca automática de
conta nem execução de uma operação por outra sessão.

## Rotas

Os caminhos abaixo têm o prefixo `/api/{session}` e usam os nomes do servidor Node.

| Método | Caminho | Entrada principal |
| --- | --- | --- |
| GET | `/all-groups` | Grupos da conta conectada. |
| GET | `/group-members/{groupId}` | Membros e identificadores disponíveis no protocolo. |
| GET | `/group-members-ids/{groupId}` | IDs dos membros. |
| GET | `/group-admins/{groupId}` | IDs dos administradores e superadministradores. |
| GET | `/group-info/{groupId}` | Assunto, descrição, datas, participantes e restrições. |
| GET | `/common-groups/{wid}` | Grupos em comum com um contato. |
| POST | `/create-group` | `name`, `participants`. |
| POST | `/add-participant-group` | `groupId`, `phone`. |
| POST | `/remove-participant-group` | `groupId`, `phone`. |
| POST | `/promote-participant-group` | `groupId`, `phone`. |
| POST | `/demote-participant-group` | `groupId`, `phone`. |
| POST | `/group-subject` | `groupId`, `title`. |
| POST | `/group-description` | `groupId`, `description`. String vazia limpa a descrição. |
| POST | `/group-property` | `groupId`, `property`, `value`. |
| POST | `/messages-admins-only` | `groupId`, `value`, booleano com padrão `true`. |
| POST | `/change-privacy-group` | `groupId`, `status`: booleano ou string `"true"`/`"false"`. |
| GET | `/group-invite-link/{groupId}` | Retorna o link de convite. |
| GET | `/group-revoke-link/{groupId}` | Revoga o convite atual e retorna o novo link. |
| POST | `/group-info-from-invite-link` | `invitecode`, com essa grafia do Node. |
| POST | `/join-code` | `inviteCode`, código ou link HTTPS do WhatsApp. |
| POST | `/leave-group` | `groupId`. |
| POST | `/group-pic` | `groupId` e `base64`, data URL em `path` ou multipart `file`. |

`groupId` aceita ID numérico moderno ou legado com hífen, com ou sem `@g.us`.
Nos corpos JSON, `groupId` aceita string, lista ou IDs separados por vírgula ou
ponto e vírgula. `phone` e `participants` aceitam esses mesmos formatos para
contatos. Números são normalizados para JID de telefone; `@c.us`,
`@s.whatsapp.net` e LIDs explícitos `@lid` são aceitos. Não se infere LID pelo
tamanho do número. IDs repetidos são removidos antes da execução.

## Criar e consultar um grupo

Os exemplos usam Bash, `curl`, `jq` e as variáveis `BASE_URL`, `SESSION` e
`TOKEN` do guia de implantação. Substitua os números pelos contatos reais.

```bash
curl -fsS -X POST "$BASE_URL/api/$SESSION/create-group" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Equipe de suporte","participants":["5511999999999","5521999999999"]}' \
  > grupo.json

jq . grupo.json
export GROUP_ID="$(jq -er '.response.groupInfo[0].id' grupo.json)"

curl -fsS "$BASE_URL/api/$SESSION/group-info/$GROUP_ID" \
  -H "Authorization: Bearer $TOKEN"
curl -fsS "$BASE_URL/api/$SESSION/group-members/$GROUP_ID" \
  -H "Authorization: Bearer $TOKEN"
```

A criação retorna HTTP `201`, com o envelope do Node:

```json
{
  "status": "success",
  "response": {
    "message": "Group(s) created successfully",
    "group": "Equipe de suporte",
    "groupInfo": [{"name":"Equipe de suporte","id":"123456789","participants":[]}]
  }
}
```

O ID em `groupInfo` não inclui `@g.us`, como no Node. Examine os participantes
retornados: o grupo pode ter sido criado mesmo se um contato não pôde ser
adicionado. Nessa situação, o status é `partial`, mantendo HTTP `201` e o ID
do grupo criado. Um participante pode conter `error` com código do WhatsApp e
`requiresInvite: true`. Não são enviados convites automaticamente.

`name` também aceita uma lista ou nomes separados por vírgula/ponto e vírgula,
criando um grupo por nome. Se uma criação posterior falhar, o erro inclui
`completed` com os grupos já criados. Não há rollback nem repetição automática.

## Participantes e administradores

```bash
curl -fsS -X POST "$BASE_URL/api/$SESSION/add-participant-group" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"groupId\":\"$GROUP_ID\",\"phone\":[\"5531999999999\"]}"

curl -fsS -X POST "$BASE_URL/api/$SESSION/promote-participant-group" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"groupId\":\"$GROUP_ID\",\"phone\":\"5531999999999\"}"

curl -fsS "$BASE_URL/api/$SESSION/group-admins/$GROUP_ID" \
  -H "Authorization: Bearer $TOKEN"
```

Use `remove-participant-group` para remover e `demote-participant-group` para
revogar administração, mantendo os mesmos campos. O sucesso retorna `201` para
inclusão/promoção/rebaixamento e `200` para remoção.

As respostas incluem os campos `participants`, `groups` e `result`. Os erros
por participante são preservados, mesmo quando a chamada global ao WhatsApp
não retorna erro. Uma resposta omitida pelo protocolo é tratada como falha,
sem presumir que a alteração aconteceu.

- Todos os participantes confirmados: `status: "success"`.
- Parte confirmada e parte recusada: HTTP `207`, `status: "partial"`.
- Todos recusados: `status: "error"`, com código HTTP associado à recusa.
  Falta de permissão retorna `403`.

`curl -f` considera `207` uma resposta HTTP bem-sucedida. Verifique também o
campo `status` e os resultados antes de repetir qualquer operação.
Alterações em vários grupos não são uma transação. Se uma chamada posterior
falhar, `completed` informa os grupos processados e seus resultados disponíveis.

Os IDs nos dados de participantes seguem o formato Node:
`{"user":"5511999999999","server":"c.us","_serialized":"5511999999999@c.us"}`.
LIDs permanecem `@lid`. Em `group-info`, `participants[].id` é string.
`group-admins` retorna uma lista de listas de IDs, uma por grupo consultado,
como o controller Node. `group-members-ids` retorna a lista de IDs diretamente
no campo `response`.

## Assunto, descrição e propriedades

```bash
curl -fsS -X POST "$BASE_URL/api/$SESSION/group-subject" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"groupId\":\"$GROUP_ID\",\"title\":\"Suporte ao cliente\"}"

curl -fsS -X POST "$BASE_URL/api/$SESSION/group-description" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"groupId\":\"$GROUP_ID\",\"description\":\"Atendimento de segunda a sexta.\"}"

curl -fsS -X POST "$BASE_URL/api/$SESSION/messages-admins-only" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"groupId\":\"$GROUP_ID\",\"value\":true}"
```

`group-property` suporta:

| `property` | `value` |
| --- | --- |
| `restrict` | Booleano. `true` permite que somente administradores editem os dados do grupo. |
| `announcement` | Booleano. `true` permite mensagens somente de administradores. |
| `ephemeral` | `0`, `86400`, `604800` ou `7776000`, em segundos. `true`/`1` usa sete dias; `false` desativa. |

O padrão de `value`, se omitido, é `true`. `null` e propriedades desconhecidas
são rejeitados. `change-privacy-group` altera `restrict`, usando `status`.
O protocolo pode recusar uma alteração conforme o papel da conta no grupo.

`group-info` retorna nome, assunto, descrição, autores/datas de atualização,
criação e participantes, além de `restrict`, `announcement` e
`ephemeralDuration`. Datas indisponíveis e `lastActivityAt` são `null`.
O último horário de atividade não é inferido a partir da criação do grupo.

## Convites, entrada e saída

```bash
curl -fsS "$BASE_URL/api/$SESSION/group-invite-link/$GROUP_ID" \
  -H "Authorization: Bearer $TOKEN"

curl -fsS -X POST "$BASE_URL/api/$SESSION/group-info-from-invite-link" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"invitecode":"CODIGO_DO_CONVITE"}'

curl -fsS -X POST "$BASE_URL/api/$SESSION/join-code" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"inviteCode":"https://chat.whatsapp.com/CODIGO_DO_CONVITE"}'

curl -fsS -X POST "$BASE_URL/api/$SESSION/leave-group" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"groupId\":\"$GROUP_ID\"}"
```

`join-code` retorna `201` quando a solicitação é aceita pelo protocolo. Grupos
com aprovação de membros podem manter a entrada pendente; a resposta não
garante que a conta já se tornou membro. Convites inválidos/revogados e
permissões insuficientes são retornados como erro.

`group-revoke-link` preserva o método GET do Node e **muda o convite**. Não o
use como consulta periódica. As rotas de convite usam `Cache-Control: no-store`.
O código é transmitido ao protocolo WhatsApp, sem fazer uma requisição HTTP
para o link fornecido pelo usuário.

## Foto do grupo

```bash
curl -fsS -X POST "$BASE_URL/api/$SESSION/group-pic" \
  -H "Authorization: Bearer $TOKEN" \
  -F "groupId=$GROUP_ID" -F 'file=@equipe.png'
```

São aceitos JPEG e PNG de até 16 MiB e 16 milhões de pixels. A imagem é cortada
no centro e convertida para JPEG quadrado de 640 pixels. Transparência recebe
fundo branco. Também são aceitos `base64` em JSON e data URL em `path`.
Caminhos do filesystem e URLs remotas não são lidos por essa rota.

## Webhooks de grupos

Eventos são encaminhados pelo mesmo `WEBHOOK_URL` dos eventos de mensagem.

`onparticipantschanged` mantém os nomes do evento de participantes do Node:

```json
{
  "event": "onparticipantschanged",
  "session": "mysession",
  "groupId": "123456789@g.us",
  "by": "5511999999999@c.us",
  "operation": "promote",
  "action": "promote",
  "who": ["5521999999999@c.us"],
  "timestamp": "2026-10-22T15:00:00.000Z"
}
```

`operation` é `add`, `remove`, `promote` ou `demote`. `action` também pode ser
`join`, para entrada por convite, ou `leaver`, quando o autor é o próprio
participante que saiu. Quando o protocolo não permite identificar o autor,
`by` é `null`; não se presume uma saída voluntária.

As extensões de eventos do Go são:

- `onaddedtogroup`: entrada da própria conta em um grupo, com `groupId`, `by`,
  `reason`, `type` e `group` contendo os metadados disponíveis.
- `ongroupchange`: alteração de metadados, com `groupId`, `by`, `timestamp` e
  `changes`. Pode informar assunto, descrição, restrições, duração de mensagens
  temporárias, aprovação, remoção/suspensão ou mudança de foto.
- Revogação de convite informa `changes.inviteLinkChanged: true`, sem incluir
  o segredo do convite no evento.

Uma notificação do protocolo com várias ações gera um evento por ação e um
evento adicional se houver mudança de metadados. Fotos de contatos individuais
não geram eventos de grupo. A entrega de webhooks é assíncrona, com fila em
memória limitada e retentativas descritas no [guia de operação](operation.pt-BR.md).
Não há fila persistente ou garantia de ordem.

## Limites, compatibilidade e erros

- Corpo JSON de até 64 KiB, exceto foto, que usa o limite de mídia.
- Até 10 nomes na criação, 20 grupos por operação e 100 participantes por lote.
- Assunto/nome de até 100 caracteres e descrição de até 2.048 caracteres.
  O WhatsApp ainda pode impor limites adicionais e regras específicas à conta.
- Timeout de 30 segundos para a comunicação com o WhatsApp por requisição,
  incluindo o lote inteiro. Fotos compartilham o limite de quatro operações
  simultâneas de mídia do processo.
- As consultas de múltiplos grupos seguem o Node: `group-admins` agrupa todos;
  membros, IDs, metadados e convites retornam o último resultado. Prefira um
  grupo por consulta. Grupos em comum retornam um array sem envelope.
- `all-groups` preserva a resposta resumida já existente do Go: `id`, `name`
  e número de `participants`. `group-members` retorna os dados de participante
  disponíveis, sem inventar campos do catálogo de contatos do servidor Node.
- Comunidades, listas de transmissão e aprovação/rejeição de solicitações de
  entrada têm contratos separados e ficam fora desta etapa.

| HTTP | Significado |
| --- | --- |
| `400` | Dados inválidos, propriedade/valor não aceitos ou rejeição de formato pelo WhatsApp. |
| `401` | Token ausente, inválido ou pertencente a outra sessão. |
| `403` | A conta não participa do grupo ou não tem permissão para a operação. |
| `404` | Sessão desconectada no gerenciador ou grupo inexistente. |
| `408` | Requisição cancelada. |
| `409` | Grupo bloqueado pelo protocolo. |
| `410` | Convite revogado. |
| `413` | Corpo ou foto acima do limite. |
| `429` | Limite de recursos ou taxa imposto pelo WhatsApp. |
| `502` | Falha de comunicação ou resposta do protocolo incompleta. |
| `503` | O cliente perdeu a conexão com o WhatsApp durante a operação. |
| `504` | Timeout. |

## Validação

Os testes de `go test ./...` exercitam o roteador HTTP com clientes de protocolo
simulados, incluindo criação, membros/admins, ações de participantes, resultados
parciais, permissões, metadados, convites, foto e isolamento por sessão. Também
cobrem normalização dos eventos, encaminhamento pelo gerenciador e entrega HTTP
dos webhooks. Não usam uma conta WhatsApp nem alteram grupos reais.

A validação manual com uma conta pareada e a publicação da imagem continuam
pendentes. As definições de protocolo utilizadas são as da revisão fixada no
`go.mod`: [cliente whatsmeow](https://pkg.go.dev/go.mau.fi/whatsmeow) e
[eventos](https://pkg.go.dev/go.mau.fi/whatsmeow/types/events).
