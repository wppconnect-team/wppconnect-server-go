# Canais do WhatsApp no Server Go

Use POST com JSON e o token da sessão em `/api/{session}/{operação}`. É preciso que a sessão esteja conectada. Um JID de canal tem a forma `123@newsletter`; o ID de servidor de uma publicação é um inteiro de 1 a 2147483647. Não use o ID textual de uma mensagem nesse campo. Consulte o [OpenAPI da versão instalada](openapi.json) antes de integrar.

| Operação | Corpo de exemplo | O que o sucesso confirma |
| --- | --- | --- |
| newsletter-info-invite | `{"invite":"Abcdef99"}` | 200: metadados retornados na consulta do convite. |
| follow-newsletter-invite | `{"invite":"Abcdef99"}` | 200: o SDK concluiu o pedido de seguir o canal resolvido. Os metadados retornados são os da consulta anterior à alteração. |
| unfollow-newsletter-invite | `{"invite":"Abcdef99"}` | 200: o SDK concluiu o pedido de deixar o canal resolvido. |
| newsletter-reaction | `{"id":"123@newsletter","serverId":42,"reaction":"👍"}` | 202: reação escrita no transporte, sem ACK remoto. Uma string vazia remove a reação. |
| newsletter-mark-viewed | `{"id":"123@newsletter","serverIds":[41,42]}` | 200: recebido ACK do protocolo para o lote. Pode alterar os contadores de visualização. |
| newsletter-message-updates | `{"id":"123@newsletter","count":20,"after":40,"since":1700000000}` | 200: atualizações disponíveis, com IDs e contadores. Não é um histórico completo. |
| newsletter-accept-tos | `{"noticeId":"123","stage":"1","accepted":true}` | 200: o SDK concluiu o pedido de aceite explícito para o aviso e a etapa informados. |
| newsletter-admin-invite | `{"id":"123@newsletter","phone":"5511999999999","caption":"Convite","expiresInSeconds":3600}` | 201: mensagem de convite enviada ao protocolo; não comprova entrega, aceite nem concessão do papel. |

Um convite admite código de 8 a 128 caracteres ou link `https://whatsapp.com/channel/CODIGO`. As operações de seguir e deixar usam exclusivamente a identidade retornada pelo WhatsApp. A reação admite até 64 bytes; as visualizações aceitam de 1 a 100 IDs únicos. A espera pelo ACK tem prazo de 30 segundos. Uma falha depois do envio não prova que a operação deixou de surtir efeito.

Na consulta de atualizações, `count` admite 1 a 100 e assume 50 quando ausente. `since` usa segundos Unix e `after` usa ID de servidor. Zero omite o respectivo filtro. A resposta omite o horário quando ele não está disponível. O campo `reactionsTruncated` informa se algum contador foi omitido; no máximo 100 tipos de reação são retornados por publicação. O SDK não oferece `before` nesta consulta: a API rejeita esse campo com 400.

O aceite de termos exige `noticeId` e `stage` com 1 a 32 dígitos, mais a decisão explícita `accepted:true`. No convite administrativo, o servidor consulta o papel da conta e o nome do canal; um assinante recebe 403. A miniatura opcional (`thumbnail`) é JPEG em base64 de até 64 KiB e 512 x 512 pixels; não se baixa imagem por URL. A legenda aceita até 2048 caracteres e a validade, de 60 segundos a sete dias (padrão: um dia).

Nas seis operações que alteram estado, use [Idempotency-Key](send-idempotency.md) para recuperar o resultado sem repetir o efeito. As duas consultas não aceitam essa chave. Para repetir uma operação use a mesma chave, sessão, rota, Content-Type e corpo. Não force um novo envio se o resultado for incerto.

## Cobertura da comparação

Os contratos acima cobrem aspectos de Z014, Z017, Z019, Z022, Z023, Z025, Z026 e Z027 da [matriz de referência](https://github.com/wppconnect-team/hypermeow/blob/main/docs/parity-contract-matrix.md). São adaptadores nativos com nomes e payloads próprios. Há diferenças explícitas, como o filtro indisponível em Z025 e os limites de imagem e de permissão em Z027. Os testes exercitam o roteador HTTP, o ledger SQLite, a composição da mensagem administrativa e o tratamento do ACK no SDK. O recebimento do convite em outra conta e a apresentação nos clientes móveis e Web ainda não foram validados.

Consulte o resultado com GET e a mesma chave em `newsletter-admin-result` (termos e convite administrativo) ou `newsletter-write-result` (seguir, deixar, reagir e marcar como visto). Cada consulta só encontra resultados da sua categoria e sessão. A consulta de envios comuns `send-result` continua isolada das duas categorias.
