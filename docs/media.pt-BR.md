# Mídia: imagens, arquivos, áudio e download

Esta etapa está implementada neste checkout. Use uma imagem construída a
partir dele ou uma release que inclua essas alterações. A imagem `0.1.2`
citada no [guia do Portainer](docker-portainer.pt-BR.md) ainda não as contém.

Todas as rotas abaixo exigem `Authorization: Bearer TOKEN` da sessão e uma
sessão conectada. Os exemplos usam Bash, `curl`, `jq` e as variáveis
`BASE_URL`, `SESSION` e `TOKEN` do guia de implantação.

## Rotas e campos

| Método e rota em `/api/{session}` | Entrada |
| --- | --- |
| `POST /send-image` | `phone`, `base64`, `caption`, `isGroup`; aceita multipart `file`. |
| `POST /send-file` | `phone`, `base64`, `filename`, `caption` ou `message`, `isGroup`; aceita multipart `file`. |
| `POST /send-file-base64` | Mesmo contrato de envio de documento. |
| `POST /send-voice-base64` | `phone`, `base64Ptt`, `isGroup`. Áudio Ogg/Opus como mensagem de voz. |
| `POST /send-voice` | Alias de voz que aceita base64 ou multipart `file`. |
| `POST /send-audio-base64` | Extensão do Go: `phone`, `base64`, `isGroup`. Áudio comum, com `PTT=false`. |
| `POST /download-media` | `messageId` como string ou objeto de webhook com `id` string. |
| `GET /get-media-by-message/{messageId}` | Mesmo retorno de download, com ID na URL. |

`phone` aceita uma string ou um array com até 100 strings. Use dígitos com DDI
e DDD para contatos. Também são aceitos os sufixos `@c.us` e
`@s.whatsapp.net`. Para grupos, envie `isGroup: true` e o identificador do
grupo, com ou sem `@g.us`.

O base64 pode ser bruto ou uma data URL, como `data:image/png;base64,...`.
O campo opcional `mimetype` é validado junto com a declaração da data URL e a
assinatura do conteúdo. Imagens preservam o MIME detectado: JPEG, PNG, GIF ou
WebP. Áudio comum aceita os formatos reconhecidos pela detecção de conteúdo,
como MP3, WAV, Ogg e FLAC. Documento usa `DocumentMessage`, preservando o nome
do arquivo e a legenda. `message` tem precedência sobre `caption`, como no Node.

O envio retorna HTTP `201`:

```json
{
  "status": "success",
  "response": [{"id": "ID_DA_MENSAGEM", "to": "5511999999999@s.whatsapp.net", "mimetype": "application/pdf", "filename": "relatorio.pdf"}],
  "mapper": "return"
}
```

Use o `id` retornado para baixar uma mídia enviada. Se houver uma falha durante
um envio para vários destinatários, os anteriores podem já ter recebido a
mensagem; o envio não é uma transação atômica.

## Enviar imagem ou arquivo

Imagem com upload multipart, sem precisar montar base64:

```bash
curl -fsS -X POST "$BASE_URL/api/$SESSION/send-image" \
  -H "Authorization: Bearer $TOKEN" \
  -F 'phone=5511999999999' \
  -F 'caption=Imagem enviada pela API Go' \
  -F 'file=@imagem.png'
```

Documento em JSON, usando o nome e os campos do servidor Node:

```bash
base64 < relatorio.pdf | tr -d '\r\n' > relatorio.b64
jq -n --rawfile base64 relatorio.b64 \
  '{phone:"5511999999999",base64:$base64,filename:"relatorio.pdf",caption:"Relatório",isGroup:false}' \
  > envio.json
curl -fsS -X POST "$BASE_URL/api/$SESSION/send-file-base64" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' --data-binary @envio.json
```

Também é possível enviar o documento como multipart `file` em `send-file`.
O nome é reduzido ao nome-base, removendo diretórios e caracteres de controle.

## Enviar voz e áudio comum

Para mensagem de voz, forneça um arquivo já codificado como **Ogg/Opus**:

```bash
base64 < voz.ogg | tr -d '\r\n' > voz.b64
jq -n --rawfile audio voz.b64 \
  '{phone:"5511999999999",base64Ptt:$audio,isGroup:false}' > voz.json
curl -fsS -X POST "$BASE_URL/api/$SESSION/send-voice-base64" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' --data-binary @voz.json
```

O MIME de voz é `audio/ogg; codecs=opus` e `PTT=true`. Não há transcodificação
nem dependência de FFmpeg. Um MP3 não se torna uma mensagem de voz apenas por
renomear o arquivo ou declarar MIME Ogg. Para enviar MP3 como áudio comum,
use `send-audio-base64` com o campo `base64`.

## Receber eventos e baixar mídia

O webhook `onmessage` mantém `event`, `session`, `id`, `from` e `body`. Agora
também informa `type`, `isMedia`, `chatId`, `author`, `fromMe`, `isGroupMsg` e
`t` (timestamp Unix). Os tipos são `chat`, `image`, `document`, `audio` e `ptt`.
Texto estendido usa o texto real; imagem e documento usam a legenda no `body`.

Exemplo de documento recebido:

```json
{
  "event": "onmessage",
  "session": "mysession",
  "id": "ID_RECEBIDO",
  "from": "5511999999999@s.whatsapp.net",
  "author": "5511999999999@s.whatsapp.net",
  "chatId": "5511999999999@s.whatsapp.net",
  "fromMe": false,
  "isGroupMsg": false,
  "t": 1792670400,
  "type": "document",
  "isMedia": true,
  "body": "Segue o relatório",
  "caption": "Segue o relatório",
  "filename": "relatorio.pdf",
  "mimetype": "application/pdf",
  "size": 1024
}
```

Áudio também informa `duration` em segundos, quando presente no protocolo.
Em grupos, `from`/`chatId` identificam o grupo e `author` identifica o remetente.
Não são expostas chaves de criptografia nem URLs internas de mídia.

O descritor é registrado **antes** de disparar o webhook. O consumidor pode
usar imediatamente o `id` para download:

```bash
curl -fsS -X POST "$BASE_URL/api/$SESSION/download-media" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"messageId":"ID_RECEBIDO"}' > download.json

# Equivalente por GET:
curl -fsS "$BASE_URL/api/$SESSION/get-media-by-message/ID_RECEBIDO" \
  -H "Authorization: Bearer $TOKEN" > download.json

# Converter o resultado para um arquivo:
jq -r .base64 download.json | base64 --decode > recebido.pdf
```

Ambas as rotas retornam HTTP `200` com os campos do servidor Node:

```json
{"base64":"JVBERi0xLjcK...","mimetype":"application/pdf"}
```

Os descritores são mantidos em memória, separados por sessão, por **24 horas**,
com até **1.024 mensagens de mídia por sessão**. As mais antigas são removidas
ao atingir o limite. Fechar a sessão ou reiniciar o processo descarta esse
índice. Não há busca de histórico antigo do WhatsApp nem persistência do índice
de downloads nesta etapa. As credenciais de sessão no SQLite continuam intactas.

## Limites, erros e diferenças de compatibilidade

- O limite por arquivo é **16 MiB**, tanto no envio quanto no download. O corpo
  de envio permite o base64 correspondente mais 64 KiB de campos e envelopes.
- Há até quatro operações de mídia simultâneas por processo e timeout de
  60 segundos para cada operação de upload/envio ou download.
- Download usa arquivo temporário privado com limite de escrita, removido ao
  terminar. O SDK verifica MAC e hashes da mídia criptografada antes do retorno.
- O servidor resolve somente IDs que já conhece na sessão autenticada.
  Objetos enviados no `messageId` não podem escolher URL, chave ou descritor.
- Upload de caminhos locais e busca de URLs remotas pelos campos `path` do
  Node não estão implementados. Use base64, data URL ou multipart `file`.
  `send-voice` oferece esses formatos, mas não lê caminhos do servidor.
- Opções avançadas do Node, como `quotedMessageId`, newsletters, LID,
  transcodificação, vídeo e stickers, ficam fora desta etapa.
- Download não funciona com token de outra sessão, mesmo conhecendo o ID.

| HTTP | Significado |
| --- | --- |
| `400` | JSON/base64 inválido, MIME incompatível, destinatário inválido ou codec de voz incorreto. |
| `401` | Token ausente, inválido ou de outra sessão. |
| `404` | Sessão desconectada ou mídia desconhecida/expirada na sessão. |
| `408` | Operação cancelada pelo contexto da requisição. |
| `413` | Arquivo ou corpo da requisição acima do limite. |
| `502` | Falha de upload, envio, download ou verificação de integridade. |
| `504` | Timeout na comunicação de mídia. |

## Validação automatizada

`go test ./...` cobre contratos HTTP, envio para grupos e múltiplos contatos,
upload multipart, dados inválidos, tamanhos declarados e reais, isolamento entre
sessões, expiração/evicção do índice, ordem de registro antes do webhook e
falhas de transporte. A descriptografia também é testada com o cliente real do
whatsmeow e um transporte HTTP simulado, sem conta WhatsApp ou rede externa.
Um teste com uma conta pareada continua sendo necessário para validar a
interoperabilidade de ponta a ponta com o serviço WhatsApp.
